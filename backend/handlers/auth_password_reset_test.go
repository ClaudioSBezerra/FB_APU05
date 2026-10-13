package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
)

var ipCounter int

func newIP() string {
	ipCounter++
	return fmt.Sprintf("198.51.100.%d", ipCounter%255+1)
}

// --- ForgotPasswordHandler ---
// Nota: SMTP_PASSWORD nunca está setada no ambiente de teste, então o
// caminho "e-mail existe e tem senha local" bateria em
// services.SendPasswordResetEmail e falharia com 500 (erro real, não
// mockável sem subir um servidor SMTP) — os testes abaixo cobrem os ramos
// que retornam ANTES de tentar enviar e-mail (mensagem genérica).

func TestForgotPasswordHandler_RejectsWrongMethod(t *testing.T) {
	handler := ForgotPasswordHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/forgot-password", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("esperado 405, obtido %d", rec.Code)
	}
}

func TestForgotPasswordHandler_RejectsMissingEmail(t *testing.T) {
	handler := ForgotPasswordHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/forgot-password", strings.NewReader(`{"email":""}`))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d", rec.Code)
	}
}

func TestForgotPasswordHandler_UnknownEmail_GenericSuccess(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, password_hash FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("naoexiste@fortesbezerra.com.br").
		WillReturnError(sql.ErrNoRows)

	handler := ForgotPasswordHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/forgot-password", strings.NewReader(`{"email":"naoexiste@fortesbezerra.com.br"}`))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200 (mensagem genérica, não revela e-mail inexistente), obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas sqlmock não satisfeitas: %v", err)
	}
}

func TestForgotPasswordHandler_SSOOnlyAccount_GenericSuccess(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, password_hash FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("sso@fortesbezerra.com.br").
		WillReturnRows(sqlmock.NewRows([]string{"id", "password_hash"}).AddRow("u1", nil))

	handler := ForgotPasswordHandler(db)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/forgot-password", strings.NewReader(`{"email":"sso@fortesbezerra.com.br"}`))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200 (mensagem genérica, conta só-SSO não tem senha pra redefinir), obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	// Nenhum INSERT em verification_tokens deveria ter sido tentado.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas sqlmock não satisfeitas: %v", err)
	}
}

// --- ResetPasswordHandler ---

func TestResetPasswordHandler_RejectsWrongMethod(t *testing.T) {
	handler := ResetPasswordHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/reset-password", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("esperado 405, obtido %d", rec.Code)
	}
}

func TestResetPasswordHandler_RejectsMismatchedPasswords(t *testing.T) {
	handler := ResetPasswordHandler(nil)
	body := `{"token":"abc","password":"senha12345","confirm_password":"outra12345"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/reset-password", strings.NewReader(body))
	req.Header.Set("X-Forwarded-For", newIP())
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestResetPasswordHandler_RejectsShortPassword(t *testing.T) {
	handler := ResetPasswordHandler(nil)
	body := `{"token":"abc","password":"curta","confirm_password":"curta"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/reset-password", strings.NewReader(body))
	req.Header.Set("X-Forwarded-For", newIP())
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestResetPasswordHandler_InvalidToken_Returns404(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT user_id, expires_at FROM verification_tokens")).
		WithArgs("token-invalido").
		WillReturnError(sql.ErrNoRows)

	handler := ResetPasswordHandler(db)
	body := `{"token":"token-invalido","password":"senha12345","confirm_password":"senha12345"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/reset-password", strings.NewReader(body))
	req.Header.Set("X-Forwarded-For", newIP())
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("esperado 404, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas sqlmock não satisfeitas: %v", err)
	}
}

func TestResetPasswordHandler_ExpiredToken_Returns410(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT user_id, expires_at FROM verification_tokens")).
		WithArgs("token-expirado").
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "expires_at"}).AddRow("u1", time.Now().Add(-1*time.Hour)))

	handler := ResetPasswordHandler(db)
	body := `{"token":"token-expirado","password":"senha12345","confirm_password":"senha12345"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/reset-password", strings.NewReader(body))
	req.Header.Set("X-Forwarded-For", newIP())
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusGone {
		t.Fatalf("esperado 410, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas sqlmock não satisfeitas: %v", err)
	}
}

func TestResetPasswordHandler_ValidToken_Succeeds(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT user_id, expires_at FROM verification_tokens")).
		WithArgs("token-valido").
		WillReturnRows(sqlmock.NewRows([]string{"user_id", "expires_at"}).AddRow("u1", time.Now().Add(1*time.Hour)))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE usuarios SET password_hash = $1, updated_at = now() WHERE id = $2")).
		WithArgs(sqlmock.AnyArg(), "u1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE verification_tokens SET used = true WHERE token = $1")).
		WithArgs("token-valido").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := ResetPasswordHandler(db)
	body := `{"token":"token-valido","password":"senha12345","confirm_password":"senha12345"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/reset-password", strings.NewReader(body))
	req.Header.Set("X-Forwarded-For", newIP())
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas sqlmock não satisfeitas: %v", err)
	}
}

// --- ChangePasswordHandler ---

func newChangePasswordRequest(userID, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/auth/change-password", strings.NewReader(body))
	claims := jwt.MapClaims{"user_id": userID, "perfil": "solicitante"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	return req.WithContext(ctx)
}

func TestChangePasswordHandler_RejectsWrongMethod(t *testing.T) {
	handler := ChangePasswordHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/change-password", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("esperado 405, obtido %d", rec.Code)
	}
}

func TestChangePasswordHandler_RejectsShortNewPassword(t *testing.T) {
	handler := ChangePasswordHandler(nil)
	body := `{"current_password":"qualquer","new_password":"curta"}`
	rec := httptest.NewRecorder()
	handler(rec, newChangePasswordRequest("user-rejects-short", body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestChangePasswordHandler_WrongCurrentPassword_Returns401(t *testing.T) {
	hash, err := HashPassword("senha-atual-123")
	if err != nil {
		t.Fatalf("HashPassword falhou: %v", err)
	}
	db, mock := newSQLMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT password_hash FROM usuarios WHERE id = $1")).
		WithArgs("user-wrong-current").
		WillReturnRows(sqlmock.NewRows([]string{"password_hash"}).AddRow(hash))

	handler := ChangePasswordHandler(db)
	body := `{"current_password":"senha-errada","new_password":"nova-senha-123"}`
	rec := httptest.NewRecorder()
	handler(rec, newChangePasswordRequest("user-wrong-current", body))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("esperado 401, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas sqlmock não satisfeitas: %v", err)
	}
}

func TestChangePasswordHandler_Succeeds(t *testing.T) {
	hash, err := HashPassword("senha-atual-123")
	if err != nil {
		t.Fatalf("HashPassword falhou: %v", err)
	}
	db, mock := newSQLMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT password_hash FROM usuarios WHERE id = $1")).
		WithArgs("user-succeeds").
		WillReturnRows(sqlmock.NewRows([]string{"password_hash"}).AddRow(hash))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE usuarios SET password_hash = $1, updated_at = now() WHERE id = $2")).
		WithArgs(sqlmock.AnyArg(), "user-succeeds").
		WillReturnResult(sqlmock.NewResult(0, 1))

	handler := ChangePasswordHandler(db)
	body := `{"current_password":"senha-atual-123","new_password":"nova-senha-123"}`
	rec := httptest.NewRecorder()
	handler(rec, newChangePasswordRequest("user-succeeds", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas sqlmock não satisfeitas: %v", err)
	}
}
