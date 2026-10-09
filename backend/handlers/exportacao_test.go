package handlers

// exportacao_test.go — cobre a I/O Matrix da Story 4.3 no nível do
// handler HTTP: validação estrutural do corpo (400) e a tradução dos
// sentinelas de internal/exportacao nos códigos HTTP certos, além dos
// dois envelopes de sucesso (lote gerado vs. aviso de verba duplicada).
// Mesmo padrão httptest+sqlmock de handlers/pendencia_test.go — a lógica
// de negócio em si já é coberta exaustivamente por
// internal/exportacao/vba_test.go; aqui o foco é o envelope HTTP.

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
)

const (
	testExportAdministradorID = "66666666-6666-6666-6666-666666666666"
	testExportOutroAdminID    = "77777777-7777-7777-7777-777777777777"
	testExportSolicitacaoID   = "88888888-8888-8888-8888-888888888888"
	testExportChave           = "chave-handler-1"
	testExportLoteID          = "99999999-9999-9999-9999-999999999999"
)

// newGerarLoteDespesaRequest monta POST /api/solicitacoes/lote-despesa com
// claims de administrador já injetadas — mesmo padrão de newAssumirRequest
// (handlers/fila_test.go).
func newGerarLoteDespesaRequest(corpo string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/solicitacoes/lote-despesa", strings.NewReader(corpo))
	claims := jwt.MapClaims{"user_id": testExportAdministradorID, "perfil": "administrador"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	return req.WithContext(ctx)
}

// escreverTemplateXLSMDeTeste grava um .xlsm sintético mínimo (aba
// "SAP Export" localizável via workbook.xml/.rels, igual ao exigido por
// internal/exportacao.montarXLSM) num arquivo temporário e aponta
// EXPORT_TEMPLATE_LOTE_DESPESA para ele — usado pelos testes do caminho de
// SUCESSO (template presente).
func escreverTemplateXLSMDeTeste(t *testing.T) {
	t.Helper()
	caminho := filepath.Join(t.TempDir(), "template.xlsm")

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	escrever := func(nome, conteudo string) {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: nome, Method: zip.Deflate})
		if err != nil {
			t.Fatalf("erro ao criar %s no template sintético: %v", nome, err)
		}
		if _, err := w.Write([]byte(conteudo)); err != nil {
			t.Fatalf("erro ao escrever %s no template sintético: %v", nome, err)
		}
	}
	escrever("xl/workbook.xml", `<workbook><sheets><sheet name="SAP Export" sheetId="1" r:id="rId1"/></sheets></workbook>`)
	escrever("xl/_rels/workbook.xml.rels", `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`)
	escrever("xl/worksheets/sheet1.xml", `<worksheet><sheetData></sheetData></worksheet>`)
	escrever("xl/vbaProject.bin", "vba-bytes")
	if err := zw.Close(); err != nil {
		t.Fatalf("erro ao finalizar template sintético: %v", err)
	}

	if err := os.WriteFile(caminho, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("erro ao gravar template sintético: %v", err)
	}
	t.Setenv("EXPORT_TEMPLATE_LOTE_DESPESA", caminho)
}

// --- Validação estrutural do corpo (400) ---

func TestGerarLoteDespesaHandler_SolicitacaoIDsVazio(t *testing.T) {
	db, _ := newSQLMock(t)
	rec := httptest.NewRecorder()
	GerarLoteDespesaHandler(db)(rec, newGerarLoteDespesaRequest(`{"solicitacao_ids":[],"chave_idempotencia":"x"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestGerarLoteDespesaHandler_ChaveIdempotenciaVazia(t *testing.T) {
	db, _ := newSQLMock(t)
	rec := httptest.NewRecorder()
	corpo := `{"solicitacao_ids":["` + testExportSolicitacaoID + `"],"chave_idempotencia":"  "}`
	GerarLoteDespesaHandler(db)(rec, newGerarLoteDespesaRequest(corpo))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestGerarLoteDespesaHandler_IDFormatoInvalido(t *testing.T) {
	db, _ := newSQLMock(t)
	rec := httptest.NewRecorder()
	corpo := `{"solicitacao_ids":["nao-e-um-uuid"],"chave_idempotencia":"x"}`
	GerarLoteDespesaHandler(db)(rec, newGerarLoteDespesaRequest(corpo))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

// --- Tradução de sentinelas ---

func TestGerarLoteDespesaHandler_NaoEncontrada(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT solicitacao_id FROM exportacoes_sap WHERE administrador_id = $1 AND chave_idempotencia = $2")).
		WithArgs(testExportAdministradorID, testExportChave).
		WillReturnRows(sqlmock.NewRows([]string{"solicitacao_id"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, s.status, s.administrador_id, u.nome")).
		WithArgs(testExportSolicitacaoID).
		WillReturnError(sql.ErrNoRows)

	rec := httptest.NewRecorder()
	corpo := `{"solicitacao_ids":["` + testExportSolicitacaoID + `"],"chave_idempotencia":"` + testExportChave + `"}`
	GerarLoteDespesaHandler(db)(rec, newGerarLoteDespesaRequest(corpo))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, esperado 404 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestGerarLoteDespesaHandler_OutroAdministrador(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT solicitacao_id FROM exportacoes_sap WHERE administrador_id = $1 AND chave_idempotencia = $2")).
		WithArgs(testExportAdministradorID, testExportChave).
		WillReturnRows(sqlmock.NewRows([]string{"solicitacao_id"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, s.status, s.administrador_id, u.nome")).
		WithArgs(testExportSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "status", "administrador_id", "nome"}).
			AddRow("transferencia", "em_atendimento", testExportOutroAdminID, "Fulano"))

	rec := httptest.NewRecorder()
	corpo := `{"solicitacao_ids":["` + testExportSolicitacaoID + `"],"chave_idempotencia":"` + testExportChave + `"}`
	GerarLoteDespesaHandler(db)(rec, newGerarLoteDespesaRequest(corpo))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestGerarLoteDespesaHandler_TipoObras(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT solicitacao_id FROM exportacoes_sap WHERE administrador_id = $1 AND chave_idempotencia = $2")).
		WithArgs(testExportAdministradorID, testExportChave).
		WillReturnRows(sqlmock.NewRows([]string{"solicitacao_id"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, s.status, s.administrador_id, u.nome")).
		WithArgs(testExportSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "status", "administrador_id", "nome"}).
			AddRow("obras", "em_atendimento", testExportAdministradorID, "Fulano"))

	rec := httptest.NewRecorder()
	corpo := `{"solicitacao_ids":["` + testExportSolicitacaoID + `"],"chave_idempotencia":"` + testExportChave + `"}`
	GerarLoteDespesaHandler(db)(rec, newGerarLoteDespesaRequest(corpo))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestGerarLoteDespesaHandler_TemplateAusente(t *testing.T) {
	db, mock := newSQLMock(t)

	t.Setenv("EXPORT_TEMPLATE_LOTE_DESPESA", filepath.Join(t.TempDir(), "nao-existe.xlsm"))

	mock.ExpectQuery(regexp.QuoteMeta("SELECT solicitacao_id FROM exportacoes_sap WHERE administrador_id = $1 AND chave_idempotencia = $2")).
		WithArgs(testExportAdministradorID, testExportChave).
		WillReturnRows(sqlmock.NewRows([]string{"solicitacao_id"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, s.status, s.administrador_id, u.nome")).
		WithArgs(testExportSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "status", "administrador_id", "nome"}).
			AddRow("transferencia", "em_atendimento", testExportAdministradorID, "Fulano"))
	mock.ExpectQuery(regexp.QuoteMeta("FROM solicitacao_lancamentos sl")).
		WithArgs(testExportSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"lado", "divisao_codigo", "cc_codigo", "conta_plano", "conta_codigo", "classe_codigo", "mes", "valor"}))
	mock.ExpectQuery(regexp.QuoteMeta("WHERE solicitacao_id = $1 AND status = 'GERADO' AND chave_idempotencia != $2")).
		WithArgs(testExportSolicitacaoID, testExportChave).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	rec := httptest.NewRecorder()
	corpo := `{"solicitacao_ids":["` + testExportSolicitacaoID + `"],"chave_idempotencia":"` + testExportChave + `"}`
	GerarLoteDespesaHandler(db)(rec, newGerarLoteDespesaRequest(corpo))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, esperado 503 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// --- Sucesso / aviso ---

func TestGerarLoteDespesaHandler_Sucesso(t *testing.T) {
	db, mock := newSQLMock(t)
	escreverTemplateXLSMDeTeste(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT solicitacao_id FROM exportacoes_sap WHERE administrador_id = $1 AND chave_idempotencia = $2")).
		WithArgs(testExportAdministradorID, testExportChave).
		WillReturnRows(sqlmock.NewRows([]string{"solicitacao_id"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, s.status, s.administrador_id, u.nome")).
		WithArgs(testExportSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "status", "administrador_id", "nome"}).
			AddRow("transferencia", "em_atendimento", testExportAdministradorID, "Fulano"))
	mock.ExpectQuery(regexp.QuoteMeta("FROM solicitacao_lancamentos sl")).
		WithArgs(testExportSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"lado", "divisao_codigo", "cc_codigo", "conta_plano", "conta_codigo", "classe_codigo", "mes", "valor"}))
	mock.ExpectQuery(regexp.QuoteMeta("WHERE solicitacao_id = $1 AND status = 'GERADO' AND chave_idempotencia != $2")).
		WithArgs(testExportSolicitacaoID, testExportChave).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT gen_random_uuid()")).
		WillReturnRows(sqlmock.NewRows([]string{"gen_random_uuid"}).AddRow(testExportLoteID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO exportacoes_sap (lote_id, solicitacao_id, administrador_id, chave_idempotencia)")).
		WithArgs(testExportLoteID, testExportSolicitacaoID, testExportAdministradorID, testExportChave).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT lote_id FROM exportacoes_sap WHERE administrador_id = $1 AND chave_idempotencia = $2 LIMIT 1")).
		WithArgs(testExportAdministradorID, testExportChave).
		WillReturnRows(sqlmock.NewRows([]string{"lote_id"}).AddRow(testExportLoteID))
	mock.ExpectCommit()

	rec := httptest.NewRecorder()
	corpo := `{"solicitacao_ids":["` + testExportSolicitacaoID + `"],"chave_idempotencia":"` + testExportChave + `"}`
	GerarLoteDespesaHandler(db)(rec, newGerarLoteDespesaRequest(corpo))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if resp["lote_id"] != testExportLoteID {
		t.Fatalf("lote_id = %v, esperado %q", resp["lote_id"], testExportLoteID)
	}
	if resp["arquivo_base64"] == nil || resp["arquivo_base64"] == "" {
		t.Fatalf("arquivo_base64 ausente/vazio: %+v", resp)
	}
	incluidas, ok := resp["solicitacoes_incluidas"].([]interface{})
	if !ok || len(incluidas) != 1 || incluidas[0] != testExportSolicitacaoID {
		t.Fatalf("solicitacoes_incluidas inesperado: %+v", resp["solicitacoes_incluidas"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// --- Lote Obra (Story 4.4) ---

const (
	testExportObraSolicitacaoID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	testExportObraLoteID        = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
)

// newGerarLoteObraRequest monta POST /api/solicitacoes/lote-obra com
// claims de administrador já injetadas — mesmo padrão de
// newGerarLoteDespesaRequest.
func newGerarLoteObraRequest(corpo string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/solicitacoes/lote-obra", strings.NewReader(corpo))
	claims := jwt.MapClaims{"user_id": testExportAdministradorID, "perfil": "administrador"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	return req.WithContext(ctx)
}

// escreverTemplateXLSMObraDeTeste é o análogo de escreverTemplateXLSMDeTeste
// para EXPORT_TEMPLATE_LOTE_OBRA.
func escreverTemplateXLSMObraDeTeste(t *testing.T) {
	t.Helper()
	caminho := filepath.Join(t.TempDir(), "template_obra.xlsm")

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	escrever := func(nome, conteudo string) {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: nome, Method: zip.Deflate})
		if err != nil {
			t.Fatalf("erro ao criar %s no template sintético: %v", nome, err)
		}
		if _, err := w.Write([]byte(conteudo)); err != nil {
			t.Fatalf("erro ao escrever %s no template sintético: %v", nome, err)
		}
	}
	escrever("xl/workbook.xml", `<workbook><sheets><sheet name="SAP Export" sheetId="1" r:id="rId1"/></sheets></workbook>`)
	escrever("xl/_rels/workbook.xml.rels", `<Relationships><Relationship Id="rId1" Target="worksheets/sheet1.xml"/></Relationships>`)
	escrever("xl/worksheets/sheet1.xml", `<worksheet><sheetData></sheetData></worksheet>`)
	escrever("xl/vbaProject.bin", "vba-bytes")
	if err := zw.Close(); err != nil {
		t.Fatalf("erro ao finalizar template sintético: %v", err)
	}

	if err := os.WriteFile(caminho, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("erro ao gravar template sintético: %v", err)
	}
	t.Setenv("EXPORT_TEMPLATE_LOTE_OBRA", caminho)
}

func TestGerarLoteObraHandler_SolicitacaoIDsVazio(t *testing.T) {
	db, _ := newSQLMock(t)
	rec := httptest.NewRecorder()
	GerarLoteObraHandler(db)(rec, newGerarLoteObraRequest(`{"solicitacao_ids":[],"chave_idempotencia":"x"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestGerarLoteObraHandler_IDFormatoInvalido(t *testing.T) {
	db, _ := newSQLMock(t)
	rec := httptest.NewRecorder()
	corpo := `{"solicitacao_ids":["nao-e-um-uuid"],"chave_idempotencia":"x"}`
	GerarLoteObraHandler(db)(rec, newGerarLoteObraRequest(corpo))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestGerarLoteObraHandler_NaoEncontrada(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, s.status, s.administrador_id, s.classificacao, u.nome")).
		WithArgs(testExportObraSolicitacaoID).
		WillReturnError(sql.ErrNoRows)

	rec := httptest.NewRecorder()
	corpo := `{"solicitacao_ids":["` + testExportObraSolicitacaoID + `"],"chave_idempotencia":"` + testExportChave + `"}`
	GerarLoteObraHandler(db)(rec, newGerarLoteObraRequest(corpo))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, esperado 404 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestGerarLoteObraHandler_TipoDiferenteDeObras(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, s.status, s.administrador_id, s.classificacao, u.nome")).
		WithArgs(testExportObraSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "status", "administrador_id", "classificacao", "nome"}).
			AddRow("transferencia", "em_atendimento", testExportAdministradorID, nil, "Fulano"))

	rec := httptest.NewRecorder()
	corpo := `{"solicitacao_ids":["` + testExportObraSolicitacaoID + `"],"chave_idempotencia":"` + testExportChave + `"}`
	GerarLoteObraHandler(db)(rec, newGerarLoteObraRequest(corpo))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestGerarLoteObraHandler_TemplateAusente(t *testing.T) {
	db, mock := newSQLMock(t)

	t.Setenv("EXPORT_TEMPLATE_LOTE_OBRA", filepath.Join(t.TempDir(), "nao-existe.xlsm"))

	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, s.status, s.administrador_id, s.classificacao, u.nome")).
		WithArgs(testExportObraSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "status", "administrador_id", "classificacao", "nome"}).
			AddRow("obras", "em_atendimento", testExportAdministradorID, "inclusao", "Fulano"))
	mock.ExpectQuery(regexp.QuoteMeta("FROM solicitacao_obras_linhas sol")).
		WithArgs(testExportObraSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"lado", "local_obra_codigo", "subgrupo_codigo", "ordem_investimento", "valor"}).
			AddRow(nil, "LO1", "SG1", "9999", 100.0))

	rec := httptest.NewRecorder()
	corpo := `{"solicitacao_ids":["` + testExportObraSolicitacaoID + `"],"chave_idempotencia":"` + testExportChave + `"}`
	GerarLoteObraHandler(db)(rec, newGerarLoteObraRequest(corpo))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, esperado 503 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestGerarLoteObraHandler_Sucesso cobre o envelope 200 com lote_id/
// arquivo_base64/solicitacoes_incluidas/bloqueados:[] quando há ao menos 1
// solicitação incluída.
func TestGerarLoteObraHandler_Sucesso(t *testing.T) {
	db, mock := newSQLMock(t)
	escreverTemplateXLSMObraDeTeste(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, s.status, s.administrador_id, s.classificacao, u.nome")).
		WithArgs(testExportObraSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "status", "administrador_id", "classificacao", "nome"}).
			AddRow("obras", "em_atendimento", testExportAdministradorID, "inclusao", "Fulano"))
	mock.ExpectQuery(regexp.QuoteMeta("FROM solicitacao_obras_linhas sol")).
		WithArgs(testExportObraSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"lado", "local_obra_codigo", "subgrupo_codigo", "ordem_investimento", "valor"}).
			AddRow(nil, "LO1", "SG1", "9999", 100.0))

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT lote_id FROM exportacoes_sap WHERE administrador_id = $1 AND chave_idempotencia = $2 LIMIT 1")).
		WithArgs(testExportAdministradorID, testExportChave).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT gen_random_uuid()")).
		WillReturnRows(sqlmock.NewRows([]string{"gen_random_uuid"}).AddRow(testExportObraLoteID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO exportacoes_sap (lote_id, solicitacao_id, administrador_id, chave_idempotencia)")).
		WithArgs(testExportObraLoteID, testExportObraSolicitacaoID, testExportAdministradorID, testExportChave).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT lote_id FROM exportacoes_sap WHERE administrador_id = $1 AND chave_idempotencia = $2 LIMIT 1")).
		WithArgs(testExportAdministradorID, testExportChave).
		WillReturnRows(sqlmock.NewRows([]string{"lote_id"}).AddRow(testExportObraLoteID))
	mock.ExpectCommit()

	rec := httptest.NewRecorder()
	corpo := `{"solicitacao_ids":["` + testExportObraSolicitacaoID + `"],"chave_idempotencia":"` + testExportChave + `"}`
	GerarLoteObraHandler(db)(rec, newGerarLoteObraRequest(corpo))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if resp["lote_id"] != testExportObraLoteID {
		t.Fatalf("lote_id = %v, esperado %q", resp["lote_id"], testExportObraLoteID)
	}
	if resp["arquivo_base64"] == nil || resp["arquivo_base64"] == "" {
		t.Fatalf("arquivo_base64 ausente/vazio: %+v", resp)
	}
	incluidas, ok := resp["solicitacoes_incluidas"].([]interface{})
	if !ok || len(incluidas) != 1 || incluidas[0] != testExportObraSolicitacaoID {
		t.Fatalf("solicitacoes_incluidas inesperado: %+v", resp["solicitacoes_incluidas"])
	}
	bloqueados, ok := resp["bloqueados"].([]interface{})
	if !ok || len(bloqueados) != 0 {
		t.Fatalf("bloqueados deveria ser [] : %+v", resp["bloqueados"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestGerarLoteObraHandler_TodoLoteBloqueado cobre "Todo o lote bloqueado"
// da I/O Matrix: resposta 200 sem lote_id/arquivo_base64, só
// solicitacoes_incluidas:[] e bloqueados preenchido.
func TestGerarLoteObraHandler_TodoLoteBloqueado(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, s.status, s.administrador_id, s.classificacao, u.nome")).
		WithArgs(testExportObraSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "status", "administrador_id", "classificacao", "nome"}).
			AddRow("obras", "em_atendimento", testExportAdministradorID, "inclusao", "Fulano"))
	mock.ExpectQuery(regexp.QuoteMeta("FROM solicitacao_obras_linhas sol")).
		WithArgs(testExportObraSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"lado", "local_obra_codigo", "subgrupo_codigo", "ordem_investimento", "valor"}).
			AddRow(nil, "LO1", "SG1", "CRIAR", 100.0))

	rec := httptest.NewRecorder()
	corpo := `{"solicitacao_ids":["` + testExportObraSolicitacaoID + `"],"chave_idempotencia":"` + testExportChave + `"}`
	GerarLoteObraHandler(db)(rec, newGerarLoteObraRequest(corpo))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if _, temLoteID := resp["lote_id"]; temLoteID {
		t.Fatalf("resposta não deveria ter lote_id quando tudo é bloqueado: %+v", resp)
	}
	if _, temArquivo := resp["arquivo_base64"]; temArquivo {
		t.Fatalf("resposta não deveria ter arquivo_base64 quando tudo é bloqueado: %+v", resp)
	}
	incluidas, ok := resp["solicitacoes_incluidas"].([]interface{})
	if !ok || len(incluidas) != 0 {
		t.Fatalf("solicitacoes_incluidas deveria ser [] : %+v", resp["solicitacoes_incluidas"])
	}
	bloqueados, ok := resp["bloqueados"].([]interface{})
	if !ok || len(bloqueados) != 1 {
		t.Fatalf("bloqueados inesperado: %+v", resp["bloqueados"])
	}
	bloqueio, ok := bloqueados[0].(map[string]interface{})
	if !ok || bloqueio["solicitacao_id"] != testExportObraSolicitacaoID {
		t.Fatalf("bloqueio inesperado: %+v", bloqueados[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestGerarLoteDespesaHandler_AvisoVerbaDuplicada(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT solicitacao_id FROM exportacoes_sap WHERE administrador_id = $1 AND chave_idempotencia = $2")).
		WithArgs(testExportAdministradorID, testExportChave).
		WillReturnRows(sqlmock.NewRows([]string{"solicitacao_id"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, s.status, s.administrador_id, u.nome")).
		WithArgs(testExportSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "status", "administrador_id", "nome"}).
			AddRow("transferencia", "em_atendimento", testExportAdministradorID, "Fulano"))
	mock.ExpectQuery(regexp.QuoteMeta("FROM solicitacao_lancamentos sl")).
		WithArgs(testExportSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"lado", "divisao_codigo", "cc_codigo", "conta_plano", "conta_codigo", "classe_codigo", "mes", "valor"}))
	mock.ExpectQuery(regexp.QuoteMeta("WHERE solicitacao_id = $1 AND status = 'GERADO' AND chave_idempotencia != $2")).
		WithArgs(testExportSolicitacaoID, testExportChave).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	rec := httptest.NewRecorder()
	corpo := `{"solicitacao_ids":["` + testExportSolicitacaoID + `"],"chave_idempotencia":"` + testExportChave + `"}`
	GerarLoteDespesaHandler(db)(rec, newGerarLoteDespesaRequest(corpo))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	aviso, ok := resp["aviso"].(map[string]interface{})
	if !ok {
		t.Fatalf("resposta sem 'aviso': %+v", resp)
	}
	if aviso["codigo"] != "verba_duplicada" {
		t.Fatalf("aviso.codigo = %v, esperado verba_duplicada", aviso["codigo"])
	}
	emRisco, ok := aviso["solicitacoes_em_risco"].([]interface{})
	if !ok || len(emRisco) != 1 || emRisco[0] != testExportSolicitacaoID {
		t.Fatalf("aviso.solicitacoes_em_risco inesperado: %+v", aviso["solicitacoes_em_risco"])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}
