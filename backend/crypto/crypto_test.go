package crypto

import (
	"os"
	"testing"
)

func TestEncryptDecryptField_RoundTrip(t *testing.T) {
	os.Setenv("ENCRYPTION_KEY", "chave-de-teste-nao-usar-em-producao")
	defer os.Unsetenv("ENCRYPTION_KEY")

	encrypted, err := EncryptField("fcosta")
	if err != nil {
		t.Fatalf("EncryptField falhou: %v", err)
	}
	if encrypted == "fcosta" {
		t.Fatalf("EncryptField devolveu o texto puro sem cifrar")
	}

	decrypted, err := DecryptField(encrypted)
	if err != nil {
		t.Fatalf("DecryptField falhou: %v", err)
	}
	if decrypted != "fcosta" {
		t.Fatalf("decrypted = %q, esperado 'fcosta'", decrypted)
	}
}

func TestDecryptField_ChaveErrada(t *testing.T) {
	os.Setenv("ENCRYPTION_KEY", "chave-a")
	encrypted, err := EncryptField("segredo")
	if err != nil {
		t.Fatalf("EncryptField falhou: %v", err)
	}

	os.Setenv("ENCRYPTION_KEY", "chave-b")
	defer os.Unsetenv("ENCRYPTION_KEY")
	if _, err := DecryptField(encrypted); err == nil {
		t.Fatal("DecryptField deveria falhar com uma ENCRYPTION_KEY diferente da usada para cifrar")
	}
}
