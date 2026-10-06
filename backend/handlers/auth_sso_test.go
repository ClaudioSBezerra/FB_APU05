package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"fb_apu05/iam"
)

// TestKeycloakSSOHandler_RejectsUnverifiedEmail_NoDBCall garante que um token
// com email_verified=false é recusado com 401 antes de qualquer acesso ao
// banco (achado de revisão herdado do FB_APU02 — ver auth_sso.go). db=nil é
// seguro aqui porque o handler retorna nesse branch sem jamais tocar *sql.DB.
func TestKeycloakSSOHandler_RejectsUnverifiedEmail_NoDBCall(t *testing.T) {
	ctx := context.WithValue(context.Background(), iam.ContextKeyIAMEmail, "pessoa@ferreiracosta.com.br")
	ctx = context.WithValue(ctx, iam.ContextKeyIAMEmailVerified, false)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/sso/keycloak", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	handler := KeycloakSSOHandler(nil)
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for email_verified=false, got %d (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestSSOConfigHandler_EnabledOnlyWhenAllFourVarsPresent cobre as 5 combinações
// relevantes: cada uma das 4 env vars obrigatórias faltando (uma de cada vez)
// deve resultar em enabled:false, e todas presentes deve resultar em
// enabled:true — exatamente a checagem que a spec desta story pede para
// replicar (achado de bug real: checagem parcial mostra um botão de SSO
// quebrado).
func TestSSOConfigHandler_EnabledOnlyWhenAllFourVarsPresent(t *testing.T) {
	required := map[string]string{
		"IAM_BASE_URL":           "https://iam.test.invalid/realms/x",
		"IAM_CLIENT_ID":          "fb-apu05",
		"IAM_REDIRECT_URI":       "http://localhost:3000/auth/callback",
		"IAM_ALLOWED_CLIENT_IDS": "fb-apu05",
	}

	setEnv := func(t *testing.T, missing string) {
		t.Helper()
		for k, v := range required {
			if k == missing {
				t.Setenv(k, "")
				continue
			}
			t.Setenv(k, v)
		}
	}

	decodeEnabled := func(t *testing.T, rec *httptest.ResponseRecorder) bool {
		t.Helper()
		var body map[string]interface{}
		if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		enabled, _ := body["enabled"].(bool)
		return enabled
	}

	for missing := range required {
		missing := missing
		t.Run("missing_"+missing, func(t *testing.T) {
			setEnv(t, missing)
			req := httptest.NewRequest(http.MethodGet, "/api/auth/sso/config", nil)
			rec := httptest.NewRecorder()
			SSOConfigHandler()(rec, req)

			if decodeEnabled(t, rec) {
				t.Fatalf("expected enabled:false with %s missing", missing)
			}
		})
	}

	t.Run("all_present", func(t *testing.T) {
		setEnv(t, "")
		req := httptest.NewRequest(http.MethodGet, "/api/auth/sso/config", nil)
		rec := httptest.NewRecorder()
		SSOConfigHandler()(rec, req)

		if !decodeEnabled(t, rec) {
			t.Fatalf("expected enabled:true when all 4 vars are present")
		}
	})
}
