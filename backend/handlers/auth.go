package handlers

// Funções compartilhadas de autenticação (AD-6/AD-9) — hoje o único fluxo de
// login do FB_APU05 é via SSO Keycloak (ver auth_sso.go); fetchUserByEmail,
// finishLogin e GenerateToken existem aqui, separadas do handler SSO, porque
// qualquer fluxo de login futuro (se algum dia existir) reaproveita a mesma
// emissão de token/refresh cookie — mesmo padrão já validado em produção no
// FB_APU02 (replicado literalmente, sem reinventar o fluxo — ver spec Code Map).

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// --- Structs ---

// User representa uma linha da tabela `usuarios`. Perfil (`solicitante`/
// `administrador`) é a coluna de autorização gerenciada pela própria
// aplicação (AD-7) — nunca deriva de role/claim do Keycloak.
type User struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Nome      string `json:"nome"`
	Perfil    string `json:"perfil"`
	CreatedAt string `json:"created_at"`
}

// AuthResponse é o payload devolvido após um login bem-sucedido.
type AuthResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

// --- Token stores ---

type refreshTokenData struct {
	UserID    string
	Perfil    string
	ExpiresAt time.Time
}

var (
	refreshTokenStore sync.Map // string(refresh token) → refreshTokenData
	tokenBlacklist    sync.Map // string(access token) → time.Time(expiry) — AD-9: blacklist no logout
)

func init() {
	// Limpeza periódica (hourly) de tokens expirados — mesmo padrão do FB_APU02.
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		for range ticker.C {
			now := time.Now()
			refreshTokenStore.Range(func(k, v interface{}) bool {
				if d, ok := v.(refreshTokenData); ok && now.After(d.ExpiresAt) {
					refreshTokenStore.Delete(k)
				}
				return true
			})
			tokenBlacklist.Range(func(k, v interface{}) bool {
				if exp, ok := v.(time.Time); ok && now.After(exp) {
					tokenBlacklist.Delete(k)
				}
				return true
			})
		}
	}()
}

// --- Utils ---

// getJWTSecret lê JWT_SECRET do ambiente a cada chamada — garante que
// godotenv.Load() em main() já tenha efeito antes do primeiro uso.
func getJWTSecret() []byte {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return []byte("super-secret-key-change-me-in-prod")
	}
	return []byte(secret)
}

// ValidateJWTSecret derruba o processo se JWT_SECRET não estiver setado.
// main.go já exige DATABASE_URL incondicionalmente (log.Fatal sem ela), então
// não existe um "modo dev sem banco" em que relaxar essa checagem faria
// sentido — todo ambiente que chega a rodar o binário precisa de um
// JWT_SECRET real (.env.example já traz um valor de placeholder pro dev local).
func ValidateJWTSecret() {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		log.Fatal("FATAL: JWT_SECRET not set — set it to a 32+ byte random value before deploying.")
	}
	if len(secret) < 32 {
		log.Printf("WARNING: JWT_SECRET tem apenas %d bytes — recomendado 32+ bytes aleatórios. Rotacione para um valor mais forte.", len(secret))
	}
}

// GenerateToken emite o JWT próprio do FB_APU05 (HS256, access 30min) — claims
// user_id/perfil/exp. Perfil vem sempre da tabela `usuarios` (AD-7), nunca de
// claim do Keycloak.
func GenerateToken(userID, perfil string) (string, error) {
	claims := jwt.MapClaims{
		"user_id": userID,
		"perfil":  perfil,
		"exp":     time.Now().Add(30 * time.Minute).Unix(), // 30 minutos
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(getJWTSecret())
}

func generateRefreshTokenString() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand falhar indica a fonte de entropia do SO indisponível —
		// não há valor seguro em prosseguir com um buffer possivelmente fraco
		// para um refresh token de 7 dias.
		log.Fatalf("FATAL: crypto/rand.Read failed generating refresh token: %v", err)
	}
	return hex.EncodeToString(b)
}

func isSecureCookie(r *http.Request) bool {
	return os.Getenv("COOKIE_SECURE") == "true" ||
		r.Header.Get("X-Forwarded-Proto") == "https"
}

func setRefreshCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    token,
		Path:     "/api/auth/",
		HttpOnly: true,
		Secure:   isSecureCookie(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   7 * 24 * 60 * 60, // 7 dias
	})
}

func clearRefreshCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/api/auth/",
		HttpOnly: true,
		Secure:   isSecureCookie(r),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// jsonErr escreve um envelope de erro padronizado {"error": message}.
func jsonErr(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// --- Shared logic ---

// fetchUserByEmail busca um usuário por e-mail — comparação case-insensitive
// (LOWER dos dois lados): o e-mail compara contra uma claim de um IdP externo
// (Keycloak), e uma diferença de maiúsculas/minúsculas não deveria virar
// "usuário não encontrado" (mesmo achado de revisão documentado no FB_APU02).
func fetchUserByEmail(db *sql.DB, email string) (user User, err error) {
	err = db.QueryRow(`
		SELECT id, email, nome, perfil, created_at
		FROM usuarios WHERE LOWER(email) = LOWER($1)
	`, email).Scan(&user.ID, &user.Email, &user.Nome, &user.Perfil, &user.CreatedAt)
	return
}

// finishLogin completa o login depois que a identidade do usuário já foi
// verificada (hoje, só via SSO Keycloak — ver KeycloakSSOHandler): emite o
// JWT próprio, o refresh token (cookie HttpOnly/SameSite=Strict, 7 dias) e
// responde com o usuário.
func finishLogin(db *sql.DB, w http.ResponseWriter, r *http.Request, user User, start time.Time) {
	token, err := GenerateToken(user.ID, user.Perfil)
	if err != nil {
		log.Printf("[Login] Erro ao gerar token para %s: %v", user.Email, err)
		jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
		return
	}

	refreshToken := generateRefreshTokenString()
	refreshTokenStore.Store(refreshToken, refreshTokenData{
		UserID:    user.ID,
		Perfil:    user.Perfil,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	})
	setRefreshCookie(w, r, refreshToken)

	log.Printf("[Login] Sucesso para %s (perfil=%s). Duração: %v", user.Email, user.Perfil, time.Since(start))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(AuthResponse{Token: token, User: user})
}

// LogoutHandler encerra a sessão: coloca o access token atual na blacklist
// (AD-9), apaga o refresh token do store e limpa o cookie. Cobre só limpeza
// local (ver spec Design Notes) — o end_session completo no Keycloak (SLO)
// fica fora desta story: não é testável neste sandbox sem um client Keycloak
// real provisionado.
func LogoutHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
			tokenString := authHeader[7:]
			tok, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
				return getJWTSecret(), nil
			}, jwt.WithValidMethods([]string{"HS256"}))
			// Só blacklista um token que de fato passa na nossa própria
			// assinatura/validade — um bearer forjado (assinatura inválida) não
			// deve ter seu `exp` reivindicado aceito e gravado na blacklist.
			if err == nil && tok != nil && tok.Valid {
				if claims, ok := tok.Claims.(jwt.MapClaims); ok {
					if exp, ok := claims["exp"].(float64); ok {
						tokenBlacklist.Store(tokenString, time.Unix(int64(exp), 0))
					}
				}
			}
		}

		if cookie, err := r.Cookie("refresh_token"); err == nil {
			refreshTokenStore.Delete(cookie.Value)
		}

		clearRefreshCookie(w, r)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Sessão encerrada com sucesso"})
	}
}
