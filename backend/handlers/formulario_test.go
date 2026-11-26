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
	"strings"
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
		WillReturnRows(sqlmock.NewRows([]string{"id", "codigo", "nome", "faz_fl", "faz_externo"}).AddRow("div-1", "100", "Controladoria", true, true))

	mock.ExpectQuery(regexp.QuoteMeta("plano = '0050'")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "codigo", "nome"}).AddRow("conta-1", "3001", "Despesas Gerais"))

	mock.ExpectQuery(regexp.QuoteMeta("FROM classes_imobilizado")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "codigo", "nome"}))

	mock.ExpectQuery(regexp.QuoteMeta("FROM locais_obra")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "codigo", "nome", "tipo_estrutura"}))

	mock.ExpectQuery(regexp.QuoteMeta("FROM subgrupos_despesa")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "codigo", "nome", "familias"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, codigo, nome, divisao_id FROM centros_custo ORDER BY codigo")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "codigo", "nome", "divisao_id"}).
			AddRow("cc-destino", "CC900", "Destino qualquer", "dv-1"))

	handler := OpcoesFormularioHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newFormularioRequest(http.MethodGet, "/api/formulario/opcoes"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}

	var resp struct {
		CentrosCusto []centroCustoOpcao `json:"centros_custo"`
		Divisoes     []opcaoItem        `json:"divisoes"`
		Contas       []opcaoItem        `json:"contas"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if len(resp.CentrosCusto) != 1 || resp.CentrosCusto[0].Codigo != "1000" {
		t.Fatalf("centros_custo = %+v, inesperado", resp.CentrosCusto)
	}
	if len(resp.Contas) != 1 || resp.Contas[0].Codigo != "3001" {
		t.Fatalf("contas do plano de contas inesperadas: %+v", resp.Contas)
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

// TestFornecedoresHandler_Busca cobre o caso feliz: busca não vazia ->
// ILIKE contra cgc/razsoc/fantas, resultado limitado e ordenado por nome.
func TestFornecedoresHandler_Busca(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM fornecedores")).
		WithArgs("%fulano%", fornecedoresLimiteBusca).
		WillReturnRows(sqlmock.NewRows([]string{"id", "cgc", "razsoc", "fantas", "uf"}).
			AddRow("forn-1", "11222333000181", "Fulano Ltda", "Fulano", "SP"))

	handler := FornecedoresHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newFormularioRequest(http.MethodGet, "/api/formulario/fornecedores?busca=fulano"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []map[string]string `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0]["razsoc"] != "Fulano Ltda" {
		t.Fatalf("items = %+v, inesperado", resp.Items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestFornecedoresHandler_SemBusca garante que, sem `busca`, a rota nunca
// toca o banco (nunca devolve a tabela inteira — pensada para autocomplete).
func TestFornecedoresHandler_SemBusca(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := FornecedoresHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newFormularioRequest(http.MethodGet, "/api/formulario/fornecedores"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"items":[]`) {
		t.Fatalf("esperado items vazio sem busca: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nada deveria tocar o banco): %v", err)
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

// F8 (26/11): Anexo Central F1.1 tem 602484 (externa) e 602492 (FL); a classificação escolhe UMA, preenchida.
func TestObraOrdensHandler_ResolvePeloGrao(t *testing.T) {
	localID := "44444444-4444-4444-4444-444444444444"
	subgrupoID := "55555555-5555-5555-5555-555555555555"
	casos := []struct{ classificacao, despesa, ordem string }{
		{"inclusao", "aquisicao_externa", "602484"},
		{"fl", "aquisicao_interna", "602492"},
	}
	for _, c := range casos {
		db, mock := newSQLMock(t)
		mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(tipo_estrutura, '') FROM locais_obra")).WithArgs(localID).
			WillReturnRows(sqlmock.NewRows([]string{"f"}).AddRow("Anexo Central"))
		mock.ExpectQuery(regexp.QuoteMeta("FROM subgrupos_despesa WHERE id = $1")).WithArgs(subgrupoID).
			WillReturnRows(sqlmock.NewRows([]string{"nome", "familias"}).AddRow("Fundações", "{Anexo Central,Antiga}"))
		mock.ExpectQuery(regexp.QuoteMeta("FROM obra_ordens WHERE local_obra_id = $1")).WithArgs(localID, subgrupoID, c.despesa).
			WillReturnRows(sqlmock.NewRows([]string{"o", "d", "f", "i", "n"}).AddRow(c.ordem, c.despesa, "aquisicao_externa", "900000001", "91F1"))
		rec := httptest.NewRecorder()
		ObraOrdensHandler(db)(rec, newFormularioRequest(http.MethodGet, "/api/formulario/obra-ordens?local_obra_id="+localID+"&subgrupo_despesa_id="+subgrupoID+"&classificacao="+c.classificacao))
		var resp struct {
			Situacao, Ordem, Dica string
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if rec.Code != 200 || resp.Situacao != "existe" || resp.Ordem != c.ordem || !strings.Contains(resp.Dica, "imobilizado 900000001") {
			t.Fatalf("%s: %d %s", c.classificacao, rec.Code, rec.Body.String())
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
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
