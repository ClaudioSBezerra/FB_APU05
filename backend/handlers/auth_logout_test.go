package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

// TestLogoutHandler_BlacklistsValidToken garante o caminho feliz: um token
// HS256 próprio e válido é colocado na blacklist no logout.
func TestLogoutHandler_BlacklistsValidToken(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-at-least-32-bytes-long!!")

	token, err := GenerateToken("user-789", "solicitante")
	if err != nil {
		t.Fatalf("GenerateToken falhou: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	LogoutHandler(nil)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if _, revoked := tokenBlacklist.Load(token); !revoked {
		t.Fatal("token válido deveria estar na blacklist após logout")
	}
	tokenBlacklist.Delete(token)
}

// TestLogoutHandler_RejectsNonHS256Token garante que jwt.WithValidMethods
// restringe de fato o logout a HS256: um token com assinatura HMAC válida
// (mesmo segredo) mas assinado com um algoritmo diferente (HS384) nunca deve
// ser aceito nem entrar na blacklist — hardening adicionado nesta story.
func TestLogoutHandler_RejectsNonHS256Token(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-at-least-32-bytes-long!!")

	claims := jwt.MapClaims{
		"user_id": "user-789",
		"perfil":  "solicitante",
		"exp":     float64(9999999999),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS384, claims)
	signed, err := token.SignedString(getJWTSecret())
	if err != nil {
		t.Fatalf("falha ao assinar token de teste com HS384: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+signed)
	rec := httptest.NewRecorder()
	LogoutHandler(nil)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200 (logout sempre responde OK mesmo sem blacklistar), obtido %d", rec.Code)
	}
	if _, revoked := tokenBlacklist.Load(signed); revoked {
		t.Fatal("token assinado com HS384 não deveria ter sido aceito nem blacklistado (jwt.WithValidMethods([]string{\"HS256\"}))")
	}
}
