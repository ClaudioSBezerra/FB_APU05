package handlers

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func reqToken(metodo, token, corpo string) *http.Request {
	r := httptest.NewRequest(metodo, "/api/aprovacao/"+token, strings.NewReader(corpo))
	r.SetPathValue("token", token)
	r.RemoteAddr = "10.0.0.1:1234"
	return r
}

func TestMoedaBR(t *testing.T) {
	for in, want := range map[float64]string{0: "R$ 0,00", 999.5: "R$ 999,50", 50000: "R$ 50.000,00", 1234567.891: "R$ 1.234.567,89"} {
		if got := moedaBR(in); got != want {
			t.Errorf("moedaBR(%v) = %q, esperado %q", in, got, want)
		}
	}
}

func TestTokenAprovacao_HashEstavelEUnico(t *testing.T) {
	a, ha, _ := novoTokenAprovacao()
	b, hb, _ := novoTokenAprovacao()
	if a == b || ha == hb || len(a) < 40 || hashToken(a) != ha || len(ha) != 64 {
		t.Fatalf("token/hash inválidos: %q %q", a, ha)
	}
}

func TestLimiteAprovacao_PadraoQuandoSemConfig(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("aprovacao_limite_transferencia")).WillReturnRows(sqlmock.NewRows([]string{"valor"}))
	if got := limiteAprovacaoTransferencia(db); got != 50000 {
		t.Errorf("padrão = %v", got)
	}
	mock.ExpectQuery(regexp.QuoteMeta("aprovacao_limite_transferencia")).WillReturnRows(sqlmock.NewRows([]string{"valor"}).AddRow("80000"))
	if got := limiteAprovacaoTransferencia(db); got != 80000 {
		t.Errorf("configurado = %v", got)
	}
}

func TestConsultarAprovacao_TokenInvalido404(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("FROM solicitacao_aprovacoes")).WillReturnRows(sqlmock.NewRows([]string{"a", "b", "c", "d"}))
	rec := httptest.NewRecorder()
	ConsultarAprovacaoHandler(db)(rec, reqToken("GET", "xxx", ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("esperado 404, veio %d", rec.Code)
	}
}

func TestDecidirPorLink_ValidaEntrada(t *testing.T) {
	db, _ := newSQLMock(t)
	dbp, _ := newSQLMock(t)
	for nome, corpo := range map[string]string{
		"decisão inválida":    `{"decisao":"talvez"}`,
		"rejeitar sem motivo": `{"decisao":"rejeitada","motivo":" "}`,
		"corpo não é json":    `isso nao e json`,
	} {
		rec := httptest.NewRecorder()
		DecidirAprovacaoPorLinkHandler(db, dbp)(rec, reqToken("POST", "tokvalida", corpo))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: esperado 400, veio %d", nome, rec.Code)
		}
	}
}

func TestDecidirPorLink_LinkUsadoOuExpirado410(t *testing.T) {
	db, mock := newSQLMock(t)
	dbp, _ := newSQLMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacao_aprovacoes")).WillReturnRows(sqlmock.NewRows([]string{"a", "b"}))
	rec := httptest.NewRecorder()
	DecidirAprovacaoPorLinkHandler(db, dbp)(rec, reqToken("POST", "usado", `{"decisao":"aprovada"}`))
	if rec.Code != http.StatusGone {
		t.Fatalf("esperado 410, veio %d", rec.Code)
	}
}

func TestDecidirPorLink_AprovaGravaStatusPelaConexaoPrivilegiada(t *testing.T) {
	db, mock := newSQLMock(t)
	dbp, mockP := newSQLMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacao_aprovacoes")).
		WithArgs(hashToken("bom"), "aprovada", "").
		WillReturnRows(sqlmock.NewRows([]string{"solicitacao_id", "aprovador_nome"}).AddRow("so1", "Diretor"))
	mockP.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs("aberta", "so1", 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "t", "s", "v", "a"}).AddRow("so1", "transferencia", "aberta", 2, nil))
	rec := httptest.NewRecorder()
	DecidirAprovacaoPorLinkHandler(db, dbp)(rec, reqToken("POST", "bom", `{"decisao":"aprovada"}`))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "aprovada") {
		t.Fatalf("esperado 200 aprovada, veio %d %s", rec.Code, rec.Body.String())
	}
	if err := mockP.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestDecidirPorLink_StatusJaMudou409_DesfazReserva(t *testing.T) {
	db, mock := newSQLMock(t)
	dbp, mockP := newSQLMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacao_aprovacoes")).
		WillReturnRows(sqlmock.NewRows([]string{"solicitacao_id", "aprovador_nome"}).AddRow("so1", "Diretor"))
	mockP.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).WillReturnRows(sqlmock.NewRows([]string{"id", "t", "s", "v", "a"}))
	mockP.ExpectQuery(regexp.QuoteMeta("SELECT versao FROM solicitacoes")).WithArgs("so1").WillReturnRows(sqlmock.NewRows([]string{"v"}).AddRow(3))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE solicitacao_aprovacoes SET decisao = NULL")).WillReturnResult(sqlmock.NewResult(0, 1))
	rec := httptest.NewRecorder()
	DecidirAprovacaoPorLinkHandler(db, dbp)(rec, reqToken("POST", "tok", `{"decisao":"rejeitada","motivo":"fora do orçamento"}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("esperado 409, veio %d", rec.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error("a reserva do convite deveria ter sido desfeita: ", err)
	}
}
