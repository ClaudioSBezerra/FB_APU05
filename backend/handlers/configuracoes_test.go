package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"fb_apu05/crypto"
	"fb_apu05/internal/sapodata"

	"github.com/DATA-DOG/go-sqlmock"
)

const (
	urlCC = "https://vhfcodevci.sap.ferreiracosta.com:44300/sap/opu/odata/sap/API_COSTCENTER_SRV"
	urlGL = "https://vhfcodevci.sap.ferreiracosta.com:44300/sap/opu/odata/sap/API_GLACCOUNTINCHARTOFACCOUNTS_SRV"
)

func ptr[T any](v T) *T { return &v }

func TestValidarEntrada_SAP(t *testing.T) {
	novo := func() configuracoesEntrada {
		var in configuracoesEntrada
		in.SAP = &sapEntrada{}
		return in
	}
	// horário
	in := novo()
	in.SAP.Horario = ptr("25:00")
	if _, err := validarEntrada(in, nil); err == nil {
		t.Error("horário 25:00 deveria ser recusado")
	}
	in.SAP.Horario = ptr("06:00")
	if g, err := validarEntrada(in, nil); err != nil || g[cfgSapHorario] != "06:00" {
		t.Errorf("06:00 deveria valer: %v %v", g, err)
	}
	// senha é gravada cifrada e decifra de volta
	in = novo()
	in.SAP.Senha = ptr("S3nh@-do-SAP")
	g, err := validarEntrada(in, nil)
	if err != nil || g[cfgSapSenha] == "" || strings.Contains(g[cfgSapSenha], "S3nh@") {
		t.Fatalf("senha deveria sair cifrada: %q %v", g[cfgSapSenha], err)
	}
	if txt, err := crypto.DecryptField(g[cfgSapSenha]); err != nil || txt != "S3nh@-do-SAP" {
		t.Errorf("senha cifrada não decifra de volta: %q %v", txt, err)
	}
	// URL de outro domínio e de outro serviço
	in = novo()
	in.SAP.Servicos = map[string]struct {
		Ativo   *bool   `json:"ativo"`
		URLBase *string `json:"url_base"`
	}{"cc": {URLBase: ptr("https://evil.com/API_COSTCENTER_SRV")}}
	if _, err := validarEntrada(in, nil); err == nil {
		t.Error("URL de outro domínio deveria ser recusada")
	}
	in.SAP.Servicos = map[string]struct {
		Ativo   *bool   `json:"ativo"`
		URLBase *string `json:"url_base"`
	}{"gl": {URLBase: ptr(urlCC)}}
	if _, err := validarEntrada(in, nil); err == nil {
		t.Error("URL de centros de custo não pode valer para contas")
	}
	in.SAP.Servicos = map[string]struct {
		Ativo   *bool   `json:"ativo"`
		URLBase *string `json:"url_base"`
	}{"xx": {Ativo: ptr(false)}}
	if _, err := validarEntrada(in, nil); err == nil {
		t.Error("serviço desconhecido deveria ser recusado")
	}
	// ativar sem credencial: recusa; com credencial já gravada: aceita
	in.SAP.Servicos = map[string]struct {
		Ativo   *bool   `json:"ativo"`
		URLBase *string `json:"url_base"`
	}{"gl": {Ativo: ptr(true)}}
	atual := map[string]string{cfgServico(sapodata.ContasContabeis, "url_base"): urlGL}
	if _, err := validarEntrada(in, atual); err == nil {
		t.Error("ativar sem usuário/senha deveria ser recusado")
	}
	atual[cfgSapUsuario], atual[cfgSapSenha] = "svc_orcamento", "cifrada"
	if g, err := validarEntrada(in, atual); err != nil || g["sap_gl_ativo"] != "true" {
		t.Errorf("ativar com credencial deveria valer: %v %v", g, err)
	}
	// limpar a senha e ativar na mesma chamada: recusa
	in.SAP.LimparSenha = true
	if _, err := validarEntrada(in, atual); err == nil {
		t.Error("limpar a senha e ativar juntos deveria ser recusado")
	}
}

func TestSapDevido(t *testing.T) {
	utc := func(h, m int) time.Time { return time.Date(2026, 11, 18, h, m, 0, 0, time.UTC) } // Recife = UTC-3
	casos := []struct {
		nome, horario, ultima string
		agora                 time.Time
		quer                  bool
	}{
		{"antes do horário", "06:00", "", utc(8, 59), false},      // 05:59 em Recife
		{"no horário, nunca rodou", "06:00", "", utc(9, 0), true}, // 06:00 em Recife
		{"depois, já rodou hoje", "06:00", utc(9, 1).Format(time.RFC3339), utc(15, 0), false},
		{"depois, rodou ontem", "06:00", utc(9, 1).AddDate(0, 0, -1).Format(time.RFC3339), utc(9, 5), true},
		{"sistema voltou à tarde", "06:00", utc(9, 1).AddDate(0, 0, -1).Format(time.RFC3339), utc(20, 0), true},
		{"horário inválido", "6h", "", utc(20, 0), false},
		{"última execução ilegível", "06:00", "lixo", utc(10, 0), true},
	}
	for _, c := range casos {
		if got := sapDevido(c.horario, c.ultima, c.agora); got != c.quer {
			t.Errorf("%s: esperado %v, obtido %v", c.nome, c.quer, got)
		}
	}
}

func linhasConfig(pares ...string) *sqlmock.Rows {
	r := sqlmock.NewRows([]string{"chave", "valor"})
	for i := 0; i+1 < len(pares); i += 2 {
		r.AddRow(pares[i], pares[i+1])
	}
	return r
}

func TestConfiguracoesGET_NaoDevolveSenha(t *testing.T) {
	db, mock := newSQLMock(t)
	cifrada, _ := crypto.EncryptField("senha-secreta-123")
	mock.ExpectQuery(regexp.QuoteMeta("SELECT chave, valor FROM configuracoes")).WillReturnRows(linhasConfig(
		cfgSapHorario, "06:00", cfgSapUsuario, "svc_orcamento", cfgSapSenha, cifrada,
		"sap_cc_ativo", "true", "sap_cc_url_base", urlCC, "sap_gl_ativo", "false", "sap_gl_url_base", urlGL,
		"sap_cc_ultimo_resultado", `{"ok":true,"total":2731,"mensagem":"conexão ok","duracao_ms":120,"origem":"agendada"}`))
	rec := httptest.NewRecorder()
	ConfiguracoesHandler(db)(rec, httptest.NewRequest(http.MethodGet, "/api/admin/configuracoes", nil))
	corpo := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(corpo, `"senha_definida":true`) || !strings.Contains(corpo, `"total":2731`) {
		t.Fatalf("resposta inesperada: %d %s", rec.Code, corpo)
	}
	if strings.Contains(corpo, cifrada) || strings.Contains(corpo, "senha-secreta") {
		t.Fatalf("a senha (ou o texto cifrado) vazou na resposta: %s", corpo)
	}
	if strings.Contains(corpo, "promocao") {
		t.Errorf("o parâmetro de promoção não existe mais: %s", corpo)
	}
}

func TestSAPTestar_UsaConfiguracaoGravadaEGuardaResultado(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.MatchExpectationsInOrder(false)
	cifrada, _ := crypto.EncryptField("senha-do-sap")
	mock.ExpectQuery(regexp.QuoteMeta("SELECT chave, valor FROM configuracoes")).WillReturnRows(linhasConfig(
		cfgSapUsuario, "svc_orcamento", cfgSapSenha, cifrada, "sap_gl_url_base", urlGL))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE configuracoes SET valor")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE configuracoes SET valor")).WillReturnResult(sqlmock.NewResult(0, 1))

	orig := sondaSAP
	t.Cleanup(func() { sondaSAP = orig })
	var visto sapodata.Config
	var servico string
	sondaSAP = func(_ context.Context, sv sapodata.Servico, c sapodata.Config) sapodata.Resultado {
		visto, servico = c, sv.Chave
		return sapodata.Resultado{OK: true, Total: 8120, Mensagem: "conexão ok"}
	}
	delete(sondaUltimoUso, "gl")

	req := httptest.NewRequest(http.MethodPost, "/api/admin/configuracoes/sap/gl/testar", nil)
	req.SetPathValue("servico", "gl")
	rec := httptest.NewRecorder()
	SAPTestarHandler(db)(rec, req)
	if rec.Code != 200 || servico != "gl" {
		t.Fatalf("esperado 200 para gl: %d %s", rec.Code, rec.Body.String())
	}
	if visto.Senha != "senha-do-sap" || visto.Usuario != "svc_orcamento" || visto.URLBase != urlGL {
		t.Errorf("a sonda não recebeu a configuração gravada: %+v", visto)
	}
	if strings.Contains(rec.Body.String(), "senha-do-sap") {
		t.Error("a resposta não pode conter a senha")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
	// repetir logo em seguida: trava de 5 s
	req2 := httptest.NewRequest(http.MethodPost, "/x", nil)
	req2.SetPathValue("servico", "gl")
	rec2 := httptest.NewRecorder()
	SAPTestarHandler(db)(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("segundo teste imediato deveria dar 429, deu %d", rec2.Code)
	}
	// serviço desconhecido
	req3 := httptest.NewRequest(http.MethodPost, "/x", nil)
	req3.SetPathValue("servico", "zz")
	rec3 := httptest.NewRecorder()
	SAPTestarHandler(db)(rec3, req3)
	if rec3.Code != http.StatusNotFound {
		t.Errorf("serviço desconhecido deveria dar 404, deu %d", rec3.Code)
	}
}

func TestRotinaSAPDiaria_SoRodaOQueEstaDevidoEAtivo(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.MatchExpectationsInOrder(false)
	cifrada, _ := crypto.EncryptField("s")
	agora := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC) // 07:00 em Recife
	mock.ExpectQuery(regexp.QuoteMeta("SELECT chave, valor FROM configuracoes")).WillReturnRows(linhasConfig(
		cfgSapHorario, "06:00", cfgSapUsuario, "u", cfgSapSenha, cifrada,
		"sap_cc_ativo", "true", "sap_cc_url_base", urlCC, "sap_cc_ultima_execucao", "",
		"sap_gl_ativo", "false", "sap_gl_url_base", urlGL)) // gl desligado: não roda
	mock.ExpectExec(regexp.QuoteMeta("UPDATE configuracoes SET valor")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE configuracoes SET valor")).WillReturnResult(sqlmock.NewResult(0, 1))

	orig := sondaSAP
	t.Cleanup(func() { sondaSAP = orig })
	var rodou []string
	sondaSAP = func(_ context.Context, sv sapodata.Servico, _ sapodata.Config) sapodata.Resultado {
		rodou = append(rodou, sv.Chave)
		return sapodata.Resultado{OK: true, Total: 1}
	}
	RotinaSAPDiaria(db, agora)
	if len(rodou) != 1 || rodou[0] != "cc" {
		t.Fatalf("deveria rodar só o cc: %v", rodou)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestValidarEntrada_Gateway(t *testing.T) {
	const urlGW = "http://10.131.1.80:8000/sap/qas/cost-center-read/"
	// http de IP privado só vale com apikey (sem ela é o modo direto: https + domínio FC)
	in := configuracoesEntrada{SAP: &sapEntrada{Servicos: map[string]struct {
		Ativo   *bool   `json:"ativo"`
		URLBase *string `json:"url_base"`
	}{"cc": {URLBase: ptr(urlGW)}}}}
	if _, err := validarEntrada(in, nil); err == nil {
		t.Error("URL do gateway sem apikey deveria ser recusada")
	}
	in.SAP.ApiKey = ptr("chave-de-teste-123")
	in.SAP.SapClient = ptr("300")
	grava, err := validarEntrada(in, nil)
	if err != nil {
		t.Fatalf("com apikey a URL do gateway deveria valer: %v", err)
	}
	if grava[cfgSapApiKey] == "" || grava[cfgSapApiKey] == "chave-de-teste-123" {
		t.Errorf("a apikey deve ser gravada criptografada: %q", grava[cfgSapApiKey])
	}
	if grava[cfgSapClient] != "300" || grava[cfgServico(sapodata.CentrosCusto, "url_base")] != urlGW {
		t.Errorf("gravação inesperada: %v", grava)
	}
	// apikey já gravada (sem reenviar) também vale para a URL
	in2 := configuracoesEntrada{SAP: &sapEntrada{Servicos: in.SAP.Servicos}}
	if _, err := validarEntrada(in2, map[string]string{cfgSapApiKey: "cifrada"}); err != nil {
		t.Errorf("apikey já gravada deveria bastar: %v", err)
	}
	// ativar exige credencial
	ativo := configuracoesEntrada{SAP: &sapEntrada{Servicos: map[string]struct {
		Ativo   *bool   `json:"ativo"`
		URLBase *string `json:"url_base"`
	}{"cc": {Ativo: ptr(true)}}}}
	if _, err := validarEntrada(ativo, map[string]string{cfgServico(sapodata.CentrosCusto, "url_base"): urlCC}); err == nil {
		t.Error("ativar sem credencial deveria ser recusado")
	}
	// apikey ruim e sap-client ruim
	for _, bad := range []sapEntrada{{ApiKey: ptr("curta")}, {ApiKey: ptr("com espaço aqui")}, {SapClient: ptr("30x")}} {
		b := bad
		if _, err := validarEntrada(configuracoesEntrada{SAP: &b}, nil); err == nil {
			t.Errorf("deveria recusar %+v", bad)
		}
	}
	// limpar
	grava, _ = validarEntrada(configuracoesEntrada{SAP: &sapEntrada{LimparApiKey: true}}, nil)
	if v, ok := grava[cfgSapApiKey]; !ok || v != "" {
		t.Errorf("limpar_apikey deveria zerar: %v", grava)
	}
}
