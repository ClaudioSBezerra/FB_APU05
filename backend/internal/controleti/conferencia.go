package controleti

// conferencia.go — aba "Conferência SAP" (fase 3). Compara, por pedido + CC + mês,
// o que a planilha marca como realizado com o que o SAP lançou (KSB1) e dá a cada
// grupo uma das 9 situações do documento "Despesas TI — 2026 e 2027" (Welton).
//
// É função pura (sem banco): entra o modelo (lançamentos já com classificação) e
// as linhas da KSB1; sai a lista de situações e o resumo.
//
// Regras:
//   - planilha = lançamentos classificados como "Realizado" (LANÇADO e ESTORNADO,
//     que é negativo) com pedido e competência;
//   - só se comparam os meses que a KSB1 cobre (do menor ao maior mês de lançamento);
//   - CC 6600 (Lei do Bem) e linhas sem pedido ficam de fora;
//   - uma KSB1 com lançamento e estorno no mesmo mês dá soma zero e some.

import (
	"math"
	"sort"
	"strings"
)

// Situações (código estável usado pelo frontend).
const (
	SitConferido       = "conferido"
	SitOutroMes        = "outro_mes"
	SitValorDiferente  = "valor_diferente"
	SitRateioDiferente = "rateio_diferente"
	SitPendente        = "pendente"
	SitNaoEncontrado   = "nao_encontrado"
	SitSoSAP           = "so_sap"
	SitEstorno         = "estorno"
	SitVerificar       = "verificar"
)

// toleranciaConferencia é a margem aceita entre planilha e SAP (R$ 0,01, a mesma
// da abertura de solicitação).
const toleranciaConferencia = 0.015

// SituacaoInfo descreve cada situação para a tela (rótulo, significado, o que fazer).
type SituacaoInfo struct {
	Codigo      string `json:"codigo"`
	Rotulo      string `json:"rotulo"`
	Significado string `json:"significado"`
	Acao        string `json:"acao"`
}

// Situacoes é a ordem de exibição (a de gravidade/ação crescente do documento).
var Situacoes = []SituacaoInfo{
	{SitConferido, "Conferido", "Mesmo pedido, CC, mês e valor", "Nada"},
	{SitOutroMes, "Lançado em outro mês", "O SAP lançou o mesmo valor em outro mês", "Trocar a competência na planilha"},
	{SitValorDiferente, "Valor diferente", "Pedido, CC e mês batem; o valor não", "Ver parcela, estorno ou digitação"},
	{SitRateioDiferente, "Rateio diferente", "O total do pedido no mês bate; a divisão entre CCs não", "Ajustar o CC SAP das linhas"},
	{SitPendente, "Pendente de lançamento", "LANÇADO na planilha, sem lançamento no SAP", "Conferir a MIRO"},
	{SitNaoEncontrado, "Pedido não encontrado no SAP", "O pedido não aparece na KSB1", "Conferir o número e o filtro de CC da exportação"},
	{SitSoSAP, "Lançado só no SAP", "Pedido nosso com lançamento sem linha na planilha", "Incluir a linha que faltou"},
	{SitEstorno, "Estorno / soma negativa", "Estorno sem o lançamento original", "Registrar com status ESTORNADO"},
	{SitVerificar, "Verificar", "Fornecedor nosso com pedido fora da planilha; não entra no realizado", "Incluir se for TI; ignorar se for de outra área"},
}

func acaoDe(codigo string) string {
	for _, s := range Situacoes {
		if s.Codigo == codigo {
			return s.Acao
		}
	}
	return ""
}

// ConfLinha é um grupo pedido + CC + mês com a sua situação.
type ConfLinha struct {
	Situacao      string   `json:"situacao"`
	Pedido        string   `json:"pedido"`
	CCSap         string   `json:"ccSap"`
	Div           string   `json:"div"`
	CC            string   `json:"cc"`
	Mes           string   `json:"mes"` // AAAA-MM: o da planilha, ou o do SAP quando só existe lá
	ValorPlanilha float64  `json:"valorPlanilha"`
	ValorSAP      float64  `json:"valorSap"`
	Diferenca     float64  `json:"diferenca"` // SAP − planilha
	Valor         float64  `json:"valor"`     // valor de referência (|planilha|, ou |SAP| se só existe no SAP)
	Fornecedor    string   `json:"fornecedor"`
	Descricao     string   `json:"descricao"`
	MesSAP        string   `json:"mesSap,omitempty"` // "lançado em outro mês": o mês em que o SAP lançou
	IDs           []string `json:"ids,omitempty"`    // linhas da planilha envolvidas
	Acao          string   `json:"acao"`
}

// ConfTotal é a contagem e o valor de uma situação.
type ConfTotal struct {
	Qtd   int     `json:"qtd"`
	Valor float64 `json:"valor"`
}

// ConfResumo resume a conferência.
type ConfResumo struct {
	Total          int                  `json:"total"` // todos os grupos listados
	Base           int                  `json:"base"`  // grupos que entram no realizado: total − "Verificar"
	Conferidos     int                  `json:"conferidos"`
	PctConferido   float64              `json:"pctConferido"` // conferidos ÷ base
	Por            map[string]ConfTotal `json:"por"`
	PrimeiroMes    string               `json:"primeiroMes"` // meses cobertos pela KSB1
	UltimoMes      string               `json:"ultimoMes"`
	LinhasKSB1     int                  `json:"linhasKsb1"`
	PedidosKSB1    int                  `json:"pedidosKsb1"`
	PedidosSemKSB1 int                  `json:"pedidosSemKsb1"` // pedidos da planilha, em meses cobertos, que a KSB1 não traz
}

// ConfResultado é o que a conferência devolve.
type ConfResultado struct {
	Resumo ConfResumo  `json:"resumo"`
	Linhas []ConfLinha `json:"linhas"`
}

type chaveConf struct{ pedido, cc, mes string }
type chavePedMes struct{ pedido, mes string }

type acumP struct {
	soma       float64
	ids        []string
	forn, desc string
}
type acumK struct {
	soma       float64
	forn, desc string
}

func ccEhLeiDoBem(ccSap string) bool { return strings.HasSuffix(ccSap, "6600") }

func presente(v float64) bool { return math.Abs(v) >= 0.005 }

func iguais(a, b float64) bool { return math.Abs(a-b) < toleranciaConferencia }

// Conferir compara a planilha com a KSB1.
func Conferir(lancs []LancRow, ksb1 []KSB1Linha) ConfResultado {
	res := ConfResultado{Resumo: ConfResumo{Por: map[string]ConfTotal{}}, Linhas: []ConfLinha{}}
	if len(ksb1) == 0 {
		return res
	}

	// ---- KSB1: cobertura e soma por pedido + CC + mês
	K := map[chaveConf]*acumK{}
	pedidosK := map[string]struct{}{}
	for _, l := range ksb1 {
		if len(l.DataLanc) < 7 || l.Pedido == "" || ccEhLeiDoBem(l.CCSap) {
			continue
		}
		mes := l.DataLanc[:7]
		if res.Resumo.PrimeiroMes == "" || mes < res.Resumo.PrimeiroMes {
			res.Resumo.PrimeiroMes = mes
		}
		if mes > res.Resumo.UltimoMes {
			res.Resumo.UltimoMes = mes
		}
		k := chaveConf{l.Pedido, l.CCSap, mes}
		a := K[k]
		if a == nil {
			a = &acumK{}
			K[k] = a
		}
		a.soma += l.Valor
		if a.forn == "" {
			a.forn = l.Fornecedor
		}
		if a.desc == "" {
			a.desc = l.Texto
		}
		pedidosK[l.Pedido] = struct{}{}
	}
	res.Resumo.LinhasKSB1 = len(ksb1)
	res.Resumo.PedidosKSB1 = len(pedidosK)
	for _, a := range K {
		a.soma = r2(a.soma)
	}

	// ---- planilha: realizado com pedido nos meses cobertos; e o universo "nosso"
	P := map[chaveConf]*acumP{}
	pedidosNossos := map[string]struct{}{}
	fornecedoresNossos := map[string]struct{}{}
	pedidosSemKSB1 := map[string]struct{}{}
	for _, l := range lancs {
		if l.Pedido != "" {
			pedidosNossos[l.Pedido] = struct{}{}
		}
		if n := normalizaForn(l.Forn); n != "" {
			fornecedoresNossos[n] = struct{}{}
		}
		if l.Classif != "Realizado" || l.Pedido == "" || l.Comp == nil || ccEhLeiDoBem(l.CCSap) || l.CC == "6600" {
			continue
		}
		mes := *l.Comp
		if mes < res.Resumo.PrimeiroMes || mes > res.Resumo.UltimoMes {
			continue
		}
		k := chaveConf{l.Pedido, l.CCSap, mes}
		a := P[k]
		if a == nil {
			a = &acumP{}
			P[k] = a
		}
		a.soma += l.Valor
		a.ids = append(a.ids, l.ID)
		if a.forn == "" {
			a.forn = l.Forn
		}
		if a.desc == "" {
			a.desc = l.Desc
		}
		if _, achou := pedidosK[l.Pedido]; !achou {
			pedidosSemKSB1[l.Pedido] = struct{}{}
		}
	}
	for _, a := range P {
		a.soma = r2(a.soma)
	}
	res.Resumo.PedidosSemKSB1 = len(pedidosSemKSB1)

	// ---- totais por pedido + mês (para o rateio) e índice dos meses de cada pedido + CC no SAP
	somaP := map[chavePedMes]float64{}
	somaK := map[chavePedMes]float64{}
	chavesGrupo := map[chavePedMes][]chaveConf{}
	uniao := map[chaveConf]struct{}{}
	for k, a := range P {
		if presente(a.soma) {
			uniao[k] = struct{}{}
			somaP[chavePedMes{k.pedido, k.mes}] += a.soma
		}
	}
	mesesSAPdoPedidoCC := map[[2]string][]string{} // (pedido, cc) -> meses com KSB1 presente
	for k, a := range K {
		if presente(a.soma) {
			uniao[k] = struct{}{}
			somaK[chavePedMes{k.pedido, k.mes}] += a.soma
			mesesSAPdoPedidoCC[[2]string{k.pedido, k.cc}] = append(mesesSAPdoPedidoCC[[2]string{k.pedido, k.cc}], k.mes)
		}
	}
	for k := range uniao {
		pm := chavePedMes{k.pedido, k.mes}
		chavesGrupo[pm] = append(chavesGrupo[pm], k)
	}
	valP := func(k chaveConf) float64 {
		if a, ok := P[k]; ok && presente(a.soma) {
			return a.soma
		}
		return 0
	}
	valK := func(k chaveConf) float64 {
		if a, ok := K[k]; ok && presente(a.soma) {
			return a.soma
		}
		return 0
	}
	// grupo (pedido, mês) é "rateio" quando o total bate mas alguma chave dele não bate
	rateio := map[chavePedMes]bool{}
	for pm, chaves := range chavesGrupo {
		if !presente(somaP[pm]) || !presente(somaK[pm]) || !iguais(somaP[pm], somaK[pm]) {
			continue
		}
		for _, k := range chaves {
			if !iguais(valP(k), valK(k)) {
				rateio[pm] = true
				break
			}
		}
	}

	ordenadas := make([]chaveConf, 0, len(uniao))
	for k := range uniao {
		ordenadas = append(ordenadas, k)
	}
	sort.Slice(ordenadas, func(i, j int) bool {
		a, b := ordenadas[i], ordenadas[j]
		if a.pedido != b.pedido {
			return a.pedido < b.pedido
		}
		if a.cc != b.cc {
			return a.cc < b.cc
		}
		return a.mes < b.mes
	})

	novaLinha := func(sit string, k chaveConf) ConfLinha {
		p, s := valP(k), valK(k)
		div, cc := DivCCDoSap(k.cc)
		l := ConfLinha{
			Situacao: sit, Pedido: k.pedido, CCSap: k.cc, Div: div, CC: cc, Mes: k.mes,
			ValorPlanilha: p, ValorSAP: s, Diferenca: r2(s - p), Acao: acaoDe(sit),
		}
		if a, ok := P[k]; ok {
			l.IDs = a.ids
			l.Fornecedor, l.Descricao = a.forn, a.desc
		}
		if l.Fornecedor == "" {
			if a, ok := K[k]; ok {
				l.Fornecedor = a.forn
			}
		}
		if l.Descricao == "" {
			if a, ok := K[k]; ok {
				l.Descricao = a.desc
			}
		}
		if presente(p) {
			l.Valor = math.Abs(p)
		} else {
			l.Valor = math.Abs(s)
		}
		return l
	}

	consumidasPeloOutroMes := map[chaveConf]bool{}
	var pSo, kSo []chaveConf
	for _, k := range ordenadas {
		p, s := valP(k), valK(k)
		pm := chavePedMes{k.pedido, k.mes}
		switch {
		case presente(p) && presente(s):
			switch {
			case iguais(p, s):
				res.Linhas = append(res.Linhas, novaLinha(SitConferido, k))
			case rateio[pm]:
				res.Linhas = append(res.Linhas, novaLinha(SitRateioDiferente, k))
			default:
				res.Linhas = append(res.Linhas, novaLinha(SitValorDiferente, k))
			}
		case presente(p):
			pSo = append(pSo, k)
		default:
			kSo = append(kSo, k)
		}
	}

	// só planilha
	for _, k := range pSo {
		pm := chavePedMes{k.pedido, k.mes}
		p := valP(k)
		if rateio[pm] {
			res.Linhas = append(res.Linhas, novaLinha(SitRateioDiferente, k))
			continue
		}
		// o SAP lançou o mesmo valor, no mesmo pedido + CC, em outro mês que a planilha não usa para ele
		var achado string
		for _, m2 := range mesesSAPdoPedidoCC[[2]string{k.pedido, k.cc}] {
			k2 := chaveConf{k.pedido, k.cc, m2}
			if m2 == k.mes || consumidasPeloOutroMes[k2] || presente(valP(k2)) {
				continue
			}
			if iguais(valK(k2), p) && (achado == "" || m2 < achado) {
				achado = m2
			}
		}
		if achado != "" {
			consumidasPeloOutroMes[chaveConf{k.pedido, k.cc, achado}] = true
			l := novaLinha(SitOutroMes, k)
			l.MesSAP = achado
			l.ValorSAP = valK(chaveConf{k.pedido, k.cc, achado})
			l.Diferenca = r2(l.ValorSAP - p)
			res.Linhas = append(res.Linhas, l)
			continue
		}
		if _, achou := pedidosK[k.pedido]; !achou {
			res.Linhas = append(res.Linhas, novaLinha(SitNaoEncontrado, k))
			continue
		}
		res.Linhas = append(res.Linhas, novaLinha(SitPendente, k))
	}

	// só SAP
	for _, k := range kSo {
		if consumidasPeloOutroMes[k] {
			continue
		}
		pm := chavePedMes{k.pedido, k.mes}
		s := valK(k)
		if rateio[pm] {
			res.Linhas = append(res.Linhas, novaLinha(SitRateioDiferente, k))
			continue
		}
		if _, nosso := pedidosNossos[k.pedido]; nosso {
			if s < 0 {
				res.Linhas = append(res.Linhas, novaLinha(SitEstorno, k))
			} else {
				res.Linhas = append(res.Linhas, novaLinha(SitSoSAP, k))
			}
			continue
		}
		// pedido que a planilha não tem: só interessa se o fornecedor é um dos nossos
		if s > 0 {
			if fornecedorNosso(fornecedoresNossos, K[k].forn) {
				res.Linhas = append(res.Linhas, novaLinha(SitVerificar, k))
			}
		}
	}

	// ---- resumo e ordenação (por situação, depois valor decrescente)
	ordem := map[string]int{}
	for i, s := range Situacoes {
		ordem[s.Codigo] = i
	}
	sort.SliceStable(res.Linhas, func(i, j int) bool {
		a, b := res.Linhas[i], res.Linhas[j]
		if a.Situacao != b.Situacao {
			return ordem[a.Situacao] < ordem[b.Situacao]
		}
		if a.Valor != b.Valor {
			return a.Valor > b.Valor
		}
		if a.Pedido != b.Pedido {
			return a.Pedido < b.Pedido
		}
		return a.Mes < b.Mes
	})
	for _, l := range res.Linhas {
		t := res.Resumo.Por[l.Situacao]
		t.Qtd++
		t.Valor = r2(t.Valor + l.Valor)
		res.Resumo.Por[l.Situacao] = t
	}
	res.Resumo.Total = len(res.Linhas)
	res.Resumo.Conferidos = res.Resumo.Por[SitConferido].Qtd
	// "Verificar" é fornecedor nosso com pedido fora da planilha e "não entra no realizado"
	// (documento do Welton): fica fora da base do percentual, senão ele puxa a taxa para baixo
	res.Resumo.Base = res.Resumo.Total - res.Resumo.Por[SitVerificar].Qtd
	if res.Resumo.Base > 0 {
		res.Resumo.PctConferido = float64(res.Resumo.Conferidos) / float64(res.Resumo.Base)
	}
	return res
}

// normalizaForn: nome de fornecedor sem acento, pontuação e espaços repetidos, para
// reconhecer o mesmo fornecedor escrito de formas diferentes na planilha e no SAP.
func normalizaForn(s string) string {
	n := normaliza(s)
	var b strings.Builder
	espaco := false
	for _, r := range n {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			espaco = false
		} else if !espaco && b.Len() > 0 {
			b.WriteRune(' ')
			espaco = true
		}
	}
	return strings.TrimSpace(b.String())
}

// fornecedorNosso reconhece o fornecedor do SAP entre os da planilha: nome igual
// depois de normalizado, ou um começando pelo outro (o SAP e a planilha cortam o
// nome em tamanhos diferentes). Prefixo só vale a partir de 14 caracteres, para
// "CLARO" não casar com qualquer coisa.
func fornecedorNosso(nossos map[string]struct{}, nome string) bool {
	n := normalizaForn(nome)
	if n == "" {
		return false
	}
	if _, ok := nossos[n]; ok {
		return true
	}
	if len(n) < 14 {
		return false
	}
	for outro := range nossos {
		if len(outro) >= 14 && (strings.HasPrefix(outro, n) || strings.HasPrefix(n, outro)) {
			return true
		}
	}
	return false
}
