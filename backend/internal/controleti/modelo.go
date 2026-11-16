package controleti

// modelo.go monta o modelo que o painel consome (mesmo formato do
// FCX_EMBED do painel HTML de 2026: meta / budget / cf / lanc). Os campos
// derivados NÃO são gravados no banco — saem de cc_sap, conta e status mais
// as tabelas de parâmetros, exatamente como a planilha faz com fórmulas.

import (
	"sort"
	"strings"
	"time"
)

// Meta descreve o modelo. LastClosed e RefDate usam AAAA-MM e AAAA-MM-DD.
type Meta struct {
	FileName   string   `json:"fileName"`
	LastClosed string   `json:"lastClosed"`
	RefDate    string   `json:"refDate"`
	ParsedAt   string   `json:"parsedAt"`
	Months     []string `json:"months"`
	// ImportadoEm: quando a planilha foi importada pela última vez (RFC 3339);
	// vazio se nunca houve importação.
	ImportadoEm string `json:"importadoEm"`
}

type BudgetRow struct {
	ID        string    `json:"id"`
	Origem    string    `json:"origem"`
	Div       string    `json:"div"`
	CC        string    `json:"cc"`
	CCDesc    string    `json:"ccDesc"`
	Conta     string    `json:"conta"`
	ContaDesc string    `json:"contaDesc"`
	Tipo      string    `json:"tipo"`
	Forn      string    `json:"forn"`
	Desc      string    `json:"desc"`
	M         []float64 `json:"m"`
	DivDesc   string    `json:"divDesc"`
	Grp       string    `json:"grp"`
}

type LancRow struct {
	ID        string  `json:"id"`
	Origem    string  `json:"origem"`
	Status    string  `json:"status"`
	Pedido    string  `json:"pedido"`
	Folha     string  `json:"folha"`
	Miro      string  `json:"miro"`
	NF        string  `json:"nf"`
	Comp      *string `json:"comp"`
	Venc      *string `json:"venc"`
	Valor     float64 `json:"valor"`
	Forn      string  `json:"forn"`
	Desc      string  `json:"desc"`
	Div       string  `json:"div"`
	DivDesc   string  `json:"divDesc"`
	CC        string  `json:"cc"`
	CCDesc    string  `json:"ccDesc"`
	Conta     string  `json:"conta"`
	ContaDesc string  `json:"contaDesc"`
	Tipo      string  `json:"tipo"`
	Classif   string  `json:"classif"`
	CCSap     string  `json:"ccSap"`
	Grp       string  `json:"grp"`
}

// CFRow é a projeção (Current Forecast) por divisão, CC e conta.
type CFRow struct {
	Key       string    `json:"key"`
	Div       string    `json:"div"`
	CC        string    `json:"cc"`
	CCDesc    string    `json:"ccDesc"`
	Conta     string    `json:"conta"`
	ContaDesc string    `json:"contaDesc"`
	Tipo      string    `json:"tipo"`
	DivDesc   string    `json:"divDesc"`
	Grp       string    `json:"grp"`
	M         []float64 `json:"m"`
}

type Modelo struct {
	Meta   Meta        `json:"meta"`
	Budget []BudgetRow `json:"budget"`
	CF     []CFRow     `json:"cf"`
	Lanc   []LancRow   `json:"lanc"`
}

// OrcDB é uma linha de orçamento como está no banco: valores por mês.
type OrcDB struct {
	Ref, Origem, Divisao, CC, Conta, Fornecedor, Descricao string
	Valores                                                map[string]float64 // AAAA-MM -> valor
}

// Entrada reúne tudo o que Montar precisa.
type Entrada struct {
	Params      Params
	Orcamento   []OrcDB
	Lancamentos []Lanc
	LastClosed  string
	Arquivo     string
	ImportadoEm string
	Agora       time.Time
}

// MesFechado devolve o último mês fechado (AAAA-MM). A regra de 2027: do dia
// 6 em diante o mês anterior está fechado (o SAP aceita lançamento retroativo
// até o dia 5); até o dia 5 o fechado ainda é o mês retrasado. override (de
// ti_config) vence quando preenchido.
func MesFechado(hoje time.Time, override string) string {
	if len(override) == 7 {
		if _, err := time.Parse("2006-01", override); err == nil {
			return override
		}
	}
	primeiro := time.Date(hoje.Year(), hoje.Month(), 1, 0, 0, 0, 0, time.UTC)
	voltar := 1
	if hoje.Day() < 6 {
		voltar = 2
	}
	return primeiro.AddDate(0, -voltar, 0).Format("2006-01")
}

// UltimoDia devolve o último dia do mês AAAA-MM (AAAA-MM-DD).
func UltimoDia(ym string) string {
	t, err := time.Parse("2006-01", ym)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 1, -1).Format("2006-01-02")
}

func descOuVerificar(achou bool, v string) string {
	if !achou {
		return "Verificar"
	}
	return v
}

// Montar aplica os parâmetros e calcula a projeção.
func Montar(in Entrada) *Modelo {
	p := in.Params
	agora := in.Agora
	if agora.IsZero() {
		agora = time.Now().UTC()
	}

	meses := mesesDoOrcamento(in.Orcamento, in.Lancamentos)
	idxMes := make(map[string]int, len(meses))
	for i, m := range meses {
		idxMes[m] = i
	}

	// "Carregado em" no painel = quando a planilha foi importada (não a hora da consulta)
	carregadoEm := agora.Format(time.RFC3339)
	if in.ImportadoEm != "" {
		carregadoEm = in.ImportadoEm
	}
	m := &Modelo{
		Meta: Meta{
			FileName: in.Arquivo, LastClosed: in.LastClosed, RefDate: UltimoDia(in.LastClosed),
			ParsedAt: carregadoEm, Months: meses, ImportadoEm: in.ImportadoEm,
		},
		Budget: make([]BudgetRow, 0, len(in.Orcamento)),
		Lanc:   make([]LancRow, 0, len(in.Lancamentos)),
	}

	contaDesc := func(c string) string { v, ok := p.Conta[c]; return descOuVerificar(ok, v.Descricao) }
	tipoDe := func(c string) string {
		if v, ok := p.Conta[c]; ok {
			return v.Tipo
		}
		return "Não classificado"
	}
	ccDesc := func(c string) string { v, ok := p.CC[c]; return descOuVerificar(ok, v.Descricao) }
	divDesc := func(d string) string { v, ok := p.Divisao[d]; return descOuVerificar(ok, v.Descricao) }
	// grupo (empresa) da divisão: Ferreira Costa, RetailX, Importadoras ou sem divisão
	grp := func(d string) string { v := p.Divisao[d]; return GrupoDaDivisao(d, v.Descricao, v.Empresa) }

	for _, o := range in.Orcamento {
		vals := make([]float64, len(meses))
		for ym, v := range o.Valores {
			if i, ok := idxMes[ym]; ok {
				vals[i] = v
			}
		}
		m.Budget = append(m.Budget, BudgetRow{
			ID: o.Ref, Origem: o.Origem, Div: o.Divisao, CC: o.CC, CCDesc: ccDesc(o.CC),
			Conta: o.Conta, ContaDesc: contaDesc(o.Conta), Tipo: tipoDe(o.Conta),
			Forn: o.Fornecedor, Desc: o.Descricao, M: vals, DivDesc: divDesc(o.Divisao), Grp: grp(o.Divisao),
		})
	}

	for _, l := range in.Lancamentos {
		div, cc := DivCCDoSap(l.CCSap)
		classif := "Comprometido"
		if c, ok := p.Status[l.Status]; ok {
			classif = c
		}
		r := LancRow{
			ID: l.Ref, Origem: l.Origem, Status: l.Status, Pedido: l.Pedido, Folha: l.Folha, Miro: l.Miro, NF: l.NF,
			Valor: l.Valor, Forn: l.Fornecedor, Desc: l.Descricao,
			Div: div, DivDesc: divDesc(div), CC: cc, CCDesc: ccDesc(cc),
			Conta: l.Conta, ContaDesc: contaDesc(l.Conta), Tipo: tipoDe(l.Conta),
			Classif: classif, CCSap: l.CCSap, Grp: grp(div),
		}
		if len(l.Competencia) >= 7 {
			c := l.Competencia[:7]
			r.Comp = &c
		}
		if l.Vencimento != "" {
			v := l.Vencimento
			r.Venc = &v
		}
		m.Lanc = append(m.Lanc, r)
	}

	m.CF = projecao(m.Budget, m.Lanc, meses, idxMes, in.LastClosed)
	for i := range m.CF {
		m.CF[i].Grp = grp(m.CF[i].Div)
	}
	return m
}

// DivCCDoSap separa o CC SAP: divisão = tudo menos os 4 últimos dígitos,
// CC = os 4 últimos. IMOB, N/D e valores curtos (< 5) ficam como N/D.
func DivCCDoSap(s string) (div, cc string) {
	if s == "" {
		return "", ""
	}
	if s == "N/D" || s == "IMOB" || len(s) < 5 {
		div = "N/D"
	} else {
		div = s[:len(s)-4]
	}
	if s == "N/D" || s == "IMOB" {
		cc = s
	} else if len(s) >= 4 {
		cc = s[len(s)-4:]
	} else {
		cc = s
	}
	return div, cc
}

// mesesDoOrcamento: os meses são os das colunas do orçamento. Sem orçamento,
// vale o intervalo contínuo dos lançamentos.
func mesesDoOrcamento(orc []OrcDB, lancs []Lanc) []string {
	set := map[string]struct{}{}
	for _, o := range orc {
		for ym := range o.Valores {
			set[ym] = struct{}{}
		}
	}
	if len(set) == 0 {
		min, max := "", ""
		for _, l := range lancs {
			if len(l.Competencia) < 7 {
				continue
			}
			c := l.Competencia[:7]
			if min == "" || c < min {
				min = c
			}
			if c > max {
				max = c
			}
		}
		if min == "" {
			return []string{}
		}
		ini, _ := time.Parse("2006-01", min)
		fim, _ := time.Parse("2006-01", max)
		for t := ini; !t.After(fim); t = t.AddDate(0, 1, 0) {
			set[t.Format("2006-01")] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for ym := range set {
		out = append(out, ym)
	}
	sort.Strings(out)
	return out
}

// projecao replica a aba Current Forecast: mês fechado = realizado; mês
// aberto = maior valor entre o orçado e o lançado (realizado + a lançar).
func projecao(budget []BudgetRow, lanc []LancRow, meses []string, idxMes map[string]int, lastClosed string) []CFRow {
	type acum struct {
		row  CFRow
		b    []float64
		real []float64
		all  []float64
	}
	mapa := map[string]*acum{}
	var ordem []string
	get := func(div, cc, ccDesc, conta, contaDesc, tipo, divDesc string) *acum {
		k := div + "|" + cc + "|" + conta
		if a, ok := mapa[k]; ok {
			return a
		}
		a := &acum{
			row: CFRow{Key: k, Div: div, CC: cc, CCDesc: ccDesc, Conta: conta, ContaDesc: contaDesc, Tipo: tipo, DivDesc: divDesc},
			b:   make([]float64, len(meses)), real: make([]float64, len(meses)), all: make([]float64, len(meses)),
		}
		mapa[k] = a
		ordem = append(ordem, k)
		return a
	}
	for _, b := range budget {
		a := get(b.Div, b.CC, b.CCDesc, b.Conta, b.ContaDesc, b.Tipo, b.DivDesc)
		for i, v := range b.M {
			a.b[i] += v
		}
	}
	for _, l := range lanc {
		if l.Comp == nil {
			continue
		}
		i, ok := idxMes[*l.Comp]
		if !ok {
			continue
		}
		a := get(l.Div, l.CC, l.CCDesc, l.Conta, l.ContaDesc, l.Tipo, l.DivDesc)
		a.all[i] += l.Valor
		if l.Classif == "Realizado" {
			a.real[i] += l.Valor
		}
	}
	out := make([]CFRow, 0, len(ordem))
	for _, k := range ordem {
		a := mapa[k]
		vals := make([]float64, len(meses))
		tem := false
		for i, ym := range meses {
			if ym <= lastClosed {
				vals[i] = r2(a.real[i])
			} else {
				v := a.b[i]
				if a.all[i] > v {
					v = a.all[i]
				}
				vals[i] = r2(v)
			}
			if vals[i] > 0.004 || vals[i] < -0.004 {
				tem = true
			}
		}
		if tem {
			a.row.M = vals
			out = append(out, a.row)
		}
	}
	return out
}

// GrupoDaDivisao replica o agrupamento por empresa do painel: IMP
// (Importadoras), RX (RetailX 4016), FC (Ferreira Costa, divisões 91..1009 e
// IMOB) ou OUT. Quando a tabela de divisões informa a Empresa, ela vale.
func GrupoDaDivisao(div, descricao, empresa string) string {
	switch normaliza(empresa) {
	case "ferreira costa":
		return "FC"
	case "retailx":
		return "RX"
	case "importadoras":
		return "IMP"
	}
	d := strings.TrimSpace(div)
	if strings.HasPrefix(strings.ToUpper(descricao), "IMPORTADORA") {
		return "IMP"
	}
	if d == "4016" {
		return "RX"
	}
	if d == "N/D" {
		return "FC"
	}
	if n, ok := inteiro(d); ok && n >= 91 && n <= 1009 {
		return "FC"
	}
	return "OUT"
}

func inteiro(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}
