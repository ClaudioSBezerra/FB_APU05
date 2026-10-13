package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// newLoginRequest monta um POST com um X-Forwarded-For único por teste —
// sem isso, todas as chamadas de teste compartilhariam a mesma chave de
// LoginRL (GetClientIP cai no RemoteAddr padrão de httptest, igual em toda
// *http.Request criada por httptest.NewRequest) e testes depois do 5º
// começariam a tomar 429 do rate limiter em vez do status que o teste
// realmente quer exercitar.
var loginTestIPCounter int

func newLoginRequest(body string) *http.Request {
	loginTestIPCounter++
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("X-Forwarded-For", fmt.Sprintf("203.0.113.%d", loginTestIPCounter%255+1))
	return req
}

// sqlmockRowsUsuarioComSenha monta a linha que fetchUserForLocalLogin espera
// (id, email, nome, perfil, created_at, password_hash) — hash=nil simula uma
// conta auto-provisionada via SSO (sem senha local definida).
func sqlmockRowsUsuarioComSenha(id, email, nome, perfil string, hash *string) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"id", "email", "nome", "perfil", "created_at", "password_hash"})
	var hashValue interface{}
	if hash != nil {
		hashValue = *hash
	}
	rows.AddRow(id, email, nome, perfil, "2026-01-01T00:00:00Z", hashValue)
	return rows
}

// --- LoginHandler: validação de entrada (db=nil, nunca chega ao banco) ---

func TestLoginHandler_RejectsWrongMethod(t *testing.T) {
	handler := LoginHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/login", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("esperado 405, obtido %d", rec.Code)
	}
}

func TestLoginHandler_RejectsInvalidBody(t *testing.T) {
	handler := LoginHandler(nil)
	rec := httptest.NewRecorder()
	handler(rec, newLoginRequest("{not json"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d", rec.Code)
	}
}

func TestLoginHandler_RejectsMissingFields(t *testing.T) {
	cases := []string{
		`{"email":"","password":"algo"}`,
		`{"email":"a@b.com","password":""}`,
		`{}`,
	}
	for _, body := range cases {
		handler := LoginHandler(nil)
		rec := httptest.NewRecorder()
		handler(rec, newLoginRequest(body))

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body=%s: esperado 400, obtido %d", body, rec.Code)
		}
	}
}

// --- LoginHandler: caminhos que chegam ao banco (sqlmock) ---

func TestLoginHandler_UnknownEmail_GenericError(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectQuery("SELECT id, email, nome, perfil, created_at, password_hash FROM usuarios").
		WithArgs("naoexiste@fortesbezerra.com.br").
		WillReturnError(sql.ErrNoRows)

	handler := LoginHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newLoginRequest(`{"email":"naoexiste@fortesbezerra.com.br","password":"qualquer123"}`))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperado 401, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "E-mail ou senha inválidos") {
		t.Fatalf("mensagem genérica esperada, obtido: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas sqlmock não satisfeitas: %v", err)
	}
}

func TestLoginHandler_SSOOnlyAccount_GenericError(t *testing.T) {
	db, mock := newSQLMock(t)
	rows := sqlmockRowsUsuarioComSenha("u1", "sso@fortesbezerra.com.br", "Fulano", "solicitante", nil)
	mock.ExpectQuery("SELECT id, email, nome, perfil, created_at, password_hash FROM usuarios").
		WithArgs("sso@fortesbezerra.com.br").
		WillReturnRows(rows)

	handler := LoginHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newLoginRequest(`{"email":"sso@fortesbezerra.com.br","password":"qualquer123"}`))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperado 401 (conta só-SSO, sem password_hash), obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestLoginHandler_WrongPassword_GenericError(t *testing.T) {
	hash, err := HashPassword("senha-correta-123")
	if err != nil {
		t.Fatalf("HashPassword falhou: %v", err)
	}

	db, mock := newSQLMock(t)
	rows := sqlmockRowsUsuarioComSenha("u2", "local@fortesbezerra.com.br", "Fulano", "solicitante", &hash)
	mock.ExpectQuery("SELECT id, email, nome, perfil, created_at, password_hash FROM usuarios").
		WithArgs("local@fortesbezerra.com.br").
		WillReturnRows(rows)

	handler := LoginHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newLoginRequest(`{"email":"local@fortesbezerra.com.br","password":"senha-errada"}`))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperado 401, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestLoginHandler_CorrectPassword_Succeeds(t *testing.T) {
	hash, err := HashPassword("senha-correta-123")
	if err != nil {
		t.Fatalf("HashPassword falhou: %v", err)
	}

	db, mock := newSQLMock(t)
	rows := sqlmockRowsUsuarioComSenha("u3", "local@fortesbezerra.com.br", "Fulano", "administrador", &hash)
	mock.ExpectQuery("SELECT id, email, nome, perfil, created_at, password_hash FROM usuarios").
		WithArgs("local@fortesbezerra.com.br").
		WillReturnRows(rows)

	handler := LoginHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newLoginRequest(`{"email":"local@fortesbezerra.com.br","password":"senha-correta-123"}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"perfil":"administrador"`) {
		t.Fatalf("resposta não contém o usuário esperado: %s", rec.Body.String())
	}
	if rec.Result().Header.Get("Set-Cookie") == "" {
		t.Fatalf("esperado cookie de refresh_token setado no login bem-sucedido")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas sqlmock não satisfeitas: %v", err)
	}
}
