// Package controleti implementa o módulo Controle TI: importação da planilha
// de despesas de TI (FC FY27 Forecast), montagem do modelo de orçado /
// realizado / a lançar / projeção e a persistência em ti_*.
//
// planilha.go lê o .xlsx. É um porte fiel do parser que acompanha o painel
// HTML de 2026 (fcxParseWorkbook): mesmas abas, mesmos cabeçalhos (comparados
// sem acento e sem diferenciar maiúsculas), mesmas regras de descarte de
// linha. Os números têm de bater com os do painel antigo.
package controleti

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Limites contra .xlsx malicioso (um .xlsx é um zip): o corpo já é limitado
// pelo handler; estes limitam o que a biblioteca descompacta.
const (
	limiteDescompactado = 200 << 20
	limiteXMLUnico      = 64 << 20
)

// ParamCC etc. são as linhas das tabelas da aba Parâmetros.
type ParamCC struct{ Descricao, Responsavel, Pasta string }
type ParamConta struct{ Descricao, Tipo string }
type ParamDivisao struct{ Descricao, Empresa, Pasta string }

// Params agrupa as tabelas da aba Parâmetros (T_CC, T_Conta, T_Status,
// T_Divisao, T_Fornecedor).
type Params struct {
	CC         map[string]ParamCC
	Conta      map[string]ParamConta
	Status     map[string]string // status -> classificação
	Divisao    map[string]ParamDivisao
	Fornecedor map[string]string   // fornecedor -> pasta
	FornBP     map[string][]string // CNPJ/CPF (só dígitos) -> BPs (T_FornBP)
}

// OrcLinha é uma linha da aba Orçamento Forecast; Valores acompanha
// Planilha.Meses.
type OrcLinha struct {
	Ref, Origem, Divisao, CC, Conta, Fornecedor, Descricao string
	Valores                                                []float64
}

// Lanc é uma linha da aba Lançamentos Mensais. Competencia e Vencimento são
// "AAAA-MM-DD" ou "" quando a célula está vazia.
type Lanc struct {
	Ref, Origem, Status, Pedido, Folha, Miro, NF string
	Competencia, Vencimento                      string
	Valor                                        float64
	Fornecedor, Descricao, CCSap, Conta          string
	// BP e CNPJ: colunas novas da planilha Despesas TI 2027 (podem não existir).
	BP, CNPJ string
	// ForcaRealizado marca as linhas criadas a partir da KSB1 (realizado oficial do
	// SAP, fase 3): contam como realizado qualquer que seja o status.
	ForcaRealizado bool
	// Subst: linha da planilha que a KSB1 substitui (continua na lista, mas não soma). Tardio/MesLanc:
	// lançamento do SAP que entrou depois do corte do fechamento (conta no mês da janela).
	Subst, Tardio bool
	MesLanc       string
}

// Planilha é o conteúdo lido do .xlsx, ainda sem campos derivados.
type Planilha struct {
	Params      Params
	Meses       []string // AAAA-MM, na ordem das colunas do orçamento
	Orcamento   []OrcLinha
	Lancamentos []Lanc
}

var (
	semAcento = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
)

// normaliza compara cabeçalhos: sem espaços nas pontas, minúsculo, sem acento.
func normaliza(s string) string {
	out, _, err := transform.String(semAcento, strings.ToLower(strings.TrimSpace(s)))
	if err != nil {
		return strings.ToLower(strings.TrimSpace(s))
	}
	return out
}

func r2(v float64) float64 { return math.Round(v*100) / 100 }

// numero converte uma célula em número (vírgula decimal aceita); texto não
// numérico vira 0, como no parser original.
func numero(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if v, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", "."), 64); err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) {
		return v
	}
	return 0
}

func textoCelula(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

// Datas do Excel chegam como número de série (dias desde 30/12/1899). O
// leitor usa RawCellValue, então a célula vem como "46054" e não como texto
// formatado — o formato visível da célula não interfere.
var baseExcel = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

func serialParaData(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		if v < 20000 { // o parser original ignora séries pequenas
			return time.Time{}, false
		}
		return baseExcel.AddDate(0, 0, int(math.Floor(v))), true
	}
	// célula gravada como data ISO (t="d")
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04:05Z", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func mesDe(s string) string {
	if t, ok := serialParaData(s); ok {
		return t.Format("2006-01")
	}
	return ""
}

func diaDe(s string) string {
	if t, ok := serialParaData(s); ok {
		return t.Format("2006-01-02")
	}
	return ""
}

func ehNumero(s string) bool {
	_, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return err == nil
}

// LerPlanilha lê o .xlsx e devolve orçamento, lançamentos e parâmetros.
func LerPlanilha(dados []byte) (*Planilha, error) {
	if len(dados) < 4 || dados[0] != 'P' || dados[1] != 'K' {
		return nil, errors.New("o arquivo não é uma planilha .xlsx")
	}
	f, err := excelize.OpenReader(bytes.NewReader(dados), excelize.Options{
		RawCellValue:      true,
		UnzipSizeLimit:    limiteDescompactado,
		UnzipXMLSizeLimit: limiteXMLUnico,
	})
	if err != nil {
		return nil, fmt.Errorf("não foi possível abrir a planilha: %w", err)
	}
	defer f.Close()

	abas := f.GetSheetList()
	linhasDa := func(nome string) ([][]string, error) {
		alvo := normaliza(nome)
		prefixo := alvo
		if len(prefixo) > 8 {
			prefixo = prefixo[:8]
		}
		achada := ""
		for _, a := range abas {
			if normaliza(a) == alvo {
				achada = a
				break
			}
		}
		if achada == "" {
			for _, a := range abas {
				if strings.HasPrefix(normaliza(a), prefixo) {
					achada = a
					break
				}
			}
		}
		if achada == "" {
			return nil, fmt.Errorf("aba não encontrada: %s", nome)
		}
		return f.GetRows(achada)
	}

	pRows, err := linhasDa("Parâmetros")
	if err != nil {
		return nil, err
	}
	params, err := lerParametros(pRows)
	if err != nil {
		return nil, err
	}

	pl := &Planilha{Params: params}

	bRows, err := linhasDa("Orçamento Forecast")
	if err != nil {
		return nil, err
	}
	if err := lerOrcamento(pl, bRows); err != nil {
		return nil, err
	}

	lRows, err := linhasDa("Lançamentos Mensais")
	if err != nil {
		return nil, err
	}
	if err := lerLancamentos(pl, lRows); err != nil {
		return nil, err
	}

	if len(pl.Orcamento) == 0 && len(pl.Lancamentos) == 0 {
		return nil, errors.New("a planilha não tem nenhuma linha de orçamento nem de lançamento")
	}
	return pl, nil
}

type cabecalho struct {
	linha int
	col   map[string]int
	meses []mesCol
}

type mesCol struct {
	ym string
	i  int
}

// acharCabecalho localiza a linha cuja 1ª coluna é primeiraColuna ("ID").
func acharCabecalho(rows [][]string, primeiraColuna string) (cabecalho, error) {
	hi := -1
	for i, r := range rows {
		if len(r) > 0 && normaliza(r[0]) == normaliza(primeiraColuna) {
			hi = i
			break
		}
	}
	if hi < 0 {
		return cabecalho{}, errors.New("cabeçalho não encontrado")
	}
	c := cabecalho{linha: hi, col: map[string]int{}}
	for i, v := range rows[hi] {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if !ehNumero(v) { // cabeçalho de texto; número = coluna de mês
			k := normaliza(v)
			if _, ja := c.col[k]; !ja {
				c.col[k] = i
			}
		}
		if ym := mesDe(v); ym != "" && ehNumero(v) {
			c.meses = append(c.meses, mesCol{ym: ym, i: i})
		}
	}
	return c, nil
}

func (c cabecalho) idx(nome string) int {
	if i, ok := c.col[normaliza(nome)]; ok {
		return i
	}
	return -1
}

func lerOrcamento(pl *Planilha, rows [][]string) error {
	h, err := acharCabecalho(rows, "ID")
	if err != nil {
		return fmt.Errorf("aba Orçamento Forecast: %w", err)
	}
	for _, m := range h.meses {
		pl.Meses = append(pl.Meses, m.ym)
	}
	iOrigem, iDiv, iCC := h.idx("Origem"), h.idx("Divisão"), h.idx("CC")
	iConta, iForn, iDesc := h.idx("Conta"), h.idx("Fornecedor"), h.idx("Descrição")

	for r := h.linha + 1; r < len(rows); r++ {
		row := rows[r]
		id := textoCelula(row, 0)
		if id == "" || normaliza(textoCelula(row, iOrigem)) == "total" {
			continue
		}
		vals := make([]float64, len(h.meses))
		for j, m := range h.meses {
			if m.i < len(row) {
				vals[j] = r2(numero(row[m.i]))
			}
		}
		pl.Orcamento = append(pl.Orcamento, OrcLinha{
			Ref: id, Origem: textoCelula(row, iOrigem), Divisao: textoCelula(row, iDiv), CC: textoCelula(row, iCC),
			Conta: textoCelula(row, iConta), Fornecedor: textoCelula(row, iForn), Descricao: textoCelula(row, iDesc),
			Valores: vals,
		})
	}
	return nil
}

func lerLancamentos(pl *Planilha, rows [][]string) error {
	h, err := acharCabecalho(rows, "ID")
	if err != nil {
		return fmt.Errorf("aba Lançamentos Mensais: %w", err)
	}
	g := func(row []string, nome string) string { return textoCelula(row, h.idx(nome)) }

	for r := h.linha + 1; r < len(rows); r++ {
		row := rows[r]
		// Linhas só com fórmulas (sem ID, fornecedor nem valor) são descartadas.
		if textoCelula(row, 0) == "" && g(row, "Fornecedor") == "" && g(row, "Valor") == "" {
			continue
		}
		valor := numero(g(row, "Valor"))
		if valor == 0 && g(row, "Fornecedor") == "" {
			continue
		}
		pl.Lancamentos = append(pl.Lancamentos, Lanc{
			Ref: textoCelula(row, 0), Origem: g(row, "Origem"), Status: strings.ToUpper(g(row, "Status")),
			Pedido: g(row, "Pedido"), Folha: g(row, "Folha"), Miro: g(row, "MIRO"), NF: g(row, "NF/Fatura"),
			Competencia: diaDe(g(row, "Competência")), Vencimento: diaDe(g(row, "Vencimento")),
			Valor: r2(valor), Fornecedor: g(row, "Fornecedor"), Descricao: g(row, "Descrição"),
			CCSap: g(row, "CC SAP"), Conta: g(row, "Conta Contábil"),
			BP: somenteDigitos(g(row, "BP")), CNPJ: g(row, "CNPJ/CPF"),
		})
	}
	return nil
}

func lerParametros(rows [][]string) (Params, error) {
	p := Params{
		CC: map[string]ParamCC{}, Conta: map[string]ParamConta{}, Status: map[string]string{},
		Divisao: map[string]ParamDivisao{}, Fornecedor: map[string]string{}, FornBP: map[string][]string{},
	}
	pHead := -1
	for i, r := range rows {
		for _, v := range r {
			if normaliza(v) == "descricao centro de custo" {
				pHead = i
				break
			}
		}
		if pHead >= 0 {
			break
		}
	}
	if pHead < 0 {
		return p, errors.New("aba Parâmetros: tabela de centros de custo não encontrada")
	}
	h := rows[pHead]

	// tabela devolve chave -> valores das colunas pedidas. As tabelas ficam
	// lado a lado na aba, então cada coluna de valor é procurada À DIREITA da
	// coluna-chave (primeira ocorrência da chave vence, como no original).
	tabela := func(chave string, valores ...string) map[string][]string {
		out := map[string][]string{}
		kc := -1
		for i, v := range h {
			if normaliza(v) == normaliza(chave) {
				kc = i
				break
			}
		}
		if kc < 0 {
			return out
		}
		vcs := make([]int, len(valores))
		for n, nome := range valores {
			vcs[n] = -1
			for j := kc + 1; j < len(h); j++ {
				if normaliza(h[j]) == normaliza(nome) {
					vcs[n] = j
					break
				}
			}
		}
		for r := pHead + 1; r < len(rows); r++ {
			k := textoCelula(rows[r], kc)
			if k == "" {
				continue
			}
			if _, ja := out[k]; ja {
				continue
			}
			vs := make([]string, len(vcs))
			for n, j := range vcs {
				vs[n] = textoCelula(rows[r], j)
			}
			out[k] = vs
		}
		return out
	}

	for k, v := range tabela("CC", "Descrição Centro de Custo", "Responsável", "Pasta") {
		p.CC[k] = ParamCC{Descricao: v[0], Responsavel: v[1], Pasta: v[2]}
	}
	for k, v := range tabela("Conta", "Descrição Conta", "Tipo de Lançamento") {
		p.Conta[k] = ParamConta{Descricao: v[0], Tipo: v[1]}
	}
	for k, v := range tabela("Status", "Classificação") {
		p.Status[k] = v[0]
	}
	for k, v := range tabela("Divisão", "Descrição Divisão", "Empresa", "Pasta") {
		p.Divisao[k] = ParamDivisao{Descricao: v[0], Empresa: v[1], Pasta: v[2]}
	}
	for k, v := range tabela("Fornecedor", "Pasta") {
		p.Fornecedor[k] = v[0]
	}

	// T_FornBP: coluna "BP" logo depois de "CNPJ/CPF"
	if jb := func() int {
		for j := 1; j < len(h); j++ {
			if normaliza(h[j]) == "bp" && normaliza(h[j-1]) == "cnpj/cpf" {
				return j
			}
		}
		return -1
	}(); jb > 0 {
		for r := pHead + 1; r < len(rows); r++ {
			d, b := somenteDigitos(textoCelula(rows[r], jb-1)), somenteDigitos(textoCelula(rows[r], jb))
			if d == "" || b == "" {
				continue
			}
			ja := false
			for _, x := range p.FornBP[d] {
				if x == b {
					ja = true
				}
			}
			if !ja {
				p.FornBP[d] = append(p.FornBP[d], b)
			}
		}
	}

	if len(p.Conta) == 0 {
		return p, errors.New("aba Parâmetros: tabela de contas (Conta / Descrição Conta / Tipo de Lançamento) vazia ou ausente")
	}
	return p, nil
}
