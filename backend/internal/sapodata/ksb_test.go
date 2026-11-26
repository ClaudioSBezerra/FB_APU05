package sapodata

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// KSB1 pela raiz do serviço: acrescenta a entidade, filtra área/data/CCs (sem zeros à esquerda), pagina e divide os
// CCs em lotes.
func TestLerRealizado_FiltraPaginaELotes(t *testing.T) {
	var filtros []string
	cfg := comServidor(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/0001/CostCenterActual") {
			t.Errorf("caminho: %s", r.URL.Path)
		}
		f := r.URL.Query().Get("$filter")
		filtros = append(filtros, f)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("$skip") == "0" && strings.Contains(f, "'10016000'") {
			var b strings.Builder
			for i := 0; i < paginaKSB; i++ {
				if i > 0 {
					b.WriteString(",")
				}
				fmt.Fprintf(&b, `{"PurchaseOrder":"45%08d","CostCenter":"10016000","AmountControllingCurrency":1.5,"PostingDate":"2026-05-12"}`, i)
			}
			fmt.Fprintf(w, `{"value":[%s]}`, b.String())
			return
		}
		fmt.Fprint(w, `{"value":[{"PurchaseOrder":"","CostCenter":"906080","AmountControllingCurrency":2.7,"PostingDate":"2026-01-28"}]}`)
	})
	cfg.URLBase = strings.Replace(cfg.URLBase, "/sap/opu/odata/sap/API_COSTCENTER_SRV", "/sap/opu/odata4/sap/zsb_cc_actual/srvd_a2x/sap/zsd_cc_actual/0001", 1)
	ccs := make([]string, 0, 45)
	ccs = append(ccs, "10016000", "0906080")
	for i := 0; i < 43; i++ {
		ccs = append(ccs, fmt.Sprintf("2016%04d", i))
	}
	de, ate := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC)
	out, err := LerRealizado(context.Background(), cfg, ccs, de, ate)
	if err != nil {
		t.Fatal(err)
	}
	// lote 1 (40 CCs): 1000 + 1 na 2ª página; lote 2 (5 CCs): 1
	if len(out) != paginaKSB+2 {
		t.Fatalf("linhas: %d (filtros: %d)", len(out), len(filtros))
	}
	if !strings.Contains(filtros[0], "ControllingArea eq 'FC01' and PostingDate ge 2026-01-01 and PostingDate le 2026-11-26") ||
		!strings.Contains(filtros[0], "'906080'") || strings.Contains(filtros[0], "'0906080'") {
		t.Errorf("filtro: %s", filtros[0])
	}
	if len(filtros) != 3 {
		t.Errorf("chamadas: %d", len(filtros))
	}
}

// Rota do gateway que já aponta para a coleção: nada é acrescentado ao caminho.
func TestURLKSB_ColecaoNaoDuplica(t *testing.T) {
	u := urlKSB(Config{URLBase: "http://10.131.1.80:8000/sap/qas/CostCenterCommitment/"}, Compromissado, map[string][]string{"$top": {"1"}})
	if !strings.HasPrefix(u, "http://10.131.1.80:8000/sap/qas/CostCenterCommitment?") {
		t.Fatal(u)
	}
	u = urlKSB(Config{URLBase: "http://10.131.1.80:8000/sap/qas/cc-commitment"}, Compromissado, map[string][]string{})
	if !strings.HasPrefix(u, "http://10.131.1.80:8000/sap/qas/cc-commitment/CostCenterCommitment?") {
		t.Fatal(u)
	}
}
