package handlers

// usuarios_carga_test.go — cobre a I/O Matrix de CarregarUsuariosHandler:
// criação de usuário novo (senha gerada e descartada, nunca aparece na
// resposta, sempre 'solicitante'), atualização de usuário existente, linha
// com cod_empresa/numcad já usado por outro usuário (rejeitada sem abortar
// a carga) e cabeçalho CSV errado (400, nunca toca o banco). Sem coluna
// "perfil" — removida a pedido do negócio.

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

	csv := "email;nome;cod_empresa;numcad;centro_custo_codigo\n" +
		"novo@ferreiracosta.com.br;Fulano de Tal;1;12345;CC1\n"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM centros_custo WHERE UPPER(codigo) = UPPER($1)")).
		WithArgs("CC1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testCentroCustoID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("novo@ferreiracosta.com.br").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO usuarios (email, nome, perfil, password_hash, cod_empresa, numcad, cc_proprio_id)")).
		WithArgs("novo@ferreiracosta.com.br", "Fulano de Tal", sqlmock.AnyArg(), 1, 12345, testCentroCustoID).
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

// TestCarregarUsuariosHandler_CabecalhoMaiusculo cobre um incidente real:
// planilhas exportadas do Oracle/Excel costumam vir com cabeçalho em
// maiúsculas (EMAIL;NOME;...) — normalizarCabecalhoCSV deixa só essa 1ª
// linha em minúsculas antes de validar, sem exigir que a pessoa edite o
// arquivo à mão.
func TestCarregarUsuariosHandler_CabecalhoMaiusculo(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "EMAIL;NOME;COD_EMPRESA;NUMCAD;CENTRO_CUSTO_CODIGO\n" +
		"novo@ferreiracosta.com.br;Fulano de Tal;1;12345;\n"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("novo@ferreiracosta.com.br").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO usuarios (email, nome, perfil, password_hash, cod_empresa, numcad, cc_proprio_id)")).
		WithArgs("novo@ferreiracosta.com.br", "Fulano de Tal", sqlmock.AnyArg(), 1, 12345, nil).
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
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestCarregarUsuariosHandler_CentroCustoNaoEncontrado_NaoBloqueiaCriacao
// cobre um incidente real: subir a planilha de usuários ANTES de carregar o
// cadastro mestre de centros de custo (Story 2.1) fazia TODA linha falhar
// por "centro de custo não encontrado" — identidade (cod_empresa/numcad) é
// o que mais importa agora, então um CC não encontrado vira só um aviso;
// a linha continua, o usuário é criado com cc_proprio_id NULL.
func TestCarregarUsuariosHandler_CentroCustoNaoEncontrado_NaoBloqueiaCriacao(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "email;nome;cod_empresa;numcad;centro_custo_codigo\n" +
		"novo@ferreiracosta.com.br;Fulano de Tal;1;12345;6450\n"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM centros_custo WHERE UPPER(codigo) = UPPER($1)")).
		WithArgs("6450").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("novo@ferreiracosta.com.br").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO usuarios (email, nome, perfil, password_hash, cod_empresa, numcad, cc_proprio_id)")).
		WithArgs("novo@ferreiracosta.com.br", "Fulano de Tal", sqlmock.AnyArg(), 1, 12345, nil).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := CarregarUsuariosHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newUsuariosCargaRequest(csv))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"criados":1`) || !strings.Contains(rec.Body.String(), `"ignorados":0`) {
		t.Fatalf("corpo não indica 1 criado + 0 ignorados (CC não encontrado não deveria bloquear): %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "não encontrado") {
		t.Fatalf("corpo não traz o aviso de CC não encontrado: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestCarregarUsuariosHandler_CentroCustoNomeCriaAutomaticamente cobre o
// ajuste pedido depois do incidente dos centros de custo ausentes: com uma
// coluna extra "centro_custo_nome" (presente ou não — aqui está), um CC não
// encontrado é CRIADO na hora (preso à divisão placeholder "SEM-DIVISAO",
// também criada na hora) em vez de só virar aviso.
func TestCarregarUsuariosHandler_CentroCustoNomeCriaAutomaticamente(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "email;nome;cod_empresa;numcad;centro_custo_codigo;centro_custo_nome\n" +
		"novo@ferreiracosta.com.br;Fulano de Tal;1;12345;6450;E-commerce Corporativo\n"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM centros_custo WHERE UPPER(codigo) = UPPER($1)")).
		WithArgs("6450").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO divisoes")).
		WithArgs("SEM-DIVISAO").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("divisao-sem-divisao-id"))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO centros_custo")).
		WithArgs("6450", "E-commerce Corporativo", "divisao-sem-divisao-id").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testCentroCustoID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("novo@ferreiracosta.com.br").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO usuarios (email, nome, perfil, password_hash, cod_empresa, numcad, cc_proprio_id)")).
		WithArgs("novo@ferreiracosta.com.br", "Fulano de Tal", sqlmock.AnyArg(), 1, 12345, testCentroCustoID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := CarregarUsuariosHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newUsuariosCargaRequest(csv))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"criados":1`) || !strings.Contains(rec.Body.String(), `"ignorados":0`) {
		t.Fatalf("corpo não indica 1 criado + 0 ignorados: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "não foi possível") {
		t.Fatalf("não deveria ter avisado falha ao criar CC: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestCarregarUsuariosHandler_NomccuComoSinonimoDeCentroCustoNome cobre o
// arquivo real do negócio: a coluna chama "NOMCCU" (convenção Senior/Oracle
// da query deles), não "centro_custo_nome" — precisa ser aceita do mesmo
// jeito, sem pedir pra renomear a planilha.
func TestCarregarUsuariosHandler_NomccuComoSinonimoDeCentroCustoNome(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "EMAIL;NOME;COD_EMPRESA;NUMCAD;CENTRO_CUSTO_CODIGO;NOMCCU\n" +
		"novo@ferreiracosta.com.br;Fulano de Tal;1;12345;6450;E-commerce Corporativo\n"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM centros_custo WHERE UPPER(codigo) = UPPER($1)")).
		WithArgs("6450").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO divisoes")).
		WithArgs("SEM-DIVISAO").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("divisao-sem-divisao-id"))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO centros_custo")).
		WithArgs("6450", "E-commerce Corporativo", "divisao-sem-divisao-id").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testCentroCustoID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("novo@ferreiracosta.com.br").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO usuarios (email, nome, perfil, password_hash, cod_empresa, numcad, cc_proprio_id)")).
		WithArgs("novo@ferreiracosta.com.br", "Fulano de Tal", sqlmock.AnyArg(), 1, 12345, testCentroCustoID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := CarregarUsuariosHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newUsuariosCargaRequest(csv))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"criados":1`) || !strings.Contains(rec.Body.String(), `"ignorados":0`) {
		t.Fatalf("corpo não indica 1 criado + 0 ignorados: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestCarregarUsuariosHandler_IgnoraColunaPerfil cobre explicitamente que uma
// coluna "perfil" presente no arquivo (formato antigo, ou planilha que o
// usuário editou por conta própria) é ignorada pelo parser por nome — nunca
// quebra a leitura nem é usada para decidir o perfil (sempre 'solicitante'
// na criação).
func TestCarregarUsuariosHandler_IgnoraColunaPerfil(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "EMAIL;NOME;PERFIL;COD_EMPRESA;NUMCAD;CENTRO_CUSTO_CODIGO;NOMCCU\n" +
		"novo@ferreiracosta.com.br;Fulano de Tal;3345;1;12345;;\n"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("novo@ferreiracosta.com.br").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO usuarios (email, nome, perfil, password_hash, cod_empresa, numcad, cc_proprio_id)")).
		WithArgs("novo@ferreiracosta.com.br", "Fulano de Tal", sqlmock.AnyArg(), 1, 12345, nil).
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
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestCarregarUsuariosHandler_AtualizaUsuarioExistente(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "email;nome;cod_empresa;numcad;centro_custo_codigo\n" +
		"ja-existe@ferreiracosta.com.br;Fulano Atualizado;1;12345;\n"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("ja-existe@ferreiracosta.com.br").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testAlvoID))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE usuarios SET")).
		WithArgs("Fulano Atualizado", 1, 12345, nil, testAlvoID).
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

	csv := "email;nome;cod_empresa;numcad;centro_custo_codigo\n" +
		"sem-arroba;Fulano;1;12345;\n" +
		"valido@ferreiracosta.com.br;Ciclano;2;54321;\n"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("valido@ferreiracosta.com.br").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO usuarios (email, nome, perfil, password_hash, cod_empresa, numcad, cc_proprio_id)")).
		WithArgs("valido@ferreiracosta.com.br", "Ciclano", sqlmock.AnyArg(), 2, 54321, nil).
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

// TestCarregarUsuariosHandler_EmailRepetidoNoArquivo_MantemPrimeiraOcorrencia
// cobre o caso real de um sócio que atua em várias empresas, uma linha por
// empresa no mesmo arquivo: só a 1ª ocorrência do e-mail é gravada (INSERT),
// a 2ª é rejeitada sem nenhuma query extra — nunca um "last write wins"
// silencioso sobre a identidade já gravada.
func TestCarregarUsuariosHandler_EmailRepetidoNoArquivo_MantemPrimeiraOcorrencia(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "email;nome;cod_empresa;numcad;centro_custo_codigo\n" +
		"socio@ferreiracosta.com.br;Fulano Socio;1;99992;\n" +
		"socio@ferreiracosta.com.br;Fulano Socio;2;2;\n"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("socio@ferreiracosta.com.br").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO usuarios (email, nome, perfil, password_hash, cod_empresa, numcad, cc_proprio_id)")).
		WithArgs("socio@ferreiracosta.com.br", "Fulano Socio", sqlmock.AnyArg(), 1, 99992, nil).
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
	if !strings.Contains(rec.Body.String(), "e-mail repetido") {
		t.Fatalf("corpo não explica a rejeição por e-mail repetido: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (a 2ª linha não deveria tocar o banco): %v", err)
	}
}

func TestCarregarUsuariosHandler_CodEmpresaNumcadDuplicado(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "email;nome;cod_empresa;numcad;centro_custo_codigo\n" +
		"novo@ferreiracosta.com.br;Fulano;1;12345;\n"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("novo@ferreiracosta.com.br").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO usuarios (email, nome, perfil, password_hash, cod_empresa, numcad, cc_proprio_id)")).
		WithArgs("novo@ferreiracosta.com.br", "Fulano", sqlmock.AnyArg(), 1, 12345, nil).
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
