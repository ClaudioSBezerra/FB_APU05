package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"fb_apu05/iam"
)

// SSOConfigHandler — GET /api/auth/sso/config (público, sem segredo).
//
// Expõe se o SSO Keycloak está configurado neste servidor e, se sim, os
// parâmetros que o frontend precisa pra montar a URL de login (nenhum é
// sensível — é o mesmo tipo de dado que já fica visível na URL de authorize de
// qualquer client público OIDC/PKCE). Buscado em runtime pelo frontend em vez
// de cravado em build-time pelo mesmo motivo documentado no FB_APU02: a mesma
// imagem de frontend pode rodar em servidores diferentes.
//
// Só reporta enabled:true se TODAS as 4 env vars que o fluxo precisa estiverem
// presentes — um .env incompleto (ex: IAM_BASE_URL setado mas IAM_CLIENT_ID
// esquecido) não pode mostrar um botão de SSO que vai falhar ao clicar
// (achado de bug real documentado na spec desta story: checagem parcial
// reproduz esse exato problema).
func SSOConfigHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		iamBaseURL := os.Getenv("IAM_BASE_URL")
		clientID := os.Getenv("IAM_CLIENT_ID")
		redirectURI := os.Getenv("IAM_REDIRECT_URI")
		if iamBaseURL == "" || clientID == "" || redirectURI == "" || os.Getenv("IAM_ALLOWED_CLIENT_IDS") == "" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"enabled": false})
			return
		}
		scopes := os.Getenv("IAM_SCOPES")
		if scopes == "" {
			scopes = "openid profile email"
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"enabled":      true,
			"base_url":     iamBaseURL,
			"client_id":    clientID,
			"redirect_uri": redirectURI,
			"scopes":       scopes,
		})
	}
}

// KeycloakSSOHandler troca um access token do Keycloak (já validado por
// iam.IAMAuthMiddleware antes desta função rodar — o handler é sempre
// registrado envolto nesse middleware, nunca exposto direto) por uma sessão
// própria do FB_APU05.
// Requisição: Authorization: Bearer <access_token do Keycloak> (mesmo header
// já validado pelo middleware, sem corpo).
//
// Diferente do fluxo de referência no FB_APU02 (que rejeita e-mail
// desconhecido): aqui, primeiro login de um e-mail verificado sem registro em
// `usuarios` AUTO-PROVISIONA — cria o registro na hora com perfil padrão
// 'solicitante' (decisão de negócio registrada na spec desta story,
// 2026-10-06).
func KeycloakSSOHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		start := time.Now()

		email := iam.GetIAMEmail(r.Context())
		if email == "" {
			log.Printf("[SSO Keycloak] Token válido mas sem claim 'email'")
			jsonErr(w, http.StatusBadRequest, "Token do Keycloak não contém e-mail.")
			return
		}

		// CRÍTICO (AD-6, achado de revisão herdado do FB_APU02): sem checar
		// email_verified, um usuário poderia editar o próprio e-mail de perfil
		// no Keycloak (se o realm permitir sem reconfirmação) e logar como outra
		// pessoa — o e-mail é a ÚNICA identidade usada aqui para casar com
		// `usuarios`.
		if !iam.GetIAMEmailVerified(r.Context()) {
			log.Printf("[SSO Keycloak] Rejeitado: e-mail não verificado no Keycloak (%s)", email)
			jsonErr(w, http.StatusUnauthorized, "E-mail não verificado no Keycloak. Confirme seu e-mail e tente novamente.")
			return
		}

		user, err := fetchUserByEmail(db, email)
		if err == sql.ErrNoRows {
			log.Printf("[SSO Keycloak] Primeiro login — auto-provisionando usuário: %s", email)
			nome := email
			if at := strings.IndexByte(email, '@'); at > 0 {
				nome = email[:at]
			}
			// ON CONFLICT (LOWER(email)) DO UPDATE ... RETURNING: protege contra a
			// corrida de duas requisições concorrentes de primeiro login do mesmo
			// e-mail, inclusive com casing diferente (ex: duplo clique, ou
			// "Foo@x.com" vs "foo@x.com") — sem isso, a segunda bateria no índice
			// único de LOWER(email) e devolveria 500 em vez de completar o login.
			err = db.QueryRow(`
				INSERT INTO usuarios (email, nome, perfil)
				VALUES ($1, $2, 'solicitante')
				ON CONFLICT (LOWER(email)) DO UPDATE SET email = EXCLUDED.email
				RETURNING id, email, nome, perfil, created_at
			`, email, nome).Scan(&user.ID, &user.Email, &user.Nome, &user.Perfil, &user.CreatedAt)
			if err != nil {
				log.Printf("[SSO Keycloak] Erro ao auto-provisionar usuário %s: %v", email, err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
		} else if err != nil {
			log.Printf("[SSO Keycloak] Erro ao buscar usuário %s: %v", email, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		log.Printf("[SSO Keycloak] Login via Keycloak: %s", email)
		finishLogin(db, w, r, user, start)
	}
}
