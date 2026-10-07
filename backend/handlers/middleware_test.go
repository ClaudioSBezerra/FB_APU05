package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestRequireAuth_MissingToken garante 401 sem Authorization header — nenhum
// DB envolvido (db=nil é seguro: o handler protegido nunca é chamado).
func TestRequireAuth_MissingToken(t *testing.T) {
	protected := RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next não deveria ser chamado sem token")
	}, "administrador")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/usuarios/x", nil)
	rec := httptest.NewRecorder()
	protected(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperado 401, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestRequireAuth_WrongPerfil garante 403 quando o token é válido mas o
// perfil não bate com o exigido (claim `perfil`, nunca `role` — AD-7).
func TestRequireAuth_WrongPerfil(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-at-least-32-bytes-long!!")

	token, err := GenerateToken("user-123", "solicitante")
	if err != nil {
		t.Fatalf("GenerateToken falhou: %v", err)
	}

	called := false
	protected := RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}, "administrador")

	req := httptest.NewRequest(http.MethodPatch, "/api/admin/usuarios/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	protected(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("esperado 403, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("next não deveria ser chamado com perfil insuficiente")
	}
}

// TestRequireAuth_RevokedToken garante 401 para um token presente na
// blacklist (gap da Story 1.2 que esta story fecha) mesmo que ele ainda seja
// uma assinatura válida e não expirada.
func TestRequireAuth_RevokedToken(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-at-least-32-bytes-long!!")

	token, err := GenerateToken("user-123", "administrador")
	if err != nil {
		t.Fatalf("GenerateToken falhou: %v", err)
	}
	tokenBlacklist.Store(token, time.Now().Add(time.Hour))
	defer tokenBlacklist.Delete(token)

	called := false
	protected := RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}, "administrador")

	req := httptest.NewRequest(http.MethodPatch, "/api/admin/usuarios/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	protected(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperado 401 para token revogado, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("next não deveria ser chamado com token revogado")
	}
}

// TestRequireAuth_ValidTokenMatchingPerfil garante que o caminho feliz chama
// next com as claims injetadas no contexto (GetUserIDFromContext).
func TestRequireAuth_ValidTokenMatchingPerfil(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-at-least-32-bytes-long!!")

	token, err := GenerateToken("user-456", "administrador")
	if err != nil {
		t.Fatalf("GenerateToken falhou: %v", err)
	}

	var gotUserID string
	protected := RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		gotUserID = GetUserIDFromContext(r)
		w.WriteHeader(http.StatusOK)
	}, "administrador")

	req := httptest.NewRequest(http.MethodPatch, "/api/admin/usuarios/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	protected(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if gotUserID != "user-456" {
		t.Fatalf("esperado user_id injetado no contexto = user-456, obtido %q", gotUserID)
	}
}
