package sapodata

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestObjnrCentroCusto(t *testing.T) {
	for in, quer := range map[string]string{"10026040": "KSFC010010026040", "816000": "KSFC010000816000", "6040": "KSFC010000006040"} {
		if got, err := ObjnrCentroCusto(in); err != nil || got != quer {
			t.Errorf("%s → %q (%v), esperava %q", in, got, err, quer)
		}
	}
	for _, ruim := range []string{"", "CC-TESTE-01", "12345678901", "1002 6040", "10026040' or '1'='1"} {
		if _, err := ObjnrCentroCusto(ruim); err == nil {
			t.Errorf("%q deveria ser recusado", ruim)
		}
	}
}

const respostaSaldo = `{"@odata.context":"$metadata#Trava_orcamentaria","@odata.count":1,"value":[{"objnr":"KSFC010010026040","kstar":"4210100001","gjahr":"2026",
 "disp_acum1":21451.62,"disp_acum2":"42903.24","plan1":21451.62,"plan2":21451.62,"real1":0.00,"real2":100.5,"comp1":0,"comp2":0,"disposto1":0,"disposto2":100.5,"disponivel1":21451.62,"disponivel2":21351.12}]}`

func TestConsultarSaldo_V4(t *testing.T) {
	var query, auth, accept string
	cfg := comServidor(t, func(w http.ResponseWriter, r *http.Request) {
		query, accept = r.URL.RawQuery, r.Header.Get("Accept")
		u, p, _ := r.BasicAuth()
		auth = u + ":" + p
		_, _ = w.Write([]byte(respostaSaldo))
	})
	cfg.URLBase = strings.Replace(cfg.URLBase, "API_COSTCENTER_SRV", "zsd_trava_orcamentaria/0001", 1)
	s, err := ConsultarSaldo(context.Background(), cfg, "KSFC010010026040", "4210100001", "2026")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Encontrado || s.DispAcum[0] != 21451.62 || s.DispAcum[1] != 42903.24 || s.Real[1] != 100.5 || s.Disponivel[1] != 21351.12 {
		t.Errorf("leitura: %+v", s)
	}
	for _, q := range []string{"%24filter=objnr%20eq%20%27KSFC010010026040%27%20and%20kstar%20eq%20%274210100001%27%20and%20gjahr%20eq%20%272026%27"} {
		if !strings.Contains(query, q) {
			t.Errorf("filtro com as 3 chaves: %s", query)
		}
	}
	if accept != "application/json" || auth != "svc_orcamento:segredo-super-secreto" {
		t.Errorf("accept=%q auth=%q", accept, auth)
	}
}

func TestConsultarSaldo_VazioE404E401(t *testing.T) {
	status := 200
	corpo := `{"@odata.count":0,"value":[]}`
	cfg := comServidor(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status); _, _ = w.Write([]byte(corpo)) })
	cfg.URLBase = strings.Replace(cfg.URLBase, "API_COSTCENTER_SRV", "zsd_trava_orcamentaria/0001", 1)
	s, err := ConsultarSaldo(context.Background(), cfg, "KSFC010010026040", "4210100001", "2026")
	if err != nil || s.Encontrado {
		t.Fatalf("value vazio = sem lançamentos, sem erro: %+v %v", s, err)
	}
	for st, trecho := range map[int]string{401: "401", 403: "403", 404: "404", 504: "504", 500: "500"} {
		status = st
		if _, err := ConsultarSaldo(context.Background(), cfg, "KSFC010010026040", "4210100001", "2026"); err == nil || !strings.Contains(err.Error(), trecho) {
			t.Errorf("HTTP %d: erro %v", st, err)
		}
	}
}

func TestConsultarSaldo_RecusaChaveForaDoPadrao(t *testing.T) {
	cfg := Config{URLBase: "https://x.ferreiracosta.com/sap/zsd_trava_orcamentaria/0001", Usuario: "u", Senha: "s"}
	for _, c := range [][3]string{{"KS1' or '1'='1", "4210100001", "2026"}, {"KSFC010010026040", "42101", "2026"}, {"KSFC010010026040", "4210100001", "26"}} {
		if _, err := ConsultarSaldo(context.Background(), cfg, c[0], c[1], c[2]); err == nil || !strings.Contains(err.Error(), "inválida") {
			t.Errorf("%v deveria ser recusada: %v", c, err)
		}
	}
}

func TestSondaSaldo(t *testing.T) {
	cfg := comServidor(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"value":[]}`)) })
	cfg.URLBase = strings.Replace(cfg.URLBase, "API_COSTCENTER_SRV", "zsd_trava_orcamentaria/0001", 1)
	if r := Sonda(context.Background(), SaldoOrcamentario, cfg); !r.OK {
		t.Fatalf("sonda: %+v", r)
	}
}

func TestLerSaldoV4FiltraVersaoDoPlano(t *testing.T) {
	corpo := []byte(`{"value":[
	 {"objnr":"X","kstar":"1","gjahr":"2026","versn":"000","plan1":"100.00","real1":"10.00","comp1":"0","disposto1":"10","disponivel1":"90","disp_acum1":"90"},
	 {"objnr":"X","kstar":"1","gjahr":"2026","versn":"001","plan1":"500.00","real1":"0","comp1":"0","disposto1":"0","disponivel1":"500","disp_acum1":"500"}]}`)
	s, err := lerSaldoV4(corpo)
	if err != nil || !s.Encontrado {
		t.Fatalf("esperava encontrado, err=%v", err)
	}
	if s.Plan[0] != 100 || s.DispAcum[0] != 90 {
		t.Fatalf("versão 001 não deveria somar: plan=%v disp=%v", s.Plan[0], s.DispAcum[0])
	}
	s, _ = lerSaldoV4([]byte(`{"value":[{"versn":"001","plan1":"5"}]}`))
	if s.Encontrado {
		t.Fatal("sem a versão configurada deve ser 'sem lançamentos'")
	}
}
