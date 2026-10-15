package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
	"github.com/lib/pq"
)

// newCreateUsuarioRequest monta um POST com claims de administrador no
// contexto, igual newAdminRequest (admin_test.go) injeta pra
// AdminUpdateUsuarioHandler.
func newCreateUsuarioRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/admin/usuarios", strings.NewReader(body))
	claims := jwt.MapClaims{"user_id": testAtorID, "perfil": "administrador"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	return req.WithContext(ctx)
}

// --- Validação de entrada (db=nil, nunca chega ao banco) ---

func TestCreateUsuarioLocalHandler_RejectsWrongMethod(t *testing.T) {
	handler := CreateUsuarioLocalHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/usuarios", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("esperado 405, obtido %d", rec.Code)
	}
}

func TestCreateUsuarioLocalHandler_RejectsInvalidBody(t *testing.T) {
	handler := CreateUsuarioLocalHandler(nil)
	rec := httptest.NewRecorder()
	handler(rec, newCreateUsuarioRequest("{not json"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d", rec.Code)
	}
}

func TestCreateUsuarioLocalHandler_RejectsInvalidEmail(t *testing.T) {
	handler := CreateUsuarioLocalHandler(nil)
	rec := httptest.NewRecorder()
	body := `{"email":"sem-arroba","nome":"Fulano","senha":"senha12345"}`
	handler(rec, newCreateUsuarioRequest(body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestCreateUsuarioLocalHandler_RejectsMissingNome(t *testing.T) {
	handler := CreateUsuarioLocalHandler(nil)
	rec := httptest.NewRecorder()
	body := `{"email":"a@b.com","nome":"","senha":"senha12345"}`
	handler(rec, newCreateUsuarioRequest(body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestCreateUsuarioLocalHandler_RejectsShortSenha(t *testing.T) {
	handler := CreateUsuarioLocalHandler(nil)
	rec := httptest.NewRecorder()
	body := `{"email":"a@b.com","nome":"Fulano","senha":"curta"}`
	handler(rec, newCreateUsuarioRequest(body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestCreateUsuarioLocalHandler_RejectsInvalidPerfil(t *testing.T) {
	handler := CreateUsuarioLocalHandler(nil)
	rec := httptest.NewRecorder()
	body := `{"email":"a@b.com","nome":"Fulano","senha":"senha12345","perfil":"super-admin","cod_empresa":1,"numcad":12345}`
	handler(rec, newCreateUsuarioRequest(body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestCreateUsuarioLocalHandler_RejectsMissingCodEmpresaOuNumcad(t *testing.T) {
	handler := CreateUsuarioLocalHandler(nil)
	rec := httptest.NewRecorder()
	body := `{"email":"a@b.com","nome":"Fulano","senha":"senha12345"}`
	handler(rec, newCreateUsuarioRequest(body))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
}

// --- Caminhos que chegam ao banco (sqlmock) ---

func TestCreateUsuarioLocalHandler_Succeeds(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO usuarios (email, nome, perfil, password_hash, cod_empresa, numcad)")).
		WithArgs("novo@fortesbezerra.com.br", "Fulano", "solicitante", sqlmock.AnyArg(), 1, 12345).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testAlvoID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO usuarios_auditoria")).
		WithArgs(testAtorID, testAlvoID, "solicitante").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := CreateUsuarioLocalHandler(db)
	rec := httptest.NewRecorder()
	body := `{"email":"novo@fortesbezerra.com.br","nome":"Fulano","senha":"senha12345","cod_empresa":1,"numcad":12345}`
	handler(rec, newCreateUsuarioRequest(body))

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), testAlvoID) {
		t.Fatalf("resposta não contém o id criado: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas sqlmock não satisfeitas: %v", err)
	}
}

func TestCreateUsuarioLocalHandler_DuplicateEmail_Returns409(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO usuarios (email, nome, perfil, password_hash, cod_empresa, numcad)")).
		WithArgs("ja-existe@fortesbezerra.com.br", "Fulano", "solicitante", sqlmock.AnyArg(), 1, 12345).
		WillReturnError(&pq.Error{Code: "23505"})
	mock.ExpectRollback()

	handler := CreateUsuarioLocalHandler(db)
	rec := httptest.NewRecorder()
	body := `{"email":"ja-existe@fortesbezerra.com.br","nome":"Fulano","senha":"senha12345","cod_empresa":1,"numcad":12345}`
	handler(rec, newCreateUsuarioRequest(body))

	if rec.Code != http.StatusConflict {
		t.Fatalf("esperado 409, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas sqlmock não satisfeitas: %v", err)
	}
}
