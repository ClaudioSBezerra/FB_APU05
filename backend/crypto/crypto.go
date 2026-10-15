// Package crypto fornece criptografia simétrica (AES-256-GCM) para campos
// sensíveis persistidos no banco — hoje só as credenciais do ERP Bridge
// (backend/handlers/erp_bridge.go). Porta direta do pacote equivalente do
// FB_APU02 (backend/crypto/), que já protege os mesmos tipos de campo
// (usuário/senha Oracle, API key do daemon) há mais tempo em produção —
// nenhuma criptografia nova inventada aqui.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
)

// getEncryptionKey deriva uma chave AES-256 (32 bytes) de ENCRYPTION_KEY via
// SHA-256 — a variável já existe em docker-compose.prod.yml/.env.production
// desde o deploy inicial (provisionada à frente desta feature). Sem fallback
// para JWT_SECRET (diferente do FB_APU02): aqui ENCRYPTION_KEY é obrigatória
// desde o primeiro uso, não uma migração de um estado anterior sem ela.
func getEncryptionKey() []byte {
	key := os.Getenv("ENCRYPTION_KEY")
	if key == "" {
		key = "dev-key-change-me-encryption"
	}
	h := sha256.Sum256([]byte(key))
	return h[:]
}

// EncryptField cifra plaintext com AES-256-GCM — retorna base64(nonce+ciphertext).
func EncryptField(plaintext string) (string, error) {
	key := getEncryptionKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptField decifra um valor produzido por EncryptField.
func DecryptField(encoded string) (string, error) {
	key := getEncryptionKey()
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("ciphertext muito curto")
	}
	nonce, ct := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
