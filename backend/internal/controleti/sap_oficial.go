package controleti

// sap_oficial.go — realizado oficial do SAP (KSB1), porte do fcxBuild do "Painel de Despesas - TI"
// (Welton, 19/11/2026):
//
//   - mês fechado COM KSB1: vale o SAP (pedidos que estão na planilha); imobilizado e itens sem pedido
//     da planilha continuam valendo. A linha da planilha cujo pedido aparece na KSB1 NÃO é apagada: fica
//     na lista (kind X) e só deixa de somar nas contas.
//   - mês fechado SEM KSB1: planilha, marcado como provisório.
//   - o mês de cada lançamento da KSB1 é o da DATA DE LANÇAMENTO (decisão de 19/11/2026: a KSB1 não terá
//     data de entrada). Não precisa de "janela de entrada": o próprio SAP só aceita lançamento retroativo
//     até o dia 5, então o mês fechado não muda depois do corte do dia 6.

import (
	"sort"
	"strings"
)

// MesEfetivo é o mês a que a linha da KSB1 conta: o da data de lançamento.
func MesEfetivo(l KSB1Linha) string {
	if len(l.DataLanc) < 7 {
		return ""
	}
	return l.DataLanc[:7]
}

func chaveForn(s string) string {
	k := chaveFornecedor(s)
	if len(k) > 18 {
		k = k[:18]
	}
	return k
}

func aplicarSAPOficial(in Entrada, meses []string) ([]Lanc, MetaSAP) {
	meta := MetaSAP{Desde: in.SapDesde, Oficiais: []string{}, Provisorios: []string{}, SapMonths: []string{}, Ignorado: map[string]float64{}}
	if meta.Desde == "" {
		meta.Desde = DesdePadraoSAP
	}
	meta.K1Modo = "sem KSB1"

	porMes := map[string][]KSB1Linha{} // por mês EFETIVO
	var de, ate string
	for _, l := range in.KSB1 {
		if len(l.DataLanc) < 7 || ccEhLeiDoBem(l.CCSap) {
			continue
		}
		if m := l.DataLanc[:7]; de == "" || m < de {
			de = m
		}
		if m := l.DataLanc[:7]; m > ate {
			ate = m
		}
		porMes[MesEfetivo(l)] = append(porMes[MesEfetivo(l)], l)
	}
	meta.Ksb1De, meta.Ksb1Ate = de, ate
	if len(in.KSB1) > 0 {
		meta.K1Modo = "data de lançamento"
	}

	elegivel := map[string]bool{}
	for _, ym := range meses {
		if ym < meta.Desde || ym > in.LastClosed {
			continue
		}
		if len(porMes[ym]) > 0 {
			elegivel[ym] = true
			meta.Oficiais = append(meta.Oficiais, ym)
			meta.SapMonths = append(meta.SapMonths, ym)
		} else {
			meta.Provisorios = append(meta.Provisorios, ym)
		}
	}

	pedPlan := map[string]struct{}{}
	fornPlan := map[string]struct{}{}
	fornDoPedido := map[string]map[string]int{}
	descDoPedido := map[string]string{}
	for _, l := range in.Lancamentos {
		if l.Pedido != "" {
			pedPlan[l.Pedido] = struct{}{}
			if fornDoPedido[l.Pedido] == nil {
				fornDoPedido[l.Pedido] = map[string]int{}
			}
			fornDoPedido[l.Pedido][l.Fornecedor]++
			if descDoPedido[l.Pedido] == "" {
				descDoPedido[l.Pedido] = l.Descricao
			}
		}
		if k := chaveForn(l.Fornecedor); len(k) >= 6 {
			fornPlan[k] = struct{}{}
		}
	}
	fornPed := func(p string) string {
		best, n := "", 0
		for f, c := range fornDoPedido[p] {
			if c > n || (c == n && f < best) {
				best, n = f, c
			}
		}
		return best
	}

	saida := make([]Lanc, len(in.Lancamentos))
	copy(saida, in.Lancamentos)
	if len(elegivel) == 0 && len(porMes) == 0 {
		return saida, meta
	}

	// K1 = linhas dos meses SAP; pedSap = pedidos que a KSB1 traz nesses meses
	type chave struct{ pedido, cc, mes string }
	somas := map[chave]*struct {
		valor            float64
		forn, txt, conta string
	}{}
	pedSap := map[string]struct{}{}
	var totSap, totNosso float64
	for ym := range elegivel {
		for _, l := range porMes[ym] {
			totSap += l.Valor
			if l.Pedido != "" {
				pedSap[l.Pedido] = struct{}{}
			}
			if _, nosso := pedPlan[l.Pedido]; l.Pedido != "" && nosso {
				totNosso += l.Valor
				k := chave{l.Pedido, l.CCSap, ym}
				a := somas[k]
				if a == nil {
					a = &struct {
						valor            float64
						forn, txt, conta string
					}{}
					somas[k] = a
				}
				a.valor += l.Valor
				if a.forn == "" {
					a.forn = l.Fornecedor
				}
				if a.txt == "" {
					a.txt = l.Texto
				}
				if a.conta == "" {
					a.conta = l.Conta
				}
			} else if _, ok := fornPlan[chaveForn(l.Fornecedor)]; !ok {
				_, cc := DivCCDoSap(l.CCSap)
				meta.Ignorado[cc] = r2(meta.Ignorado[cc] + l.Valor)
			}
		}
	}
	meta.TotSap, meta.TotNosso = r2(totSap), r2(totNosso)

	realizado := func(l Lanc) bool {
		return l.ForcaRealizado || in.Params.Status[l.Status] == "Realizado"
	}
	// a planilha mantém as linhas, mas as que o SAP substitui deixam de somar
	for i, l := range saida {
		if !realizado(l) || l.Pedido == "" || len(l.Competencia) < 7 || !elegivel[l.Competencia[:7]] {
			continue
		}
		if _, ok := pedSap[l.Pedido]; !ok {
			continue
		}
		if _, cc := DivCCDoSap(l.CCSap); cc == "IMOB" {
			continue
		}
		saida[i].Subst = true
	}

	chaves := make([]chave, 0, len(somas))
	for k := range somas {
		chaves = append(chaves, k)
	}
	sort.Slice(chaves, func(i, j int) bool {
		a, b := chaves[i], chaves[j]
		if a.mes != b.mes {
			return a.mes < b.mes
		}
		if a.pedido != b.pedido {
			return a.pedido < b.pedido
		}
		return a.cc < b.cc
	})
	for _, k := range chaves {
		a := somas[k]
		if v := r2(a.valor); presente(v) {
			forn := fornPed(k.pedido)
			if forn == "" {
				forn = a.forn
			}
			d := descDoPedido[k.pedido]
			if d == "" {
				d = a.txt
			}
			saida = append(saida, Lanc{
				Ref: "SAP-" + k.pedido + "-" + strings.ReplaceAll(k.mes, "-", ""), Origem: "SAP (KSB1)", Status: "LANÇADO",
				Pedido: k.pedido, Competencia: k.mes + "-01", Valor: v, Fornecedor: forn, Descricao: d,
				CCSap: k.cc, Conta: a.conta, ForcaRealizado: true,
			})
		}
	}

	meta.Tardios = TardiosSAP{Mes: mesSoma(in.LastClosed, 1)} // sem data de entrada não há "tardios"
	return saida, meta
}
