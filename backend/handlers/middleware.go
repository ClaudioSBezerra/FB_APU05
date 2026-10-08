package handlers

// RequireAuth — primeiro middleware de "exige login" do FB_APU05 (Story 1.3).
// Fecha uma lacuna deixada pela Story 1.2: a `tokenBlacklist` populada no
// logout (ver auth.go — LogoutHandler) nunca era lida em lugar nenhum, então
// nenhum token revogado era de fato recusado por qualquer rota.
//
// Forma geral adaptada de AuthMiddleware do FB_APU02
// (backend/handlers/auth.go:214-266) — middleware parametrizado por perfil
// exigido, extrai claims do JWT próprio, injeta no contexto. Duas diferenças
// deliberadas em relação ao modelo:
//   - checa a blacklist antes de validar a assinatura (o gap que esta story
//     fecha; o FB_APU02 já fazia isso, aqui ainda não existia nenhum
//     consumidor de tokenBlacklist);
//   - usa a claim `perfil` (não `role` — AD-7: perfil é coluna da aplicação,
//     nunca claim do Keycloak) e exige igualdade exata com perfilExigido, sem
//     o bypass implícito "admin sempre passa" do modelo original — hoje só
//     existe um perfil protegido (`administrador`); se surgir um nível
//     intermediário no futuro, um bypass implícito seria fonte de bug sutil.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

type contextKey string

// ClaimsContextKey é a chave de context onde RequireAuth injeta as claims do
// JWT próprio validado (jwt.MapClaims) — inclui `user_id`, `perfil`, `exp`.
const ClaimsContextKey contextKey = "claims"

// RequireAuth exige um JWT próprio válido (assinado com JWT_SECRET, não
// expirado, não presente na blacklist). Quando perfilExigido não é vazio,
// também exige que a claim `perfil` do token bata exatamente com ele.
//
// 401 (Unauthorized) se o header Authorization estiver ausente/malformado, o
// token for inválido/expirado, ou o token estiver na blacklist (revogado por
// logout). 403 (Forbidden) se o token for válido mas o perfil não bater.
func RequireAuth(next http.HandlerFunc, perfilExigido string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if len(authHeader) < 7 || !strings.EqualFold(authHeader[:7], "Bearer ") {
			jsonErr(w, http.StatusUnauthorized, "Autenticação necessária")
			return
		}
		tokenString := authHeader[7:]

		// Checar a blacklist antes mesmo de validar a assinatura: um token
		// revogado no logout nunca deve autorizar nada, mesmo que ainda esteja
		// dentro do seu `exp` original.
		if _, revoked := tokenBlacklist.Load(tokenString); revoked {
			jsonErr(w, http.StatusUnauthorized, "Token revogado")
			return
		}

		token, err := jwt.Parse(tokenString, func(t *jwt.Token) (interface{}, error) {
			return getJWTSecret(), nil
		}, jwt.WithValidMethods([]string{"HS256"}))
		if err != nil || token == nil || !token.Valid {
			jsonErr(w, http.StatusUnauthorized, "Token inválido ou expirado")
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			jsonErr(w, http.StatusUnauthorized, "Token inválido")
			return
		}

		if perfilExigido != "" {
			perfil, _ := claims["perfil"].(string)
			if perfil != perfilExigido {
				jsonErr(w, http.StatusForbidden, "Permissão insuficiente")
				return
			}
		}

		ctx := context.WithValue(r.Context(), ClaimsContextKey, claims)
		next(w, r.WithContext(ctx))
	}
}

// GetUserIDFromContext extrai a claim `user_id` das claims injetadas por
// RequireAuth. Retorna string vazia se RequireAuth não foi aplicado antes
// (não deveria acontecer em nenhuma rota registrada em main.go).
func GetUserIDFromContext(r *http.Request) string {
	claims, ok := r.Context().Value(ClaimsContextKey).(jwt.MapClaims)
	if !ok {
		return ""
	}
	userID, _ := claims["user_id"].(string)
	return userID
}

// jsonErrConflitoVersao escreve o envelope de conflito de lock otimista da
// Story 4.1 (I/O Matrix da spec): {"erro":{"codigo":"conflito_versao",
// "mensagem":string,"versao_atual":int}} — formato aninhado "erro",
// deliberadamente distinto do jsonErr "error" flat já existente (fica ao
// lado, nunca o substitui: nenhum chamador de jsonErr precisa mudar).
func jsonErrConflitoVersao(w http.ResponseWriter, versaoAtual int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"erro": map[string]interface{}{
			"codigo":       "conflito_versao",
			"mensagem":     "a solicitação já foi assumida ou não está mais aberta",
			"versao_atual": versaoAtual,
		},
	})
}
