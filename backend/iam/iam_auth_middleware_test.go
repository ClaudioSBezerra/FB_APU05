package iam

// Testes unitários do parsing JWT/JWKS (chaves de teste geradas localmente,
// nunca contra o Keycloak real) — exatamente o que a spec da Story 1.2 (seção
// Verification) aponta como o que esta sessão consegue verificar, já que o
// fluxo PKCE completo não pode ser exercitado neste sandbox (sem client
// Keycloak provisionado nem Docker pra subir a stack).

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testIssuer = "https://iam.test.invalid/realms/ferreiracosta-test"

// testKeySet gera um par de chaves RSA local e expõe um servidor HTTP de teste
// que serve o JWKS correspondente — substitui o Keycloak real para estes testes.
type testKeySet struct {
	kid        string
	privateKey *rsa.PrivateKey
	server     *httptest.Server
	requests   int
}

func newTestKeySet(t *testing.T, kid string) *testKeySet {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	ks := &testKeySet{kid: kid, privateKey: priv}
	ks.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ks.requests++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwksResponse{Keys: []jwkKey{ks.jwk()}})
	}))
	return ks
}

func (ks *testKeySet) jwk() jwkKey {
	pub := ks.privateKey.PublicKey
	return jwkKey{
		Kty: "RSA",
		Kid: ks.kid,
		Use: "sig",
		Alg: "RS256",
		N:   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		E:   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
}

// signToken assina um JWT RS256 com a chave privada de teste, com o kid no
// header (igual ao que o Keycloak faz) e as claims informadas.
func (ks *testKeySet) signToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = ks.kid
	signed, err := token.SignedString(ks.privateKey)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}
	return signed
}

func validClaims(azp string) jwt.MapClaims {
	return jwt.MapClaims{
		"sub":            "test-sub-123",
		"azp":            azp,
		"email":          "pessoa@ferreiracosta.com.br",
		"email_verified": true,
		"iss":            testIssuer,
		"exp":            time.Now().Add(5 * time.Minute).Unix(),
	}
}

// --- JWKSClient / KeyProvider ---

func TestJWKSClient_GetKey_Success(t *testing.T) {
	ks := newTestKeySet(t, "kid-1")
	defer ks.server.Close()

	client := NewJWKSClient("", ks.server.URL, time.Minute, false)
	key, err := client.GetKey("kid-1")
	if err != nil {
		t.Fatalf("GetKey: unexpected error: %v", err)
	}
	if _, ok := key.(*rsa.PublicKey); !ok {
		t.Fatalf("GetKey: expected *rsa.PublicKey, got %T", key)
	}
}

func TestJWKSClient_GetKey_UnknownKid(t *testing.T) {
	ks := newTestKeySet(t, "kid-1")
	defer ks.server.Close()

	client := NewJWKSClient("", ks.server.URL, time.Minute, false)
	_, err := client.GetKey("does-not-exist")
	if err != ErrKeyNotFound {
		t.Fatalf("GetKey: expected ErrKeyNotFound, got %v", err)
	}
}

func TestJWKSClient_GetKey_CachesWithinTTL(t *testing.T) {
	ks := newTestKeySet(t, "kid-1")
	defer ks.server.Close()

	client := NewJWKSClient("", ks.server.URL, time.Hour, false)
	if _, err := client.GetKey("kid-1"); err != nil {
		t.Fatalf("first GetKey: %v", err)
	}
	if _, err := client.GetKey("kid-1"); err != nil {
		t.Fatalf("second GetKey: %v", err)
	}
	if ks.requests != 1 {
		t.Fatalf("expected JWKS endpoint to be hit once (cache within TTL), got %d requests", ks.requests)
	}
}

// --- IAMAuthMiddleware ---

func newMiddlewareUnderTest(ks *testKeySet, allowedClientIDs []string) http.Handler {
	jwksClient := NewJWKSClient("", ks.server.URL, time.Hour, false)
	mw := IAMAuthMiddleware(jwksClient, testIssuer, allowedClientIDs, nil, nil)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Email", GetIAMEmail(r.Context()))
		if GetIAMEmailVerified(r.Context()) {
			w.Header().Set("X-Email-Verified", "true")
		}
		w.Header().Set("X-Sub", GetIAMSub(r.Context()))
		w.WriteHeader(http.StatusOK)
	})
	return mw(inner)
}

func TestIAMAuthMiddleware_ValidToken_InjectsContext(t *testing.T) {
	ks := newTestKeySet(t, "kid-1")
	defer ks.server.Close()

	handler := newMiddlewareUnderTest(ks, []string{"fb-apu05"})
	token := ks.signToken(t, validClaims("fb-apu05"))

	req := httptest.NewRequest(http.MethodPost, "/api/auth/sso/keycloak", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Email"); got != "pessoa@ferreiracosta.com.br" {
		t.Fatalf("expected injected email, got %q", got)
	}
	if got := rec.Header().Get("X-Email-Verified"); got != "true" {
		t.Fatalf("expected email_verified injected as true, got %q", got)
	}
	if got := rec.Header().Get("X-Sub"); got != "test-sub-123" {
		t.Fatalf("expected injected sub, got %q", got)
	}
}

func TestIAMAuthMiddleware_MissingAuthorizationHeader(t *testing.T) {
	ks := newTestKeySet(t, "kid-1")
	defer ks.server.Close()

	handler := newMiddlewareUnderTest(ks, []string{"fb-apu05"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/sso/keycloak", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestIAMAuthMiddleware_AzpNotInAllowlist(t *testing.T) {
	ks := newTestKeySet(t, "kid-1")
	defer ks.server.Close()

	// allowlist só conhece "fb-apu05" — nunca deve aceitar azp de outro client
	// (ex: um token válido emitido pro client isolado do FB_APU02).
	handler := newMiddlewareUnderTest(ks, []string{"fb-apu05"})
	token := ks.signToken(t, validClaims("fb-apu02"))

	req := httptest.NewRequest(http.MethodPost, "/api/auth/sso/keycloak", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for azp fora da allowlist, got %d", rec.Code)
	}
}

func TestIAMAuthMiddleware_ExpiredToken(t *testing.T) {
	ks := newTestKeySet(t, "kid-1")
	defer ks.server.Close()

	handler := newMiddlewareUnderTest(ks, []string{"fb-apu05"})
	claims := validClaims("fb-apu05")
	claims["exp"] = time.Now().Add(-1 * time.Hour).Unix()
	token := ks.signToken(t, claims)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/sso/keycloak", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for expired token, got %d", rec.Code)
	}
}

func TestIAMAuthMiddleware_WrongIssuer(t *testing.T) {
	ks := newTestKeySet(t, "kid-1")
	defer ks.server.Close()

	handler := newMiddlewareUnderTest(ks, []string{"fb-apu05"})
	claims := validClaims("fb-apu05")
	claims["iss"] = "https://attacker.invalid/realms/fake"
	token := ks.signToken(t, claims)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/sso/keycloak", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for issuer divergente, got %d", rec.Code)
	}
}

func TestIAMAuthMiddleware_UnknownKid(t *testing.T) {
	ks := newTestKeySet(t, "kid-1")
	defer ks.server.Close()
	other := newTestKeySet(t, "kid-2") // chave diferente, nunca publicada no JWKS de `ks`
	defer other.server.Close()

	handler := newMiddlewareUnderTest(ks, []string{"fb-apu05"})
	// Assinado com a chave de `other` mas apontando pro JWKS de `ks` — simula um
	// token forjado/de outro IdP.
	token := other.signToken(t, validClaims("fb-apu05"))

	req := httptest.NewRequest(http.MethodPost, "/api/auth/sso/keycloak", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for kid desconhecido/assinatura inválida, got %d", rec.Code)
	}
}
