package iam

// Este arquivo vive junto com iam_jwks_client.go e iam_auth_middleware.go
// no mesmo pacote — veja a nota em iam_auth_middleware.go.

import (
	"crypto"
	"errors"
)

// ErrKeyNotFound é retornado quando o kid não é encontrado no JWKS do IdP,
// mesmo após recarregar o conjunto de chaves.
var ErrKeyNotFound = errors.New("iam: key not found in JWKS")

// KeyProvider busca chaves públicas do IdP (Keycloak) para validação de JWT RS256.
// Implementações devem fazer cache das chaves (TTL mínimo 1 hora) para
// evitar chamadas repetidas ao endpoint JWKS.
type KeyProvider interface {
	// GetKey retorna a chave pública RSA correspondente ao kid informado.
	// Busca do JWKS na primeira chamada e armazena em cache por um TTL configurável.
	// Retorna ErrKeyNotFound se o kid não existir nem após reload do JWKS.
	GetKey(kid string) (crypto.PublicKey, error)
}
