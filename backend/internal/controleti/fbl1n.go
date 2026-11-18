package controleti

// fbl1n.go — pagamentos pela FBL1N (partidas de fornecedor), fase "Pagamentos" do documento do
// Welton (18/11/2026). Dois pedaços:
//
//   - LerFBL1N lê o export da FBL1N (.xlsx) e devolve as partidas já normalizadas;
//   - CruzarPagamentos liga cada nota lançada (realizada) a uma ou mais partidas e diz se está
//     Paga, Em aberto, Vencida ou Não localizada.
//
// O cruzamento é port fiel da função fcxPag do Dashboard TI 2027.html (o gabarito, ver
// fbl1n_oraculo_test.go). Hoje a FBL1N mostra o documento contábil (5102…) e não a MIRO, então a
// ligação é aproximada: BP + NF, BP + NF parcial, BP + valor + mês, BP + MIRO agrupada. Quando o SAP
// vier por API (documento de referência = MIRO) a ligação passa a ser exata.

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TipoFBL1N é o tipo de arquivo da FBL1N na importação.
const TipoFBL1N = "fbl1n"

var colunasFBL1N = []string{"Conta", "Referência", "Data de compensação", "Doc.compensação"}

// PartidaFBL1N é uma linha da FBL1N.
type PartidaFBL1N struct {
	Empresa    string
	BP         string
	Fornecedor string
	Tipo       string // tipo de documento (RE, KR, ZP…) em maiúsculas
	Ref        string
	Valor      float64 // positivo = nota a pagar (o SAP exporta o líquido com o sinal invertido)
	Doc        string
	DtLanc     string // AAAA-MM-DD
	Venc       string
	DtComp     string // vazio = em aberto
	DocComp    string
	Bloq       bool
	Divisao    string
	Texto      string
}

// ResumoFBL1N conta o que foi lido.
type ResumoFBL1N struct {
	Linhas       int      `json:"linhas"`
	Aproveitadas int      `json:"aproveitadas"`
	Empresas     []string `json:"empresas"`
}

var reZerosBP = regexp.MustCompile(`^0+(\d{10})`)

// LerFBL1N lê o export da FBL1N.
func LerFBL1N(dados []byte) ([]PartidaFBL1N, ResumoFBL1N, error) {
	var res ResumoFBL1N
	f, err := abrirXLSX(dados)
	if err != nil {
		return nil, res, err
	}
	defer f.Close()
	abas := f.GetSheetList()
	if len(abas) == 0 {
		return nil, res, errors.New("a planilha não tem nenhuma aba")
	}
	rows, err := f.GetRows(abas[0])
	if err != nil {
		return nil, res, fmt.Errorf("não foi possível ler a aba %q: %w", abas[0], err)
	}
	hi, col, ok := cabecalhoSAP(rows, colunasFBL1N...)
	if !ok {
		return nil, res, errors.New("não parece uma FBL1N: faltam as colunas Conta, Referência, Data de compensação e Doc.compensação")
	}
	vcol := ""
	for _, c := range []string{"Valor Liquido", "Montante em moeda interna", "Montante em MI"} {
		if _, tem := col[normaliza(c)]; tem {
			vcol = c
			break
		}
	}
	if vcol == "" {
		return nil, res, errors.New("a FBL1N não tem a coluna de valor (Valor Liquido ou Montante em moeda interna)")
	}
	doc := "Nº documento"
	if _, tem := col[normaliza(doc)]; !tem {
		doc = "No documento"
	}

	empresas := map[string]bool{}
	var out []PartidaFBL1N
	for r := hi + 1; r < len(rows); r++ {
		row := rows[r]
		g := func(n string) string { return celulaSAP(row, col, n) }
		if g("Conta") == "" && g(vcol) == "" {
			continue
		}
		res.Linhas++
		bp := reZerosBP.ReplaceAllString(g("Conta"), "$1")
		if bp == "" {
			continue
		}
		p := PartidaFBL1N{
			Empresa: g("Empresa"), BP: bp, Fornecedor: g("Descrição do Fornecedor"),
			Tipo: strings.ToUpper(g("Tipo de documento")), Ref: g("Referência"),
			Valor: -numeroFBL(g(vcol)), Doc: g(doc),
			DtLanc: dataSAP(g("Data de lançamento")), Venc: dataSAP(g("Vencimento líquido")),
			DtComp: dataSAP(g("Data de compensação")), DocComp: g("Doc.compensação"),
			Bloq: g("Bloqueio pgto.") != "", Divisao: g("Divisão"), Texto: cortar(g("Texto"), 60),
		}
		out = append(out, p)
		if p.Empresa != "" {
			empresas[p.Empresa] = true
		}
	}
	res.Aproveitadas = len(out)
	for e := range empresas {
		res.Empresas = append(res.Empresas, e)
	}
	sort.Strings(res.Empresas)
	if len(out) == 0 {
		return nil, res, errors.New("a FBL1N não tem nenhuma partida de fornecedor")
	}
	return out, res, nil
}

var (
	reDecimalBR = regexp.MustCompile(`,\d{1,2}-?$`)
	reSufixoNeg = regexp.MustCompile(`^(.*)-$`)
)

// numeroFBL converte o valor: número puro ("-4180.00") ou texto brasileiro ("1.234,56-").
func numeroFBL(s string) float64 {
	t := strings.Join(strings.Fields(s), "")
	if t == "" {
		return 0
	}
	var x string
	if reDecimalBR.MatchString(t) {
		x = strings.ReplaceAll(t, ".", "")
		x = strings.Replace(x, ",", ".", 1)
		x = reSufixoNeg.ReplaceAllString(x, "-$1")
	} else {
		x = strings.ReplaceAll(t, ",", "")
	}
	v, err := strconv.ParseFloat(x, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// ---- cruzamento com os lançamentos ----

// Pag é o status de pagamento de uma nota lançada.
type Pag struct {
	St      string  `json:"st"` // Paga | Em aberto | Vencida | Não localizada
	How     string  `json:"how"`
	DtComp  string  `json:"dtComp,omitempty"`
	DocComp string  `json:"docComp,omitempty"`
	Liq     float64 `json:"liq,omitempty"`
	VencSap string  `json:"vencSap,omitempty"`
	Ref     string  `json:"ref,omitempty"`
	Emp     string  `json:"emp,omitempty"`
	Bloq    bool    `json:"bloq,omitempty"`
}

// Orfa é uma nota de fornecedor da FBL1N que nenhuma linha da planilha explica.
type Orfa struct {
	Emp     string  `json:"emp"`
	BP      string  `json:"bp"`
	Forn    string  `json:"forn"`
	Ref     string  `json:"ref"`
	Doc     string  `json:"doc"`
	DtLanc  string  `json:"dtLanc,omitempty"`
	Venc    string  `json:"venc,omitempty"`
	Liq     float64 `json:"liq"`
	DtComp  string  `json:"dtComp,omitempty"`
	DocComp string  `json:"docComp,omitempty"`
	Div     string  `json:"div"`
	Txt     string  `json:"txt"`
}

// PagMeta descreve a carga da FBL1N usada.
type PagMeta struct {
	Fonte    string   `json:"fonte"`
	Extraido string   `json:"extraido"`
	Empresas []string `json:"empresas"`
	Linhas   int      `json:"linhas"`
}

// PagModelo é o bloco "pag" do modelo.
type PagModelo struct {
	Meta    PagMeta `json:"meta"`
	Orphans []Orfa  `json:"orphans"`
}

// bpPorNomeNaFBL1N liga o último recurso para achar o BP de um lançamento sem BP: o BP que a própria
// FBL1N traz para um fornecedor de mesmo nome (extensão em relação ao dashboard do Welton, que só
// usa a coluna BP da planilha; o teste de gabarito desliga isto para comparar campo a campo).
var bpPorNomeNaFBL1N = true

var (
	rePrefixoNF  = regexp.MustCompile(`^(NFS-?E|NFE|NFS|NF|FAT|FATURA|INVOICE)[^0-9]*`)
	reNumeroNF   = regexp.MustCompile(`^0*(\d+)`)
	reBPValido   = regexp.MustCompile(`^\d{8,10}$`)
	reNaoAlfanum = regexp.MustCompile(`[^a-z0-9]`)
)

// nfNumero reduz a referência da nota ao número (sem prefixo NF/FAT e sem zeros à esquerda).
func nfNumero(x string) string {
	x = rePrefixoNF.ReplaceAllString(strings.ToUpper(strings.TrimSpace(x)), "")
	if m := reNumeroNF.FindStringSubmatch(x); m != nil {
		return m[1]
	}
	return ""
}

func chaveFornecedor(s string) string {
	k := reNaoAlfanum.ReplaceAllString(normaliza(s), "")
	if len(k) > 15 {
		k = k[:15]
	}
	return k
}

func mesDaData(s string) string {
	if len(s) >= 7 {
		return s[:7]
	}
	return ""
}

// mesSoma soma k meses a um AAAA-MM.
func mesSoma(ym string, k int) string {
	a, _ := strconv.Atoi(ym[:4])
	b, _ := strconv.Atoi(ym[5:7])
	b = b - 1 + k
	a += int(math.Floor(float64(b) / 12))
	b = ((b % 12) + 12) % 12
	return fmt.Sprintf("%04d-%02d", a, b+1)
}

type grupoNota struct {
	chave string
	bp    string
	nf    string
	ls    []int // índices em rows
	total float64
}

// CruzarPagamentos preenche Pag nas linhas realizadas do modelo e devolve o bloco "pag". Só entram
// linhas da planilha (não as vindas da KSB1), classificadas como Realizado e com competência.
// `hoje` (AAAA-MM-DD) decide quando "Em aberto" vira "Vencida".
func CruzarPagamentos(rows []LancRow, partidas []PartidaFBL1N, fonte, extraido, hoje string) PagModelo {
	type partida struct {
		PartidaFBL1N
		i int
	}
	F := make([]partida, len(partidas))
	for i, p := range partidas {
		F[i] = partida{p, i}
	}
	var inv []partida
	byBP := map[string][]partida{}
	bps := map[string]bool{}
	for _, x := range F {
		bps[x.BP] = true
		if x.Tipo == "RE" || x.Tipo == "KR" || (x.Tipo == "" && x.Valor > 0) {
			inv = append(inv, x)
			byBP[x.BP] = append(byBP[x.BP], x)
		}
	}

	// BP de cada lançamento: coluna BP; se vazia, o BP mais usado pelo mesmo fornecedor na planilha;
	// se ainda assim não houver, o BP que a própria FBL1N traz para um fornecedor de mesmo nome.
	votos := map[string]map[string]int{}
	ordemVoto := map[string][]string{}
	votar := func(m map[string]map[string]int, o map[string][]string, forn, bp string) {
		k := chaveFornecedor(forn)
		if m[k] == nil {
			m[k] = map[string]int{}
		}
		if m[k][bp] == 0 {
			o[k] = append(o[k], bp)
		}
		m[k][bp]++
	}
	for _, l := range rows {
		if reBPValido.MatchString(strings.TrimSpace(l.BP)) {
			votar(votos, ordemVoto, l.Forn, strings.TrimSpace(l.BP))
		}
	}
	votosFBL, ordemFBL := map[string]map[string]int{}, map[string][]string{}
	for _, x := range F {
		if x.Fornecedor != "" {
			votar(votosFBL, ordemFBL, x.Fornecedor, x.BP)
		}
	}
	maisVotado := func(m map[string]map[string]int, o map[string][]string, k string) string {
		best, n := "", 0
		for _, bp := range o[k] {
			if m[k][bp] > n {
				best, n = bp, m[k][bp]
			}
		}
		return best
	}
	bpDe := func(l LancRow) string {
		if b := strings.TrimSpace(l.BP); reBPValido.MatchString(b) {
			return b
		}
		k := chaveFornecedor(l.Forn)
		if b := maisVotado(votos, ordemVoto, k); b != "" {
			return b
		}
		if !bpPorNomeNaFBL1N {
			return ""
		}
		return maisVotado(votosFBL, ordemFBL, k)
	}

	// linhas candidatas e agrupamento por BP + NF
	var real []int
	for i, l := range rows {
		if !l.daSAP && l.Classif == "Realizado" && l.Comp != nil && *l.Comp != "" {
			real = append(real, i)
		}
	}
	grp := map[string]*grupoNota{}
	var chaves []string
	for _, i := range real {
		l := rows[i]
		bp, nf := bpDe(l), nfNumero(l.NF)
		k := bp + "|" + nf
		g := grp[k]
		if g == nil {
			g = &grupoNota{chave: k, bp: bp, nf: nf}
			grp[k] = g
			chaves = append(chaves, k)
		}
		g.ls = append(g.ls, i)
		g.total += l.Valor
	}
	order := make([]*grupoNota, 0, len(chaves))
	for _, k := range chaves {
		order = append(order, grp[k])
	}
	sort.SliceStable(order, func(a, b int) bool { return order[a].total > order[b].total })

	used := map[int]bool{}
	res := map[int]*Pag{}
	fin := func(ls []int, fr []partida, how string) {
		for _, x := range fr {
			used[x.i] = true
		}
		var pagas []partida
		for _, x := range fr {
			if x.DtComp != "" {
				pagas = append(pagas, x)
			}
		}
		p := &Pag{How: how, Bloq: false}
		if len(pagas) == len(fr) {
			p.St = "Paga"
			for _, x := range pagas {
				if x.DtComp > p.DtComp {
					p.DtComp = x.DtComp
				}
			}
		} else {
			p.St = "Em aberto"
		}
		p.DocComp = joinUnicos(func(yield func(string)) {
			for _, x := range pagas {
				yield(x.DocComp)
			}
		})
		var soma float64
		for _, x := range fr {
			soma += x.Valor
			if x.Bloq {
				p.Bloq = true
			}
		}
		p.Liq = math.Round(soma*100) / 100
		for _, x := range fr {
			if x.Venc != "" && (p.VencSap == "" || x.Venc < p.VencSap) {
				p.VencSap = x.Venc
			}
		}
		p.Ref = joinUnicos(func(yield func(string)) {
			for _, x := range fr {
				yield(x.Ref)
			}
		})
		p.Emp = joinUnicos(func(yield func(string)) {
			for _, x := range fr {
				yield(x.Empresa)
			}
		})
		for _, i := range ls {
			res[i] = p
		}
	}
	free := func(bp string) []partida {
		var out []partida
		for _, x := range byBP[bp] {
			if !used[x.i] {
				out = append(out, x)
			}
		}
		return out
	}
	totalDe := func(ls []int) float64 {
		var t float64
		for _, i := range ls {
			t += rows[i].Valor
		}
		return t
	}

	// 1) BP + NF
	for _, g := range order {
		if g.bp == "" || g.nf == "" {
			continue
		}
		var fr []partida
		for _, x := range free(g.bp) {
			if nfNumero(x.Ref) == g.nf {
				fr = append(fr, x)
			}
		}
		if len(fr) > 0 {
			fin(g.ls, fr, "BP + NF")
		}
	}
	// 2) BP + NF parcial (um único candidato de valor compatível)
	for _, g := range order {
		if _, ok := res[g.ls[0]]; ok || g.bp == "" || len(g.nf) < 3 {
			continue
		}
		t := totalDe(g.ls)
		var fr []partida
		for _, x := range free(g.bp) {
			r := nfNumero(x.Ref)
			if len(r) >= 3 && (strings.HasSuffix(r, g.nf) || strings.HasSuffix(g.nf, r)) && x.Valor >= 0.75*t && x.Valor <= 1.01*t {
				fr = append(fr, x)
			}
		}
		if len(fr) == 1 {
			fin(g.ls, fr, "BP + NF parcial")
		}
	}
	// 3) BP + valor + mês (mês da competência ±1 pela data de lançamento)
	for _, g := range order {
		if _, ok := res[g.ls[0]]; ok || g.bp == "" {
			continue
		}
		t, c := totalDe(g.ls), *rows[g.ls[0]].Comp
		c = c[:7]
		janela := map[string]bool{mesSoma(c, -1): true, c: true, mesSoma(c, 1): true}
		var fr []partida
		for _, x := range free(g.bp) {
			if janela[mesDaData(x.DtLanc)] && x.Valor >= 0.75*t && x.Valor <= 1.001*t {
				fr = append(fr, x)
			}
		}
		sort.SliceStable(fr, func(a, b int) bool { return math.Abs(fr[a].Valor-t) < math.Abs(fr[b].Valor-t) })
		if len(fr) > 0 {
			fin(g.ls, []partida{fr[0]}, "BP + valor + mês")
		}
	}
	// 4) BP + MIRO agrupada + valor
	mg := map[string][]int{}
	var mgOrdem []string
	for _, i := range real {
		l := rows[i]
		if _, ok := res[i]; ok || l.Miro == "" {
			continue
		}
		if b := bpDe(l); b != "" {
			k := b + "|" + l.Miro
			if _, ja := mg[k]; !ja {
				mgOrdem = append(mgOrdem, k)
			}
			mg[k] = append(mg[k], i)
		}
	}
	for _, k := range mgOrdem {
		ls := mg[k]
		bp := strings.SplitN(k, "|", 2)[0]
		t := totalDe(ls)
		janela := map[string]bool{}
		for _, i := range ls {
			c := (*rows[i].Comp)[:7]
			for _, d := range []int{-1, 0, 1} {
				janela[mesSoma(c, d)] = true
			}
		}
		var fr []partida
		for _, x := range free(bp) {
			if janela[mesDaData(x.DtLanc)] && x.Valor >= 0.75*t && x.Valor <= 1.001*t {
				fr = append(fr, x)
			}
		}
		sort.SliceStable(fr, func(a, b int) bool { return math.Abs(fr[a].Valor-t) < math.Abs(fr[b].Valor-t) })
		if len(fr) > 0 {
			fin(ls, []partida{fr[0]}, "BP + MIRO agrupada + valor")
		}
	}

	for _, i := range real {
		p, ok := res[i]
		if !ok {
			b := bpDe(rows[i])
			how := "NF/valor não encontrados no BP"
			if b == "" {
				how = "sem BP (preencha CNPJ/CPF na planilha)"
			} else if !bps[b] {
				how = "BP não está na extração da FBL1N"
			}
			p = &Pag{St: "Não localizada", How: how}
		} else if p.St == "Em aberto" && p.VencSap != "" && p.VencSap < hoje {
			c := *p
			c.St = "Vencida"
			p = &c
		}
		rows[i].Pag = p
	}

	orfas := []Orfa{}
	empresas := map[string]bool{}
	for _, x := range F {
		if x.Empresa != "" {
			empresas[x.Empresa] = true
		}
	}
	for _, x := range inv {
		if !used[x.i] && (x.Tipo == "RE" || x.Tipo == "KR") {
			orfas = append(orfas, Orfa{Emp: x.Empresa, BP: x.BP, Forn: x.Fornecedor, Ref: x.Ref, Doc: x.Doc, DtLanc: x.DtLanc,
				Venc: x.Venc, Liq: math.Round(x.Valor*100) / 100, DtComp: x.DtComp, DocComp: x.DocComp, Div: x.Divisao, Txt: x.Texto})
		}
	}
	emps := make([]string, 0, len(empresas))
	for e := range empresas {
		emps = append(emps, e)
	}
	sort.Strings(emps)
	return PagModelo{Meta: PagMeta{Fonte: fonte, Extraido: extraido, Empresas: emps, Linhas: len(F)}, Orphans: orfas}
}

// joinUnicos junta, sem repetição e na ordem em que aparecem, os valores não vazios.
func joinUnicos(gera func(yield func(string))) string {
	vistos := map[string]bool{}
	var out []string
	gera(func(s string) {
		if s != "" && !vistos[s] {
			vistos[s] = true
			out = append(out, s)
		}
	})
	return strings.Join(out, ", ")
}

// hojeISO formata o dia de referência.
func hojeISO(t time.Time) string { return t.Format("2006-01-02") }
