package controleti

import (
	"os"
	"testing"
)

// Arquivos reais do Welton (não ficam no repositório): TI_DESPESAS_2026, TI_ORCAMENTO_2026, TI_DESPESAS_2027.
func TestLerDespesasReais(t *testing.T) {
	cam := os.Getenv("TI_DESPESAS_2026")
	if cam == "" {
		t.Skip("defina TI_DESPESAS_2026")
	}
	dados, _ := os.ReadFile(cam)
	if tipo, err := DetectarTipo(dados); err != nil || tipo != TipoDespesas {
		t.Fatalf("tipo = %q err=%v", tipo, err)
	}
	d, err := LerDespesas(dados)
	if err != nil {
		t.Fatal(err)
	}
	comBP, lancado, vLanc := 0, 0, 0.0
	for _, l := range d.Lancamentos {
		if l.BP != "" {
			comBP++
		}
		if l.Status == "LANÇADO" && len(l.Competencia) >= 7 && l.Competencia[:7] == "2026-10" {
			lancado++
			vLanc += l.Valor
		}
	}
	t.Logf("ano %d, %d lançamentos (%d com BP), parâmetros: %d CCs %d contas %d divisões", d.Ano, len(d.Lancamentos), comBP, len(d.Params.CC), len(d.Params.Conta), len(d.Params.Divisao))
	t.Logf("out/26 LANÇADO: %d linhas, R$ %.2f", lancado, vLanc)
	if d.Ano != 2026 || len(d.Lancamentos) != 1595 {
		t.Errorf("esperava 2026 com 1.595 lançamentos")
	}
	if lancado != 147 || vLanc < 4887114 || vLanc > 4887115 {
		t.Errorf("out/26: esperava 147 lançadas, R$ 4.887.114,77")
	}
}

func TestLerOrcamentoTIReal(t *testing.T) {
	cam := os.Getenv("TI_ORCAMENTO_2026")
	if cam == "" {
		t.Skip("defina TI_ORCAMENTO_2026")
	}
	dados, _ := os.ReadFile(cam)
	if tipo, err := DetectarTipo(dados); err != nil || tipo != TipoOrcamento {
		t.Fatalf("tipo = %q err=%v", tipo, err)
	}
	o, err := LerOrcamentoTI(dados)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ano %d, %d meses, %d linhas", o.Ano, len(o.Meses), len(o.Linhas))
	if o.Ano != 2026 || len(o.Meses) != 12 {
		t.Errorf("esperava 2026 com 12 meses")
	}
}

func TestDespesas2027ModeloVazio(t *testing.T) {
	cam := os.Getenv("TI_DESPESAS_2027")
	if cam == "" {
		t.Skip("defina TI_DESPESAS_2027")
	}
	dados, _ := os.ReadFile(cam)
	d, err := LerDespesas(dados)
	if err != nil {
		t.Fatal(err)
	}
	if d.Ano != 2027 || len(d.Lancamentos) != 0 {
		t.Errorf("modelo 2027 deveria vir sem lançamentos: ano %d, %d", d.Ano, len(d.Lancamentos))
	}
}
