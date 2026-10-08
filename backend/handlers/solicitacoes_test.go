package handlers

// solicitacoes_test.go — cobre a I/O Matrix da Story 3.1 (Abrir
// Transferência com aprovação calculada): tipo não suportado, lados
// desbalanceados, mais de um exercício, CC divergente entre linhas, CC não
// autorizado, conta do plano SFC, sem alçada cadastrada (422) e o envio
// feliz (201). Mesmo padrão httptest+sqlmock de cadastros_test.go/
// admin_test.go.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
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
	testSolicitacaoCentroCustoID       = "cc000000-0000-0000-0000-000000000001"
	testSolicitacaoDivisaoID           = "dd000000-0000-0000-0000-000000000002"
	testSolicitacaoContaOrigem         = "ca000000-0000-0000-0000-000000000001"
	testSolicitacaoContaDestino        = "ca000000-0000-0000-0000-000000000002"
	testSolicitacaoContaSFC            = "ca000000-0000-0000-0000-000000000003"
	testSolicitacaoContaBIFC           = "ca000000-0000-0000-0000-000000000004"
	testSolicitacaoAutorizadorID       = "aa000000-0000-0000-0000-000000000001"
	testSolicitacaoClasseImobilizadoID = "ce000000-0000-0000-0000-000000000001"
	testObraLocalObraID                = "10000000-0000-0000-0000-000000000001"
	testObraSubgrupoDespesaID          = "50000000-0000-0000-0000-000000000001"
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

// corpoInclusao monta o corpo de uma Inclusão (Story 3.3, autorizador
// nominal) — análogo a corpoInclusaoSFC, mas com o campo autorizador_id no
// nível da solicitação.
func corpoInclusao(autorizadorID, lancamentosJSON string) string {
	return fmt.Sprintf(
		`{"tipo_solicitacao":"inclusao","autorizador_id":%q,"lancamentos":[%s]}`,
		autorizadorID, lancamentosJSON,
	)
}

func linhaJSON(lado, divisaoID, centroCustoID, contaID, mes string, valor float64) string {
	return fmt.Sprintf(
		`{"lado":"%s","divisao_id":"%s","centro_custo_id":"%s","conta_id":"%s","mes":"%s","valor":%g}`,
		lado, divisaoID, centroCustoID, contaID, mes, valor,
	)
}

// corpoImobilizado monta o corpo de uma Imobilizado (Story 3.4, anexo de
// cotação) — análogo a corpoInclusao (autorizador_id no nível da
// solicitação), mas com uma lista `anexos` adicional, também no nível da
// solicitação.
func corpoImobilizado(autorizadorID, lancamentosJSON, anexosJSON string) string {
	return fmt.Sprintf(
		`{"tipo_solicitacao":"imobilizado","autorizador_id":%q,"lancamentos":[%s],"anexos":[%s]}`,
		autorizadorID, lancamentosJSON, anexosJSON,
	)
}

// linhaImobilizadoJSON é o análogo de linhaJSON para Imobilizado —
// classe_imobilizado_id no lugar de conta_id/mes (Boundaries "Never" da spec
// 3.4: nenhum campo de fornecedor/mês é exposto para este tipo).
func linhaImobilizadoJSON(lado, divisaoID, centroCustoID, classeImobilizadoID string, valor float64) string {
	return fmt.Sprintf(
		`{"lado":"%s","divisao_id":"%s","centro_custo_id":"%s","classe_imobilizado_id":"%s","valor":%g}`,
		lado, divisaoID, centroCustoID, classeImobilizadoID, valor,
	)
}

// corpoObras monta o corpo de uma Obras (Story 3.5, multi-linha) — análogo
// a corpoImobilizado (autorizador_id no nível da solicitação), mas com
// `classificacao` e `obras_linhas` no lugar de `anexos`/`lancamentos`.
func corpoObras(autorizadorID, classificacao, linhasJSON string) string {
	return fmt.Sprintf(
		`{"tipo_solicitacao":"obras","autorizador_id":%q,"classificacao":%q,"obras_linhas":[%s]}`,
		autorizadorID, classificacao, linhasJSON,
	)
}

// linhaObraJSON monta uma linha do corpo de Obras — local de obra +
// subgrupo de despesa + ordem de investimento + valor; `lado` é opcional
// (string vazia = campo ausente no JSON seria mais fiel, mas omiti-lo aqui
// simplifica os testes que não usam transferencia_saldo).
func linhaObraJSON(lado, localObraID, subgrupoDespesaID, ordemInvestimento string, valor float64) string {
	if lado == "" {
		return fmt.Sprintf(
			`{"local_obra_id":"%s","subgrupo_despesa_id":"%s","ordem_investimento":"%s","valor":%g}`,
			localObraID, subgrupoDespesaID, ordemInvestimento, valor,
		)
	}
	return fmt.Sprintf(
		`{"lado":"%s","local_obra_id":"%s","subgrupo_despesa_id":"%s","ordem_investimento":"%s","valor":%g}`,
		lado, localObraID, subgrupoDespesaID, ordemInvestimento, valor,
	)
}

// anexoJSON monta um item da lista `anexos` do corpo de Imobilizado.
func anexoJSON(nomeArquivo, contentType, conteudoBase64 string) string {
	return fmt.Sprintf(
		`{"nome_arquivo":%q,"content_type":%q,"conteudo_base64":%q}`,
		nomeArquivo, contentType, conteudoBase64,
	)
}

// testAnexoConteudo/testAnexoConteudoBase64 são reaproveitados pelos testes
// de Imobilizado abaixo — conteúdo fixo "cotação de teste" (nunca dado real
// de pessoa/fornecedor, Epic 3 context).
var (
	testAnexoConteudo       = []byte("cotação de teste — anexo válido")
	testAnexoConteudoBase64 = base64.StdEncoding.EncodeToString(testAnexoConteudo)
)

// sha256HexDeTeste replica o cálculo de hash de internal/anexos.Salvar —
// usado só para montar a expectativa do mock (o hash em si é sempre
// calculado pela implementação, nunca recebido do cliente).
func sha256HexDeTeste(dados []byte) string {
	soma := sha256.Sum256(dados)
	return hex.EncodeToString(soma[:])
}

// --- validações estruturais (sem DB) ---

func TestAbrirSolicitacaoHandler_TipoNaoSuportado(t *testing.T) {
	db, mock := newSQLMock(t)

	// "obras" (Story 3.5) já tem handler a partir desta story — "doacao" é
	// um tipo_solicitacao fora do Glossário inteiro (nunca vai ter handler),
	// mesmo papel que "obras" tinha como exemplo antes da Story 3.5
	// (Boundaries "Never" da spec 3.4).
	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(`{"tipo_solicitacao":"doacao","lancamentos":[]}`)
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
		WithArgs(solicitacaoID, "origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaOrigem, "2026-10-01", nil, 1000.0).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO solicitacao_lancamentos")).
		WithArgs(solicitacaoID, "destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaDestino, "2026-10-01", nil, 1000.0).
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
// (validarContagemELadoPorTipo), validação de valor (valor<=0), validação de
// plano SFC (validarPlanoConta) e o caminho feliz/sem-alçada de ponta a
// ponta.

func TestAbrirSolicitacaoHandler_InclusaoSFC_ValorInvalido(t *testing.T) {
	for _, valor := range []float64{0, -100} {
		t.Run(fmt.Sprintf("valor=%g", valor), func(t *testing.T) {
			db, mock := newSQLMock(t)

			corpo := corpoInclusaoSFC(
				linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaSFC, "2026-10-01", valor),
			)

			handler := AbrirSolicitacaoHandler(db)
			req := newSolicitacaoRequest(corpo)
			rec := httptest.NewRecorder()
			handler(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "maior que zero") {
				t.Fatalf("corpo não cita a validação de valor esperada: %s", rec.Body.String())
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
			}
		})
	}
}

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
		WithArgs(solicitacaoID, "destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaSFC, "2026-10-01", nil, 1000.0).
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

// --- Story 3.3 — Abrir Inclusão com autorizador nominal ---
//
// AutorizadorNominal.Resolve (a consulta a autorizadores_formulario) ainda
// não tinha cobertura própria em internal/aprovacao (Code Map da spec 3.3
// só lista a asserção de dispatch em calculado_test.go) — os testes abaixo
// cobrem o Resolve de ponta a ponta via handler, além do wiring novo
// (autorizador_id ausente/malformado, contagem/lado, valor<=0, plano BIFC):
// I/O Matrix da spec 3.3.

func TestAbrirSolicitacaoHandler_Inclusao_AutorizadorIDAusente(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoInclusao("",
		linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaBIFC, "2026-10-01", 1000),
	)

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "campo 'autorizador_id' inválido") {
		t.Fatalf("corpo não cita a validação esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

func TestAbrirSolicitacaoHandler_Inclusao_AutorizadorIDMalformado(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoInclusao("nao-e-um-uuid",
		linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaBIFC, "2026-10-01", 1000),
	)

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "campo 'autorizador_id' inválido") {
		t.Fatalf("corpo não cita a validação esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

func TestAbrirSolicitacaoHandler_Inclusao_ValorInvalido(t *testing.T) {
	for _, valor := range []float64{0, -100} {
		t.Run(fmt.Sprintf("valor=%g", valor), func(t *testing.T) {
			db, mock := newSQLMock(t)

			corpo := corpoInclusao(testSolicitacaoAutorizadorID,
				linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaBIFC, "2026-10-01", valor),
			)

			handler := AbrirSolicitacaoHandler(db)
			req := newSolicitacaoRequest(corpo)
			rec := httptest.NewRecorder()
			handler(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "maior que zero") {
				t.Fatalf("corpo não cita a validação de valor esperada: %s", rec.Body.String())
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
			}
		})
	}
}

func TestAbrirSolicitacaoHandler_Inclusao_ZeroLinhas(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoInclusao(testSolicitacaoAutorizadorID, "")

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Inclusão aceita apenas uma linha") {
		t.Fatalf("corpo não cita a restrição de contagem esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

func TestAbrirSolicitacaoHandler_Inclusao_MaisDeUmaLinha(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoInclusao(testSolicitacaoAutorizadorID,
		linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaBIFC, "2026-10-01", 1000)+","+
			linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaBIFC, "2026-10-01", 500),
	)

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Inclusão aceita apenas uma linha") {
		t.Fatalf("corpo não cita a restrição de contagem esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

func TestAbrirSolicitacaoHandler_Inclusao_LadoOrigemInvalido(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoInclusao(testSolicitacaoAutorizadorID,
		linhaJSON("origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaBIFC, "2026-10-01", 1000),
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

func TestAbrirSolicitacaoHandler_Inclusao_ContaPlanoSFC(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoInclusao(testSolicitacaoAutorizadorID,
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
	mock.ExpectRollback()

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "BIFC") || !strings.Contains(rec.Body.String(), "Inclusão") {
		t.Fatalf("corpo não cita a restrição de plano esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

func TestAbrirSolicitacaoHandler_Inclusao_AutorizadorForaDoCCOuFaixa(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoInclusao(testSolicitacaoAutorizadorID,
		linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaBIFC, "2026-10-01", 1000),
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
		WithArgs(testSolicitacaoContaBIFC).
		WillReturnRows(sqlmock.NewRows([]string{"plano"}).AddRow("BIFC"))
	mock.ExpectQuery(regexp.QuoteMeta("FROM autorizadores_formulario")).
		WithArgs("CC1", testSolicitacaoAutorizadorID, 1000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "autorizador selecionado não é válido") {
		t.Fatalf("corpo não cita a mensagem esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

func TestAbrirSolicitacaoHandler_Inclusao_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	const solicitacaoID = "so000000-0000-0000-0000-000000000003"
	const autorizadorRegraID = "ar000000-0000-0000-0000-000000000001"

	corpo := corpoInclusao(testSolicitacaoAutorizadorID,
		linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaBIFC, "2026-10-01", 1000),
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
		WithArgs(testSolicitacaoContaBIFC).
		WillReturnRows(sqlmock.NewRows([]string{"plano"}).AddRow("BIFC"))
	mock.ExpectQuery(regexp.QuoteMeta("FROM autorizadores_formulario")).
		WithArgs("CC1", testSolicitacaoAutorizadorID, 1000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(autorizadorRegraID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 1) FROM cadastro_historico")).
		WithArgs("autorizadores-formulario", autorizadorRegraID).
		WillReturnRows(sqlmock.NewRows([]string{"versao"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT nome FROM usuarios WHERE id = $1")).
		WithArgs(testSolicitacaoAutorizadorID).
		WillReturnRows(sqlmock.NewRows([]string{"nome"}).AddRow("Autorizador Nominal"))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO solicitacoes")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(solicitacaoID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO solicitacao_lancamentos")).
		WithArgs(solicitacaoID, "destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaBIFC, "2026-10-01", nil, 1000.0).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"tipo":"pessoa"`) {
		t.Fatalf("corpo não indica Tipo=pessoa: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"Autorizador Nominal"`) {
		t.Fatalf("corpo não contém o aprovador esperado: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// --- Story 3.4 — Abrir Imobilizado com anexo de cotação ---
//
// Cobre a I/O Matrix da spec 3.4: sem anexo, anexo com base64 inválido,
// anexo sem nome, classe_imobilizado_id ausente/malformado, classe
// inexistente, autorizador fora do CC/faixa, mais de uma linha/lado!=destino
// e o envio feliz (201, com hash SHA-256 do anexo gravado).

func TestAbrirSolicitacaoHandler_Imobilizado_SemAnexo(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoImobilizado(testSolicitacaoAutorizadorID,
		linhaImobilizadoJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoClasseImobilizadoID, 1000),
		"",
	)

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Imobilizado exige ao menos um anexo de cotação") {
		t.Fatalf("corpo não cita a exigência de anexo: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

func TestAbrirSolicitacaoHandler_Imobilizado_AnexoBase64Invalido(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoImobilizado(testSolicitacaoAutorizadorID,
		linhaImobilizadoJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoClasseImobilizadoID, 1000),
		anexoJSON("cotacao.pdf", "application/pdf", "!!!nao-e-base64!!!"),
	)

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "conteúdo não é base64 válido") {
		t.Fatalf("corpo não cita a validação de base64: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

func TestAbrirSolicitacaoHandler_Imobilizado_AnexoSemNome(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoImobilizado(testSolicitacaoAutorizadorID,
		linhaImobilizadoJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoClasseImobilizadoID, 1000),
		anexoJSON("", "application/pdf", testAnexoConteudoBase64),
	)

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "campo 'nome_arquivo' obrigatório") {
		t.Fatalf("corpo não cita a validação de nome_arquivo: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

func TestAbrirSolicitacaoHandler_Imobilizado_ClasseImobilizadoIDMalformado(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoImobilizado(testSolicitacaoAutorizadorID,
		linhaImobilizadoJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, "nao-e-um-uuid", 1000),
		anexoJSON("cotacao.pdf", "application/pdf", testAnexoConteudoBase64),
	)

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "campo 'classe_imobilizado_id' inválido") {
		t.Fatalf("corpo não cita a validação de classe_imobilizado_id: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

// TestAbrirSolicitacaoHandler_Imobilizado_AutorizadorIDMalformado cobre a
// guarda de formato de autorizador_id (compartilhada com "inclusao", já
// testada em TestAbrirSolicitacaoHandler_Inclusao_AutorizadorIDMalformado)
// também para tipo_solicitacao="imobilizado" — até aqui só o caso de
// violação em nível de banco ("fora do CC/faixa") tinha teste para
// Imobilizado.
func TestAbrirSolicitacaoHandler_Imobilizado_AutorizadorIDMalformado(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoImobilizado("nao-e-um-uuid",
		linhaImobilizadoJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoClasseImobilizadoID, 1000),
		anexoJSON("cotacao.pdf", "application/pdf", testAnexoConteudoBase64),
	)

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "campo 'autorizador_id' inválido") {
		t.Fatalf("corpo não cita a validação esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

func TestAbrirSolicitacaoHandler_Imobilizado_MaisDeUmaLinha(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoImobilizado(testSolicitacaoAutorizadorID,
		linhaImobilizadoJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoClasseImobilizadoID, 1000)+","+
			linhaImobilizadoJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoClasseImobilizadoID, 500),
		anexoJSON("cotacao.pdf", "application/pdf", testAnexoConteudoBase64),
	)

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Imobilizado aceita apenas uma linha") {
		t.Fatalf("corpo não cita a restrição de contagem esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

func TestAbrirSolicitacaoHandler_Imobilizado_LadoOrigemInvalido(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoImobilizado(testSolicitacaoAutorizadorID,
		linhaImobilizadoJSON("origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoClasseImobilizadoID, 1000),
		anexoJSON("cotacao.pdf", "application/pdf", testAnexoConteudoBase64),
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

func TestAbrirSolicitacaoHandler_Imobilizado_ValorInvalido(t *testing.T) {
	for _, valor := range []float64{0, -100} {
		t.Run(fmt.Sprintf("valor=%g", valor), func(t *testing.T) {
			db, mock := newSQLMock(t)

			corpo := corpoImobilizado(testSolicitacaoAutorizadorID,
				linhaImobilizadoJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoClasseImobilizadoID, valor),
				anexoJSON("cotacao.pdf", "application/pdf", testAnexoConteudoBase64),
			)

			handler := AbrirSolicitacaoHandler(db)
			req := newSolicitacaoRequest(corpo)
			rec := httptest.NewRecorder()
			handler(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "maior que zero") {
				t.Fatalf("corpo não cita a validação de valor esperada: %s", rec.Body.String())
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
			}
		})
	}
}

func TestAbrirSolicitacaoHandler_Imobilizado_ClasseInexistente(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoImobilizado(testSolicitacaoAutorizadorID,
		linhaImobilizadoJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoClasseImobilizadoID, 1000),
		anexoJSON("cotacao.pdf", "application/pdf", testAnexoConteudoBase64),
	)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo, divisao_id, filial FROM centros_custo WHERE id = $1")).
		WithArgs(testSolicitacaoCentroCustoID).
		WillReturnRows(sqlmock.NewRows([]string{"codigo", "divisao_id", "filial"}).
			AddRow("CC1", testSolicitacaoDivisaoID, "F1"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT cc_proprio_id FROM usuarios WHERE id = $1")).
		WithArgs(testAtorID).
		WillReturnRows(sqlmock.NewRows([]string{"cc_proprio_id"}).AddRow(testSolicitacaoCentroCustoID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM classes_imobilizado WHERE id = $1")).
		WithArgs(testSolicitacaoClasseImobilizadoID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "classe de imobilizado não encontrada") {
		t.Fatalf("corpo não cita a mensagem esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

func TestAbrirSolicitacaoHandler_Imobilizado_AutorizadorForaDoCCOuFaixa(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoImobilizado(testSolicitacaoAutorizadorID,
		linhaImobilizadoJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoClasseImobilizadoID, 1000),
		anexoJSON("cotacao.pdf", "application/pdf", testAnexoConteudoBase64),
	)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo, divisao_id, filial FROM centros_custo WHERE id = $1")).
		WithArgs(testSolicitacaoCentroCustoID).
		WillReturnRows(sqlmock.NewRows([]string{"codigo", "divisao_id", "filial"}).
			AddRow("CC1", testSolicitacaoDivisaoID, "F1"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT cc_proprio_id FROM usuarios WHERE id = $1")).
		WithArgs(testAtorID).
		WillReturnRows(sqlmock.NewRows([]string{"cc_proprio_id"}).AddRow(testSolicitacaoCentroCustoID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM classes_imobilizado WHERE id = $1")).
		WithArgs(testSolicitacaoClasseImobilizadoID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testSolicitacaoClasseImobilizadoID))
	mock.ExpectQuery(regexp.QuoteMeta("FROM autorizadores_formulario")).
		WithArgs("CC1", testSolicitacaoAutorizadorID, 1000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "autorizador selecionado não é válido") {
		t.Fatalf("corpo não cita a mensagem esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

func TestAbrirSolicitacaoHandler_Imobilizado_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	const solicitacaoID = "so000000-0000-0000-0000-000000000004"
	const autorizadorRegraID = "ar000000-0000-0000-0000-000000000002"
	const anexoID = "an000000-0000-0000-0000-000000000001"

	corpo := corpoImobilizado(testSolicitacaoAutorizadorID,
		linhaImobilizadoJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoClasseImobilizadoID, 1000),
		anexoJSON("cotacao.pdf", "application/pdf", testAnexoConteudoBase64),
	)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo, divisao_id, filial FROM centros_custo WHERE id = $1")).
		WithArgs(testSolicitacaoCentroCustoID).
		WillReturnRows(sqlmock.NewRows([]string{"codigo", "divisao_id", "filial"}).
			AddRow("CC1", testSolicitacaoDivisaoID, "F1"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT cc_proprio_id FROM usuarios WHERE id = $1")).
		WithArgs(testAtorID).
		WillReturnRows(sqlmock.NewRows([]string{"cc_proprio_id"}).AddRow(testSolicitacaoCentroCustoID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM classes_imobilizado WHERE id = $1")).
		WithArgs(testSolicitacaoClasseImobilizadoID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testSolicitacaoClasseImobilizadoID))
	mock.ExpectQuery(regexp.QuoteMeta("FROM autorizadores_formulario")).
		WithArgs("CC1", testSolicitacaoAutorizadorID, 1000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(autorizadorRegraID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 1) FROM cadastro_historico")).
		WithArgs("autorizadores-formulario", autorizadorRegraID).
		WillReturnRows(sqlmock.NewRows([]string{"versao"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT nome FROM usuarios WHERE id = $1")).
		WithArgs(testSolicitacaoAutorizadorID).
		WillReturnRows(sqlmock.NewRows([]string{"nome"}).AddRow("Autorizador Nominal"))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO solicitacoes")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(solicitacaoID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO solicitacao_lancamentos")).
		WithArgs(solicitacaoID, "destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, nil, nil, testSolicitacaoClasseImobilizadoID, 1000.0).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO solicitacao_anexos")).
		WithArgs(solicitacaoID, "cotacao.pdf", "application/pdf", len(testAnexoConteudo), sha256HexDeTeste(testAnexoConteudo), testAnexoConteudo).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(anexoID))
	mock.ExpectCommit()

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"tipo":"pessoa"`) {
		t.Fatalf("corpo não indica Tipo=pessoa: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// --- Story 3.5: Abrir Obras multi-linha (FR-9/FR-11) ---

// TestAbrirSolicitacaoHandler_Obras_AutorizadorIDMalformado cobre a
// checagem de FORMATO de autorizador_id (sem acesso a banco) — mesmo
// padrão de Inclusão/Imobilizado.
func TestAbrirSolicitacaoHandler_Obras_AutorizadorIDMalformado(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoObras("nao-e-um-uuid", "inclusao",
		linhaObraJSON("", testObraLocalObraID, testObraSubgrupoDespesaID, "CRIAR", 1000),
	)

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "campo 'autorizador_id' inválido") {
		t.Fatalf("corpo não cita a mensagem esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

// TestAbrirSolicitacaoHandler_Obras_ClassificacaoInvalida cobre
// "classificacao inválida/ausente" da I/O Matrix — validação puramente
// estrutural, antes de qualquer acesso a banco.
func TestAbrirSolicitacaoHandler_Obras_ClassificacaoInvalida(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoObras(testSolicitacaoAutorizadorID, "classificacao-invalida",
		linhaObraJSON("", testObraLocalObraID, testObraSubgrupoDespesaID, "CRIAR", 1000),
	)

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "campo 'classificacao' inválido") {
		t.Fatalf("corpo não cita a mensagem esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

// TestAbrirSolicitacaoHandler_Obras_ZeroLinhas cobre "Zero linhas" da I/O
// Matrix — obras_linhas vazio é recusado antes de qualquer acesso a banco.
func TestAbrirSolicitacaoHandler_Obras_ZeroLinhas(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoObras(testSolicitacaoAutorizadorID, "inclusao", "")

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "a solicitação de obras precisa de ao menos uma linha") {
		t.Fatalf("corpo não cita a mensagem esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

// TestAbrirSolicitacaoHandler_Obras_TransferenciaSaldoSemOsDoisBlocos cobre
// "Transferência de saldo sem os 2 blocos" da I/O Matrix — só linhas
// lado="retirada" (nenhuma "inclusao") é recusado antes de qualquer acesso
// a banco.
func TestAbrirSolicitacaoHandler_Obras_TransferenciaSaldoSemOsDoisBlocos(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoObras(testSolicitacaoAutorizadorID, classificacaoTransferenciaSaldo,
		linhaObraJSON("retirada", testObraLocalObraID, testObraSubgrupoDespesaID, "CRIAR", 1000),
	)

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "transferência de saldo exige ao menos uma linha de retirada e uma de inclusão") {
		t.Fatalf("corpo não cita a mensagem esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

// TestAbrirSolicitacaoHandler_Obras_TransferenciaSaldoDesbalanceada cobre
// "Transferência de saldo desbalanceada" da I/O Matrix — soma(retirada) !=
// soma(inclusao), fora da tolerância de R$0,01.
func TestAbrirSolicitacaoHandler_Obras_TransferenciaSaldoDesbalanceada(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoObras(testSolicitacaoAutorizadorID, classificacaoTransferenciaSaldo,
		linhaObraJSON("retirada", testObraLocalObraID, testObraSubgrupoDespesaID, "CRIAR", 1000)+","+
			linhaObraJSON("inclusao", testObraLocalObraID, testObraSubgrupoDespesaID, "CRIAR", 500),
	)

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "os dois lados da transferência de saldo não batem") {
		t.Fatalf("corpo não cita a mensagem esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nada deveria tocar o banco): %v", err)
	}
}

// TestAbrirSolicitacaoHandler_Obras_LocalObraInexistente cobre
// "local_obra_id inexistente" da I/O Matrix — UUID bem formado sem linha
// correspondente em locais_obra.
func TestAbrirSolicitacaoHandler_Obras_LocalObraInexistente(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoObras(testSolicitacaoAutorizadorID, "inclusao",
		linhaObraJSON("", testObraLocalObraID, testObraSubgrupoDespesaID, "CRIAR", 1000),
	)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM locais_obra WHERE id = $1)")).
		WithArgs(testObraLocalObraID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "linha 1: local de obra não encontrado") {
		t.Fatalf("corpo não cita a mensagem esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestAbrirSolicitacaoHandler_Obras_SubgrupoDespesaInexistente cobre
// "subgrupo_despesa_id inexistente" da I/O Matrix.
func TestAbrirSolicitacaoHandler_Obras_SubgrupoDespesaInexistente(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoObras(testSolicitacaoAutorizadorID, "inclusao",
		linhaObraJSON("", testObraLocalObraID, testObraSubgrupoDespesaID, "CRIAR", 1000),
	)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM locais_obra WHERE id = $1)")).
		WithArgs(testObraLocalObraID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM subgrupos_despesa WHERE id = $1)")).
		WithArgs(testObraSubgrupoDespesaID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "linha 1: subgrupo de despesa não encontrado") {
		t.Fatalf("corpo não cita a mensagem esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestAbrirSolicitacaoHandler_Obras_OrdemInexistente cobre "Ordem
// inexistente (não é CRIAR)" da I/O Matrix.
func TestAbrirSolicitacaoHandler_Obras_OrdemInexistente(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoObras(testSolicitacaoAutorizadorID, "inclusao",
		linhaObraJSON("", testObraLocalObraID, testObraSubgrupoDespesaID, "OI-999", 1000),
	)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM locais_obra WHERE id = $1)")).
		WithArgs(testObraLocalObraID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM subgrupos_despesa WHERE id = $1)")).
		WithArgs(testObraSubgrupoDespesaID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT local_obra_id, subgrupo_despesa_id FROM obra_ordens WHERE numero_ordem = $1")).
		WithArgs("OI-999").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "linha 1: ordem de investimento não encontrada") {
		t.Fatalf("corpo não cita a mensagem esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestAbrirSolicitacaoHandler_Obras_OrdemNaoCasaLocalSubgrupo cobre "Ordem
// existe mas local/subgrupo não casam" da I/O Matrix.
func TestAbrirSolicitacaoHandler_Obras_OrdemNaoCasaLocalSubgrupo(t *testing.T) {
	db, mock := newSQLMock(t)

	const outroLocalObraID = "lo000000-0000-0000-0000-000000000099"

	corpo := corpoObras(testSolicitacaoAutorizadorID, "inclusao",
		linhaObraJSON("", testObraLocalObraID, testObraSubgrupoDespesaID, "OI-001", 1000),
	)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM locais_obra WHERE id = $1)")).
		WithArgs(testObraLocalObraID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM subgrupos_despesa WHERE id = $1)")).
		WithArgs(testObraSubgrupoDespesaID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT local_obra_id, subgrupo_despesa_id FROM obra_ordens WHERE numero_ordem = $1")).
		WithArgs("OI-001").
		WillReturnRows(sqlmock.NewRows([]string{"local_obra_id", "subgrupo_despesa_id"}).
			AddRow(outroLocalObraID, testObraSubgrupoDespesaID))
	mock.ExpectRollback()

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "linha 1: ordem de investimento não pertence ao local de obra/subgrupo de despesa informado") {
		t.Fatalf("corpo não cita a mensagem esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestAbrirSolicitacaoHandler_Obras_OrdemCasaLocalSubgrupoCaseInsensitive
// cobre o achado de revisão (2026-10-08): validarLocalSubgrupoOrdemObras
// comparava local_obra_id/subgrupo_despesa_id lidos de obra_ordens contra o
// texto literal enviado pelo cliente com !=, rejeitando um UUID idêntico
// só porque o Postgres devolveu em outra caixa (maiúsculas) — a comparação
// precisa ser case-insensitive (strings.EqualFold).
func TestAbrirSolicitacaoHandler_Obras_OrdemCasaLocalSubgrupoCaseInsensitive(t *testing.T) {
	db, mock := newSQLMock(t)

	const solicitacaoID = "so000000-0000-0000-0000-000000000007"
	const autorizadorRegraID = "ar000000-0000-0000-0000-000000000005"

	corpo := corpoObras(testSolicitacaoAutorizadorID, "inclusao",
		linhaObraJSON("", testObraLocalObraID, testObraSubgrupoDespesaID, "OI-001", 1000),
	)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM locais_obra WHERE id = $1)")).
		WithArgs(testObraLocalObraID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM subgrupos_despesa WHERE id = $1)")).
		WithArgs(testObraSubgrupoDespesaID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT local_obra_id, subgrupo_despesa_id FROM obra_ordens WHERE numero_ordem = $1")).
		WithArgs("OI-001").
		WillReturnRows(sqlmock.NewRows([]string{"local_obra_id", "subgrupo_despesa_id"}).
			AddRow(strings.ToUpper(testObraLocalObraID), strings.ToUpper(testObraSubgrupoDespesaID)))
	mock.ExpectQuery(regexp.QuoteMeta("FROM aprovadores_obra")).
		WithArgs(testSolicitacaoAutorizadorID, 1000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(autorizadorRegraID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 1) FROM cadastro_historico")).
		WithArgs("aprovadores-obra", autorizadorRegraID).
		WillReturnRows(sqlmock.NewRows([]string{"versao"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT nome FROM usuarios WHERE id = $1")).
		WithArgs(testSolicitacaoAutorizadorID).
		WillReturnRows(sqlmock.NewRows([]string{"nome"}).AddRow("Aprovador de Obras"))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO solicitacoes")).
		WithArgs(testAtorID, sqlmock.AnyArg(), "inclusao").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(solicitacaoID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO solicitacao_obras_linhas")).
		WithArgs(solicitacaoID, nil, testObraLocalObraID, testObraSubgrupoDespesaID, "OI-001", 1000.0).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestAbrirSolicitacaoHandler_Obras_AutorizadorForaDoTeto cobre
// "Autorizador acima do teto individual" da I/O Matrix — ErrAutorizadorInvalido
// com CentroCusto="" (teto individual, nunca CC/faixa).
func TestAbrirSolicitacaoHandler_Obras_AutorizadorForaDoTeto(t *testing.T) {
	db, mock := newSQLMock(t)

	corpo := corpoObras(testSolicitacaoAutorizadorID, "inclusao",
		linhaObraJSON("", testObraLocalObraID, testObraSubgrupoDespesaID, "CRIAR", 1000),
	)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM locais_obra WHERE id = $1)")).
		WithArgs(testObraLocalObraID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM subgrupos_despesa WHERE id = $1)")).
		WithArgs(testObraSubgrupoDespesaID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("FROM aprovadores_obra")).
		WithArgs(testSolicitacaoAutorizadorID, 1000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "autorizador selecionado não é válido para o teto de alçada desta pessoa") {
		t.Fatalf("corpo não cita a mensagem esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestAbrirSolicitacaoHandler_Obras_Success cobre "Envio feliz (inclusão)"
// da I/O Matrix: classificacao="inclusao", 1 linha válida com
// ordem_investimento="CRIAR" — 201, solicitacao_obras_linhas grava 1 linha,
// centro_custo_id NULL, aprovador_snapshot.tipo="pessoa". Nenhuma ordem é
// criada em obra_ordens nesta etapa (ordem_investimento="CRIAR" pula a
// checagem de obra_ordens por completo).
func TestAbrirSolicitacaoHandler_Obras_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	const solicitacaoID = "so000000-0000-0000-0000-000000000005"
	const autorizadorRegraID = "ar000000-0000-0000-0000-000000000003"

	corpo := corpoObras(testSolicitacaoAutorizadorID, "inclusao",
		linhaObraJSON("", testObraLocalObraID, testObraSubgrupoDespesaID, "CRIAR", 1000),
	)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM locais_obra WHERE id = $1)")).
		WithArgs(testObraLocalObraID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM subgrupos_despesa WHERE id = $1)")).
		WithArgs(testObraSubgrupoDespesaID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("FROM aprovadores_obra")).
		WithArgs(testSolicitacaoAutorizadorID, 1000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(autorizadorRegraID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 1) FROM cadastro_historico")).
		WithArgs("aprovadores-obra", autorizadorRegraID).
		WillReturnRows(sqlmock.NewRows([]string{"versao"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT nome FROM usuarios WHERE id = $1")).
		WithArgs(testSolicitacaoAutorizadorID).
		WillReturnRows(sqlmock.NewRows([]string{"nome"}).AddRow("Aprovador de Obras"))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO solicitacoes")).
		WithArgs(testAtorID, sqlmock.AnyArg(), "inclusao").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(solicitacaoID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO solicitacao_obras_linhas")).
		WithArgs(solicitacaoID, nil, testObraLocalObraID, testObraSubgrupoDespesaID, "CRIAR", 1000.0).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"tipo":"pessoa"`) {
		t.Fatalf("corpo não indica Tipo=pessoa: %s", body)
	}
	if !strings.Contains(body, `"centro_custo_id":null`) {
		t.Fatalf("corpo deveria trazer centro_custo_id NULL: %s", body)
	}
	if !strings.Contains(body, `"classificacao":"inclusao"`) {
		t.Fatalf("corpo deveria trazer classificacao=inclusao: %s", body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestAbrirSolicitacaoHandler_Obras_TransferenciaSaldoBalanceada_Success
// cobre "Transferência de saldo balanceada" da I/O Matrix: linhas com lado
// somando igual (±R$0,01) em retirada/inclusao — 201. valorTotal resolvido
// para o aprovador é o maior dos 2 lados (1000, não 2000 = soma de ambos —
// achado de revisão 2026-10-08: somaTotalObras dobrava o valor movimentado
// para transferencia_saldo antes desta correção).
func TestAbrirSolicitacaoHandler_Obras_TransferenciaSaldoBalanceada_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	const solicitacaoID = "so000000-0000-0000-0000-000000000006"
	const autorizadorRegraID = "ar000000-0000-0000-0000-000000000004"

	corpo := corpoObras(testSolicitacaoAutorizadorID, classificacaoTransferenciaSaldo,
		linhaObraJSON("retirada", testObraLocalObraID, testObraSubgrupoDespesaID, "CRIAR", 1000)+","+
			linhaObraJSON("inclusao", testObraLocalObraID, testObraSubgrupoDespesaID, "CRIAR", 1000),
	)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM locais_obra WHERE id = $1)")).
		WithArgs(testObraLocalObraID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM subgrupos_despesa WHERE id = $1)")).
		WithArgs(testObraSubgrupoDespesaID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM locais_obra WHERE id = $1)")).
		WithArgs(testObraLocalObraID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM subgrupos_despesa WHERE id = $1)")).
		WithArgs(testObraSubgrupoDespesaID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("FROM aprovadores_obra")).
		WithArgs(testSolicitacaoAutorizadorID, 1000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(autorizadorRegraID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 1) FROM cadastro_historico")).
		WithArgs("aprovadores-obra", autorizadorRegraID).
		WillReturnRows(sqlmock.NewRows([]string{"versao"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT nome FROM usuarios WHERE id = $1")).
		WithArgs(testSolicitacaoAutorizadorID).
		WillReturnRows(sqlmock.NewRows([]string{"nome"}).AddRow("Aprovador de Obras"))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO solicitacoes")).
		WithArgs(testAtorID, sqlmock.AnyArg(), classificacaoTransferenciaSaldo).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(solicitacaoID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO solicitacao_obras_linhas")).
		WithArgs(solicitacaoID, "retirada", testObraLocalObraID, testObraSubgrupoDespesaID, "CRIAR", 1000.0).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO solicitacao_obras_linhas")).
		WithArgs(solicitacaoID, "inclusao", testObraLocalObraID, testObraSubgrupoDespesaID, "CRIAR", 1000.0).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := AbrirSolicitacaoHandler(db)
	req := newSolicitacaoRequest(corpo)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestValidarAnexos_NomeArquivoExcede255 cobre o achado de revisão
// (2026-10-08) sobre `nome_arquivo`/VARCHAR(255): sem esta checagem, um nome
// maior que a coluna estourava como erro de banco (500) em vez de um 400 de
// validação limpo.
func TestValidarAnexos_NomeArquivoExcede255(t *testing.T) {
	nomeLongo := strings.Repeat("a", 256)
	_, msg := validarAnexos([]anexoRequest{{NomeArquivo: nomeLongo, ContentType: "application/pdf", ConteudoBase64: testAnexoConteudoBase64}})
	if !strings.Contains(msg, "'nome_arquivo' excede 255 caracteres") {
		t.Fatalf("esperada mensagem de nome_arquivo excedendo 255 caracteres, obtido: %q", msg)
	}
}

// TestValidarAnexos_ContentTypeExcede100 cobre o mesmo achado de revisão
// para `content_type`/VARCHAR(100).
func TestValidarAnexos_ContentTypeExcede100(t *testing.T) {
	contentTypeLongo := strings.Repeat("a", 101)
	_, msg := validarAnexos([]anexoRequest{{NomeArquivo: "cotacao.pdf", ContentType: contentTypeLongo, ConteudoBase64: testAnexoConteudoBase64}})
	if !strings.Contains(msg, "'content_type' excede 100 caracteres") {
		t.Fatalf("esperada mensagem de content_type excedendo 100 caracteres, obtido: %q", msg)
	}
}

// TestValidarAnexos_NomeArquivoComAcentosDentroDoLimite prova que a checagem
// de 255 conta caracteres (runas), não bytes: um nome com exatamente 255
// caracteres acentuados (multi-byte em UTF-8, portanto >255 bytes) deve ser
// aceito, pois a coluna VARCHAR(255) do Postgres também conta caracteres.
// Achado de revisão (pass de acompanhamento 2026-10-08): `len()` em Go mede
// bytes, não runas.
func TestValidarAnexos_NomeArquivoComAcentosDentroDoLimite(t *testing.T) {
	nome255Runas := strings.Repeat("ç", 255)
	if len(nome255Runas) <= 255 {
		t.Fatalf("pré-condição do teste inválida: nome deveria exceder 255 bytes (tem %d)", len(nome255Runas))
	}
	_, msg := validarAnexos([]anexoRequest{{NomeArquivo: nome255Runas, ContentType: "application/pdf", ConteudoBase64: testAnexoConteudoBase64}})
	if msg != "" {
		t.Fatalf("nome com 255 caracteres (ainda que >255 bytes) não deveria ser rejeitado, obtido: %q", msg)
	}
}

// TestValidarLancamentosEstrutura_ValorAntesDeMesParaTiposNaoImobilizado
// cobre o achado de revisão (pass de acompanhamento 2026-10-08) de que a
// bifurcação por tipo introduzida pela Story 3.4 invertera a ordem de
// checagem conta_id->valor->mes para conta_id->mes->valor nos tipos
// não-imobilizado. Uma linha com `mes` malformado E `valor<=0`
// simultaneamente deve continuar reportando o erro de `valor` primeiro
// (mesma precedência de antes da Story 3.4), não o de `mes`.
func TestValidarLancamentosEstrutura_ValorAntesDeMesParaTiposNaoImobilizado(t *testing.T) {
	linhas := []lancamentoRequest{{
		Lado:          "destino",
		DivisaoID:     testSolicitacaoDivisaoID,
		CentroCustoID: testSolicitacaoCentroCustoID,
		ContaID:       testSolicitacaoContaDestino,
		Mes:           "2026-10-15", // malformado: não é dia 1
		Valor:         0,            // também inválido
	}}
	_, msg := validarLancamentosEstrutura(linhas, "inclusao")
	if !strings.Contains(msg, "campo 'valor' deve ser maior que zero") {
		t.Fatalf("esperado erro de 'valor' (precedência restaurada), obtido: %q", msg)
	}
}

// TestValidarLancamentosEstrutura_Imobilizado_ContaIDEMesIgnorados cobre o
// Boundary "Never" da spec 3.4 (nenhum campo de fornecedor/mês é exigido ou
// validado para Imobilizado): mesmo que `conta_id`/`mes` venham preenchidos
// no corpo (campos compartilhados pela mesma struct, precedente de
// `autorizador_id` na Story 3.3), eles são ignorados — não bloqueiam a
// validação nem são propostos para a linha validada.
func TestValidarLancamentosEstrutura_Imobilizado_ContaIDEMesIgnorados(t *testing.T) {
	linhas := []lancamentoRequest{{
		Lado:                "destino",
		DivisaoID:           testSolicitacaoDivisaoID,
		CentroCustoID:       testSolicitacaoCentroCustoID,
		ContaID:             "nao-e-um-uuid-e-nem-deveria-importar",
		Mes:                 "nao-e-uma-data-e-nem-deveria-importar",
		ClasseImobilizadoID: testSolicitacaoClasseImobilizadoID,
		Valor:               1000,
	}}
	validadas, msg := validarLancamentosEstrutura(linhas, "imobilizado")
	if msg != "" {
		t.Fatalf("conta_id/mes malformados não deveriam bloquear Imobilizado, obtido erro: %q", msg)
	}
	if len(validadas) != 1 {
		t.Fatalf("esperada 1 linha validada, obtido %d", len(validadas))
	}
	if validadas[0].ContaID != "" || !validadas[0].Mes.IsZero() {
		t.Fatalf("conta_id/mes deveriam ser ignorados para Imobilizado, obtido ContaID=%q Mes=%v", validadas[0].ContaID, validadas[0].Mes)
	}
	if validadas[0].ClasseImobilizadoID != testSolicitacaoClasseImobilizadoID {
		t.Fatalf("classe_imobilizado_id esperado preservado, obtido %q", validadas[0].ClasseImobilizadoID)
	}
}
