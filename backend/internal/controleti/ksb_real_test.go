package controleti

// Leitura das exportações reais do SAP (KSB1/KSB2). Só roda com
// TI_KSB1/TI_KSB2 apontando os .xlsx — os arquivos têm dados financeiros e não
// ficam no repositório.

import (
	"os"
	"testing"
)

func TestLerKSBReais(t *testing.T) {
	p1, p2, pf := os.Getenv("TI_KSB1"), os.Getenv("TI_KSB2"), os.Getenv("TI_PLANILHA")
	if p1 == "" || p2 == "" {
		t.Skip("defina TI_KSB1 e TI_KSB2")
	}
	for _, c := range []struct{ arq, esperado string }{{p1, TipoKSB1}, {p2, TipoKSB2}, {pf, TipoForecast}} {
		if c.arq == "" {
			continue
		}
		d, err := os.ReadFile(c.arq)
		if err != nil {
			t.Fatal(err)
		}
		tipo, err := DetectarTipo(d)
		if err != nil || tipo != c.esperado {
			t.Fatalf("DetectarTipo(%s) = %q, %v; esperado %q", c.arq, tipo, err, c.esperado)
		}
	}
	d1, _ := os.ReadFile(p1)
	l1, r1, err := LerKSB1(d1)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("KSB1: %+v", r1)
	if len(l1) != r1.Aproveitadas || r1.Linhas != 5041 || r1.SemPedido != 1222 || r1.PrimeiroMes != "2026-01" || r1.UltimoMes != "2026-09" {
		t.Errorf("KSB1 inesperada: %+v (linhas lidas %d)", r1, len(l1))
	}
	d2, _ := os.ReadFile(p2)
	l2, r2, err := LerKSB2(d2)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("KSB2: %+v", r2)
	if len(l2) != 5286 || r2.Totais != 2483 {
		t.Errorf("KSB2 inesperada: %d detalhes, %d totais", len(l2), r2.Totais)
	}
	var soma float64
	for _, l := range l2 {
		soma += l.Valor
	}
	if soma < 520038.28 || soma > 520038.30 {
		t.Errorf("soma do detalhe da KSB2 = %.2f, esperado 520038,29 (os totais não podem entrar)", soma)
	}
}

// modeloDaPlanilha monta o modelo a partir do .xlsx (sem banco) — para os testes com dados reais.
func modeloDaPlanilha(t *testing.T, caminho, lastClosed string) *Modelo {
	t.Helper()
	d, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatal(err)
	}
	pl, err := LerPlanilha(d)
	if err != nil {
		t.Fatal(err)
	}
	orc := make([]OrcDB, len(pl.Orcamento))
	for i, o := range pl.Orcamento {
		v := map[string]float64{}
		for j, ym := range pl.Meses {
			v[ym] = o.Valores[j]
		}
		orc[i] = OrcDB{Ref: o.Ref, Origem: o.Origem, Divisao: o.Divisao, CC: o.CC, Conta: o.Conta, Fornecedor: o.Fornecedor, Descricao: o.Descricao, Valores: v}
	}
	return Montar(Entrada{Params: pl.Params, Orcamento: orc, Lancamentos: pl.Lancamentos, LastClosed: lastClosed})
}

// A conferência nos dados reais de jan–set/2026. O documento do Welton cita, para um teste
// seu: 852 de 1.158 pedidos/CC/mês conferidos (74%), 75 lançados em outro mês (R$ 4,08 mi) e
// 103 lançados só no SAP (R$ 1,74 mi). A planilha usada lá não é esta (aqui são 1.102 grupos
// de planilha), então os números não são idênticos: em 16/11 deu 838 de 1.179 (71%), 62 em outro
// mês (R$ 4,00 mi) e 106 só no SAP (R$ 1,75 mi) — mesma ordem de grandeza. A soma das situações
// tem de fechar com o total.
func TestConferenciaReal(t *testing.T) {
	p1, pf := os.Getenv("TI_KSB1"), os.Getenv("TI_PLANILHA")
	if p1 == "" || pf == "" {
		t.Skip("defina TI_KSB1 e TI_PLANILHA")
	}
	d1, err := os.ReadFile(p1)
	if err != nil {
		t.Fatal(err)
	}
	ksb1, _, err := LerKSB1(d1)
	if err != nil {
		t.Fatal(err)
	}
	m := modeloDaPlanilha(t, pf, "2026-09")
	r := Conferir(m.Lanc, ksb1)

	t.Logf("cobertura %s a %s | KSB1 %d linhas, %d pedidos | pedidos da planilha sem KSB1: %d",
		r.Resumo.PrimeiroMes, r.Resumo.UltimoMes, r.Resumo.LinhasKSB1, r.Resumo.PedidosKSB1, r.Resumo.PedidosSemKSB1)
	for _, s := range Situacoes {
		v := r.Resumo.Por[s.Codigo]
		t.Logf("  %-32s %5d grupos  R$ %14.2f", s.Rotulo, v.Qtd, v.Valor)
	}
	t.Logf("TOTAL %d grupos | base (sem Verificar) %d | conferidos %d (%.1f%%)", r.Resumo.Total, r.Resumo.Base, r.Resumo.Conferidos, 100*r.Resumo.PctConferido)

	var soma int
	for _, v := range r.Resumo.Por {
		soma += v.Qtd
	}
	if soma != r.Resumo.Total || r.Resumo.Total != len(r.Linhas) {
		t.Errorf("as situações (%d) não fecham com o total (%d / %d linhas)", soma, r.Resumo.Total, len(r.Linhas))
	}
	if r.Resumo.PctConferido < 0.65 || r.Resumo.PctConferido > 0.85 {
		t.Errorf("%% conferido fora do plausível: %.1f%% (o documento cita 74%%)", 100*r.Resumo.PctConferido)
	}
}
