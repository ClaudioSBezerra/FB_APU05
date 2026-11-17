package sapodata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestValidarURLBase(t *testing.T) {
	bons := []string{
		"https://vhfcodevci.sap.ferreiracosta.com:44300/sap/opu/odata/sap/API_COSTCENTER_SRV",
		"https://sap.ferreiracosta.com.br/sap/opu/odata/sap/API_COSTCENTER_SRV/",
	}
	for _, u := range bons {
		if err := ValidarURLBase(u, CentrosCusto); err != nil {
			t.Errorf("%q deveria valer: %v", u, err)
		}
	}
	ruins := map[string]string{
		"http://vhfcodevci.sap.ferreiracosta.com:44300/sap/opu/odata/sap/API_COSTCENTER_SRV": "https",
		"https://172.25.22.207:44300/sap/opu/odata/sap/API_COSTCENTER_SRV":                   "IP",
		"https://evil.com/sap/opu/odata/sap/API_COSTCENTER_SRV":                              "domínio",
		"https://ferreiracosta.com.evil.com/API_COSTCENTER_SRV":                              "domínio",
		"https://x.ferreiracosta.com/API_COSTCENTER_SRV?a=1":                                 "parâmetros",
		"https://user:pass@x.ferreiracosta.com/API_COSTCENTER_SRV":                           "usuário",
		"https://x.ferreiracosta.com/sap/outro/servico":                                      "API_COSTCENTER_SRV",
		"": "inválida",
	}
	for u, dica := range ruins {
		if err := ValidarURLBase(u, CentrosCusto); err == nil {
			t.Errorf("%q deveria ser recusada", u)
		} else if !strings.Contains(err.Error(), dica) {
			t.Logf("%q recusada com: %v", u, err)
		}
	}
}

// servidor de teste no lugar do SAP (HTTPS com certificado próprio; sem a trava de domínio)
func comServidor(t *testing.T, h http.HandlerFunc) Config {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	origV, origC := validarURL, novoCliente
	validarURL = func(string, Servico) error { return nil }
	novoCliente = func() *http.Client {
		c := srv.Client()
		c.Timeout = 5 * time.Second
		c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		return c
	}
	t.Cleanup(func() { validarURL, novoCliente = origV, origC })
	return Config{URLBase: srv.URL + "/sap/opu/odata/sap/API_COSTCENTER_SRV", Usuario: "svc_orcamento", Senha: "segredo-super-secreto"}
}

func TestSonda_Sucesso(t *testing.T) {
	var path, auth, accept string
	cfg := comServidor(t, func(w http.ResponseWriter, r *http.Request) {
		path, accept = r.URL.RequestURI(), r.Header.Get("Accept")
		u, p, _ := r.BasicAuth()
		auth = u + ":" + p
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"d":{"__count":"2731","results":[{"ControllingArea":"A000","CostCenter":"0000001000"}]}}`))
	})
	r := Sonda(context.Background(), CentrosCusto, cfg)
	if !r.OK || r.Total != 2731 {
		t.Fatalf("esperava ok com 2731, veio %+v", r)
	}
	if !strings.Contains(path, "A_CostCenter?$top=1&$inlinecount=allpages") || accept != "application/json" || auth != "svc_orcamento:segredo-super-secreto" {
		t.Errorf("requisição inesperada: path=%q accept=%q auth=%q", path, accept, auth)
	}
}

func TestSonda_ErrosDeHTTPNaoVazamCredencial(t *testing.T) {
	for _, c := range []struct {
		status int
		trecho string
	}{{401, "usuário/senha"}, {403, "arranjo de comunicação"}, {404, "não encontrado"}, {500, "HTTP 500"}} {
		cfg := comServidor(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			_, _ = w.Write([]byte("erro interno com segredo-super-secreto"))
		})
		r := Sonda(context.Background(), CentrosCusto, cfg)
		if r.OK || !strings.Contains(r.Mensagem, c.trecho) {
			t.Errorf("status %d: %+v", c.status, r)
		}
		if strings.Contains(r.Mensagem, "segredo-super-secreto") || strings.Contains(r.Mensagem, "svc_orcamento") {
			t.Errorf("mensagem vaza credencial/corpo: %q", r.Mensagem)
		}
	}
}

func TestSonda_NaoSegueRedirecionamento(t *testing.T) {
	var chegouNoOutro int32
	outro := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&chegouNoOutro, 1)
	}))
	defer outro.Close()
	cfg := comServidor(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, outro.URL+"/roubo", http.StatusFound)
	})
	r := Sonda(context.Background(), CentrosCusto, cfg)
	if r.OK || !strings.Contains(r.Mensagem, "redirecionamento") {
		t.Fatalf("esperava recusa do redirecionamento: %+v", r)
	}
	if atomic.LoadInt32(&chegouNoOutro) != 0 {
		t.Fatal("a requisição (com a credencial) foi seguida para outro host")
	}
}

func TestSonda_RespostaInesperadaESemCredencial(t *testing.T) {
	cfg := comServidor(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<xml/>")) })
	if r := Sonda(context.Background(), CentrosCusto, cfg); r.OK || !strings.Contains(r.Mensagem, "formato inesperado") {
		t.Errorf("xml: %+v", r)
	}
	cfg.Senha = ""
	if r := Sonda(context.Background(), CentrosCusto, cfg); r.OK || !strings.Contains(r.Mensagem, "ainda não foram informados") {
		t.Errorf("sem senha: %+v", r)
	}
}

func TestSonda_URLForaDaListaNaoEhChamada(t *testing.T) {
	r := Sonda(context.Background(), CentrosCusto, Config{URLBase: "https://evil.com/API_COSTCENTER_SRV", Usuario: "u", Senha: "s"})
	if r.OK || !strings.Contains(r.Mensagem, "domínio") {
		t.Fatalf("deveria recusar o domínio: %+v", r)
	}
}

func TestContasContabeis_ServicoEPath(t *testing.T) {
	var path string
	cfg := comServidor(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.RequestURI()
		_, _ = w.Write([]byte(`{"d":{"__count":"8120","results":[]}}`))
	})
	r := Sonda(context.Background(), ContasContabeis, cfg)
	if !r.OK || r.Total != 8120 || !strings.Contains(r.Mensagem, "contas contábeis") {
		t.Fatalf("esperava ok com 8120 contas: %+v", r)
	}
	if !strings.Contains(path, "A_GLAccountInChartOfAccounts?$top=1&$inlinecount=allpages&$select=ChartOfAccounts,GLAccount") {
		t.Errorf("entidade/seleção inesperadas: %q", path)
	}
}

func TestValidarURLBase_CadaServicoExigeOSeuSegmento(t *testing.T) {
	gl := "https://vhfcodevci.sap.ferreiracosta.com:44300/sap/opu/odata/sap/API_GLACCOUNTINCHARTOFACCOUNTS_SRV"
	cc := "https://vhfcodevci.sap.ferreiracosta.com:44300/sap/opu/odata/sap/API_COSTCENTER_SRV"
	if err := ValidarURLBase(gl, ContasContabeis); err != nil {
		t.Errorf("URL de contas deveria valer: %v", err)
	}
	if err := ValidarURLBase(cc, ContasContabeis); err == nil {
		t.Error("URL de centros de custo não pode valer para o serviço de contas")
	}
	if err := ValidarURLBase(gl, CentrosCusto); err == nil {
		t.Error("URL de contas não pode valer para o serviço de centros de custo")
	}
	if sv, ok := PorChave("gl"); !ok || sv.Entidade != "A_GLAccountInChartOfAccounts" {
		t.Errorf("PorChave(gl): %+v %v", sv, ok)
	}
	if _, ok := PorChave("xx"); ok {
		t.Error("chave desconhecida não pode existir")
	}
}
