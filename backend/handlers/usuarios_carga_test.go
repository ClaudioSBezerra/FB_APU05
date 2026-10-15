package handlers

// usuarios_carga_test.go — cobre a I/O Matrix de CarregarUsuariosHandler:
// criação de usuário novo (senha gerada e descartada, nunca aparece na
// resposta), atualização de usuário existente (perfil vazio preserva o
// atual), linha com cod_empresa/numcad já usado por outro usuário (rejeitada
// sem abortar a carga) e cabeçalho CSV errado (400, nunca toca o banco).

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
)

func newUsuariosCargaRequest(body string) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/api/admin/usuarios/carga", strings.NewReader(body))
}

func TestCarregarUsuariosHandler_CabecalhoErrado(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := CarregarUsuariosHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newUsuariosCargaRequest("coluna_errada\nvalor\n"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nada deveria tocar o banco): %v", err)
	}
}

func TestCarregarUsuariosHandler_CriaUsuarioNovo(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "email;nome;perfil;cod_empresa;numcad;centro_custo_codigo\n" +
		"novo@ferreiracosta.com.br;Fulano de Tal;;1;12345;CC1\n"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM centros_custo WHERE UPPER(codigo) = UPPER($1)")).
		WithArgs("CC1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testCentroCustoID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("novo@ferreiracosta.com.br").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO usuarios (email, nome, perfil, password_hash, cod_empresa, numcad, cc_proprio_id)")).
		WithArgs("novo@ferreiracosta.com.br", "Fulano de Tal", "solicitante", sqlmock.AnyArg(), 1, 12345, testCentroCustoID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := CarregarUsuariosHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newUsuariosCargaRequest(csv))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"criados":1`) {
		t.Fatalf("corpo não indica 1 criado: %s", rec.Body.String())
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "senha") {
		t.Fatalf("resposta nunca deveria mencionar senha: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestCarregarUsuariosHandler_AtualizaUsuarioExistente_PerfilVazioPreserva(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "email;nome;perfil;cod_empresa;numcad;centro_custo_codigo\n" +
		"ja-existe@ferreiracosta.com.br;Fulano Atualizado;;1;12345;\n"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("ja-existe@ferreiracosta.com.br").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testAlvoID))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE usuarios SET")).
		WithArgs("Fulano Atualizado", "", 1, 12345, nil, testAlvoID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := CarregarUsuariosHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newUsuariosCargaRequest(csv))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"atualizados":1`) {
		t.Fatalf("corpo não indica 1 atualizado: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestCarregarUsuariosHandler_LinhaInvalidaNaoAbortaCarga(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "email;nome;perfil;cod_empresa;numcad;centro_custo_codigo\n" +
		"sem-arroba;Fulano;;1;12345;\n" +
		"valido@ferreiracosta.com.br;Ciclano;;2;54321;\n"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("valido@ferreiracosta.com.br").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO usuarios (email, nome, perfil, password_hash, cod_empresa, numcad, cc_proprio_id)")).
		WithArgs("valido@ferreiracosta.com.br", "Ciclano", "solicitante", sqlmock.AnyArg(), 2, 54321, nil).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := CarregarUsuariosHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newUsuariosCargaRequest(csv))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"criados":1`) || !strings.Contains(rec.Body.String(), `"ignorados":1`) {
		t.Fatalf("corpo não indica 1 criado + 1 ignorado: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestCarregarUsuariosHandler_CodEmpresaNumcadDuplicado(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "email;nome;perfil;cod_empresa;numcad;centro_custo_codigo\n" +
		"novo@ferreiracosta.com.br;Fulano;;1;12345;\n"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("novo@ferreiracosta.com.br").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO usuarios (email, nome, perfil, password_hash, cod_empresa, numcad, cc_proprio_id)")).
		WithArgs("novo@ferreiracosta.com.br", "Fulano", "solicitante", sqlmock.AnyArg(), 1, 12345, nil).
		WillReturnError(&pq.Error{Code: "23505", Constraint: "idx_usuarios_cod_empresa_numcad"})
	mock.ExpectCommit()

	handler := CarregarUsuariosHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newUsuariosCargaRequest(csv))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200 (carga continua, só a linha é rejeitada), obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ignorados":1`) {
		t.Fatalf("corpo não indica 1 ignorado: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}
