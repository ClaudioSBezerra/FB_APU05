package handlers

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func sqlmockRowsListaUsuarios() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "email", "nome", "perfil", "ativo", "created_at", "cod_empresa", "numcad", "centro_custo_id", "centro_custo_nome", "aprovador"}).
		AddRow(testAlvoID, "claudio.bezerra@ferreiracosta.com.br", "Claudio Bezerra", "administrador", true, "2026-01-01T00:00:00Z", nil, nil, nil, nil, false)
}

func TestListarUsuariosHandler_RejectsWrongMethod(t *testing.T) {
	handler := ListarUsuariosHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/usuarios", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("esperado 405, obtido %d", rec.Code)
	}
}

func TestListarUsuariosHandler_Succeeds(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT u.id, u.email, u.nome, u.perfil, u.ativo, u.created_at, u.cod_empresa, u.numcad")).
		WithArgs(20, 0).
		WillReturnRows(sqlmockRowsListaUsuarios())

	handler := ListarUsuariosHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/usuarios", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"pagina":1`) || !strings.Contains(rec.Body.String(), `"tamanho":20`) {
		t.Fatalf("resposta não contém paginação esperada: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "claudio.bezerra@ferreiracosta.com.br") {
		t.Fatalf("resposta não contém o usuário esperado: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas sqlmock não satisfeitas: %v", err)
	}
}
