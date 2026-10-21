package handlers

// main_test.go — TestMain do pacote. Define um JWT_SECRET padrão pra todo
// teste do pacote (21/10, companion da remoção do fallback hardcoded de
// getJWTSecret em auth.go): sem isso, qualquer teste que exercite um
// caminho que emite/valida token (LoginHandler, RequireAuth, etc.) sem
// passar por t.Setenv("JWT_SECRET", ...) próprio derrubaria o binário de
// teste inteiro (getJWTSecret agora chama log.Fatal em vez de devolver um
// valor hardcoded). Testes que precisam de um valor específico (ex.
// JWT_SECRET fraco, pra exercitar ValidateJWTSecret) continuam livres pra
// usar t.Setenv — só sobrescreve por aquele teste, reverte pra este default
// depois.

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("JWT_SECRET") == "" {
		_ = os.Setenv("JWT_SECRET", "test-secret-default-para-todo-o-pacote-32bytes+")
	}
	os.Exit(m.Run())
}
