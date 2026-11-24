package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"

	"fb_apu05/internal/sapodata"
)

const (
	sCC1   = "11111111-1111-1111-1111-111111111111" // CC do usuário (origem)
	sCC2   = "22222222-2222-2222-2222-222222222222" // outro CC (destino)
	sConta = "33333333-3333-3333-3333-333333333333"
)

func reqSaldo(t *testing.T, corpo string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/solicitacoes/saldo", bytes.NewBufferString(corpo))
	claims := jwt.MapClaims{"user_id": testTIUsuarioID, "perfil": "solicitante"}
	return req.WithContext(context.WithValue(req.Context(), ClaimsContextKey, claims))
}

func esperaBaseSaldo(mock sqlmock.Sqlmock, ativo string) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT chave, valor FROM configuracoes")).WillReturnRows(sqlmock.NewRows([]string{"chave", "valor"}).
		AddRow("sap_bg_ativo", ativo).AddRow("sap_bg_url_base", "https://vhfcoqasci.sap.ferreiracosta.com:44300/sap/opu/odata4/sap/zsb_trava_orcamentaria/srvd_a2x/sap/zsd_trava_orcamentaria/0001").
		AddRow("sap_usuario", "u"))
	mock.ExpectQuery("FROM centros_custo cc").WithArgs(testTIUsuarioID).WillReturnRows(sqlmock.NewRows([]string{"id", "codigo", "nome", "divisao_id"}).AddRow(sCC1, "10026040", "TI", "d1"))
}

func esperaCodigos(mock sqlmock.Sqlmock, ccID, cc string) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo FROM centros_custo WHERE id = $1")).WithArgs(ccID).WillReturnRows(sqlmock.NewRows([]string{"codigo"}).AddRow(cc))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo FROM contas WHERE id = $1")).WithArgs(sConta).WillReturnRows(sqlmock.NewRows([]string{"codigo"}).AddRow("4210100001"))
}

func saldoFalso(disp [12]float64) func(context.Context, sapodata.Config, string, string, string) (*sapodata.Saldo, error) {
	return func(_ context.Context, _ sapodata.Config, objnr, kstar, gjahr string) (*sapodata.Saldo, error) {
		return &sapodata.Saldo{Encontrado: true, DispAcum: disp, Disponivel: disp, Plan: disp}, nil
	}
}

func decSaldo(t *testing.T, rec *httptest.ResponseRecorder) []saldoLinhaSaida {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d %s", rec.Code, rec.Body.String())
	}
	var o struct{ Linhas []saldoLinhaSaida }
	if err := json.Unmarshal(rec.Body.Bytes(), &o); err != nil {
		t.Fatal(err)
	}
	return o.Linhas
}

func TestSaldo_Origem_AvisoQuandoExcede_DestinoNaoConsulta(t *testing.T) {
	cacheSaldo = map[string]cacheSaldoItem{}
	db, mock := newSQLMock(t)
	orig := consultarSaldoSAP
	t.Cleanup(func() { consultarSaldoSAP = orig })
	var chamadas []string
	consultarSaldoSAP = func(ctx context.Context, c sapodata.Config, o, k, g string) (*sapodata.Saldo, error) {
		chamadas = append(chamadas, o+"|"+k+"|"+g)
		return saldoFalso([12]float64{1000, 1500, 1500, 1500, 1500, 1500, 1500, 1500, 1500, 1500, 1500, 1500})(ctx, c, o, k, g)
	}
	esperaBaseSaldo(mock, "true")
	// duas origens (jan 800 e fev 900) no mesmo CC/conta + destino em outro CC (não é consultado)
	esperaCodigos(mock, sCC1, "10026040")
	esperaCodigos(mock, sCC1, "10026040")
	esperaCodigos(mock, sCC2, "10016000")
	corpo := `{"lancamentos":[
	 {"lado":"origem","centro_custo_id":"` + sCC1 + `","conta_id":"` + sConta + `","mes":"2026-01-01","valor":800},
	 {"lado":"origem","centro_custo_id":"` + sCC1 + `","conta_id":"` + sConta + `","mes":"2026-02-01","valor":900},
	 {"lado":"destino","centro_custo_id":"` + sCC2 + `","conta_id":"` + sConta + `","mes":"2026-02-01","valor":1700}]}`
	rec := httptest.NewRecorder()
	SaldoOrcamentoHandler(db)(rec, reqSaldo(t, corpo))
	l := decSaldo(t, rec)
	if len(l) != 3 {
		t.Fatalf("linhas: %d", len(l))
	}
	if l[0].Status != "ok" || l[0].Excede || *l[0].RetiradaAcum != 800 || *l[0].DispAcum != 1000 {
		t.Errorf("jan: 800 de 1000 não excede: %+v", l[0])
	}
	// fev: retirada ACUMULADA 800+900 = 1700 > disp_acum 1500 → aviso, falta 200
	if !l[1].Excede || *l[1].RetiradaAcum != 1700 || *l[1].Falta != 200 {
		t.Errorf("fev: deveria avisar (retirada acumulada 1700 > 1500): %+v", l[1])
	}
	if l[2].Status != "nao_identificado" || l[2].DispAcum != nil || !strings.Contains(l[2].Motivo, "só para a origem") {
		t.Errorf("o destino não pode devolver saldo: %+v", l[2])
	}
	if len(chamadas) != 1 || !strings.HasPrefix(chamadas[0], "KSFC01") {
		t.Errorf("só a origem é consultada (1 chave distinta): %v", chamadas)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestSaldo_SAPForaDoAr_NaoIdentificado(t *testing.T) {
	cacheSaldo = map[string]cacheSaldoItem{}
	db, mock := newSQLMock(t)
	orig := consultarSaldoSAP
	t.Cleanup(func() { consultarSaldoSAP = orig })
	consultarSaldoSAP = func(context.Context, sapodata.Config, string, string, string) (*sapodata.Saldo, error) {
		return nil, errors.New("tempo esgotado ao falar com o SAP (20 s)")
	}
	esperaBaseSaldo(mock, "true")
	esperaCodigos(mock, sCC1, "10026040")
	rec := httptest.NewRecorder()
	SaldoOrcamentoHandler(db)(rec, reqSaldo(t, `{"lancamentos":[{"lado":"origem","centro_custo_id":"`+sCC1+`","conta_id":"`+sConta+`","mes":"2026-03-01","valor":50}]}`))
	l := decSaldo(t, rec)
	if l[0].Status != "nao_identificado" || l[0].Excede || l[0].DispAcum != nil || !strings.Contains(l[0].Motivo, "SAP") {
		t.Errorf("SAP fora do ar deve virar não identificado, sem aviso de estouro: %+v", l[0])
	}
}

func TestSaldo_Desligado_EscondeSaldo(t *testing.T) {
	cacheSaldo = map[string]cacheSaldoItem{}
	db, mock := newSQLMock(t)
	orig := consultarSaldoSAP
	t.Cleanup(func() { consultarSaldoSAP = orig })
	consultarSaldoSAP = func(context.Context, sapodata.Config, string, string, string) (*sapodata.Saldo, error) {
		t.Error("não deveria consultar o SAP")
		return nil, nil
	}
	esperaBaseSaldo(mock, "false")
	rec := httptest.NewRecorder()
	SaldoOrcamentoHandler(db)(rec, reqSaldo(t, `{"lancamentos":[{"lado":"origem","centro_custo_id":"`+sCC1+`","conta_id":"`+sConta+`","mes":"2026-03-01","valor":50}]}`))
	var r struct {
		Ativo  bool  `json:"ativo"`
		Linhas []any `json:"linhas"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil || r.Ativo || len(r.Linhas) != 0 {
		t.Errorf("desligado deve devolver ativo=false e nenhuma linha: %s", rec.Body.String())
	}
}

func TestSaldo_CCAntigo_SemPermissao(t *testing.T) {
	cacheSaldo = map[string]cacheSaldoItem{}
	db, mock := newSQLMock(t)
	orig := consultarSaldoSAP
	t.Cleanup(func() { consultarSaldoSAP = orig })
	consultarSaldoSAP = func(context.Context, sapodata.Config, string, string, string) (*sapodata.Saldo, error) {
		return nil, errors.New("SAP fora do ar")
	}
	esperaBaseSaldo(mock, "true")
	esperaCodigos(mock, sCC1, "10026040")    // origem válida, SAP indisponível
	esperaCodigos(mock, sCC2, "CC-TESTE-01") // destino com código fora do padrão do SAP
	esperaCodigos(mock, sCC2, "10016000")    // origem de CC que NÃO é do usuário
	corpo := `{"lancamentos":[
	 {"lado":"origem","centro_custo_id":"` + sCC1 + `","conta_id":"` + sConta + `","mes":"2026-03-01","valor":50},
	 {"lado":"destino","centro_custo_id":"` + sCC2 + `","conta_id":"` + sConta + `","mes":"2026-03-01","valor":50},
	 {"lado":"origem","centro_custo_id":"` + sCC2 + `","conta_id":"` + sConta + `","mes":"2026-03-01","valor":50}]}`
	rec := httptest.NewRecorder()
	SaldoOrcamentoHandler(db)(rec, reqSaldo(t, corpo))
	l := decSaldo(t, rec)
	if !strings.Contains(l[0].Motivo, "SAP") || !strings.Contains(l[1].Motivo, "só para a origem") || !strings.Contains(l[2].Motivo, "sem permissão") {
		t.Errorf("motivos: %q | %q | %q", l[0].Motivo, l[1].Motivo, l[2].Motivo)
	}
	for _, x := range l {
		if x.Status != "nao_identificado" {
			t.Errorf("status: %+v", x)
		}
	}
}

func TestSaldo_ValidacaoDeEntrada(t *testing.T) {
	db, _ := newSQLMock(t)
	for _, corpo := range []string{`{"lancamentos":[]}`, `lixo`} {
		rec := httptest.NewRecorder()
		SaldoOrcamentoHandler(db)(rec, reqSaldo(t, corpo))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%q: esperado 400, veio %d", corpo, rec.Code)
		}
	}
}

// A consulta ao SAP usa o ANO da linha (gjahr): 2026 e 2027 em janeiro consultam anos diferentes.
func TestSaldo_UsaOAnoEscolhido(t *testing.T) {
	for _, ano := range []string{"2026", "2027"} {
		cacheSaldo = map[string]cacheSaldoItem{}
		db, mock := newSQLMock(t)
		orig := consultarSaldoSAP
		var recebido string
		consultarSaldoSAP = func(_ context.Context, _ sapodata.Config, _, _, gjahr string) (*sapodata.Saldo, error) {
			recebido = gjahr
			return &sapodata.Saldo{Encontrado: true}, nil
		}
		esperaBaseSaldo(mock, "true")
		esperaCodigos(mock, sCC1, "10026040")
		rec := httptest.NewRecorder()
		SaldoOrcamentoHandler(db)(rec, reqSaldo(t, `{"lancamentos":[{"lado":"origem","centro_custo_id":"`+sCC1+`","conta_id":"`+sConta+`","mes":"`+ano+`-01-01","valor":50}]}`))
		consultarSaldoSAP = orig
		if rec.Code != http.StatusOK || recebido != ano {
			t.Errorf("mês de %s: o SAP deveria ser consultado com gjahr=%s, recebeu %q (status %d)", ano, ano, recebido, rec.Code)
		}
	}
}

// Retorno da homologação (item 6): o aviso também compara a retirada do MÊS com o disponível do mês,
// mesmo quando o acumulado comporta (exemplo: R$ 8.000 contra R$ 7.700 no mês e R$ 409.700 acumulados).
func TestSaldo_AvisoDoMes_AcumuladoComporta(t *testing.T) {
	cacheSaldo = map[string]cacheSaldoItem{}
	db, mock := newSQLMock(t)
	orig := consultarSaldoSAP
	t.Cleanup(func() { consultarSaldoSAP = orig })
	consultarSaldoSAP = func(context.Context, sapodata.Config, string, string, string) (*sapodata.Saldo, error) {
		var mes, acum [12]float64
		mes[10], acum[10] = 7700, 409700
		return &sapodata.Saldo{Encontrado: true, Disponivel: mes, DispAcum: acum}, nil
	}
	esperaBaseSaldo(mock, "true")
	esperaCodigos(mock, sCC1, "10026040")
	rec := httptest.NewRecorder()
	SaldoOrcamentoHandler(db)(rec, reqSaldo(t, `{"lancamentos":[{"lado":"origem","centro_custo_id":"`+sCC1+`","conta_id":"`+sConta+`","mes":"2026-11-01","valor":8000}]}`))
	l := decSaldo(t, rec)
	if l[0].Excede || !l[0].ExcedeMes || *l[0].FaltaMes != 300 {
		t.Errorf("deveria avisar só pelo mês (falta 300): %+v", l[0])
	}
}
