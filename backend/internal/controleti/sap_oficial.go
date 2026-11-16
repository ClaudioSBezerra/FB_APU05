package controleti

// sap_oficial.go — a regra de 2027: "Realizado oficial: mês fechado com KSB1: valor da KSB1
// dos pedidos que estão na planilha + imobilizado e itens sem pedido da planilha. Mês sem
// KSB1: planilha, marcado como provisório." (documento do Welton, seção 6).
//
// Vale a partir de `Desde` (padrão 2027-01). Antes disso — todo 2026 — o realizado é o que a
// planilha marca como LANÇADO e a KSB1 só alimenta a Conferência SAP; os números de 2026,
// já validados contra o painel antigo, não mudam.

import (
	"sort"
	"strings"
)

func aplicarSAPOficial(in Entrada, meses []string) ([]Lanc, MetaSAP) {
	meta := MetaSAP{Desde: in.SapDesde, Oficiais: []string{}, Provisorios: []string{}}
	if meta.Desde == "" {
		meta.Desde = DesdePadraoSAP
	}

	var de, ate string
	for _, l := range in.KSB1 {
		if len(l.DataLanc) < 7 {
			continue
		}
		mes := l.DataLanc[:7]
		if de == "" || mes < de {
			de = mes
		}
		if mes > ate {
			ate = mes
		}
	}
	meta.Ksb1De, meta.Ksb1Ate = de, ate

	elegivel := map[string]bool{} // meses em que o SAP é o oficial
	for _, ym := range meses {
		if ym < meta.Desde || ym > in.LastClosed {
			continue
		}
		if de != "" && ym >= de && ym <= ate {
			elegivel[ym] = true
			meta.Oficiais = append(meta.Oficiais, ym)
		} else {
			meta.Provisorios = append(meta.Provisorios, ym)
		}
	}
	if len(elegivel) == 0 {
		return in.Lancamentos, meta
	}

	realizado := func(l Lanc) bool {
		if l.ForcaRealizado {
			return true
		}
		return in.Params.Status[l.Status] == "Realizado"
	}

	pedidosNossos := map[string]struct{}{}
	for _, l := range in.Lancamentos {
		if l.Pedido != "" {
			pedidosNossos[l.Pedido] = struct{}{}
		}
	}

	// a planilha perde o realizado COM pedido dos meses oficiais; o resto (sem pedido,
	// imobilizado, a lançar, outros meses) fica
	saida := make([]Lanc, 0, len(in.Lancamentos))
	for _, l := range in.Lancamentos {
		if realizado(l) && l.Pedido != "" && len(l.Competencia) >= 7 && elegivel[l.Competencia[:7]] {
			continue
		}
		saida = append(saida, l)
	}

	// e ganha o valor da KSB1 dos pedidos que são nossos, por pedido + CC + mês
	type chave struct{ pedido, cc, mes string }
	somas := map[chave]*struct {
		valor            float64
		forn, txt, conta string
	}{}
	for _, l := range in.KSB1 {
		if len(l.DataLanc) < 7 || ccEhLeiDoBem(l.CCSap) {
			continue
		}
		mes := l.DataLanc[:7]
		if !elegivel[mes] {
			continue
		}
		if _, nosso := pedidosNossos[l.Pedido]; !nosso {
			continue
		}
		k := chave{l.Pedido, l.CCSap, mes}
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
			saida = append(saida, Lanc{
				Ref: "SAP-" + k.pedido + "-" + strings.ReplaceAll(k.mes, "-", ""), Origem: "SAP (KSB1)", Status: "LANÇADO",
				Pedido: k.pedido, Competencia: k.mes + "-01", Valor: v, Fornecedor: a.forn, Descricao: a.txt,
				CCSap: k.cc, Conta: a.conta, ForcaRealizado: true,
			})
		}
	}
	return saida, meta
}
