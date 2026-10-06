package iam

// Os 3 arquivos desta pasta (iam_key_provider.go, iam_jwks_client.go,
// iam_auth_middleware.go) foram escritos para viver juntos no mesmo pacote.
//
// Adaptado do template da skill keycloak-identity-react-go: (1) troca de
// go.uber.org/zap pelo pacote log da stdlib, seguindo a convenção do projeto
// (ver CLAUDE.md); (2) adicionada extração da claim `email` (ContextKeyIAMEmail/
// GetIAMEmail) — o template original só expõe sub/azp, mas o fluxo de SSO deste
// projeto precisa do e-mail pra casar com o usuário já existente em `users`.

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// iamContextKey é o tipo para chaves de context injetadas pelo IAMAuthMiddleware.
type iamContextKey string

const (
	// ContextKeyIAMSub é a chave de context para o subject (sub) do token.
	ContextKeyIAMSub iamContextKey = "iam_sub"
	// ContextKeyIAMClientID é a chave de context para o client (azp) do token.
	ContextKeyIAMClientID iamContextKey = "iam_client_id"
	// ContextKeyIAMEmail é a chave de context para o e-mail do usuário autenticado.
	ContextKeyIAMEmail iamContextKey = "iam_email"
	// ContextKeyIAMEmailVerified é a chave de context para a claim email_verified.
	ContextKeyIAMEmailVerified iamContextKey = "iam_email_verified"
)

// IAMAuthMiddleware valida tokens Bearer JWT emitidos pelo Keycloak.
//
// Verifica:
//   - Presença do header Authorization: Bearer <token>
//   - Assinatura RS256 via keyProvider.GetKey(kid)
//   - Claims: exp (não expirado), iss (deve ser iamBaseURL — a URL do realm),
//     azp (deve estar em allowedClientIDs)
//
// Em caso de sucesso: injeta iam_sub, iam_client_id (azp) e iam_email no context da requisição.
// Em caso de falha: retorna 401 com {"error": "<code>", "trace_id": "<id>"}.
//
// onAuthResult, se não-nil, é chamado a cada tentativa (ok=true em sucesso,
// reason preenchido em falha) — plugue aqui suas métricas Prometheus/OTel.
// getTraceID, se não-nil, extrai um trace_id do contexto da requisição para
// incluir na resposta de erro (troque pela sua convenção de tracing).
func IAMAuthMiddleware(
	keyProvider KeyProvider,
	iamBaseURL string,
	allowedClientIDs []string,
	onAuthResult func(ok bool, reason string),
	getTraceID func(ctx context.Context) string,
) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var traceID string
			if getTraceID != nil {
				traceID = getTraceID(r.Context())
			}

			// 1. Extrair Bearer token
			authHeader := r.Header.Get("Authorization")
			var tokenString string
			if strings.HasPrefix(authHeader, "Bearer ") {
				tokenString = strings.TrimPrefix(authHeader, "Bearer ")
			}
			if tokenString == "" {
				report(onAuthResult, false, "missing_token")
				sendIAMError(w, http.StatusUnauthorized, "missing_token", traceID)
				return
			}

			// 2. Parsear e validar JWT
			token, err := jwt.Parse(tokenString,
				func(t *jwt.Token) (interface{}, error) {
					// Verificar algoritmo RS256
					if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
						return nil, jwt.ErrSignatureInvalid
					}
					// Extrair kid do header
					kid, _ := t.Header["kid"].(string)
					return keyProvider.GetKey(kid)
				},
				jwt.WithIssuer(iamBaseURL),
				jwt.WithExpirationRequired(),
				jwt.WithLeeway(30*time.Second), // tolera pequeno desvio de relógio Keycloak↔API
			)

			if err != nil {
				reason := classifyJWTError(err)
				report(onAuthResult, false, reason)
				log.Printf("[iam] auth failed trace_id=%s reason=%s err=%v", traceID, reason, err)
				sendIAMError(w, http.StatusUnauthorized, "invalid_token", traceID)
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok || !token.Valid {
				report(onAuthResult, false, "invalid_claims")
				sendIAMError(w, http.StatusUnauthorized, "invalid_token", traceID)
				return
			}

			// 3. Validar azp (authorized party — client Keycloak que emitiu o token)
			// contra a lista de client IDs permitidos (uma entrada por SPA registrada
			// no Keycloak, ex: ["minha-app-web", "minha-app-admin"]).
			azp, _ := claims["azp"].(string)
			if !isAllowedClientID(azp, allowedClientIDs) {
				report(onAuthResult, false, "invalid_claims")
				log.Printf("[iam] auth failed: azp not allowed trace_id=%s azp=%s", traceID, azp)
				sendIAMError(w, http.StatusUnauthorized, "invalid_token", traceID)
				return
			}

			// 4. Injetar sub, azp (client_id), email e email_verified no context.
			// email_verified é injetado (não checado aqui) de propósito — este middleware
			// é genérico e pode ganhar outros consumidores futuros que não dependam de
			// e-mail; quem usa e-mail como identidade (ex: KeycloakSSOHandler) checa
			// GetIAMEmailVerified() explicitamente antes de confiar na claim.
			sub, _ := claims["sub"].(string)
			email, _ := claims["email"].(string)
			emailVerified, _ := claims["email_verified"].(bool)
			ctx := context.WithValue(r.Context(), ContextKeyIAMSub, sub)
			ctx = context.WithValue(ctx, ContextKeyIAMClientID, azp)
			ctx = context.WithValue(ctx, ContextKeyIAMEmail, email)
			ctx = context.WithValue(ctx, ContextKeyIAMEmailVerified, emailVerified)

			// Ponto de extensão: se seu projeto precisa de RBAC, leia
			// claims["realm_access"].(map[string]any)["roles"] ou
			// claims["resource_access"] aqui e injete no context também.
			// Keycloak carrega essas claims no próprio JWT — não existe (nem deve
			// existir) uma chamada HTTP separada tipo "/permissions" para isso.

			report(onAuthResult, true, "")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// report chama onAuthResult com segurança contra nil.
func report(onAuthResult func(ok bool, reason string), ok bool, reason string) {
	if onAuthResult != nil {
		onAuthResult(ok, reason)
	}
}

// GetIAMSub extrai o subject (sub claim) do token do context da requisição.
// Retorna string vazia se o middleware não foi aplicado ou o token não tinha sub.
func GetIAMSub(ctx context.Context) string {
	sub, _ := ctx.Value(ContextKeyIAMSub).(string)
	return sub
}

// GetIAMEmail extrai o e-mail do token do context da requisição.
// Retorna string vazia se o middleware não foi aplicado ou o token não tinha email.
func GetIAMEmail(ctx context.Context) string {
	email, _ := ctx.Value(ContextKeyIAMEmail).(string)
	return email
}

// GetIAMEmailVerified extrai a claim email_verified do context da requisição.
// Retorna false se o middleware não foi aplicado ou a claim não veio no token — quem
// usa e-mail como identidade (ex: KeycloakSSOHandler) DEVE checar isso antes de confiar
// no e-mail (achado de revisão: um e-mail não verificado pode ter sido alterado pelo
// próprio usuário no Keycloak sem reconfirmação, abrindo caminho pra login como outra
// pessoa se usado sem essa checagem).
func GetIAMEmailVerified(ctx context.Context) bool {
	verified, _ := ctx.Value(ContextKeyIAMEmailVerified).(bool)
	return verified
}

// classifyJWTError mapeia erros do jwt/v5 para códigos curtos, úteis como
// label de métrica (reason em onAuthResult).
func classifyJWTError(err error) string {
	switch {
	case isError(err, jwt.ErrTokenExpired):
		return "token_expired"
	case isError(err, jwt.ErrTokenSignatureInvalid):
		return "invalid_signature"
	case isError(err, jwt.ErrTokenInvalidClaims),
		isError(err, jwt.ErrTokenInvalidAudience),
		isError(err, jwt.ErrTokenInvalidIssuer):
		return "invalid_claims"
	case isError(err, ErrKeyNotFound):
		return "key_not_found"
	default:
		return "invalid_token"
	}
}

// isError verifica se err contém o erro alvo na chain de erros.
func isError(err, target error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), target.Error())
}

// isAllowedClientID verifica se azp está na lista de client IDs permitidos.
func isAllowedClientID(azp string, allowedClientIDs []string) bool {
	if azp == "" {
		return false
	}
	for _, id := range allowedClientIDs {
		if id == azp {
			return true
		}
	}
	return false
}

// sendIAMError escreve uma resposta de erro padronizada para falhas de autenticação.
func sendIAMError(w http.ResponseWriter, status int, code string, traceID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := map[string]string{"error": code}
	if traceID != "" {
		resp["trace_id"] = traceID
	}
	json.NewEncoder(w).Encode(resp)
}
