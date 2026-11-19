package controleti

// Validação ponta a ponta com os arquivos reais do Welton (19/11/2026): Despesas TI 2026 + Orçamento
// TI 2026 + KSB1/KSB2 + FBL1N, comparando com os números do painel dele:
//   pagamentos 1.250 pagas / 41 em aberto / 10 vencidas / 124 não localizadas; realizado de out/26 R$ 4.920.941,56.
// TI_PASTA = pasta com os arquivos (não ficam no repositório).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func lerArq(t *testing.T, pasta, nome string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(pasta, nome))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestValidacao2026(t *testing.T) {
	pasta := os.Getenv("TI_PASTA")
	if pasta == "" {
		t.Skip("defina TI_PASTA")
	}
	d, err := LerDespesas(lerArq(t, pasta, "Despesas TI 2026.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	o, err := LerOrcamentoTI(lerArq(t, pasta, "Orçamento TI 2026.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	ksb1, _, err := LerKSB1(lerArq(t, pasta, "KSB1_Realizado_Jan à Out_2026 1.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	ksb2, _, err := LerKSB2(lerArq(t, pasta, "KSB2_Compromissado_Jan à Out_2026 1.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	fbl, _, err := LerFBL1N(lerArq(t, pasta, "FBL1N_EXPORT_20261118_094948.xlsx"))
	if err != nil {
		t.Fatal(err)
	}
	var orc []OrcDB
	for _, l := range o.Linhas {
		v := map[string]float64{}
		for j, ym := range o.Meses {
			v[ym] = l.Valores[j]
		}
		orc = append(orc, OrcDB{Ref: l.Ref, Origem: l.Origem, Divisao: l.Divisao, CC: l.CC, Conta: l.Conta, Fornecedor: l.Fornecedor, Descricao: l.Descricao, Valores: v})
	}
	if fc := os.Getenv("TI_FORECAST"); fc != "" { // orçado de 2027 em diante vem da FC FY27 Forecast (como em produção)
		pl, err := LerPlanilha(lerArq(t, filepath.Dir(fc), filepath.Base(fc)))
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range pl.Orcamento {
			v := map[string]float64{}
			for j, ym := range pl.Meses {
				if ym >= "2027-01" && j < len(l.Valores) {
					v[ym] = l.Valores[j]
				}
			}
			orc = append(orc, OrcDB{Ref: l.Ref, Origem: l.Origem, Divisao: l.Divisao, CC: l.CC, Conta: l.Conta, Fornecedor: l.Fornecedor, Descricao: l.Descricao, Valores: v})
		}
	}
	saldos := map[string]float64{}
	for _, k := range ksb2 {
		saldos[k.Pedido] += k.Valor
	}
	agora := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)

	for _, desde := range []string{"2026-01", "2027-01"} {
		in := Entrada{Params: d.Params, Orcamento: orc, Lancamentos: d.Lancamentos, LastClosed: "2026-10", Agora: agora,
			KSB1: ksb1, SaldosSAP: saldos, SapDesde: desde, FBL1N: fbl, FBL1NFonte: "FBL1N", FBL1NEm: "2026-11-18T09:49:48Z"}
		m := Montar(in)
		var real float64
		cont := map[string]int{}
		for _, l := range m.Lanc {
			if l.Comp != nil && *l.Comp == "2026-10" && l.Classif == "Realizado" && l.Kind != "X" {
				real += l.Valor
			}
			if l.Pag != nil {
				cont[l.Pag.St]++
			}
		}
		if dump := os.Getenv("TI_DUMP"); dump != "" && desde == "2026-01" {
			esc := Escopo{CCs: map[string]bool{"6710": true}, Grupos: map[string]bool{"RX": true}}
			for nome, mod := range map[string]*Modelo{"adm": m, "gestor": FiltrarModelo(m, esc)} {
				b, _ := json.Marshal(mod)
				_ = os.WriteFile(filepath.Join(dump, "modelo_"+nome+".json"), b, 0o600)
			}
		}
		if desde == "2026-01" && os.Getenv("TI_PAINEL_HTML") != "" {
			cmpPainel(t, m.Lanc, os.Getenv("TI_PAINEL_HTML"))
		}
		if desde == "2026-01" {
			if real < 4920941.55 || real > 4920941.57 {
				t.Errorf("realizado out/26 = %.2f, esperava R$ 4.920.941,56", real)
			}
			if cont["Paga"] != 1250 || cont["Em aberto"] != 41 || cont["Vencida"] != 10 || cont["Não localizada"] != 124 {
				t.Errorf("pagamentos = %v, esperava 1250 pagas / 41 em aberto / 10 vencidas / 124 não localizadas", cont)
			}
		}
		t.Logf("SAP oficial desde %s: realizado out/26 = R$ %.2f | pagamentos %v", desde, real, cont)
	}
}

// cmpPainel compara, linha a linha (pela ordem), o status de pagamento com o do painel do Welton.
func cmpPainel(t *testing.T, mine []LancRow, html string) {
	d := carregarOraculoHTML(t, html)
	var theirs []oraculoLanc
	for _, l := range d.Lanc {
		if l.Src != "SAP" {
			theirs = append(theirs, l)
		}
	}
	var ours []LancRow
	for _, l := range mine {
		if l.Src != "SAP" {
			ours = append(ours, l)
		}
	}
	t.Logf("linhas da planilha: nossas %d, dele %d", len(ours), len(theirs))
	n := 0
	for i := 0; i < len(ours) && i < len(theirs); i++ {
		a, b := "", ""
		if ours[i].Pag != nil {
			a = ours[i].Pag.St
		}
		if theirs[i].Pag != nil {
			b = theirs[i].Pag.St
		}
		if a != b && n < 6 {
			n++
			t.Logf("dif #%d %s: nosso=%q dele=%q valor=%.2f forn=%s nf=%s bp=%s/%s", i, theirs[i].ID, a, b, ours[i].Valor, ours[i].Forn, ours[i].NF, ours[i].BP, theirs[i].BP)
		}
	}
}
