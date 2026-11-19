package controleti

// ksb.go — leitura das exportações do SAP usadas na conferência (fase 3):
//
//	KSB1  realizado (lançamentos contábeis por pedido, CC e data de lançamento)
//	KSB2  compromissado (saldo em aberto dos pedidos)
//
// Os dois são .xlsx de uma aba só ("Data"), exportados do SAP com os centros de
// custo da relação. O tipo de cada arquivo é detectado pelo cabeçalho, para a
// importação aceitar os três arquivos juntos (Forecast + KSB1 + KSB2) sem o
// usuário dizer qual é qual.
//
// Minimização (LGPD): só pedido, CC, classe de custo, fornecedor/CNPJ, valor,
// datas e referências de estorno ficam. O "Nome do usuário" do SAP não é lido.

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// Tipos de arquivo aceitos na importação.
const (
	TipoForecast = "forecast"
	TipoKSB1     = "ksb1"
	TipoKSB2     = "ksb2"
)

// KSB1Linha é uma linha da KSB1 que tem pedido (as sem pedido — folha,
// provisões — não entram na conferência e não são guardadas).
type KSB1Linha struct {
	Pedido     string
	CCSap      string // "Centro custo": divisão + CC de 4 dígitos, igual ao CC SAP da planilha
	Conta      string
	Fornecedor string
	CNPJ       string
	Texto      string
	Valor      float64
	DataLanc   string // AAAA-MM-DD
	// DataEntrada: data de entrada (criação) do documento no SAP; vazia quando o layout da KSB1 ainda não a traz.
	DataEntrada string
	DocRef      string
	Estornado   bool
	DocEstorno  string
	RefEstorno  string
}

// KSB2Linha é uma linha de detalhe da KSB2 (saldo em aberto). As linhas de
// total (sem classe de custo) são descartadas: somá-las dobraria o saldo.
type KSB2Linha struct {
	Pedido     string // "Nº doc.de referência"
	Item       string
	CCSap      string
	Conta      string
	Fornecedor string
	Valor      float64
	DataDoc    string
	Texto      string
}

// ResumoLeituraSAP conta o que foi lido de uma KSB.
type ResumoLeituraSAP struct {
	Linhas       int    `json:"linhas"`       // linhas de dados no arquivo
	Aproveitadas int    `json:"aproveitadas"` // as que entram (com pedido na KSB1; detalhe na KSB2)
	SemPedido    int    `json:"sem_pedido"`   // KSB1: descartadas por não terem pedido
	Totais       int    `json:"totais"`       // KSB2: linhas de total descartadas
	PrimeiroMes  string `json:"primeiro_mes"` // KSB1: menor mês de lançamento (AAAA-MM)
	UltimoMes    string `json:"ultimo_mes"`   // KSB1: maior mês de lançamento (AAAA-MM)
}

func abrirXLSX(dados []byte) (*excelize.File, error) {
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
	return f, nil
}

// cabecalhoSAP procura, nas primeiras linhas, a que contém as colunas
// obrigatórias; devolve o índice da linha e o mapa nome-normalizado -> coluna.
func cabecalhoSAP(rows [][]string, obrigatorias ...string) (int, map[string]int, bool) {
	limite := len(rows)
	if limite > 10 {
		limite = 10
	}
	for i := 0; i < limite; i++ {
		col := map[string]int{}
		for j, v := range rows[i] {
			k := normaliza(v)
			if k == "" {
				continue
			}
			if _, ja := col[k]; !ja {
				col[k] = j
			}
		}
		ok := true
		for _, o := range obrigatorias {
			if _, tem := col[normaliza(o)]; !tem {
				ok = false
				break
			}
		}
		if ok {
			return i, col, true
		}
	}
	return 0, nil, false
}

var (
	colunasKSB1 = []string{"Documento de compras", "Data de lançamento", "Valor/MR", "Centro custo"}
	colunasKSB2 = []string{"Nº doc.de referência", "Quantidade total", "Valor/MR", "Centro custo"}
)

// DetectarTipo diz se o arquivo é a planilha Forecast, uma KSB1, uma KSB2 ou uma FBL1N.
func DetectarTipo(dados []byte) (string, error) {
	f, err := abrirXLSX(dados)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if ehDespesas(f) {
		return TipoDespesas, nil
	}
	if ehOrcamentoTI(f) {
		return TipoOrcamento, nil
	}
	for _, aba := range f.GetSheetList() {
		if strings.HasPrefix(normaliza(aba), "parametr") {
			return TipoForecast, nil
		}
	}
	abas := f.GetSheetList()
	if len(abas) == 0 {
		return "", errors.New("a planilha não tem nenhuma aba")
	}
	rows, err := f.GetRows(abas[0])
	if err != nil {
		return "", fmt.Errorf("não foi possível ler a primeira aba: %w", err)
	}
	if _, _, ok := cabecalhoSAP(rows, colunasKSB1...); ok {
		return TipoKSB1, nil
	}
	if _, _, ok := cabecalhoSAP(rows, colunasKSB2...); ok {
		return TipoKSB2, nil
	}
	if _, _, ok := cabecalhoSAP(rows, colunasFBL1N...); ok {
		return TipoFBL1N, nil
	}
	return "", errors.New("arquivo não reconhecido: não é a planilha Forecast (aba Parâmetros), nem uma KSB1, KSB2 ou FBL1N")
}

func celulaSAP(row []string, col map[string]int, nome string) string {
	i, ok := col[normaliza(nome)]
	if !ok {
		return ""
	}
	return textoCelula(row, i)
}

// dataSAP aceita número de série do Excel, ISO e os formatos brasileiros.
func dataSAP(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if d := diaDe(s); d != "" {
		return d
	}
	for _, layout := range []string{"02.01.2006", "02/01/2006", "2006/01/02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return ""
}

// LerKSB1 lê a KSB1 e devolve só as linhas com pedido.
func LerKSB1(dados []byte) ([]KSB1Linha, ResumoLeituraSAP, error) {
	var res ResumoLeituraSAP
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
	hi, col, ok := cabecalhoSAP(rows, colunasKSB1...)
	if !ok {
		return nil, res, errors.New("não parece uma KSB1: faltam as colunas Documento de compras, Data de lançamento, Valor/MR e Centro custo")
	}

	var out []KSB1Linha
	for r := hi + 1; r < len(rows); r++ {
		row := rows[r]
		cc := celulaSAP(row, col, "Centro custo")
		valorTxt := celulaSAP(row, col, "Valor/MR")
		if cc == "" && valorTxt == "" {
			continue // linha em branco
		}
		res.Linhas++
		pedido := celulaSAP(row, col, "Documento de compras")
		if pedido == "" {
			res.SemPedido++
			continue
		}
		data := dataSAP(celulaSAP(row, col, "Data de lançamento"))
		if data == "" {
			return nil, res, fmt.Errorf("linha %d: data de lançamento ausente ou inválida", r+1)
		}
		out = append(out, KSB1Linha{
			Pedido: pedido, CCSap: cc, Conta: celulaSAP(row, col, "Classe de custo"),
			Fornecedor: celulaSAP(row, col, "Razão Social Fornecedor"), CNPJ: somenteDigitos(celulaSAP(row, col, "CNPJ")),
			Texto: celulaSAP(row, col, "Texto do pedido"), Valor: r2(numero(valorTxt)), DataLanc: data,
			DataEntrada: dataSAP(primeiraCelula(row, col, colunasEntrada...)),
			DocRef:      celulaSAP(row, col, "Nº doc.de referência"),
			Estornado:   strings.EqualFold(celulaSAP(row, col, "estornado"), "X"),
			DocEstorno:  celulaSAP(row, col, "Documento de estorno"), RefEstorno: celulaSAP(row, col, "Nº ref.estorno"),
		})
		m := data[:7]
		if res.PrimeiroMes == "" || m < res.PrimeiroMes {
			res.PrimeiroMes = m
		}
		if m > res.UltimoMes {
			res.UltimoMes = m
		}
	}
	res.Aproveitadas = len(out)
	if len(out) == 0 {
		return nil, res, errors.New("a KSB1 não tem nenhuma linha com pedido")
	}
	return out, res, nil
}

// LerKSB2 lê a KSB2 (saldo em aberto), descartando as linhas de total.
func LerKSB2(dados []byte) ([]KSB2Linha, ResumoLeituraSAP, error) {
	var res ResumoLeituraSAP
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
	hi, col, ok := cabecalhoSAP(rows, colunasKSB2...)
	if !ok {
		return nil, res, errors.New("não parece uma KSB2: faltam as colunas Nº doc.de referência, Quantidade total, Valor/MR e Centro custo")
	}

	var out []KSB2Linha
	for r := hi + 1; r < len(rows); r++ {
		row := rows[r]
		classe := celulaSAP(row, col, "Classe de custo")
		pedido := celulaSAP(row, col, "Nº doc.de referência")
		if classe == "" && pedido == "" && celulaSAP(row, col, "Valor/MR") == "" {
			continue
		}
		res.Linhas++
		// as linhas de total não têm classe de custo
		if classe == "" {
			res.Totais++
			continue
		}
		if pedido == "" {
			continue
		}
		out = append(out, KSB2Linha{
			Pedido: pedido, Item: celulaSAP(row, col, "Item referência"), CCSap: celulaSAP(row, col, "Centro custo"),
			Conta: classe, Fornecedor: celulaSAP(row, col, "Fornecedor"),
			Valor: r2(numero(celulaSAP(row, col, "Valor/MR"))), DataDoc: dataSAP(celulaSAP(row, col, "Data do documento")),
			Texto: celulaSAP(row, col, "Texto breve material"),
		})
	}
	res.Aproveitadas = len(out)
	if len(out) == 0 {
		return nil, res, errors.New("a KSB2 não tem nenhuma linha de detalhe")
	}
	return out, res, nil
}

// colunasEntrada: nomes aceitos para a data de entrada do documento no SAP (KSB1 e FBL1N).
var colunasEntrada = []string{"Data de entrada", "Entrado em", "Data entrada", "Data de criação", "Criado em"}

func primeiraCelula(row []string, col map[string]int, nomes ...string) string {
	for _, n := range nomes {
		if _, ok := col[normaliza(n)]; ok {
			return celulaSAP(row, col, n)
		}
	}
	return ""
}

func somenteDigitos(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
