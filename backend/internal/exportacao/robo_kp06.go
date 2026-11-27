package exportacao

// robo_kp06.go — lote de Despesa no formato do "Robo KP06 Despesa (novo).xlsm" (27/11/2026).
//   - aba `Chamados` (linhas 5–50): uma linha por chamado. `A` Processar? sai "NÃO": a liberação é HUMANA, na
//     planilha (o robô só grava chamado marcado "SIM" e com validação "OK").
//   - aba `Operacoes SAP` (linhas 5–104): uma linha por lançamento; transferência = origem negativa + destino
//     positivo (soma zero); inclusão = positivos. As fórmulas da planilha (A, V, W, X, AB) são escritas em cada linha.
// As linhas de demonstração do modelo (GO-4, GO-7…) e a prévia de "Linhas Base" são apagadas.

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

const (
	abaRoboKP06Chamados   = "Chamados"
	abaRoboKP06Operacoes  = "Operacoes SAP"
	primeiraLinhaKP06     = 5
	ultimaChamadoKP06     = 50
	ultimaOperacaoKP06    = 104
	maxChamadosLoteKP06   = ultimaChamadoKP06 - primeiraLinhaKP06 + 1
	maxOperacoesLoteKP06  = ultimaOperacaoKP06 - primeiraLinhaKP06 + 1
	semInformacaoRoboKP06 = "-"
)

type chamadoKP06 struct {
	codigo, tipo, solicitante, responsavel, autorizador, titulo, descricao, fornecedor string
}

type operacaoKP06 struct {
	lado, divisao, cc, conta string
	ano, mes                 int
	valor                    float64
}

func tipoKP06(tipo string) (string, bool) {
	switch tipo {
	case "transferencia":
		return "TRANSFERÊNCIA", true
	case "inclusao", "inclusao_sfc":
		return "INCLUSÃO", true
	}
	return "", false
}

func ouTraco(s string) string {
	if strings.TrimSpace(s) == "" {
		return semInformacaoRoboKP06
	}
	return strings.TrimSpace(s)
}

func lerChamadoKP06(db *sql.DB, id, administradorID string) (chamadoKP06, string, error) {
	var numero int64
	var tipo, solicitante string
	var responsavel, autorizador, descricao, fornecedor, titulo sql.NullString
	err := db.QueryRow(`
		SELECT s.numero, s.tipo_solicitacao, u.nome,
		       (SELECT nome FROM usuarios WHERE id = COALESCE(s.administrador_id, $2::uuid)),
		       (SELECT a.aprovador_nome FROM solicitacao_aprovacoes a WHERE a.solicitacao_id = s.id AND a.decisao = 'aprovada'
		         ORDER BY a.decidido_em DESC NULLS LAST LIMIT 1),
		       s.descricao, s.fornecedor, LEFT(s.descricao, 80)
		FROM solicitacoes s JOIN usuarios u ON u.id = s.solicitante_id
		WHERE s.id = $1`, id, administradorID).
		Scan(&numero, &tipo, &solicitante, &responsavel, &autorizador, &descricao, &fornecedor, &titulo)
	if err != nil {
		return chamadoKP06{}, "", fmt.Errorf("exportacao: ler chamado %s para o robô: %w", id, err)
	}
	c := chamadoKP06{codigo: fmt.Sprintf(codigoChamadoFmt, numero), solicitante: solicitante, responsavel: responsavel.String,
		autorizador: autorizador.String, descricao: descricao.String, fornecedor: fornecedor.String, titulo: titulo.String}
	return c, tipo, nil
}

func lerOperacoesKP06(db *sql.DB, id string) ([]operacaoKP06, error) {
	rows, err := db.Query(`
		SELECT sl.lado, d.codigo, cc.codigo, COALESCE(c.codigo, ''), sl.mes, sl.valor
		FROM solicitacao_lancamentos sl
		JOIN divisoes d ON d.id = sl.divisao_id
		JOIN centros_custo cc ON cc.id = sl.centro_custo_id
		LEFT JOIN contas c ON c.id = sl.conta_id
		WHERE sl.solicitacao_id = $1
		ORDER BY CASE sl.lado WHEN 'origem' THEN 0 ELSE 1 END, sl.mes, sl.created_at, sl.id`, id)
	if err != nil {
		return nil, fmt.Errorf("exportacao: ler lançamentos de %s para o robô: %w", id, err)
	}
	defer rows.Close()
	var out []operacaoKP06
	for rows.Next() {
		var o operacaoKP06
		var mes sql.NullTime
		if err := rows.Scan(&o.lado, &o.divisao, &o.cc, &o.conta, &mes, &o.valor); err != nil {
			return nil, err
		}
		if !mes.Valid || o.conta == "" {
			return nil, &ErrLoteRobo{Msg: "o robô KP06 só grava lançamentos com conta e mês — este chamado tem linha sem conta/mês (imobilizado?)"}
		}
		o.ano, o.mes = mes.Time.Year(), int(mes.Time.Month())
		if o.lado == "origem" {
			o.valor = -o.valor
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func montarLoteRoboKP06(db *sql.DB, templateBytes []byte, solicitacaoIDs []string, administradorID string) ([]byte, error) {
	if len(solicitacaoIDs) > maxChamadosLoteKP06 {
		return nil, &ErrLoteRobo{Msg: fmt.Sprintf("o robô KP06 aceita até %d chamados por lote; este tem %d", maxChamadosLoteKP06, len(solicitacaoIDs))}
	}
	ch := map[string]celulaRobo{}
	op := map[string]celulaRobo{}
	linhaOp := primeiraLinhaKP06
	for i, id := range solicitacaoIDs {
		c, tipo, err := lerChamadoKP06(db, id, administradorID)
		if err != nil {
			return nil, err
		}
		tipoRobo, ok := tipoKP06(tipo)
		if !ok {
			return nil, &ErrLoteRobo{Msg: fmt.Sprintf("o chamado %s é de %s — o robô KP06 só grava transferência e inclusão", c.codigo, tipo)}
		}
		ops, err := lerOperacoesKP06(db, id)
		if err != nil {
			if e, ok := err.(*ErrLoteRobo); ok {
				e.Msg = c.codigo + ": " + e.Msg
			}
			return nil, err
		}
		if linhaOp+len(ops)-1 > ultimaOperacaoKP06 {
			return nil, &ErrLoteRobo{Msg: fmt.Sprintf("o robô KP06 aceita até %d lançamentos por lote — divida em lotes menores", maxOperacoesLoteKP06)}
		}
		n := strconv.Itoa(primeiraLinhaKP06 + i)
		solicitado := 0.0
		for _, o := range ops {
			if o.valor > 0 {
				solicitado += o.valor
			}
		}
		ch["A"+n] = txt("NÃO")
		ch["B"+n] = formula(`IF(COUNTA(C` + n + `:E` + n + `,G` + n + `:K` + n + `)<8,"PENDÊNCIA: CAMPOS",IF(E` + n + `<>"EM ATENDIMENTO","PENDÊNCIA: STATUS",IF(D` + n + `="TRANSFERÊNCIA",IF(ABS(M` + n + `)>0.01,"ERRO: TRANSFERÊNCIA DESBALANCEADA","OK"),IF(D` + n + `="INCLUSÃO",IF(COUNTIFS('Operacoes SAP'!$B$5:$B$104,C` + n + `,'Operacoes SAP'!$M$5:$M$104,"<=0")>0,"ERRO: INCLUSÃO NÃO POSITIVA","OK"),"ERRO: TIPO"))))`)
		ch["C"+n] = txt(c.codigo)
		ch["D"+n] = txt(tipoRobo)
		ch["E"+n] = txt("EM ATENDIMENTO")
		ch["F"+n] = txt(ouTraco(c.responsavel))
		ch["G"+n] = txt(ouTraco(c.solicitante))
		ch["H"+n] = txt(ouTraco(c.autorizador))
		ch["I"+n] = txt(ouTraco(c.titulo))
		ch["J"+n] = txt(ouTraco(c.descricao))
		ch["K"+n] = txt(ouTraco(c.fornecedor))
		ch["L"+n] = num(arred2(solicitado))
		ch["M"+n] = formula(`SUMIFS('Operacoes SAP'!$M$5:$M$104,'Operacoes SAP'!$B$5:$B$104,C` + n + `)`)

		for seq, o := range ops {
			r := strconv.Itoa(linhaOp)
			papel := "INCLUSÃO"
			if tipoRobo == "TRANSFERÊNCIA" {
				papel = map[bool]string{true: "ORIGEM", false: "DESTINO"}[o.valor < 0]
			}
			op["A"+r] = formula(`IF(IFERROR(INDEX(Chamados!$B$5:$B$50,MATCH(B` + r + `,Chamados!$C$5:$C$50,0)),"")<>"OK","BLOQUEADO",IF(IFERROR(INDEX(Chamados!$A$5:$A$50,MATCH(B` + r + `,Chamados!$C$5:$C$50,0)),"NÃO")="SIM","PRONTO","AGUARDANDO LIBERAÇÃO"))`)
			op["B"+r] = txt(c.codigo)
			op["C"+r] = txt(c.codigo + "-OP01")
			op["D"+r] = txt(fmt.Sprintf("%s-L%02d", c.codigo, seq+1))
			op["E"+r] = num(float64(seq + 1))
			op["F"+r] = txt(tipoRobo)
			op["G"+r] = txt(papel)
			op["H"+r] = num(float64(o.ano))
			op["I"+r] = num(float64(o.mes))
			op["J"+r] = numeroOuTexto(o.divisao)
			op["K"+r] = numeroOuTexto(o.cc)
			op["L"+r] = numeroOuTexto(o.conta)
			op["M"+r] = num(arred2(o.valor))
			op["V"+r] = formula(`IF(P` + r + `="","",ROUND(P` + r + `+M` + r + `,2))`)
			op["W"+r] = formula(`IF(T` + r + `="","",ROUND(T` + r + `+M` + r + `,2))`)
			op["X"+r] = formula(`MAX(Parametros!$B$18,I` + r + `)`)
			op["Y"+r] = txt("PENDENTE")
			op["AB"+r] = formula(`IF(COUNTIF($D$5:$D$104,D` + r + `)>1,"DUPLICADO","")`)
			linhaOp++
		}
	}
	return montarXLSMRobo(templateBytes, []edicaoAba{
		{Aba: abaRoboKP06Chamados, Celulas: ch, LimparLinhas: [2]int{primeiraLinhaKP06, ultimaChamadoKP06}},
		{Aba: abaRoboKP06Operacoes, Celulas: op, LimparLinhas: [2]int{primeiraLinhaKP06, ultimaOperacaoKP06}},
		{Aba: "Linhas Base", Celulas: map[string]celulaRobo{}, LimparLinhas: [2]int{5, 100}},
		{Aba: "Resumo", Celulas: map[string]celulaRobo{"A14": {}}},
	})
}

func arred2(v float64) float64 { return float64(int64(v*100+sinalValor(v)*0.5)) / 100 }

// numeroOuTexto: códigos só com dígitos vão como número (o robô monta "divisão & centro" e compara contas);
// códigos com zero à esquerda ficam texto para não perder o zero.
func numeroOuTexto(s string) celulaRobo {
	s = strings.TrimSpace(s)
	if s != "" && !strings.HasPrefix(s, "0") {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			return num(float64(v))
		}
	}
	return txt(s)
}
