package sapodata

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// Rota do gateway em PRD: a URL já é a da coleção; vai a apikey, não vai sap-client, e pagina por $skip.
func TestLerCentros_GatewayPaginaSemSapClient(t *testing.T) {
	var caminhos []string
	cfg := comServidor(t, func(w http.ResponseWriter, r *http.Request) {
		caminhos = append(caminhos, r.URL.Path)
		if r.Header.Get("apikey") != "chave" || r.URL.Query().Get("sap-client") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("$skip") == "0" {
			var b strings.Builder
			for i := 0; i < tamanhoPaginaCentros; i++ {
				if i > 0 {
					b.WriteString(",")
				}
				fmt.Fprintf(&b, `{"Centro":"%04d","Nome":"Centro %d","CNPJ":"10230480004209","Cidade":"RECIFE","Estado":"PE","GrupoContas":"CENT","Empresa":"FC01"}`, i, i)
			}
			fmt.Fprintf(w, `{"value":[%s]}`, b.String())
			return
		}
		fmt.Fprint(w, `{"value":[{"Centro":"9999","Nome":"Último","Estado":"BA"},{"Centro":"0001","Nome":"repetido"}]}`)
	})
	cfg.URLBase = strings.Replace(cfg.URLBase, "/sap/opu/odata/sap/API_COSTCENTER_SRV", "/sap/prd/centros/", 1)
	cfg.Usuario, cfg.Senha, cfg.APIKey, cfg.SapClient = "", "", "chave", "300"
	cs, err := LerCentros(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != tamanhoPaginaCentros+1 || cs[len(cs)-1].Codigo != "9999" || cs[1].UF != "PE" || cs[1].Empresa != "FC01" {
		t.Fatalf("centros: %d, último %+v", len(cs), cs[len(cs)-1])
	}
	for _, c := range caminhos {
		if c != "/sap/prd/centros" {
			t.Errorf("a URL da coleção não pode ganhar segmento extra: %s", c)
		}
	}
	if res := Sonda(context.Background(), Centros, cfg); !res.OK || res.Total != tamanhoPaginaCentros+1 {
		t.Errorf("sonda: %+v", res)
	}
}
