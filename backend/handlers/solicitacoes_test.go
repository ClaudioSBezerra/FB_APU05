package handlers

// solicitacoes_test.go — cobre a I/O Matrix da Story 3.1 (Abrir
// Transferência com aprovação calculada): tipo não suportado, lados
// desbalanceados, mais de um exercício, CC divergente entre linhas, CC não
// autorizado, conta do plano SFC, sem alçada cadastrada (422) e o envio
// feliz (201). Mesmo padrão httptest+sqlmock de cadastros_test.go/
// admin_test.go.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
)

const (
	testSolicitacaoCentroCustoID = "cc000000-0000-0000-0000-000000000001"
	testSolicitacaoDivisaoID     = "dd000000-0000-0000-0000-000000000002"
	testSolicitacaoContaOrigem   = "ca000000-0000-0000-0000-000000000001"
	testSolicitacaoContaDestino  = "ca000000-0000-0000-0000-000000000002"
	testSolicitacaoContaSFC      = "ca000000-0000-0000-0000-000000000003"
)

// newSolicitacaoRequest monta uma requisição POST com claims de solicitante
// (não administrador — Story 3.1 é a primeira rota sem exigir esse perfil)
// já injetadas no contexto, do jeito que RequireAuth(..., "") faria.
func newSolicitacaoRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/solicitacoes", strings.NewReader(body))
	claims := jwt.MapClaims{"user_id": testAtorID, "perfil": "solicitante"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	return req.WithContext(ctx)
}

func corpoTransferencia(lancamentosJSON string) string {
	return `{"tipo_solicitacao":"transferencia","lancamentos":[` + lancamentosJSON + `]}`
}

// corpoInclusaoSFC monta o corpo de uma Inclusão SFC (Story 3.2) — mesmo
// helper de linha (linhaJSON), outro tipo_solicitacao.
func corpoInclusaoSFC(lancamentosJSON string) string {
	return `{"tipo_solicitacao":"inclusao_sfc","lancamentos":[` + lancamentosJSON + `]}`
}

func linhaJSON(lado, divisaoID, centroCustoID, contaID, mes string, valor float64) string {
	return fmt.Sprintf(
		`{"lado":"%s","divisao_id":"%s","centro_custo_id":"%s","conta_id":"%s","mes":"%s","valor":%g}`,
		lado, divisaoID, centroCustoID, contaID, mes, valor,
	)
}

// --- validações estruturais (sem DB) ---

func TestAbrirSolicitacaoHandler_TipoNaoSuportado(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(`{"tipo_solicitacao":"inclusao","lancamentos":[]}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "tipo de solicitação não suportado ainda") {
		t.Fatalf("corpo não contém a mensagem esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

func TestAbrirSolicitacaoHandler_MenosDeDuasLinhas(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoTransferencia(
		linhaJSON("origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaOrigem, "2026-10-01", 1000),
	)

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "ao menos uma linha de origem e uma de destino") {
		t.Fatalf("corpo não cita a linha mínima esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

func TestAbrirSolicitacaoHandler_MesForaDaConvencaoDia1(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoTransferencia(
		linhaJSON("origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaOrigem, "2026-10-15", 1000) + "," +
			linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaDestino, "2026-10-01", 1000),
	)

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "dia 1 do mês de competência") {
		t.Fatalf("corpo não cita a convenção de dia 1: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

func TestAbrirSolicitacaoHandler_DivisaoDivergenteDoCC(t *testing.T) {
	db, mock := newSQLMock(t)

	const outraDivisaoID = "dd000000-0000-0000-0000-000000000099"
	corpo := corpoTransferencia(
		linhaJSON("origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaOrigem, "2026-10-01", 1000) + "," +
			linhaJSON("destino", outraDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaDestino, "2026-10-01", 1000),
	)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo, divisao_id, filial FROM centros_custo WHERE id = $1")).
		WithArgs(testSolicitacaoCentroCustoID).
		WillReturnRows(sqlmock.NewRows([]string{"codigo", "divisao_id", "filial"}).
			AddRow("CC1", testSolicitacaoDivisaoID, "F1"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT cc_proprio_id FROM usuarios WHERE id = $1")).
		WithArgs(testAtorID).
		WillReturnRows(sqlmock.NewRows([]string{"cc_proprio_id"}).AddRow(testSolicitacaoCentroCustoID))
	mock.ExpectRollback()

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "divisao_id") {
		t.Fatalf("corpo não cita a divergência de divisão: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

func TestAbrirSolicitacaoHandler_LadosDesbalanceados(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoTransferencia(
		linhaJSON("origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaOrigem, "2026-10-01", 1000) + "," +
			linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaDestino, "2026-10-01", 900),
	)

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "não batem") {
		t.Fatalf("corpo não cita o desbalanceamento: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

func TestAbrirSolicitacaoHandler_MaisDeUmExercicio(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoTransferencia(
		linhaJSON("origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaOrigem, "2026-10-01", 1000) + "," +
			linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaDestino, "2027-01-01", 1000),
	)

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "mais de um exercício orçamentário") {
		t.Fatalf("corpo não cita o erro esperado: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

func TestAbrirSolicitacaoHandler_CCDivergenteEntreLinhas(t *testing.T) {
	db, mock := newSQLMock(t)

	const outroCentroCustoID = "cc000000-0000-0000-0000-000000000099"
	corpo := corpoTransferencia(
		linhaJSON("origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaOrigem, "2026-10-01", 1000) + "," +
			linhaJSON("destino", testSolicitacaoDivisaoID, outroCentroCustoID, testSolicitacaoContaDestino, "2026-10-01", 1000),
	)

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "diverge") {
		t.Fatalf("corpo não cita a divergência de CC: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

// --- validações que dependem de DB (dentro da transação) ---

func TestAbrirSolicitacaoHandler_CCNaoAutorizado(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoTransferencia(
		linhaJSON("origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaOrigem, "2026-10-01", 1000) + "," +
			linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaDestino, "2026-10-01", 1000),
	)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo, divisao_id, filial FROM centros_custo WHERE id = $1")).
		WithArgs(testSolicitacaoCentroCustoID).
		WillReturnRows(sqlmock.NewRows([]string{"codigo", "divisao_id", "filial"}).
			AddRow("CC1", testSolicitacaoDivisaoID, "F1"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT cc_proprio_id FROM usuarios WHERE id = $1")).
		WithArgs(testAtorID).
		WillReturnRows(sqlmock.NewRows([]string{"cc_proprio_id"}).AddRow("cc000000-0000-0000-0000-000000000099"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM cc_excecao WHERE colaborador_id = $1 AND centro_custo_id = $2)")).
		WithArgs(testAtorID, testSolicitacaoCentroCustoID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("esperado 403, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

func TestAbrirSolicitacaoHandler_ContaPlanoSFC(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoTransferencia(
		linhaJSON("origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaOrigem, "2026-10-01", 1000) + "," +
			linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaDestino, "2026-10-01", 1000),
	)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo, divisao_id, filial FROM centros_custo WHERE id = $1")).
		WithArgs(testSolicitacaoCentroCustoID).
		WillReturnRows(sqlmock.NewRows([]string{"codigo", "divisao_id", "filial"}).
			AddRow("CC1", testSolicitacaoDivisaoID, "F1"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT cc_proprio_id FROM usuarios WHERE id = $1")).
		WithArgs(testAtorID).
		WillReturnRows(sqlmock.NewRows([]string{"cc_proprio_id"}).AddRow(testSolicitacaoCentroCustoID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT plano FROM contas WHERE id = $1")).
		WithArgs(testSolicitacaoContaOrigem).
		WillReturnRows(sqlmock.NewRows([]string{"plano"}).AddRow("SFC"))
	mock.ExpectRollback()

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "BIFC") {
		t.Fatalf("corpo não cita a restrição de plano: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

func TestAbrirSolicitacaoHandler_SemAlcadaCadastrada(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoTransferencia(
		linhaJSON("origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaOrigem, "2026-10-01", 50000) + "," +
			linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaDestino, "2026-10-01", 50000),
	)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo, divisao_id, filial FROM centros_custo WHERE id = $1")).
		WithArgs(testSolicitacaoCentroCustoID).
		WillReturnRows(sqlmock.NewRows([]string{"codigo", "divisao_id", "filial"}).
			AddRow("CC1", testSolicitacaoDivisaoID, "F1"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT cc_proprio_id FROM usuarios WHERE id = $1")).
		WithArgs(testAtorID).
		WillReturnRows(sqlmock.NewRows([]string{"cc_proprio_id"}).AddRow(testSolicitacaoCentroCustoID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT plano FROM contas WHERE id = $1")).
		WithArgs(testSolicitacaoContaOrigem).
		WillReturnRows(sqlmock.NewRows([]string{"plano"}).AddRow("BIFC"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT plano FROM contas WHERE id = $1")).
		WithArgs(testSolicitacaoContaDestino).
		WillReturnRows(sqlmock.NewRows([]string{"plano"}).AddRow("BIFC"))
	mock.ExpectQuery(regexp.QuoteMeta("FROM regras_aprovacao")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("FROM alcadas")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("centro_custo_id = $2")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("centro_custo_id IS NULL AND divisao_id = $2")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("centro_custo_id IS NULL AND divisao_id IS NULL")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}))
	mock.ExpectRollback()

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("esperado 422, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "sem alçada cadastrada") {
		t.Fatalf("corpo não cita a mensagem esperada: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "CC1") || !strings.Contains(rec.Body.String(), "F1") {
		t.Fatalf("corpo não cita o CC e a filial (I/O Matrix exige citar os dois): %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

func TestAbrirSolicitacaoHandler_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	const solicitacaoID = "so000000-0000-0000-0000-000000000001"
	const alcadaID = "al000000-0000-0000-0000-000000000001"

	corpo := corpoTransferencia(
		linhaJSON("origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaOrigem, "2026-10-01", 1000) + "," +
			linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaDestino, "2026-10-01", 1000),
	)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo, divisao_id, filial FROM centros_custo WHERE id = $1")).
		WithArgs(testSolicitacaoCentroCustoID).
		WillReturnRows(sqlmock.NewRows([]string{"codigo", "divisao_id", "filial"}).
			AddRow("CC1", testSolicitacaoDivisaoID, "F1"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT cc_proprio_id FROM usuarios WHERE id = $1")).
		WithArgs(testAtorID).
		WillReturnRows(sqlmock.NewRows([]string{"cc_proprio_id"}).AddRow(testSolicitacaoCentroCustoID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT plano FROM contas WHERE id = $1")).
		WithArgs(testSolicitacaoContaOrigem).
		WillReturnRows(sqlmock.NewRows([]string{"plano"}).AddRow("BIFC"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT plano FROM contas WHERE id = $1")).
		WithArgs(testSolicitacaoContaDestino).
		WillReturnRows(sqlmock.NewRows([]string{"plano"}).AddRow("BIFC"))
	mock.ExpectQuery(regexp.QuoteMeta("FROM regras_aprovacao")).
		WithArgs("F1", "CC1", 1000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("FROM alcadas")).
		WithArgs("F1", "CC1", 1000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "papel_aprovador"}).AddRow(alcadaID, "Gerente Financeiro"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 1) FROM cadastro_historico")).
		WithArgs("alcadas", alcadaID).
		WillReturnRows(sqlmock.NewRows([]string{"versao"}).AddRow(3))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO solicitacoes")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(solicitacaoID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO solicitacao_lancamentos")).
		WithArgs(solicitacaoID, "origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaOrigem, "2026-10-01", 1000.0).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO solicitacao_lancamentos")).
		WithArgs(solicitacaoID, "destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaDestino, "2026-10-01", 1000.0).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"Gerente Financeiro"`) {
		t.Fatalf("corpo não contém o aprovador esperado: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// --- Story 3.2 — Abrir Inclusão SFC com aprovação calculada ---
//
// Resolver calculado (as 5 etapas do encadeamento) já está coberto pelos
// testes de internal/aprovacao/calculado_test.go (Story 3.1) e é 100%
// reaproveitado por este tipo (Design Notes da spec 3.2) — os testes abaixo
// cobrem só o wiring novo deste handler: contagem/lado tipo-aware
// (validarContagemELadoPorTipo), validação de plano SFC (validarPlanoConta)
// e o caminho feliz/sem-alçada de ponta a ponta.

func TestAbrirSolicitacaoHandler_InclusaoSFC_MaisDeUmaLinha(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoInclusaoSFC(
		linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaSFC, "2026-10-01", 1000) + "," +
			linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaSFC, "2026-10-01", 500),
	)

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "apenas uma linha") {
		t.Fatalf("corpo não cita a restrição de contagem esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

func TestAbrirSolicitacaoHandler_InclusaoSFC_LadoOrigemInvalido(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoInclusaoSFC(
		linhaJSON("origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaSFC, "2026-10-01", 1000),
	)

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `lado=`) || !strings.Contains(rec.Body.String(), "destino") {
		t.Fatalf("corpo não cita a exigência de lado=destino: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

func TestAbrirSolicitacaoHandler_InclusaoSFC_ContaPlanoBIFC(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoInclusaoSFC(
		linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaSFC, "2026-10-01", 1000),
	)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo, divisao_id, filial FROM centros_custo WHERE id = $1")).
		WithArgs(testSolicitacaoCentroCustoID).
		WillReturnRows(sqlmock.NewRows([]string{"codigo", "divisao_id", "filial"}).
			AddRow("CC1", testSolicitacaoDivisaoID, "F1"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT cc_proprio_id FROM usuarios WHERE id = $1")).
		WithArgs(testAtorID).
		WillReturnRows(sqlmock.NewRows([]string{"cc_proprio_id"}).AddRow(testSolicitacaoCentroCustoID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT plano FROM contas WHERE id = $1")).
		WithArgs(testSolicitacaoContaSFC).
		WillReturnRows(sqlmock.NewRows([]string{"plano"}).AddRow("BIFC"))
	mock.ExpectRollback()

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "SFC") || !strings.Contains(rec.Body.String(), "Inclusão SFC") {
		t.Fatalf("corpo não cita a restrição de plano esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

func TestAbrirSolicitacaoHandler_InclusaoSFC_SemAlcadaCadastrada(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoInclusaoSFC(
		linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaSFC, "2026-10-01", 50000),
	)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo, divisao_id, filial FROM centros_custo WHERE id = $1")).
		WithArgs(testSolicitacaoCentroCustoID).
		WillReturnRows(sqlmock.NewRows([]string{"codigo", "divisao_id", "filial"}).
			AddRow("CC1", testSolicitacaoDivisaoID, "F1"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT cc_proprio_id FROM usuarios WHERE id = $1")).
		WithArgs(testAtorID).
		WillReturnRows(sqlmock.NewRows([]string{"cc_proprio_id"}).AddRow(testSolicitacaoCentroCustoID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT plano FROM contas WHERE id = $1")).
		WithArgs(testSolicitacaoContaSFC).
		WillReturnRows(sqlmock.NewRows([]string{"plano"}).AddRow("SFC"))
	mock.ExpectQuery(regexp.QuoteMeta("FROM regras_aprovacao")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("FROM alcadas")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("centro_custo_id = $2")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("centro_custo_id IS NULL AND divisao_id = $2")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("centro_custo_id IS NULL AND divisao_id IS NULL")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}))
	mock.ExpectRollback()

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("esperado 422, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "sem alçada cadastrada") {
		t.Fatalf("corpo não cita a mensagem esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

func TestAbrirSolicitacaoHandler_InclusaoSFC_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	const solicitacaoID = "so000000-0000-0000-0000-000000000002"
	const alcadaID = "al000000-0000-0000-0000-000000000002"

	corpo := corpoInclusaoSFC(
		linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaSFC, "2026-10-01", 1000),
	)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo, divisao_id, filial FROM centros_custo WHERE id = $1")).
		WithArgs(testSolicitacaoCentroCustoID).
		WillReturnRows(sqlmock.NewRows([]string{"codigo", "divisao_id", "filial"}).
			AddRow("CC1", testSolicitacaoDivisaoID, "F1"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT cc_proprio_id FROM usuarios WHERE id = $1")).
		WithArgs(testAtorID).
		WillReturnRows(sqlmock.NewRows([]string{"cc_proprio_id"}).AddRow(testSolicitacaoCentroCustoID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT plano FROM contas WHERE id = $1")).
		WithArgs(testSolicitacaoContaSFC).
		WillReturnRows(sqlmock.NewRows([]string{"plano"}).AddRow("SFC"))
	mock.ExpectQuery(regexp.QuoteMeta("FROM regras_aprovacao")).
		WithArgs("F1", "CC1", 1000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("FROM alcadas")).
		WithArgs("F1", "CC1", 1000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "papel_aprovador"}).AddRow(alcadaID, "Gerente Financeiro"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 1) FROM cadastro_historico")).
		WithArgs("alcadas", alcadaID).
		WillReturnRows(sqlmock.NewRows([]string{"versao"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO solicitacoes")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(solicitacaoID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO solicitacao_lancamentos")).
		WithArgs(solicitacaoID, "destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaSFC, "2026-10-01", 1000.0).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"Gerente Financeiro"`) {
		t.Fatalf("corpo não contém o aprovador esperado: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}
