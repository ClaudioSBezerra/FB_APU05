package handlers

// fila_test.go — cobre a I/O Matrix da Story 4.1 no nível do handler HTTP:
// assumir com sucesso (200), conflito de versão (409 conflito_versao),
// solicitação inexistente (404) e a listagem da fila decorada com
// prazo_limite/atrasada via internal/sla. Mesmo padrão httptest+sqlmock de
// solicitacoes_test.go/admin_test.go.

import (
	"context"
	"database/sql"
	"encoding/json"
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

const (
	testFilaHandlerSolicitacaoID   = "33333333-3333-3333-3333-333333333333"
	testFilaHandlerAdministradorID = "44444444-4444-4444-4444-444444444444"
)

// newAssumirRequest monta POST /api/solicitacoes/{id}/assumir com claims de
// administrador já injetadas — mesmo padrão de newAdminRequest.
func newAssumirRequest(id, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/solicitacoes/"+id+"/assumir", strings.NewReader(body))
	req.SetPathValue("id", id)
	claims := jwt.MapClaims{"user_id": testFilaHandlerAdministradorID, "perfil": "administrador"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	return req.WithContext(ctx)
}

// newFilaListRequest monta GET /api/fila/solicitacoes com claims de
// administrador.
func newFilaListRequest(query string) *http.Request {
	url := "/api/fila/solicitacoes"
	if query != "" {
		url += "?" + query
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	claims := jwt.MapClaims{"user_id": testFilaHandlerAdministradorID, "perfil": "administrador"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	return req.WithContext(ctx)
}

// TestAssumirSolicitacaoHandler_Sucesso cobre "Assumir solicitação livre":
// 200, status=em_atendimento, administrador_id=<admin do contexto>,
// versao=2. Confirma também que administrador_id enviado é o do
// GetUserIDFromContext, nunca um valor do corpo (Boundaries "Always" da
// spec) — o corpo só manda `versao`.
func TestAssumirSolicitacaoHandler_Sucesso(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs(testFilaHandlerAdministradorID, testFilaHandlerSolicitacaoID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tipo_solicitacao", "status", "versao", "administrador_id"}).
			AddRow(testFilaHandlerSolicitacaoID, "transferencia", "em_atendimento", 2, testFilaHandlerAdministradorID))

	handler := AssumirSolicitacaoHandler(db)
	req := newAssumirRequest(testFilaHandlerSolicitacaoID, `{"versao":1}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if resp["status"] != "em_atendimento" {
		t.Fatalf("status na resposta = %v, esperado em_atendimento", resp["status"])
	}
	if resp["administrador_id"] != testFilaHandlerAdministradorID {
		t.Fatalf("administrador_id na resposta = %v, esperado %s", resp["administrador_id"], testFilaHandlerAdministradorID)
	}
	if resp["versao"].(float64) != 2 {
		t.Fatalf("versao na resposta = %v, esperado 2", resp["versao"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestAssumirSolicitacaoHandler_ConflitoVersao cobre "Conflito de versão" e
// "Já em atendimento" da I/O Matrix: UPDATE não afeta nenhuma linha, SELECT
// de fallback encontra a solicitação com outra versão -> 409 envelope
// {"erro":{"codigo":"conflito_versao",...,"versao_atual":N}}.
func TestAssumirSolicitacaoHandler_ConflitoVersao(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs(testFilaHandlerAdministradorID, testFilaHandlerSolicitacaoID, 1).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT versao FROM solicitacoes WHERE id = $1")).
		WithArgs(testFilaHandlerSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"versao"}).AddRow(2))

	handler := AssumirSolicitacaoHandler(db)
	req := newAssumirRequest(testFilaHandlerSolicitacaoID, `{"versao":1}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, esperado 409 (body=%s)", rec.Code, rec.Body.String())
	}

	var resp struct {
		Erro struct {
			Codigo      string `json:"codigo"`
			VersaoAtual int    `json:"versao_atual"`
		} `json:"erro"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if resp.Erro.Codigo != "conflito_versao" {
		t.Fatalf("codigo = %q, esperado conflito_versao", resp.Erro.Codigo)
	}
	if resp.Erro.VersaoAtual != 2 {
		t.Fatalf("versao_atual = %d, esperado 2", resp.Erro.VersaoAtual)
	}
}

// TestAssumirSolicitacaoHandler_NaoEncontrada cobre "Solicitação
// inexistente" da I/O Matrix: 404 no envelope jsonErr já existente.
func TestAssumirSolicitacaoHandler_NaoEncontrada(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs(testFilaHandlerAdministradorID, testFilaHandlerSolicitacaoID, 1).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT versao FROM solicitacoes WHERE id = $1")).
		WithArgs(testFilaHandlerSolicitacaoID).
		WillReturnError(sql.ErrNoRows)

	handler := AssumirSolicitacaoHandler(db)
	req := newAssumirRequest(testFilaHandlerSolicitacaoID, `{"versao":1}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, esperado 404 (body=%s)", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if resp["error"] != "solicitação não encontrada" {
		t.Fatalf("error = %q, esperado 'solicitação não encontrada'", resp["error"])
	}
}

// TestAssumirSolicitacaoHandler_IDFormatoInvalido garante que um {id} que
// nem parece UUID recebe o MESMO 404 (nunca um 400 que sugira que o formato
// em si importa para o cliente) — e nunca chega a tocar o banco.
func TestAssumirSolicitacaoHandler_IDFormatoInvalido(t *testing.T) {
	db, _ := newSQLMock(t)

	handler := AssumirSolicitacaoHandler(db)
	req := newAssumirRequest("nao-é-um-uuid", `{"versao":1}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, esperado 404 (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestAssumirSolicitacaoHandler_VersaoAusente cobre o achado de revisão
// (2026-10-08): corpo sem `versao` (ou `versao:0`/negativa) decaía para
// Versao=0, o UPDATE condicional nunca casava nenhuma linha, e o cliente
// recebia 409 conflito_versao (mensagem de conflito genuíno) em vez de um
// 400 de validação claro sobre o corpo malformado — nunca deveria tocar o
// banco.
func TestAssumirSolicitacaoHandler_VersaoAusente(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := AssumirSolicitacaoHandler(db)
	req := newAssumirRequest(testFilaHandlerSolicitacaoID, `{}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "'versao'") {
		t.Fatalf("corpo não cita o campo 'versao': %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nada deveria tocar o banco): %v", err)
	}
}

// TestAssumirSolicitacaoHandler_VersaoZeroOuNegativa cobre o mesmo achado
// para `versao:0` e `versao:-1` explícitos no corpo.
func TestAssumirSolicitacaoHandler_VersaoZeroOuNegativa(t *testing.T) {
	for _, versao := range []int{0, -1} {
		db, mock := newSQLMock(t)

		handler := AssumirSolicitacaoHandler(db)
		req := newAssumirRequest(testFilaHandlerSolicitacaoID, fmt.Sprintf(`{"versao":%d}`, versao))
		rec := httptest.NewRecorder()
		handler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("versao=%d: status = %d, esperado 400 (body=%s)", versao, rec.Code, rec.Body.String())
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("versao=%d: expectativas não cumpridas (nada deveria tocar o banco): %v", versao, err)
		}
	}
}

// TestListarFilaHandler_DecoraComSLA cobre a listagem paginada: só
// `status='aberta'`, decorada com prazo_limite/atrasada vindos
// exclusivamente de internal/sla (nunca recalculado inline no handler) —
// uma linha criada há muito tempo (atrasada=true) e outra criada agora
// (atrasada=false).
func TestListarFilaHandler_DecoraComSLA(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT data FROM feriados")).
		WillReturnRows(sqlmock.NewRows([]string{"data"}))

	antiga := time.Now().Add(-15 * 24 * time.Hour)
	recente := time.Now().Add(-1 * time.Hour)

	mock.ExpectQuery(regexp.QuoteMeta("FROM solicitacoes")).
		WithArgs(20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tipo_solicitacao", "centro_custo_id", "status", "versao", "created_at"}).
			AddRow("aaaaaaaa-0000-0000-0000-000000000001", "transferencia", "cc00000-0000-0000-0000-000000000001", "aberta", 1, antiga).
			AddRow("bbbbbbbb-0000-0000-0000-000000000002", "obras", nil, "aberta", 1, recente))

	handler := ListarFilaHandler(db)
	req := newFilaListRequest("")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}

	var resp struct {
		Items   []map[string]interface{} `json:"items"`
		Pagina  int                      `json:"pagina"`
		Tamanho int                      `json:"tamanho"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("len(items) = %d, esperado 2", len(resp.Items))
	}
	if resp.Items[0]["atrasada"] != true {
		t.Fatalf("item antigo deveria estar atrasada=true: %+v", resp.Items[0])
	}
	if resp.Items[1]["atrasada"] != false {
		t.Fatalf("item recente deveria estar atrasada=false: %+v", resp.Items[1])
	}
	if resp.Items[1]["centro_custo_id"] != nil {
		t.Fatalf("centro_custo_id de obras deveria ser nil: %+v", resp.Items[1]["centro_custo_id"])
	}
	if resp.Items[0]["prazo_limite"] == nil {
		t.Fatalf("prazo_limite ausente no item: %+v", resp.Items[0])
	}
}
