package exportacao

import "testing"

func TestFaturamentoPadrao_ComoNaReferencia(t *testing.T) {
	casos := []struct{ no, familia, despesa, quer string }{
		{"M2", "Antiga", DespesaExterna, "direto"},
		{"R1", "Nova", DespesaInterna, "direto"},
		{"A14", "Nova", DespesaExterna, "aquisicao_externa"},
		{"F1", "Anexo Central", DespesaInterna, "fl"},
		{"D1LOJA", "Fortaleza", DespesaExterna, "direto"},
		{"K1", "Filial", DespesaInterna, "diversos"},
	}
	for _, c := range casos {
		if got := FaturamentoPadrao(c.no, c.familia, c.despesa); got != c.quer {
			t.Errorf("%+v: %s", c, got)
		}
	}
	if DespesaDaClassificacao("fl") != DespesaInterna || DespesaDaClassificacao("inclusao") != DespesaExterna || NoDaEstrutura("A14.2") != "A14" {
		t.Error("despesa/nó")
	}
}
