package controleti

// despesas.go — estrutura de 2027 (Welton, 19/11/2026): "Despesas TI AAAA.xlsx" (abas Lançamentos e
// Parâmetros, com BP e CNPJ/CPF por nota) e "Orçamento TI AAAA.xlsx" (aba Orçamento, meses "MM.AAAA").
// Um arquivo por ano; reimportar um ano não toca nos outros (2026 fica como histórico).

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Tipos novos de arquivo.
const (
	TipoDespesas  = "despesas"
	TipoOrcamento = "orcamento"
)

// Despesas é o conteúdo da Despesas TI AAAA.xlsx.
type Despesas struct {
	Ano         int
	Params      Params
	Lancamentos []Lanc
}

// OrcamentoTI é o conteúdo da Orçamento TI AAAA.xlsx.
type OrcamentoTI struct {
	Ano    int
	Meses  []string // AAAA-MM
	Linhas []OrcLinha
}

var (
	reAno    = regexp.MustCompile(`\b(20\d{2})\b`)
	reMesPto = regexp.MustCompile(`^(\d{2})\.(\d{4})$`)
)

func abrirPlanilha(dados []byte) (*excelize.File, error) {
	if len(dados) < 4 || dados[0] != 'P' || dados[1] != 'K' {
		return nil, errors.New("o arquivo não é uma planilha .xlsx")
	}
	f, err := excelize.OpenReader(bytes.NewReader(dados), excelize.Options{
		RawCellValue: true, UnzipSizeLimit: limiteDescompactado, UnzipXMLSizeLimit: limiteXMLUnico,
	})
	if err != nil {
		return nil, fmt.Errorf("não foi possível abrir a planilha: %w", err)
	}
	return f, nil
}

func abaExata(f *excelize.File, nome string) string {
	for _, a := range f.GetSheetList() {
		if normaliza(a) == normaliza(nome) {
			return a
		}
	}
	return ""
}

func anoDoTitulo(rows [][]string) int {
	for i := 0; i < len(rows) && i < 3; i++ {
		if len(rows[i]) > 0 {
			if m := reAno.FindString(rows[i][0]); m != "" {
				n, _ := strconv.Atoi(m)
				return n
			}
		}
	}
	return 0
}

// ehDespesas: aba "Lançamentos" (exata, a Forecast antiga tem "Lançamentos Mensais") com cabeçalho Origem/Status.
func ehDespesas(f *excelize.File) bool {
	a := abaExata(f, "Lançamentos")
	if a == "" || abaExata(f, "Parâmetros") == "" {
		return false
	}
	rows, err := f.GetRows(a)
	if err != nil {
		return false
	}
	_, e := acharCabecalho(rows, "Origem")
	return e == nil
}

func ehOrcamentoTI(f *excelize.File) bool {
	a := abaExata(f, "Orçamento")
	if a == "" {
		return false
	}
	rows, err := f.GetRows(a)
	if err != nil {
		return false
	}
	h, e := acharCabecalho(rows, "Origem")
	if e != nil {
		return false
	}
	for _, v := range rows[h.linha] {
		if reMesPto.MatchString(strings.TrimSpace(v)) {
			return true
		}
	}
	return false
}

// LerDespesas lê a Despesas TI AAAA.xlsx.
func LerDespesas(dados []byte) (*Despesas, error) {
	f, err := abrirPlanilha(dados)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if !ehDespesas(f) {
		return nil, errors.New("não parece a Despesas TI: faltam as abas Lançamentos (com cabeçalho Origem) e Parâmetros")
	}
	pRows, err := f.GetRows(abaExata(f, "Parâmetros"))
	if err != nil {
		return nil, err
	}
	params, err := lerParametros(pRows)
	if err != nil {
		return nil, err
	}
	lRows, err := f.GetRows(abaExata(f, "Lançamentos"))
	if err != nil {
		return nil, err
	}
	d := &Despesas{Params: params, Ano: anoDoTitulo(lRows)}
	h, _ := acharCabecalho(lRows, "Origem")
	g := func(row []string, nome string) string { return textoCelula(row, h.idx(nome)) }
	for r := h.linha + 1; r < len(lRows); r++ {
		row := lRows[r]
		valor := numero(g(row, "Valor"))
		if g(row, "Fornecedor") == "" && valor == 0 {
			continue // linha-modelo/em branco
		}
		d.Lancamentos = append(d.Lancamentos, Lanc{
			Origem: g(row, "Origem"), Status: strings.ToUpper(g(row, "Status")),
			Pedido: g(row, "Pedido"), Folha: g(row, "Folha"), Miro: g(row, "MIRO"), NF: g(row, "NF/Fatura"),
			Competencia: diaDe(g(row, "Competência")), Vencimento: diaDe(g(row, "Vencimento")),
			Valor: r2(valor), Fornecedor: g(row, "Fornecedor"), Descricao: g(row, "Descrição"),
			CCSap: g(row, "CC SAP"), Conta: g(row, "Conta Contábil"),
			BP: somenteDigitos(g(row, "BP")), CNPJ: g(row, "CNPJ/CPF"),
		})
		d.Lancamentos[len(d.Lancamentos)-1].Ref = "" // preenchida abaixo, com o ano
	}
	if d.Ano == 0 {
		for _, l := range d.Lancamentos {
			if len(l.Competencia) >= 4 {
				if n, _ := strconv.Atoi(l.Competencia[:4]); n > d.Ano {
					d.Ano = n
				}
			}
		}
	}
	if d.Ano == 0 {
		return nil, errors.New("não consegui descobrir o ano da planilha (título DESPESAS TI AAAA)")
	}
	for i := range d.Lancamentos {
		d.Lancamentos[i].Ref = fmt.Sprintf("%d-L%d", d.Ano, i+1)
	}
	return d, nil
}

// LerOrcamentoTI lê a Orçamento TI AAAA.xlsx.
func LerOrcamentoTI(dados []byte) (*OrcamentoTI, error) {
	f, err := abrirPlanilha(dados)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if !ehOrcamentoTI(f) {
		return nil, errors.New("não parece o Orçamento TI: falta a aba Orçamento com colunas de mês (MM.AAAA)")
	}
	rows, err := f.GetRows(abaExata(f, "Orçamento"))
	if err != nil {
		return nil, err
	}
	h, _ := acharCabecalho(rows, "Origem")
	o := &OrcamentoTI{}
	type mc struct {
		ym string
		i  int
	}
	var cols []mc
	for i, v := range rows[h.linha] {
		if m := reMesPto.FindStringSubmatch(strings.TrimSpace(v)); m != nil {
			cols = append(cols, mc{m[2] + "-" + m[1], i})
			o.Meses = append(o.Meses, m[2]+"-"+m[1])
		}
	}
	o.Ano, _ = strconv.Atoi(o.Meses[0][:4])
	g := func(row []string, nome string) string { return textoCelula(row, h.idx(nome)) }
	for r := h.linha + 1; r < len(rows); r++ {
		row := rows[r]
		origem := g(row, "Origem")
		if origem == "" || normaliza(origem) == "total" {
			continue
		}
		vals := make([]float64, len(cols))
		for j, c := range cols {
			if c.i < len(row) {
				vals[j] = r2(numero(row[c.i]))
			}
		}
		o.Linhas = append(o.Linhas, OrcLinha{
			Ref: fmt.Sprintf("OR%d-%d", o.Ano, r+1), Origem: origem, Divisao: g(row, "Divisão"), CC: g(row, "CC"),
			Conta: g(row, "Conta"), Fornecedor: g(row, "Fornecedor"), Descricao: g(row, "Descrição"), Valores: vals,
		})
	}
	if len(o.Linhas) == 0 {
		return nil, errors.New("o Orçamento TI não tem nenhuma linha")
	}
	return o, nil
}
