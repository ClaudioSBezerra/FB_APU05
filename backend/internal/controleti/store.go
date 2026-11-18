package controleti

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// FonteForecast identifica as linhas de orçamento vindas da planilha FC FY27
// Forecast: reimportar substitui só elas (um Orçamento TI 2027 futuro entra
// com outra fonte e não é tocado).
const FonteForecast = "forecast_xlsx"

// FontePlanilha identifica os lançamentos importados da planilha; lançamentos
// digitados no sistema ('manual') ou vindos do SAP ('sap_api') sobrevivem à
// reimportação.
const FontePlanilha = "planilha"

// Resumo conta o que a importação gravou.
type Resumo struct {
	Parametros      map[string]int `json:"parametros"`
	OrcamentoLinhas int            `json:"orcamento_linhas"`
	Meses           int            `json:"meses"`
	PrimeiroMes     string         `json:"primeiro_mes"`
	UltimoMes       string         `json:"ultimo_mes"`
	Lancamentos     int            `json:"lancamentos"`
}

// insereEmLotes monta INSERTs multilinha parametrizados (limite de 65535
// parâmetros por comando no Postgres).
func insereEmLotes(tx *sql.Tx, tabela string, colunas []string, n int, valores func(i int) []any) error {
	if n == 0 {
		return nil
	}
	porLote := 60000 / len(colunas)
	if porLote > 2000 {
		porLote = 2000
	}
	for ini := 0; ini < n; ini += porLote {
		fim := ini + porLote
		if fim > n {
			fim = n
		}
		var sb strings.Builder
		args := make([]any, 0, (fim-ini)*len(colunas))
		fmt.Fprintf(&sb, "INSERT INTO %s (%s) VALUES ", tabela, strings.Join(colunas, ", "))
		for i := ini; i < fim; i++ {
			if i > ini {
				sb.WriteByte(',')
			}
			sb.WriteByte('(')
			for c := range colunas {
				if c > 0 {
					sb.WriteByte(',')
				}
				fmt.Fprintf(&sb, "$%d", len(args)+c+1)
			}
			sb.WriteByte(')')
			args = append(args, valores(i)...)
		}
		if _, err := tx.Exec(sb.String(), args...); err != nil {
			return fmt.Errorf("inserir em %s: %w", tabela, err)
		}
	}
	return nil
}

func nuloSeVazio(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Importar grava a planilha numa transação: parâmetros são substituídos por
// inteiro (a planilha é a fonte deles), o orçamento da fonte forecast_xlsx e
// os lançamentos de fonte 'planilha' também; nada mais é tocado.
func Importar(db *sql.DB, p *Planilha, usuarioID, arquivo string) (Resumo, error) {
	var res Resumo
	tx, err := db.Begin()
	if err != nil {
		return res, fmt.Errorf("iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// --- parâmetros
	for _, t := range []string{"ti_param_cc", "ti_param_conta", "ti_param_status", "ti_param_divisao", "ti_param_fornecedor"} {
		if _, err := tx.Exec("DELETE FROM " + t); err != nil {
			return res, fmt.Errorf("limpar %s: %w", t, err)
		}
	}
	res.Parametros = map[string]int{}

	cc := chavesOrdenadas(p.Params.CC)
	if err := insereEmLotes(tx, "ti_param_cc", []string{"cc", "descricao", "responsavel", "pasta"}, len(cc), func(i int) []any {
		v := p.Params.CC[cc[i]]
		return []any{cc[i], v.Descricao, v.Responsavel, v.Pasta}
	}); err != nil {
		return res, err
	}
	res.Parametros["centros_custo"] = len(cc)

	contas := chavesOrdenadas(p.Params.Conta)
	if err := insereEmLotes(tx, "ti_param_conta", []string{"conta", "descricao", "tipo"}, len(contas), func(i int) []any {
		v := p.Params.Conta[contas[i]]
		return []any{contas[i], v.Descricao, v.Tipo}
	}); err != nil {
		return res, err
	}
	res.Parametros["contas"] = len(contas)

	status := chavesOrdenadas(p.Params.Status)
	if err := insereEmLotes(tx, "ti_param_status", []string{"status", "classificacao"}, len(status), func(i int) []any {
		return []any{status[i], p.Params.Status[status[i]]}
	}); err != nil {
		return res, err
	}
	res.Parametros["status"] = len(status)

	divs := chavesOrdenadas(p.Params.Divisao)
	if err := insereEmLotes(tx, "ti_param_divisao", []string{"divisao", "descricao", "empresa", "pasta"}, len(divs), func(i int) []any {
		v := p.Params.Divisao[divs[i]]
		return []any{divs[i], v.Descricao, v.Empresa, v.Pasta}
	}); err != nil {
		return res, err
	}
	res.Parametros["divisoes"] = len(divs)

	forns := chavesOrdenadas(p.Params.Fornecedor)
	if err := insereEmLotes(tx, "ti_param_fornecedor", []string{"fornecedor", "pasta"}, len(forns), func(i int) []any {
		return []any{forns[i], p.Params.Fornecedor[forns[i]]}
	}); err != nil {
		return res, err
	}
	res.Parametros["fornecedores"] = len(forns)

	// --- orçamento (valores apagados em cascata)
	if _, err := tx.Exec(`DELETE FROM ti_orcamento_linhas WHERE fonte = $1`, FonteForecast); err != nil {
		return res, fmt.Errorf("limpar orçamento: %w", err)
	}
	type valor struct {
		linha int64
		mes   string
		v     float64
	}
	var valores []valor
	for _, o := range p.Orcamento {
		var id int64
		if err := tx.QueryRow(`
			INSERT INTO ti_orcamento_linhas (fonte, ref, origem, divisao, cc, conta, fornecedor, descricao)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id
		`, FonteForecast, o.Ref, o.Origem, o.Divisao, o.CC, o.Conta, o.Fornecedor, o.Descricao).Scan(&id); err != nil {
			return res, fmt.Errorf("inserir linha de orçamento %s: %w", o.Ref, err)
		}
		for j, ym := range p.Meses {
			v := 0.0
			if j < len(o.Valores) {
				v = o.Valores[j]
			}
			valores = append(valores, valor{linha: id, mes: ym + "-01", v: v})
		}
	}
	if err := insereEmLotes(tx, "ti_orcamento_valores", []string{"linha_id", "mes", "valor"}, len(valores), func(i int) []any {
		return []any{valores[i].linha, valores[i].mes, valores[i].v}
	}); err != nil {
		return res, err
	}
	res.OrcamentoLinhas = len(p.Orcamento)
	res.Meses = len(p.Meses)
	if len(p.Meses) > 0 {
		res.PrimeiroMes, res.UltimoMes = p.Meses[0], p.Meses[len(p.Meses)-1]
	}

	// --- lançamentos
	if _, err := tx.Exec(`DELETE FROM ti_lancamentos WHERE fonte = $1`, FontePlanilha); err != nil {
		return res, fmt.Errorf("limpar lançamentos: %w", err)
	}
	if err := insereEmLotes(tx, "ti_lancamentos",
		[]string{"fonte", "ref", "origem", "status", "pedido", "folha", "miro", "nf", "competencia", "vencimento", "valor", "fornecedor", "descricao", "cc_sap", "conta", "bp", "cnpj"},
		len(p.Lancamentos), func(i int) []any {
			l := p.Lancamentos[i]
			return []any{FontePlanilha, l.Ref, l.Origem, l.Status, l.Pedido, l.Folha, l.Miro, l.NF,
				nuloSeVazio(l.Competencia), nuloSeVazio(l.Vencimento), l.Valor, l.Fornecedor, l.Descricao, l.CCSap, l.Conta,
				cortar(l.BP, 20), cortar(l.CNPJ, 30)}
		}); err != nil {
		return res, err
	}
	res.Lancamentos = len(p.Lancamentos)

	resumoJSON, _ := json.Marshal(res)
	var uid any
	if usuarioID != "" {
		uid = usuarioID
	}
	if _, err := tx.Exec(`INSERT INTO ti_importacoes (usuario_id, arquivo, resumo) VALUES ($1, $2, $3)`, uid, arquivo, string(resumoJSON)); err != nil {
		return res, fmt.Errorf("registrar importação: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("confirmar importação: %w", err)
	}
	return res, nil
}

func chavesOrdenadas[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Carregar lê o banco e monta o modelo. agora define o mês fechado
// automático (injetado para os testes).
// carregarEntrada lê os parâmetros, o orçamento e os lançamentos da planilha.
func carregarEntrada(db *sql.DB, agora time.Time) (Entrada, error) {
	in := Entrada{Agora: agora}
	in.Params = Params{
		CC: map[string]ParamCC{}, Conta: map[string]ParamConta{}, Status: map[string]string{},
		Divisao: map[string]ParamDivisao{}, Fornecedor: map[string]string{},
	}

	rows, err := db.Query(`SELECT cc, descricao, responsavel, pasta FROM ti_param_cc`)
	if err != nil {
		return Entrada{}, fmt.Errorf("ler ti_param_cc: %w", err)
	}
	for rows.Next() {
		var k string
		var v ParamCC
		if err := rows.Scan(&k, &v.Descricao, &v.Responsavel, &v.Pasta); err != nil {
			rows.Close()
			return Entrada{}, err
		}
		in.Params.CC[k] = v
	}
	rows.Close()

	rows, err = db.Query(`SELECT conta, descricao, tipo FROM ti_param_conta`)
	if err != nil {
		return Entrada{}, fmt.Errorf("ler ti_param_conta: %w", err)
	}
	for rows.Next() {
		var k string
		var v ParamConta
		if err := rows.Scan(&k, &v.Descricao, &v.Tipo); err != nil {
			rows.Close()
			return Entrada{}, err
		}
		in.Params.Conta[k] = v
	}
	rows.Close()

	rows, err = db.Query(`SELECT status, classificacao FROM ti_param_status`)
	if err != nil {
		return Entrada{}, fmt.Errorf("ler ti_param_status: %w", err)
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			rows.Close()
			return Entrada{}, err
		}
		in.Params.Status[k] = v
	}
	rows.Close()

	rows, err = db.Query(`SELECT divisao, descricao, empresa, pasta FROM ti_param_divisao`)
	if err != nil {
		return Entrada{}, fmt.Errorf("ler ti_param_divisao: %w", err)
	}
	for rows.Next() {
		var k string
		var v ParamDivisao
		if err := rows.Scan(&k, &v.Descricao, &v.Empresa, &v.Pasta); err != nil {
			rows.Close()
			return Entrada{}, err
		}
		in.Params.Divisao[k] = v
	}
	rows.Close()

	rows, err = db.Query(`
		SELECT l.id, l.ref, l.origem, l.divisao, l.cc, l.conta, l.fornecedor, l.descricao,
		       to_char(v.mes, 'YYYY-MM'), v.valor::float8
		FROM ti_orcamento_linhas l
		LEFT JOIN ti_orcamento_valores v ON v.linha_id = l.id
		ORDER BY l.id, v.mes
	`)
	if err != nil {
		return Entrada{}, fmt.Errorf("ler orçamento: %w", err)
	}
	var atual int64 = -1
	for rows.Next() {
		var id int64
		var o OrcDB
		var ym sql.NullString
		var val sql.NullFloat64
		if err := rows.Scan(&id, &o.Ref, &o.Origem, &o.Divisao, &o.CC, &o.Conta, &o.Fornecedor, &o.Descricao, &ym, &val); err != nil {
			rows.Close()
			return Entrada{}, err
		}
		if id != atual {
			o.Valores = map[string]float64{}
			in.Orcamento = append(in.Orcamento, o)
			atual = id
		}
		if ym.Valid {
			in.Orcamento[len(in.Orcamento)-1].Valores[ym.String] = val.Float64
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Entrada{}, err
	}
	rows.Close()

	rows, err = db.Query(`
		SELECT ref, origem, status, pedido, folha, miro, nf,
		       COALESCE(to_char(competencia, 'YYYY-MM-DD'), ''), COALESCE(to_char(vencimento, 'YYYY-MM-DD'), ''),
		       valor::float8, fornecedor, descricao, cc_sap, conta, bp, cnpj
		FROM ti_lancamentos ORDER BY id
	`)
	if err != nil {
		return Entrada{}, fmt.Errorf("ler lançamentos: %w", err)
	}
	for rows.Next() {
		var l Lanc
		if err := rows.Scan(&l.Ref, &l.Origem, &l.Status, &l.Pedido, &l.Folha, &l.Miro, &l.NF,
			&l.Competencia, &l.Vencimento, &l.Valor, &l.Fornecedor, &l.Descricao, &l.CCSap, &l.Conta, &l.BP, &l.CNPJ); err != nil {
			rows.Close()
			return Entrada{}, err
		}
		in.Lancamentos = append(in.Lancamentos, l)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Entrada{}, err
	}
	rows.Close()

	var override string
	_ = db.QueryRow(`SELECT valor FROM ti_config WHERE chave = 'ultimo_mes_fechado'`).Scan(&override)
	in.LastClosed = MesFechado(agora, override)

	var quando sql.NullTime
	_ = db.QueryRow(`SELECT arquivo, created_at FROM ti_importacoes ORDER BY id DESC LIMIT 1`).Scan(&in.Arquivo, &quando)
	if quando.Valid {
		in.ImportadoEm = quando.Time.Format(time.RFC3339)
	}

	return in, nil
}

// Carregar monta o modelo do painel: planilha + (se houver) KSB1/KSB2.
func Carregar(db *sql.DB, agora time.Time) (*Modelo, error) {
	in, err := carregarEntrada(db, agora)
	if err != nil {
		return nil, err
	}
	if err := carregarSAP(db, &in); err != nil {
		return nil, err
	}
	return Montar(in), nil
}

// Conferencia compara a planilha PURA com a KSB1. Não usa o modelo do painel: a partir
// de 2027 ele troca o realizado pelo do SAP, e conferir o SAP contra ele mesmo não prova nada.
func Conferencia(db *sql.DB, agora time.Time) (*ConfResultado, error) {
	in, err := carregarEntrada(db, agora)
	if err != nil {
		return nil, err
	}
	ksb1, err := CarregarKSB1(db)
	if err != nil {
		return nil, err
	}
	m := Montar(in)
	r := Conferir(m.Lanc, ksb1)
	return &r, nil
}
