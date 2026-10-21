package handlers

// Funções compartilhadas de autenticação (AD-6/AD-9) — dois fluxos de login
// levam ao mesmo finishLogin: SSO Keycloak (auth_sso.go, auto-provisiona
// 'solicitante' no primeiro login) e login local e-mail+senha (LoginHandler
// abaixo, usado quando IAM_BASE_URL não está configurado — contas locais só
// são criadas por um administrador, admin.go/CreateUsuarioLocalHandler).
// fetchUserByEmail/finishLogin/GenerateToken ficam aqui, compartilhadas pelos
// dois — mesmo padrão de emissão de token/refresh cookie já validado em
// produção no FB_APU02 (ver spec Code Map).

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"fb_apu05/services"
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
//
// Sem fallback hardcoded (removido 21/10): ValidateJWTSecret() já derruba o
// processo no boot se a env estiver ausente, então em operação normal esta
// função nunca vê string vazia — um `log.Fatal` aqui é só a rede de
// segurança caso algum caminho futuro (um teste, um worker) chame isto sem
// passar por esse boot-check; um valor conhecido e previsível no binário
// seria pior do que simplesmente falhar.
func getJWTSecret() []byte {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		log.Fatal("FATAL: getJWTSecret chamado sem JWT_SECRET setado — ValidateJWTSecret() deveria ter impedido o processo de chegar aqui.")
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

// GenerateToken emite o JWT próprio do FB_APU05 (HS256, access 8h) — claims
// user_id/perfil/exp. Perfil vem sempre da tabela `usuarios` (AD-7), nunca de
// claim do Keycloak.
//
// Era 30min (AD-9); aumentado porque não existe endpoint de refresh
// silencioso ainda (/api/auth/refresh não está registrado em main.go — o
// refresh_token em cookie é gerado no login mas nunca consumido) — na
// prática, a sessão morria de verdade a cada 30min, forçando login manual
// repetido o dia inteiro, inclusive esbarrando no rate limit de login
// (LoginRL, 5 tentativas/15min por IP) quando várias contas de teste são
// usadas do mesmo IP num intervalo curto. 8h cobre um turno de trabalho;
// reduz drasticamente a frequência de login sem eliminar a expiração.
func GenerateToken(userID, perfil string) (string, error) {
	claims := jwt.MapClaims{
		"user_id": userID,
		"perfil":  perfil,
		"exp":     time.Now().Add(8 * time.Hour).Unix(),
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

// HashPassword/CheckPasswordHash: mesmo custo (14) e biblioteca do FB_APU02
// (golang.org/x/crypto/bcrypt) — usados só pelo login local (LoginHandler) e
// pela criação de conta local pelo administrador (admin.go,
// CreateUsuarioLocalHandler). Contas auto-provisionadas via SSO Keycloak
// nunca têm password_hash setado (NULL).
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

func CheckPasswordHash(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
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

// LoginRequest é o corpo de POST /api/auth/login.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// fetchUserForLocalLogin busca um usuário por e-mail igual fetchUserByEmail,
// mas também devolve password_hash (NULL para contas só-SSO) — separado de
// fetchUserByEmail (usada pelo fluxo Keycloak) para não misturar a coluna de
// senha num caminho de código que nunca precisa dela.
func fetchUserForLocalLogin(db *sql.DB, email string) (user User, passwordHash sql.NullString, err error) {
	err = db.QueryRow(`
		SELECT id, email, nome, perfil, created_at, password_hash
		FROM usuarios WHERE LOWER(email) = LOWER($1)
	`, email).Scan(&user.ID, &user.Email, &user.Nome, &user.Perfil, &user.CreatedAt, &passwordHash)
	return
}

// LoginHandler autentica por e-mail+senha — alternativa ao SSO Keycloak
// quando este não está configurado (ver Login.tsx: a tela só mostra este
// caminho se SSOConfigHandler responder enabled=false). Contas locais só são
// criadas por um administrador (admin.go, CreateUsuarioLocalHandler) — não
// existe autocadastro público aqui (decisão de escopo, diferente do
// RegisterHandler do FB_APU02).
//
// Mensagem de erro idêntica para "e-mail não encontrado", "conta sem senha
// local definida (só SSO)" e "senha errada" — evita enumeração de contas,
// mesmo padrão do FB_APU02 (LoginHandler, fetchUserByEmail).
func LoginHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		start := time.Now()

		if !LoginRL.Allow(GetClientIP(r)) {
			jsonErr(w, http.StatusTooManyRequests, "Muitas tentativas de login. Tente novamente mais tarde.")
			return
		}

		var req LoginRequest
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, http.StatusBadRequest, "Corpo da requisição inválido")
			return
		}
		if req.Email == "" || req.Password == "" {
			jsonErr(w, http.StatusBadRequest, "Informe e-mail e senha")
			return
		}

		const genericErr = "E-mail ou senha inválidos"

		user, passwordHash, err := fetchUserForLocalLogin(db, req.Email)
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusUnauthorized, genericErr)
			return
		}
		if err != nil {
			log.Printf("[Login] Erro ao buscar usuário %s: %v", req.Email, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if !passwordHash.Valid || !CheckPasswordHash(req.Password, passwordHash.String) {
			jsonErr(w, http.StatusUnauthorized, genericErr)
			return
		}

		log.Printf("[Login] Login local: %s", user.Email)
		finishLogin(db, w, r, user, start)
	}
}

// ForgotPasswordRequest é o corpo de POST /api/auth/forgot-password.
type ForgotPasswordRequest struct {
	Email string `json:"email"`
}

// ForgotPasswordHandler gera um token de redefinição (1h) e envia por
// e-mail (services.SendPasswordResetEmail) — mesmo padrão do FB_APU02
// (handlers/auth.go, ForgotPasswordHandler). Só se aplica a contas com
// password_hash definido (login local); uma conta só-SSO não tem senha
// pra redefinir, mas a resposta é a mesma mensagem genérica de sucesso
// independente disso — nunca revela se o e-mail existe/tem senha local
// (evita enumeração de contas).
func ForgotPasswordHandler(db *sql.DB) http.HandlerFunc {
	const genericMsg = "Se o e-mail estiver cadastrado, você receberá um link de redefinição em instantes."

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req ForgotPasswordRequest
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, http.StatusBadRequest, "Corpo da requisição inválido")
			return
		}
		if req.Email == "" {
			jsonErr(w, http.StatusBadRequest, "Informe o e-mail")
			return
		}

		if !ForgotPasswordRL.Allow(req.Email) {
			jsonErr(w, http.StatusTooManyRequests, "Muitas solicitações de recuperação. Tente novamente mais tarde.")
			return
		}

		var userID string
		var passwordHash sql.NullString
		err := db.QueryRow(`SELECT id, password_hash FROM usuarios WHERE LOWER(email) = LOWER($1)`, req.Email).
			Scan(&userID, &passwordHash)
		if err == sql.ErrNoRows || !passwordHash.Valid {
			// E-mail inexistente OU conta só-SSO (sem password_hash): mesma
			// resposta genérica, sem token gerado nem e-mail enviado.
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"message": genericMsg})
			return
		}
		if err != nil {
			log.Printf("[ForgotPassword] Erro ao buscar usuário %s: %v", req.Email, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		if err := gerarEEnviarTokenDeReset(db, userID, req.Email); err != nil {
			log.Printf("[ForgotPassword] %v (e-mail=%s)", err, req.Email)
			jsonErr(w, http.StatusInternalServerError, "Erro ao enviar e-mail de recuperação")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"message": genericMsg})
	}
}

// gerarEEnviarTokenDeReset cria um token de reset de senha (verification_tokens,
// expira em 1h) e envia o e-mail de redefinição — lógica única compartilhada
// por ForgotPasswordHandler (fluxo público, a própria pessoa inicia) e por
// AdminLiberarAcessoHandler (admin.go — fluxo administrativo: admin aciona
// para outra conta, ex. recém-criada por carga em lote) — nunca duplicada
// entre os dois.
func gerarEEnviarTokenDeReset(db *sql.DB, userID, email string) error {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return fmt.Errorf("crypto/rand.Read falhou: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	if _, err := db.Exec(`
		INSERT INTO verification_tokens (user_id, token, type, expires_at)
		VALUES ($1, $2, 'password_reset', $3)
	`, userID, token, time.Now().Add(1*time.Hour)); err != nil {
		return fmt.Errorf("erro ao criar token de reset: %w", err)
	}

	if err := services.SendPasswordResetEmail(email, token); err != nil {
		return fmt.Errorf("erro ao enviar e-mail de reset: %w", err)
	}
	return nil
}

// ResetPasswordRequest é o corpo de POST /api/auth/reset-password.
type ResetPasswordRequest struct {
	Token           string `json:"token"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
}

// ResetPasswordHandler troca a senha usando o token emitido por
// ForgotPasswordHandler — mesma validação (match, mínimo 8 caracteres,
// token não usado/não expirado) do FB_APU02.
func ResetPasswordHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if !ResetPasswordRL.Allow(GetClientIP(r)) {
			jsonErr(w, http.StatusTooManyRequests, "Muitas tentativas. Tente novamente mais tarde.")
			return
		}

		var req ResetPasswordRequest
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, http.StatusBadRequest, "Corpo da requisição inválido")
			return
		}
		if req.Password != req.ConfirmPassword {
			jsonErr(w, http.StatusBadRequest, "As senhas não coincidem")
			return
		}
		if len(req.Password) < 8 {
			jsonErr(w, http.StatusBadRequest, "A senha deve ter no mínimo 8 caracteres")
			return
		}

		var userID string
		var expiresAt time.Time
		err := db.QueryRow(`
			SELECT user_id, expires_at FROM verification_tokens
			WHERE token = $1 AND type = 'password_reset' AND used = false
		`, req.Token).Scan(&userID, &expiresAt)
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusNotFound, "Token inválido ou já utilizado")
			return
		}
		if err != nil {
			log.Printf("[ResetPassword] Erro no banco: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if time.Now().After(expiresAt) {
			jsonErr(w, http.StatusGone, "Token expirado. Solicite uma nova recuperação de senha")
			return
		}

		hash, err := HashPassword(req.Password)
		if err != nil {
			log.Printf("[ResetPassword] Erro ao gerar hash: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		tx, err := db.Begin()
		if err != nil {
			log.Printf("[ResetPassword] Erro ao iniciar transação: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer func() { _ = tx.Rollback() }()

		if _, err = tx.Exec(`UPDATE usuarios SET password_hash = $1, updated_at = now() WHERE id = $2`, hash, userID); err != nil {
			log.Printf("[ResetPassword] Erro ao atualizar senha de %s: %v", userID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if _, err = tx.Exec(`UPDATE verification_tokens SET used = true WHERE token = $1`, req.Token); err != nil {
			log.Printf("[ResetPassword] Erro ao marcar token como usado: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if err := tx.Commit(); err != nil {
			log.Printf("[ResetPassword] Erro ao commitar: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		log.Printf("[ResetPassword] Senha redefinida com sucesso para usuário %s", userID)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"message": "Senha redefinida com sucesso. Você já pode fazer login com a nova senha.",
		})
	}
}

// ChangePasswordRequest é o corpo de POST /api/auth/change-password.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// ChangePasswordHandler permite a um usuário autenticado trocar a própria
// senha (exige a senha atual) — registrado atrás de RequireAuth(..., "")
// em main.go (qualquer perfil, só a própria conta). Diferente de
// ResetPasswordHandler: não envolve e-mail/token, só a sessão já válida.
func ChangePasswordHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		userID := GetUserIDFromContext(r)
		if !ChangePasswordRL.Allow(userID) {
			jsonErr(w, http.StatusTooManyRequests, "Muitas tentativas. Tente novamente mais tarde.")
			return
		}

		var req ChangePasswordRequest
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, http.StatusBadRequest, "Corpo da requisição inválido")
			return
		}
		if len(req.NewPassword) < 8 {
			jsonErr(w, http.StatusBadRequest, "A nova senha deve ter no mínimo 8 caracteres")
			return
		}

		var hash sql.NullString
		if err := db.QueryRow(`SELECT password_hash FROM usuarios WHERE id = $1`, userID).Scan(&hash); err != nil {
			log.Printf("[ChangePassword] Erro ao buscar usuário %s: %v", userID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if !hash.Valid || !CheckPasswordHash(req.CurrentPassword, hash.String) {
			jsonErr(w, http.StatusUnauthorized, "Senha atual incorreta")
			return
		}

		newHash, err := HashPassword(req.NewPassword)
		if err != nil {
			log.Printf("[ChangePassword] Erro ao gerar hash: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if _, err = db.Exec(`UPDATE usuarios SET password_hash = $1, updated_at = now() WHERE id = $2`, newHash, userID); err != nil {
			log.Printf("[ChangePassword] Erro ao salvar senha de %s: %v", userID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		log.Printf("[ChangePassword] Senha alterada com sucesso para usuário %s", userID)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Senha alterada com sucesso"})
	}
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
