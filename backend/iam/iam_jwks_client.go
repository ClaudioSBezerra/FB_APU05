package iam

// Este arquivo vive junto com iam_key_provider.go e iam_auth_middleware.go
// no mesmo pacote — veja a nota em iam_auth_middleware.go.
// Depende de ErrKeyNotFound (iam_key_provider.go).
//
// Adaptado do template da skill keycloak-identity-react-go: troca de go.uber.org/zap
// pelo pacote log da stdlib, seguindo a convenção do projeto (sem lib de logging
// estruturado — ver CLAUDE.md).

import (
	"crypto"
	"crypto/rsa"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// jwksResponse representa a resposta do endpoint /protocol/openid-connect/certs do Keycloak.
type jwksResponse struct {
	Keys []jwkKey `json:"keys"`
}

// jwkKey representa uma chave pública no formato JWK (JSON Web Key).
type jwkKey struct {
	Kty string `json:"kty"` // Key type: "RSA"
	Kid string `json:"kid"` // Key ID
	Use string `json:"use"` // Usage: "sig"
	Alg string `json:"alg"` // Algorithm: "RS256"
	N   string `json:"n"`   // RSA modulus (base64url)
	E   string `json:"e"`   // RSA exponent (base64url)
}

// JWKSClient busca e faz cache de chaves públicas do Keycloak para validação de JWT RS256.
// Implementa a interface KeyProvider (iam_key_provider.go).
//
// Thread-safe: usa sync.RWMutex para acesso concorrente ao cache.
// O cache é invalidado automaticamente após cacheTTL (default: 1h).
type JWKSClient struct {
	baseURL    string
	httpClient *http.Client

	// OnFetchDuration, se definido, é chamado com a duração (em segundos) de
	// cada fetch ao endpoint JWKS — plugue aqui sua métrica Prometheus/OTel.
	OnFetchDuration func(seconds float64)

	mu       sync.RWMutex
	keys     map[string]crypto.PublicKey
	cachedAt time.Time
	cacheTTL time.Duration
}

// NewJWKSClient cria um novo JWKSClient com o TTL de cache especificado.
// jwksURL sobrescreve a URL do endpoint JWKS (útil para redes Docker internas);
// se vazio, usa baseURL + "/protocol/openid-connect/certs" (padrão Keycloak).
// baseURL é a URL do realm (ex: http://host:8180/realms/<realm>).
// Se cacheTTL for zero ou negativo, usa 1 hora como default.
// insecureSkipVerify desabilita a verificação TLS (use apenas com certificados autoassinados).
func NewJWKSClient(baseURL string, jwksURL string, cacheTTL time.Duration, insecureSkipVerify bool) *JWKSClient {
	if cacheTTL <= 0 {
		cacheTTL = 1 * time.Hour
	}
	if jwksURL == "" {
		jwksURL = baseURL + "/protocol/openid-connect/certs"
	}
	transport := http.DefaultTransport
	if insecureSkipVerify {
		transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // self-signed cert on internal IdP
		}
	}
	return &JWKSClient{
		baseURL:  jwksURL,
		cacheTTL: cacheTTL,
		keys:     make(map[string]crypto.PublicKey),
		httpClient: &http.Client{
			Timeout:   5 * time.Second,
			Transport: transport,
		},
	}
}

// GetKey retorna a chave pública RSA correspondente ao kid informado.
// Usa o cache quando válido; recarrega o JWKS quando o TTL expira ou o kid não é encontrado.
// Retorna ErrKeyNotFound se o kid não existir mesmo após reload.
func (c *JWKSClient) GetKey(kid string) (crypto.PublicKey, error) {
	// Tentativa 1: cache hit com RLock
	c.mu.RLock()
	if !c.isCacheExpiredLocked() {
		if key, ok := c.keys[kid]; ok {
			c.mu.RUnlock()
			return key, nil
		}
	}
	c.mu.RUnlock()

	// Cache expirado ou kid ausente: recarregar com Lock exclusivo
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check: outro goroutine pode ter atualizado enquanto esperávamos o Lock
	if !c.isCacheExpiredLocked() {
		if key, ok := c.keys[kid]; ok {
			return key, nil
		}
	}

	// Recarregar JWKS do Keycloak
	if err := c.fetchJWKSLocked(); err != nil {
		return nil, fmt.Errorf("iam jwks: fetch failed: %w", err)
	}

	key, ok := c.keys[kid]
	if !ok {
		return nil, ErrKeyNotFound
	}
	return key, nil
}

// isCacheExpiredLocked verifica se o cache está expirado.
// DEVE ser chamado com mu lock (read ou write) já adquirido.
func (c *JWKSClient) isCacheExpiredLocked() bool {
	return c.cachedAt.IsZero() || time.Since(c.cachedAt) > c.cacheTTL
}

// fetchJWKSLocked busca o JWKS do Keycloak e atualiza o cache.
// DEVE ser chamado com mu write lock já adquirido.
func (c *JWKSClient) fetchJWKSLocked() error {
	start := time.Now()
	url := c.baseURL

	resp, err := c.httpClient.Get(url)
	elapsed := time.Since(start)

	if c.OnFetchDuration != nil {
		c.OnFetchDuration(elapsed.Seconds())
	}

	if err != nil {
		log.Printf("[iam] jwks fetch failed url=%s elapsed=%v err=%v", url, elapsed, err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("iam jwks: unexpected status %d from %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("iam jwks: read body: %w", err)
	}

	var jwks jwksResponse
	if err := json.Unmarshal(body, &jwks); err != nil {
		return fmt.Errorf("iam jwks: parse response: %w", err)
	}

	newKeys := make(map[string]crypto.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		pubKey, err := parseRSAPublicKey(k)
		if err != nil {
			log.Printf("[iam] jwks: skip invalid key kid=%s err=%v", k.Kid, err)
			continue
		}
		newKeys[k.Kid] = pubKey
	}

	c.keys = newKeys
	c.cachedAt = time.Now()

	log.Printf("[iam] jwks refreshed keys_count=%d fetch_duration=%v", len(newKeys), elapsed)
	return nil
}

// parseRSAPublicKey constrói uma *rsa.PublicKey a partir de um JWK com kty=RSA.
func parseRSAPublicKey(k jwkKey) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode modulus n: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode exponent e: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := new(big.Int).SetBytes(eBytes)

	if !e.IsInt64() {
		return nil, fmt.Errorf("exponent too large")
	}

	return &rsa.PublicKey{N: n, E: int(e.Int64())}, nil
}
