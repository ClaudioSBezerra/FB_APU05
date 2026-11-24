package handlers

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// Relógio fixo para a suíte (os demais testes usam datas de 2026): 24/11/2026, meio-dia em Recife.
func init() {
	agoraRecife = func() time.Time { return time.Date(2026, 11, 24, 12, 0, 0, 0, locRecife) }
}

func comRelogio(t *testing.T, quando time.Time) {
	t.Helper()
	ant := agoraRecife
	agoraRecife = func() time.Time { return quando }
	t.Cleanup(func() { agoraRecife = ant })
}

func recife(ano int, mes time.Month, dia, h, m, s int) time.Time {
	return time.Date(ano, mes, dia, h, m, s, 0, locRecife)
}

// Critérios de aceite do pedido.
func TestAnosPermitidos_CriteriosDeAceite(t *testing.T) {
	casos := []struct {
		nome   string
		agora  time.Time
		limite string
		quer   []int
	}{
		{"24/11/2026: só 2026", recife(2026, time.November, 24, 10, 0, 0), "01-31", []int{2026}},
		{"15/01/2027: 2027 (padrão) e 2026", recife(2027, time.January, 15, 9, 0, 0), "01-31", []int{2027, 2026}},
		{"31/01/2027 23:59 Recife: ainda aceita 2026", recife(2027, time.January, 31, 23, 59, 0), "01-31", []int{2027, 2026}},
		{"01/02/2027 00:00 Recife: só 2027", recife(2027, time.February, 1, 0, 0, 0), "01-31", []int{2027}},
		{"01/01 00:00: já vale o ano novo, com o anterior", recife(2027, time.January, 1, 0, 0, 0), "01-31", []int{2027, 2026}},
		{"parâmetro alterado para 15/02", recife(2027, time.February, 10, 12, 0, 0), "02-15", []int{2027, 2026}},
		{"parâmetro alterado para 15/01: 16/01 já fechou", recife(2027, time.January, 16, 12, 0, 0), "01-15", []int{2027}},
		{"parâmetro inválido cai no padrão 31/01", recife(2027, time.January, 20, 12, 0, 0), "lixo", []int{2027, 2026}},
	}
	for _, c := range casos {
		got := anosPermitidos(c.agora, c.limite)
		if len(got) != len(c.quer) || (len(got) > 0 && got[0] != c.quer[0]) || (len(got) > 1 && got[1] != c.quer[1]) {
			t.Errorf("%s: veio %v, esperado %v", c.nome, got, c.quer)
		}
	}
}

// O fuso é o de Recife, não o do servidor/UTC: 01/02/2027 01:30 UTC ainda é 31/01 22:30 em Recife.
func TestAnosPermitidos_UsaFusoDeRecife(t *testing.T) {
	utc := time.Date(2027, time.February, 1, 1, 30, 0, 0, time.UTC)
	if got := anosPermitidos(utc, "01-31"); len(got) != 2 {
		t.Errorf("01/02 01:30 UTC = 31/01 22:30 em Recife: ainda aceita o ano anterior, veio %v", got)
	}
	utc = time.Date(2027, time.February, 1, 3, 0, 0, 0, time.UTC) // 00:00 em Recife
	if got := anosPermitidos(utc, "01-31"); len(got) != 1 {
		t.Errorf("01/02 03:00 UTC = 01/02 00:00 em Recife: só o ano corrente, veio %v", got)
	}
}

func TestLimiteAnoAnteriorValido(t *testing.T) {
	for v, ok := range map[string]bool{"01-31": true, "02-29": true, "02-30": false, "13-01": false, "00-10": false, "1-31": false, "01/31": false, "04-31": false, "": false} {
		if limiteAnoAnteriorValido(v) != ok {
			t.Errorf("limiteAnoAnteriorValido(%q) esperado %v", v, ok)
		}
	}
}

func abrirComRelogio(t *testing.T, quando time.Time, corpo string) *httptest.ResponseRecorder {
	t.Helper()
	comRelogio(t, quando)
	db, _ := newSQLMock(t) // as validações de ano rodam antes de qualquer gravação; a config cai no padrão
	rec := httptest.NewRecorder()
	AbrirSolicitacaoHandler(db)(rec, newSolicitacaoRequest(corpo))
	return rec
}

func transf(mesOrigem, mesDestino string) string {
	return corpoTransferencia(
		linhaJSON("origem", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaOrigem, mesOrigem, 1000) + "," +
			linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaDestino, mesDestino, 1000))
}

func TestAbrir_AnoDiferenteDoCorrente_RecusadoNaAPI(t *testing.T) {
	// "chamada direta à API com ano 2026 em fevereiro de 2027: recusada com erro de validação"
	rec := abrirComRelogio(t, recife(2027, time.February, 10, 12, 0, 0), transf("2026-12-01", "2026-12-01"))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "O ano 2026 não é permitido") || !strings.Contains(rec.Body.String(), "2027") {
		t.Fatalf("esperado 400 citando 2026 e 2027, veio %d %s", rec.Code, rec.Body.String())
	}
	// ano futuro também
	rec = abrirComRelogio(t, recife(2026, time.November, 24, 12, 0, 0), transf("2027-01-01", "2027-01-01"))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "O ano 2027 não é permitido") {
		t.Fatalf("ano futuro deve ser recusado: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAbrir_TransferenciaEntreAnos_RecusadaMesmoEmJaneiro(t *testing.T) {
	rec := abrirComRelogio(t, recife(2027, time.January, 15, 9, 0, 0), transf("2026-12-01", "2027-01-01"))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "mesmo ano") {
		t.Fatalf("origem 2026 e destino 2027 deve ser recusada: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAbrir_JaneiroAceitaOAnoAnterior(t *testing.T) {
	// em janeiro, 2026 passa pela validação de ano (o que vem depois depende do banco, que aqui é simulado)
	rec := abrirComRelogio(t, recife(2027, time.January, 31, 23, 59, 0), transf("2026-12-01", "2026-12-01"))
	if strings.Contains(rec.Body.String(), "não é permitido") || strings.Contains(rec.Body.String(), "mesmo ano") {
		t.Fatalf("2026 deveria passar até 31/01 23:59: %d %s", rec.Code, rec.Body.String())
	}
	rec = abrirComRelogio(t, recife(2027, time.February, 1, 0, 0, 0), transf("2026-12-01", "2026-12-01"))
	if !strings.Contains(rec.Body.String(), "O ano 2026 não é permitido") {
		t.Fatalf("01/02 00:00: 2026 deve ser recusado: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAbrir_InclusaoSegueARegraDoAno(t *testing.T) {
	corpo := corpoInclusaoSFC(linhaJSON("destino", testSolicitacaoDivisaoID, testSolicitacaoCentroCustoID, testSolicitacaoContaSFC, "2026-12-01", 500))
	rec := abrirComRelogio(t, recife(2027, time.March, 3, 12, 0, 0), corpo)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "O ano 2026 não é permitido") {
		t.Fatalf("inclusão SFC em 2026 durante 2027 deve ser recusada: %d %s", rec.Code, rec.Body.String())
	}
}

func TestOpcoesFormulario_TrazOAnoDoServidor(t *testing.T) {
	comRelogio(t, recife(2027, time.January, 15, 9, 0, 0))
	db, mock := newSQLMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("orcamento_ano_anterior_ate")).WillReturnRows(sqlmock.NewRows([]string{"valor"}).AddRow("01-31"))
	if got := anosPermitidosAgora(db); len(got) != 2 || got[0] != 2027 || got[1] != 2026 {
		t.Errorf("anos = %v", got)
	}
}
