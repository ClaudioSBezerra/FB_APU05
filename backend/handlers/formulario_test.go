package handlers

// formulario_test.go — cobre os 3 handlers de opções de formulário
// (formulario.go): agregação de cadastros + centros de custo autorizados
// (cc_proprio_id/cc_excecao), lista de autorizadores por CC/faixa e por
// teto de Obras, e lista de ordens de investimento por par local/subgrupo.
// Mesmo padrão httptest+sqlmock de painel_test.go.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
)

const testUsuarioFormularioID = "33333333-3333-3333-3333-333333333333"

func newFormularioRequest(method, url string) *http.Request {
	req := httptest.NewRequest(method, url, nil)
	claims := jwt.MapClaims{"user_id": testUsuarioFormularioID, "perfil": "solicitante"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	return req.WithContext(ctx)
}

func TestOpcoesFormularioHandler_Sucesso(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM centros_custo cc")).
		WithArgs(testUsuarioFormularioID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "codigo", "nome", "divisao_id"}).
			AddRow("cc-1", "1000", "CC Administrativo", "div-1"))

	mock.ExpectQuery(regexp.QuoteMeta("FROM divisoes")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "codigo", "nome"}).AddRow("div-1", "100", "Controladoria"))

	mock.ExpectQuery(regexp.QuoteMeta("plano = 'BIFC'")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "codigo", "nome"}).AddRow("conta-1", "3001", "Despesas Gerais"))

	mock.ExpectQuery(regexp.QuoteMeta("plano = 'SFC'")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "codigo", "nome"}))

	mock.ExpectQuery(regexp.QuoteMeta("FROM classes_imobilizado")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "codigo", "nome"}))

	mock.ExpectQuery(regexp.QuoteMeta("FROM locais_obra")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "codigo", "nome"}))

	mock.ExpectQuery(regexp.QuoteMeta("FROM subgrupos_despesa")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "codigo", "nome"}))

	handler := OpcoesFormularioHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newFormularioRequest(http.MethodGet, "/api/formulario/opcoes"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}

	var resp struct {
		CentrosCusto []centroCustoOpcao `json:"centros_custo"`
		Divisoes     []opcaoItem        `json:"divisoes"`
		ContasBifc   []opcaoItem        `json:"contas_bifc"`
		ContasSfc    []opcaoItem        `json:"contas_sfc"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if len(resp.CentrosCusto) != 1 || resp.CentrosCusto[0].Codigo != "1000" {
		t.Fatalf("centros_custo = %+v, inesperado", resp.CentrosCusto)
	}
	if len(resp.ContasBifc) != 1 || len(resp.ContasSfc) != 0 {
		t.Fatalf("contas BIFC/SFC não separadas corretamente: %+v / %+v", resp.ContasBifc, resp.ContasSfc)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestAutorizadoresHandler_TipoInclusao(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM autorizadores_formulario af")).
		WithArgs("1000", 500.0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "nome"}).AddRow("colab-1", "Fulano de Tal"))

	handler := AutorizadoresHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newFormularioRequest(http.MethodGet, "/api/formulario/autorizadores?tipo=inclusao&centro_custo_codigo=1000&valor=500"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []autorizadorOpcao `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Nome != "Fulano de Tal" {
		t.Fatalf("items = %+v, inesperado", resp.Items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestAutorizadoresHandler_IncluiAprovadorGlobal cobre o fallback de
// aprovador global (20/10, Gestão de Usuários): o UNION com
// `usuarios.aprovador = true` traz um colaborador mesmo sem cadastro em
// autorizadores_formulario para o CC/faixa consultado.
func TestAutorizadoresHandler_IncluiAprovadorGlobal(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM autorizadores_formulario af")).
		WithArgs("1000", 500.0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "nome"}).
			AddRow("colab-1", "Fulano de Tal").
			AddRow("colab-9", "Diretor Aprovador"))

	handler := AutorizadoresHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newFormularioRequest(http.MethodGet, "/api/formulario/autorizadores?tipo=inclusao&centro_custo_codigo=1000&valor=500"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []autorizadorOpcao `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("items = %+v, esperado 2 (nominal + aprovador global)", resp.Items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestAutorizadoresHandler_TipoInclusaoSemCentroCusto(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := AutorizadoresHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newFormularioRequest(http.MethodGet, "/api/formulario/autorizadores?tipo=inclusao&valor=500"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nada deveria tocar o banco): %v", err)
	}
}

func TestAutorizadoresHandler_TipoObras(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM aprovadores_obra ao")).
		WithArgs(15000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "nome"}).AddRow("colab-2", "Ciclana de Tal"))

	handler := AutorizadoresHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newFormularioRequest(http.MethodGet, "/api/formulario/autorizadores?tipo=obras&valor=15000"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestAutorizadoresHandler_ValorInvalido(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := AutorizadoresHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newFormularioRequest(http.MethodGet, "/api/formulario/autorizadores?tipo=obras&valor=-10"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nada deveria tocar o banco): %v", err)
	}
}

func TestObraOrdensHandler_Sucesso(t *testing.T) {
	db, mock := newSQLMock(t)

	localID := "44444444-4444-4444-4444-444444444444"
	subgrupoID := "55555555-5555-5555-5555-555555555555"

	mock.ExpectQuery(regexp.QuoteMeta("FROM obra_ordens")).
		WithArgs(localID, subgrupoID).
		WillReturnRows(sqlmock.NewRows([]string{"numero_ordem"}).AddRow("OI-001").AddRow("OI-002"))

	handler := ObraOrdensHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newFormularioRequest(http.MethodGet, "/api/formulario/obra-ordens?local_obra_id="+localID+"&subgrupo_despesa_id="+subgrupoID))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []string `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if len(resp.Items) != 2 || resp.Items[0] != "OI-001" {
		t.Fatalf("items = %+v, inesperado", resp.Items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestObraOrdensHandler_UUIDInvalido(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := ObraOrdensHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newFormularioRequest(http.MethodGet, "/api/formulario/obra-ordens?local_obra_id=abc&subgrupo_despesa_id=def"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nada deveria tocar o banco): %v", err)
	}
}
