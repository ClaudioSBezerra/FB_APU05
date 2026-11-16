package controleti

// Teste de regressão contra o painel de 2026: o parser JavaScript original
// (fcxParseWorkbook) serve de gabarito. Só roda quando TI_PLANILHA aponta o
// .xlsx e TI_ORACULO o JSON gerado pelo parser original sobre o mesmo
// arquivo — a planilha real tem valores financeiros e não fica no
// repositório.
//
//	TI_PLANILHA=".../FC FY27 Forecast - ajustado.xlsx" TI_ORACULO=.../oraculo.json \
//	  go test ./internal/controleti -run Oraculo -v

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"
)

func perto(a, b float64) bool { return math.Abs(a-b) < 0.005 }

func strPtr(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

// comparar devolve as diferenças entre o modelo calculado (got) e o do
// painel de 2026 (want); vazio = idêntico.
func comparar(got, want *Modelo) []string {
	var diffs []string
	add := func(f string, a ...any) {
		if len(diffs) < 40 {
			diffs = append(diffs, fmt.Sprintf(f, a...))
		}
	}

	if fmt.Sprint(got.Meta.Months) != fmt.Sprint(want.Meta.Months) {
		add("meses diferentes: got %d, want %d", len(got.Meta.Months), len(want.Meta.Months))
	}
	if got.Meta.RefDate != want.Meta.RefDate {
		add("refDate got %s want %s", got.Meta.RefDate, want.Meta.RefDate)
	}

	if len(got.Budget) != len(want.Budget) {
		add("budget: got %d linhas, want %d", len(got.Budget), len(want.Budget))
	}
	for i := 0; i < len(got.Budget) && i < len(want.Budget); i++ {
		g, w := got.Budget[i], want.Budget[i]
		if g.ID != w.ID || g.Origem != w.Origem || g.Div != w.Div || g.CC != w.CC || g.CCDesc != w.CCDesc ||
			g.Conta != w.Conta || g.ContaDesc != w.ContaDesc || g.Tipo != w.Tipo || g.Forn != w.Forn ||
			g.Desc != w.Desc || g.DivDesc != w.DivDesc {
			add("budget[%d] campos: got %+v want %+v", i, g, w)
		}
		if len(g.M) != len(w.M) {
			add("budget[%d] len(m) got %d want %d", i, len(g.M), len(w.M))
			continue
		}
		for j := range g.M {
			if !perto(g.M[j], w.M[j]) {
				add("budget[%d] id=%s mês %d: got %.2f want %.2f", i, g.ID, j, g.M[j], w.M[j])
				break
			}
		}
	}

	if len(got.Lanc) != len(want.Lanc) {
		add("lanc: got %d linhas, want %d", len(got.Lanc), len(want.Lanc))
	}
	for i := 0; i < len(got.Lanc) && i < len(want.Lanc); i++ {
		g, w := got.Lanc[i], want.Lanc[i]
		if g.ID != w.ID || g.Origem != w.Origem || g.Status != w.Status || g.Pedido != w.Pedido || g.Folha != w.Folha ||
			g.Miro != w.Miro || g.NF != w.NF || strPtr(g.Comp) != strPtr(w.Comp) || strPtr(g.Venc) != strPtr(w.Venc) ||
			!perto(g.Valor, w.Valor) || g.Forn != w.Forn || g.Desc != w.Desc || g.Div != w.Div || g.DivDesc != w.DivDesc ||
			g.CC != w.CC || g.CCDesc != w.CCDesc || g.Conta != w.Conta || g.ContaDesc != w.ContaDesc ||
			g.Tipo != w.Tipo || g.Classif != w.Classif || g.CCSap != w.CCSap {
			add("lanc[%d] id=%s:\n   got  %+v\n   want %+v", i, g.ID, g, w)
		}
	}

	if len(got.CF) != len(want.CF) {
		add("cf: got %d linhas, want %d", len(got.CF), len(want.CF))
	}
	wantCF := map[string]CFRow{}
	for _, c := range want.CF {
		wantCF[c.Key] = c
	}
	for _, g := range got.CF {
		w, ok := wantCF[g.Key]
		if !ok {
			add("cf: chave %s só existe no Go", g.Key)
			continue
		}
		for j := range g.M {
			if j < len(w.M) && !perto(g.M[j], w.M[j]) {
				add("cf %s mês %s: got %.2f want %.2f", g.Key, got.Meta.Months[j], g.M[j], w.M[j])
				break
			}
		}
	}

	return diffs
}

func TestOraculoPainel2026(t *testing.T) {
	arq, orac := os.Getenv("TI_PLANILHA"), os.Getenv("TI_ORACULO")
	if arq == "" || orac == "" {
		t.Skip("defina TI_PLANILHA e TI_ORACULO para comparar com o painel de 2026")
	}
	dados, err := os.ReadFile(arq)
	if err != nil {
		t.Fatal(err)
	}
	var want Modelo
	bruto, err := os.ReadFile(orac)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(bruto, &want); err != nil {
		t.Fatal(err)
	}

	pl, err := LerPlanilha(dados)
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
	got := Montar(Entrada{Params: pl.Params, Orcamento: orc, Lancamentos: pl.Lancamentos, LastClosed: want.Meta.LastClosed, Arquivo: "teste"})

	diffs := comparar(got, &want)
	t.Logf("Go: budget=%d lanc=%d cf=%d meses=%d | oráculo: budget=%d lanc=%d cf=%d meses=%d",
		len(got.Budget), len(got.Lanc), len(got.CF), len(got.Meta.Months),
		len(want.Budget), len(want.Lanc), len(want.CF), len(want.Meta.Months))
	for _, d := range diffs {
		t.Error(d)
	}
}
