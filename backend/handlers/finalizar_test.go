package handlers

// finalizar_test.go — cobre a I/O Matrix da Story 4.5 no nível do handler
// HTTP: validação estrutural do corpo (400) e a tradução dos sentinelas de
// internal/exportacao.Finalizar nos códigos HTTP certos. Mesmo padrão
// httptest+sqlmock de handlers/pendencia_test.go/handlers/exportacao_test.go
// — a lógica de negócio em si já é coberta exaustivamente por
// internal/exportacao/finalizar_test.go; aqui o foco é o envelope HTTP.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
	"github.com/lib/pq"
)

const (
	testFinalizarHandlerSolicitacaoID   = "11111111-1111-1111-1111-111111111111"
	testFinalizarHandlerAdministradorID = "22222222-2222-2222-2222-222222222222"
	testFinalizarHandlerOutroAdminID    = "33333333-3333-3333-3333-333333333333"
	testFinalizarHandlerLinhaCriarID    = "44444444-4444-4444-4444-444444444444"
	testFinalizarHandlerLocalObraID     = "55555555-5555-5555-5555-555555555555"
	testFinalizarHandlerSubgrupoID      = "66666666-6666-6666-6666-666666666666"
)

// newFinalizarRequest monta POST /api/solicitacoes/{id}/finalizar com
// claims de administrador já injetadas — mesmo padrão de
// newPendenciaRequest/newGerarLoteDespesaRequest.
func newFinalizarRequest(id, corpo string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/solicitacoes/"+id+"/finalizar", strings.NewReader(corpo))
	req.SetPathValue("id", id)
	claims := jwt.MapClaims{"user_id": testFinalizarHandlerAdministradorID, "perfil": "administrador"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	return req.WithContext(ctx)
}

// --- Sucesso ---

func TestFinalizarSolicitacaoHandler_SucessoSemLinhaCriar(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs("finalizada_sucesso", testFinalizarHandlerSolicitacaoID, 3, testFinalizarHandlerAdministradorID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tipo_solicitacao", "status", "versao", "administrador_id"}).
			AddRow(testFinalizarHandlerSolicitacaoID, "transferencia", "finalizada_sucesso", 4, testFinalizarHandlerAdministradorID))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE exportacoes_sap SET status = 'FINALIZADO'")).
		WithArgs(testFinalizarHandlerSolicitacaoID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, local_obra_id, subgrupo_despesa_id")).
		WithArgs(testFinalizarHandlerSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "local_obra_id", "subgrupo_despesa_id"}))
	mock.ExpectCommit()

	handler := FinalizarSolicitacaoHandler(db)
	req := newFinalizarRequest(testFinalizarHandlerSolicitacaoID, `{"versao":3,"resultado":"sucesso"}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if resp["status"] != "finalizada_sucesso" || resp["versao"].(float64) != 4 {
		t.Fatalf("resposta inesperada: %+v", resp)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestFinalizarSolicitacaoHandler_SucessoComLinhaCriar(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs("finalizada_sucesso", testFinalizarHandlerSolicitacaoID, 1, testFinalizarHandlerAdministradorID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tipo_solicitacao", "status", "versao", "administrador_id"}).
			AddRow(testFinalizarHandlerSolicitacaoID, "obras", "finalizada_sucesso", 2, testFinalizarHandlerAdministradorID))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE exportacoes_sap SET status = 'FINALIZADO'")).
		WithArgs(testFinalizarHandlerSolicitacaoID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, local_obra_id, subgrupo_despesa_id")).
		WithArgs(testFinalizarHandlerSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "local_obra_id", "subgrupo_despesa_id"}).
			AddRow(testFinalizarHandlerLinhaCriarID, testFinalizarHandlerLocalObraID, testFinalizarHandlerSubgrupoID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO obra_ordens")).
		WithArgs("SAP-0001", testFinalizarHandlerLocalObraID, testFinalizarHandlerSubgrupoID).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE solicitacao_obras_linhas SET ordem_investimento")).
		WithArgs("SAP-0001", testFinalizarHandlerLinhaCriarID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := FinalizarSolicitacaoHandler(db)
	corpo := `{"versao":1,"resultado":"sucesso","ordens_criadas":[{"linha_id":"` + testFinalizarHandlerLinhaCriarID + `","numero_ordem":" SAP-0001 "}]}`
	req := newFinalizarRequest(testFinalizarHandlerSolicitacaoID, corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// --- Validação estrutural (400, nunca toca o banco) ---

func TestFinalizarSolicitacaoHandler_VersaoAusente(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := FinalizarSolicitacaoHandler(db)
	req := newFinalizarRequest(testFinalizarHandlerSolicitacaoID, `{"resultado":"sucesso"}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nada deveria tocar o banco): %v", err)
	}
}

func TestFinalizarSolicitacaoHandler_ResultadoInvalido(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := FinalizarSolicitacaoHandler(db)
	req := newFinalizarRequest(testFinalizarHandlerSolicitacaoID, `{"versao":1,"resultado":"indefinido"}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nada deveria tocar o banco): %v", err)
	}
}

// TestFinalizarSolicitacaoHandler_OrdensCriadasComResultadoErro cobre
// "ordens_criadas com resultado=erro" da I/O Matrix -> 400 (Boundaries
// "Never" da spec: nunca criar ordem real quando resultado='erro').
func TestFinalizarSolicitacaoHandler_OrdensCriadasComResultadoErro(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := FinalizarSolicitacaoHandler(db)
	corpo := `{"versao":1,"resultado":"erro","ordens_criadas":[{"linha_id":"` + testFinalizarHandlerLinhaCriarID + `","numero_ordem":"SAP-0001"}]}`
	req := newFinalizarRequest(testFinalizarHandlerSolicitacaoID, corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nada deveria tocar o banco): %v", err)
	}
}

func TestFinalizarSolicitacaoHandler_LinhaIDFormatoInvalido(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := FinalizarSolicitacaoHandler(db)
	corpo := `{"versao":1,"resultado":"sucesso","ordens_criadas":[{"linha_id":"nao-e-uuid","numero_ordem":"SAP-0001"}]}`
	req := newFinalizarRequest(testFinalizarHandlerSolicitacaoID, corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nada deveria tocar o banco): %v", err)
	}
}

func TestFinalizarSolicitacaoHandler_NumeroOrdemVazio(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := FinalizarSolicitacaoHandler(db)
	corpo := `{"versao":1,"resultado":"sucesso","ordens_criadas":[{"linha_id":"` + testFinalizarHandlerLinhaCriarID + `","numero_ordem":"   "}]}`
	req := newFinalizarRequest(testFinalizarHandlerSolicitacaoID, corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nada deveria tocar o banco): %v", err)
	}
}

func TestFinalizarSolicitacaoHandler_LinhaIDDuplicado(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := FinalizarSolicitacaoHandler(db)
	corpo := `{"versao":1,"resultado":"sucesso","ordens_criadas":[` +
		`{"linha_id":"` + testFinalizarHandlerLinhaCriarID + `","numero_ordem":"SAP-0001"},` +
		`{"linha_id":"` + testFinalizarHandlerLinhaCriarID + `","numero_ordem":"SAP-0002"}` +
		`]}`
	req := newFinalizarRequest(testFinalizarHandlerSolicitacaoID, corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nada deveria tocar o banco): %v", err)
	}
}

func TestFinalizarSolicitacaoHandler_IDFormatoInvalido(t *testing.T) {
	db, _ := newSQLMock(t)

	handler := FinalizarSolicitacaoHandler(db)
	req := newFinalizarRequest("nao-e-uuid", `{"versao":1,"resultado":"sucesso"}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, esperado 404 (body=%s)", rec.Code, rec.Body.String())
	}
}

// --- Tradução de sentinelas ---

func TestFinalizarSolicitacaoHandler_NaoEncontrada(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs("finalizada_sucesso", testFinalizarHandlerSolicitacaoID, 1, testFinalizarHandlerAdministradorID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT administrador_id, status, versao FROM solicitacoes WHERE id = $1")).
		WithArgs(testFinalizarHandlerSolicitacaoID).
		WillReturnError(sql.ErrNoRows)

	handler := FinalizarSolicitacaoHandler(db)
	req := newFinalizarRequest(testFinalizarHandlerSolicitacaoID, `{"versao":1,"resultado":"sucesso"}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, esperado 404 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestFinalizarSolicitacaoHandler_DonoErrado(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs("finalizada_sucesso", testFinalizarHandlerSolicitacaoID, 1, testFinalizarHandlerAdministradorID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT administrador_id, status, versao FROM solicitacoes WHERE id = $1")).
		WithArgs(testFinalizarHandlerSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"administrador_id", "status", "versao"}).
			AddRow(testFinalizarHandlerOutroAdminID, "em_atendimento", 1))

	handler := FinalizarSolicitacaoHandler(db)
	req := newFinalizarRequest(testFinalizarHandlerSolicitacaoID, `{"versao":1,"resultado":"sucesso"}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestFinalizarSolicitacaoHandler_ConflitoVersao(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs("finalizada_sucesso", testFinalizarHandlerSolicitacaoID, 1, testFinalizarHandlerAdministradorID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT administrador_id, status, versao FROM solicitacoes WHERE id = $1")).
		WithArgs(testFinalizarHandlerSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"administrador_id", "status", "versao"}).
			AddRow(testFinalizarHandlerAdministradorID, "pendente", 5))

	handler := FinalizarSolicitacaoHandler(db)
	req := newFinalizarRequest(testFinalizarHandlerSolicitacaoID, `{"versao":1,"resultado":"sucesso"}`)
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
	if resp.Erro.Codigo != "conflito_versao" || resp.Erro.VersaoAtual != 5 {
		t.Fatalf("resposta inesperada: %+v", resp)
	}
}

func TestFinalizarSolicitacaoHandler_AindaNaoExportada(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs("finalizada_sucesso", testFinalizarHandlerSolicitacaoID, 1, testFinalizarHandlerAdministradorID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tipo_solicitacao", "status", "versao", "administrador_id"}).
			AddRow(testFinalizarHandlerSolicitacaoID, "transferencia", "finalizada_sucesso", 2, testFinalizarHandlerAdministradorID))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE exportacoes_sap SET status = 'FINALIZADO'")).
		WithArgs(testFinalizarHandlerSolicitacaoID).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	handler := FinalizarSolicitacaoHandler(db)
	req := newFinalizarRequest(testFinalizarHandlerSolicitacaoID, `{"versao":1,"resultado":"sucesso"}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, esperado 409 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestFinalizarSolicitacaoHandler_OrdensCriadasInvalidas(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs("finalizada_sucesso", testFinalizarHandlerSolicitacaoID, 1, testFinalizarHandlerAdministradorID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tipo_solicitacao", "status", "versao", "administrador_id"}).
			AddRow(testFinalizarHandlerSolicitacaoID, "obras", "finalizada_sucesso", 2, testFinalizarHandlerAdministradorID))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE exportacoes_sap SET status = 'FINALIZADO'")).
		WithArgs(testFinalizarHandlerSolicitacaoID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, local_obra_id, subgrupo_despesa_id")).
		WithArgs(testFinalizarHandlerSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "local_obra_id", "subgrupo_despesa_id"}))
	mock.ExpectRollback()

	handler := FinalizarSolicitacaoHandler(db)
	corpo := `{"versao":1,"resultado":"sucesso","ordens_criadas":[{"linha_id":"` + testFinalizarHandlerLinhaCriarID + `","numero_ordem":"SAP-0001"}]}`
	req := newFinalizarRequest(testFinalizarHandlerSolicitacaoID, corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestFinalizarSolicitacaoHandler_NumeroOrdemJaExiste(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs("finalizada_sucesso", testFinalizarHandlerSolicitacaoID, 1, testFinalizarHandlerAdministradorID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tipo_solicitacao", "status", "versao", "administrador_id"}).
			AddRow(testFinalizarHandlerSolicitacaoID, "obras", "finalizada_sucesso", 2, testFinalizarHandlerAdministradorID))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE exportacoes_sap SET status = 'FINALIZADO'")).
		WithArgs(testFinalizarHandlerSolicitacaoID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, local_obra_id, subgrupo_despesa_id")).
		WithArgs(testFinalizarHandlerSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "local_obra_id", "subgrupo_despesa_id"}).
			AddRow(testFinalizarHandlerLinhaCriarID, testFinalizarHandlerLocalObraID, testFinalizarHandlerSubgrupoID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO obra_ordens")).
		WithArgs("SAP-0001", testFinalizarHandlerLocalObraID, testFinalizarHandlerSubgrupoID).
		WillReturnError(&pq.Error{Code: "23505"})
	mock.ExpectRollback()

	handler := FinalizarSolicitacaoHandler(db)
	corpo := `{"versao":1,"resultado":"sucesso","ordens_criadas":[{"linha_id":"` + testFinalizarHandlerLinhaCriarID + `","numero_ordem":"SAP-0001"}]}`
	req := newFinalizarRequest(testFinalizarHandlerSolicitacaoID, corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, esperado 409 (body=%s)", rec.Code, rec.Body.String())
	}
}
