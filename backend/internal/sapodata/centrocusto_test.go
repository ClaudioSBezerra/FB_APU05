package sapodata

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParseDataOData(t *testing.T) {
	if g := ParseDataOData(`/Date(1704067200000)/`); !g.Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("data errada: %v", g)
	}
	for _, ruim := range []string{"", "x", "/Date()/"} {
		if !ParseDataOData(ruim).IsZero() {
			t.Errorf("%q deveria virar zero", ruim)
		}
	}
}

func TestValidarURLGateway(t *testing.T) {
	t.Setenv("SAP_API_KEY", "k")
	for _, u := range []string{
		"http://10.131.1.80:8000/sap/qas/cost-center-read/",
		"http://sap-api-gateway.ferreiracosta.com:8000/sap/qas/cost-center-read",
	} {
		if err := ValidarURLBase(u, CentrosCusto); err != nil {
			t.Errorf("%q deveria valer: %v", u, err)
		}
	}
	for _, u := range []string{
		"http://8.8.8.8:8000/sap/x",           // IP público
		"http://evil.com/sap/x",               // domínio de fora
		"ftp://10.1.1.1/sap/x",                // esquema
		"http://u:p@10.1.1.1/sap/x",           // credencial embutida
		"http://10.1.1.1:8000/",               // sem rota
		"http://10.1.1.1:8000/sap/x?apikey=1", // parâmetros
	} {
		if err := ValidarURLBase(u, CentrosCusto); err == nil {
			t.Errorf("%q deveria ser recusada", u)
		}
	}
}

func pagina(n, ini int, bloco string) string {
	var b strings.Builder
	b.WriteString(`{"d":{"results":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, bloco, ini+i)
	}
	b.WriteString(`]}}`)
	return b.String()
}

func TestLerCentrosCusto_PaginaENormaliza(t *testing.T) {
	var chamadas []string
	var apikey string
	cfg := comServidor(t, func(w http.ResponseWriter, r *http.Request) {
		chamadas = append(chamadas, r.URL.RawQuery)
		apikey = r.Header.Get("apikey")
		skip := r.URL.Query().Get("$skip")
		if skip == "0" {
			_, _ = w.Write([]byte(pagina(tamanhoPagina, 0,
				`{"ControllingArea":"FC01","CostCenter":"%d","ValidityStartDate":"/Date(1704067200000)/","BusinessArea":"0081","to_Text":{"results":[{"Language":"PT","CostCenterName":"Curto","CostCenterDescription":"DESCRICAO %[1]d"}]}}`)))
			return
		}
		// 2ª página: repete o código 0 com vigência mais nova e traz um sem texto em PT
		_, _ = w.Write([]byte(`{"d":{"results":[
		 {"ControllingArea":"FC01","CostCenter":"0","ValidityStartDate":"/Date(1735689600000)/","BusinessArea":"0090","to_Text":{"results":[{"Language":"PT","CostCenterName":"Novo","CostCenterDescription":""}]}},
		 {"ControllingArea":"FC01","CostCenter":" 999 ","ValidityStartDate":"/Date(1704067200000)/","BusinessArea":"0081","to_Text":{"results":[{"Language":"EN","CostCenterName":"English","CostCenterDescription":""}]}},
		 {"ControllingArea":"FC01","CostCenter":"","BusinessArea":"0081"}]}}`))
	})
	cfg.APIKey, cfg.SapClient, cfg.Usuario, cfg.Senha = "chave-x", "300", "", ""

	ccs, err := LerCentrosCusto(context.Background(), cfg, time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(chamadas) != 2 {
		t.Fatalf("esperava 2 páginas, veio %d", len(chamadas))
	}
	if apikey != "chave-x" {
		t.Errorf("header apikey = %q", apikey)
	}
	q := chamadas[0]
	for _, esperado := range []string{"sap-client=300", "%24top=500", "%24filter=ValidityEndDate%20ge%20datetime", "%24expand=to_Text"} {
		if !strings.Contains(q, esperado) {
			t.Errorf("query sem %q: %s", esperado, q)
		}
	}
	if strings.Contains(q, "+") {
		t.Errorf("espaço deve ir como %%20: %s", q)
	}
	if len(ccs) != tamanhoPagina+1 { // 500 + o "999"; o "" é descartado; o "0" não duplica
		t.Fatalf("esperava %d, veio %d", tamanhoPagina+1, len(ccs))
	}
	if ccs[0].Codigo != "0" || ccs[0].Nome != "Novo" || ccs[0].AreaNegocio != "0090" {
		t.Errorf("vigência mais nova deveria vencer: %+v", ccs[0])
	}
	if ccs[1].Nome != "DESCRICAO 1" {
		t.Errorf("descrição PT deveria vencer o nome curto: %+v", ccs[1])
	}
	if u := ccs[len(ccs)-1]; u.Codigo != "999" || u.Nome != "English" {
		t.Errorf("sem PT cai para outro idioma e o código é aparado: %+v", u)
	}
}

func TestLerCentrosCusto_ErroHTTP(t *testing.T) {
	cfg := comServidor(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	cfg.APIKey = "k"
	if _, err := LerCentrosCusto(context.Background(), cfg, time.Now()); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("esperava erro 401, veio %v", err)
	}
}

func TestSonda_ModoGatewayUsaApikeyENaoBasic(t *testing.T) {
	var basic bool
	var key, query string
	cfg := comServidor(t, func(w http.ResponseWriter, r *http.Request) {
		_, _, basic = r.BasicAuth()
		key, query = r.Header.Get("apikey"), r.URL.RawQuery
		_, _ = w.Write([]byte(`{"d":{"__count":"4716","results":[]}}`))
	})
	cfg.APIKey, cfg.SapClient, cfg.Usuario, cfg.Senha = "chave-x", "300", "", ""
	r := Sonda(context.Background(), CentrosCusto, cfg)
	if !r.OK || r.Total != 4716 || basic || key != "chave-x" || !strings.Contains(query, "sap-client=300") {
		t.Fatalf("gateway: %+v basic=%v key=%q q=%s", r, basic, key, query)
	}
}

func TestLerContas_FiltraPlanoEUsaNomePT(t *testing.T) {
	var query string
	cfg := comServidor(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"d":{"results":[
		 {"ChartOfAccounts":"0050","GLAccount":"4110100005","IsProfitLossAccount":true,"to_Text":{"results":[{"Language":"EN","GLAccountName":"Freight","GLAccountLongName":""},{"Language":"PT","GLAccountName":"Frete","GLAccountLongName":"Custo Frete Revenda"}]}},
		 {"ChartOfAccounts":"0050","GLAccount":"4110100005","IsProfitLossAccount":true,"to_Text":{"results":[]}},
		 {"ChartOfAccounts":"0050","GLAccount":" 1110100001 ","IsProfitLossAccount":false,"to_Text":{"results":[{"Language":"PT","GLAccountName":"Caixa","GLAccountLongName":""}]}}]}}`))
	})
	cfg.APIKey = "k"
	cs, err := LerContas(context.Background(), cfg, "0050")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 || cs[0].Nome != "Custo Frete Revenda" || cs[1].Codigo != "1110100001" || cs[1].Nome != "Caixa" || cs[1].ResultadoP {
		t.Fatalf("leitura inesperada: %+v", cs)
	}
	for _, esperado := range []string{"ChartOfAccounts%20eq%20%270050%27", "AccountIsMarkedForDeletion%20eq%20false", "AccountIsBlockedForPosting%20eq%20false"} {
		if !strings.Contains(query, esperado) {
			t.Errorf("filtro sem %q: %s", esperado, query)
		}
	}
	if _, err := LerContas(context.Background(), cfg, "0050' or '1'='1"); err == nil {
		t.Error("plano com aspas deveria ser recusado")
	}
}
