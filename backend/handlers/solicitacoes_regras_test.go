package handlers

// Regras da lista de melhorias de 11/11/2026: transferência com várias
// origens para 1 destino (que pode ser de outro CC), anexo obrigatório em
// todos os tipos, CNPJ do fornecedor e quantidade de material no
// Imobilizado.

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

const (
	testCCDestinoID       = "cc000000-0000-0000-0000-000000000050"
	testDivisaoDestinoID  = "dd000000-0000-0000-0000-000000000050"
	testRegrasSolicitacao = "so000000-0000-0000-0000-000000000077"
)

func abrir(t *testing.T, corpo string, prepara func(sqlmock.Sqlmock)) *httptest.ResponseRecorder {
	t.Helper()
	db, mock := newSQLMock(t)
	if prepara != nil {
		prepara(mock)
	}
	rec := httptest.NewRecorder()
	AbrirSolicitacaoHandler(db)(rec, newSolicitacaoRequest(corpo))
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v (body=%s)", err, rec.Body.String())
	}
	return rec
}

func esperar400(t *testing.T, rec *httptest.ResponseRecorder, trecho string) {
	t.Helper()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), trecho) {
		t.Fatalf("corpo não contém %q: %s", trecho, rec.Body.String())
	}
}

// Meses 01, 03 e 05 de um CC somam para o mês 10 de OUTRO CC: aceito, com a
// alçada e a permissão resolvidas pelo CC de origem e a linha de destino
// gravada com o CC/divisão de destino.
func TestTransferencia_VariasOrigensParaDestinoEmOutroCC(t *testing.T) {
	const alcadaID = "al000000-0000-0000-0000-000000000077"
	corpo := corpoTransferencia(
		linhaJSON("origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaOrigem, "2026-01-01", 500) + "," +
			linhaJSON("origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaOrigem, "2026-03-01", 300) + "," +
			linhaJSON("origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaOrigem, "2026-05-01", 200) + "," +
			linhaJSON("destino", testDivisaoDestinoID, testCCDestinoID, testSolicitacaoContaDestino, "2026-10-01", 1000),
	)

	rec := abrir(t, corpo, func(mock sqlmock.Sqlmock) {
		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo, divisao_id, filial FROM centros_custo WHERE id = $1")).
			WithArgs(testSolicitacaoCentroCustoID).
			WillReturnRows(sqlmock.NewRows([]string{"codigo", "divisao_id", "filial"}).AddRow("CC1", testSolicitacaoDivisaoID, "F1"))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT cc_proprio_id FROM usuarios WHERE id = $1")).
			WithArgs(testAtorID).
			WillReturnRows(sqlmock.NewRows([]string{"cc_proprio_id"}).AddRow(testSolicitacaoCentroCustoID))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT divisao_id FROM centros_custo WHERE id = $1")).
			WithArgs(testCCDestinoID).
			WillReturnRows(sqlmock.NewRows([]string{"divisao_id"}).AddRow(testDivisaoDestinoID))
		for _, conta := range []string{testSolicitacaoContaOrigem, testSolicitacaoContaOrigem, testSolicitacaoContaOrigem, testSolicitacaoContaDestino} {
			mock.ExpectQuery(regexp.QuoteMeta("SELECT plano FROM contas WHERE id = $1")).
				WithArgs(conta).
				WillReturnRows(sqlmock.NewRows([]string{"plano"}).AddRow("BIFC"))
		}
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
			WithArgs("transferencia", testAtorID, testSolicitacaoCentroCustoID, sqlmock.AnyArg(), nil, nil, nil, false, nil).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testRegrasSolicitacao))
		for _, l := range []struct {
			mes   string
			valor float64
		}{{"2026-01-01", 500}, {"2026-03-01", 300}, {"2026-05-01", 200}} {
			mock.ExpectExec(regexp.QuoteMeta("INSERT INTO solicitacao_lancamentos")).
				WithArgs(testRegrasSolicitacao, "origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaOrigem, l.mes, nil, l.valor).
				WillReturnResult(sqlmock.NewResult(0, 1))
		}
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO solicitacao_lancamentos")).
			WithArgs(testRegrasSolicitacao, "destino", testDivisaoDestinoID, testCCDestinoID, testSolicitacaoContaDestino, "2026-10-01", nil, 1000.0).
			WillReturnResult(sqlmock.NewResult(0, 1))
		esperarAnexoPadrao(mock)
		mock.ExpectCommit()
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestTransferencia_DoisDestinos_Rejeita(t *testing.T) {
	corpo := corpoTransferencia(
		linhaJSON("origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaOrigem, "2026-01-01", 1000) + "," +
			linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaDestino, "2026-10-01", 500) + "," +
			linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaDestino, "2026-11-01", 500),
	)
	esperar400(t, abrir(t, corpo, nil), "exatamente uma linha de destino")
}

func TestTransferencia_CCDestinoInexistente_Rejeita(t *testing.T) {
	corpo := corpoTransferencia(
		linhaJSON("origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaOrigem, "2026-01-01", 1000) + "," +
			linhaJSON("destino", testDivisaoDestinoID, testCCDestinoID, testSolicitacaoContaDestino, "2026-10-01", 1000),
	)
	rec := abrir(t, corpo, func(mock sqlmock.Sqlmock) {
		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo, divisao_id, filial FROM centros_custo WHERE id = $1")).
			WithArgs(testSolicitacaoCentroCustoID).
			WillReturnRows(sqlmock.NewRows([]string{"codigo", "divisao_id", "filial"}).AddRow("CC1", testSolicitacaoDivisaoID, "F1"))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT cc_proprio_id FROM usuarios WHERE id = $1")).
			WithArgs(testAtorID).
			WillReturnRows(sqlmock.NewRows([]string{"cc_proprio_id"}).AddRow(testSolicitacaoCentroCustoID))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT divisao_id FROM centros_custo WHERE id = $1")).
			WithArgs(testCCDestinoID).
			WillReturnRows(sqlmock.NewRows([]string{"divisao_id"}))
		mock.ExpectRollback()
	})
	esperar400(t, rec, "centro de custo de destino inválido")
}

func TestAbertura_SemAnexo_Rejeita(t *testing.T) {
	corpo := `{"tipo_solicitacao":"transferencia","lancamentos":[` +
		linhaJSON("origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaOrigem, "2026-01-01", 1000) + "," +
		linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaDestino, "2026-10-01", 1000) + `]}`
	rec := abrir(t, corpo, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400 sem anexo, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestImobilizado_SemQuantidade_Rejeita(t *testing.T) {
	corpo := strings.Replace(
		corpoImobilizado("aa000000-0000-0000-0000-000000000001",
			linhaImobilizadoJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, "cd000000-0000-0000-0000-000000000001", 1000),
			anexoJSON("cotacao.pdf", "application/pdf", testAnexoConteudoBase64)),
		`"quantidade_material":1,`, "", 1)
	esperar400(t, abrir(t, corpo, nil), "quantidade_material")
}

func TestFornecedorCNPJInvalido_Rejeita(t *testing.T) {
	corpo := strings.Replace(corpoTransferencia(
		linhaJSON("origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaOrigem, "2026-01-01", 1000)+","+
			linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaDestino, "2026-10-01", 1000)),
		`"tipo_solicitacao":"transferencia",`, `"tipo_solicitacao":"transferencia","fornecedor_cnpj":"12.345",`, 1)
	esperar400(t, abrir(t, corpo, nil), "fornecedor_cnpj")
}

func TestValidarCNPJFornecedor_NormalizaDigitos(t *testing.T) {
	v, msg := validarCNPJFornecedor("12.345.678/0001-90")
	if msg != "" || !v.Valid || v.String != "12345678000190" {
		t.Fatalf("CNPJ normalizado = %+v (msg=%q)", v, msg)
	}
	if v, msg := validarCNPJFornecedor("  "); msg != "" || v.Valid {
		t.Fatalf("vazio deveria ser NULL sem erro: %+v %q", v, msg)
	}
}
