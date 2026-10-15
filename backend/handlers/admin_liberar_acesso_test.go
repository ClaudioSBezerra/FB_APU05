package handlers

// admin_liberar_acesso_test.go — cobre AdminLiberarAcessoHandler até o ponto
// onde o envio de e-mail de verdade começaria (SendPasswordResetEmail exige
// SMTP real, fora do escopo de teste unitário — mesmo limite já aceito por
// auth_password_reset_test.go, que também nunca cobre o caminho feliz de
// ForgotPasswordHandler por esse motivo).

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

func newLiberarAcessoRequest(id string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/admin/usuarios/"+id+"/liberar-acesso", nil)
	req.SetPathValue("id", id)
	return req
}

func TestAdminLiberarAcessoHandler_RejectsWrongMethod(t *testing.T) {
	handler := AdminLiberarAcessoHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/usuarios/"+testAlvoID+"/liberar-acesso", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("esperado 405, obtido %d", rec.Code)
	}
}

func TestAdminLiberarAcessoHandler_RejectsInvalidID(t *testing.T) {
	handler := AdminLiberarAcessoHandler(nil)
	rec := httptest.NewRecorder()
	handler(rec, newLiberarAcessoRequest("nao-e-uuid"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestAdminLiberarAcessoHandler_UsuarioNaoEncontrado(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT email FROM usuarios WHERE id = $1")).
		WithArgs(testAlvoID).
		WillReturnError(sql.ErrNoRows)

	handler := AdminLiberarAcessoHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newLiberarAcessoRequest(testAlvoID))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("esperado 404, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}
