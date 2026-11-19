package handlers

// centros_custo_sap_carga.go — carga diária (06:00) dos centros de custo lidos do SAP pelo gateway
// (internal/sapodata.LerCentrosCusto) para a tabela centros_custo. Cadastro: o SAP manda — nome e
// divisão são sobrescritos a cada carga; os demais campos do orçamento não são tocados.
//
// Modelo (regra do negócio, doc 04 e 05): o código SAP do centro de custo é FILIAL + CC de 4 dígitos
// (10016000 = filial 1001 + CC 6000). Cada código SAP completo vira um centro de custo, e a
// DIVISÃO é a filial = o código sem os 4 últimos dígitos, com 4 caracteres ("81" → "0081"; a coluna `filial` recebe o mesmo valor,
// só se ainda estiver vazia). A divisão é criada na hora quando não existe (nome: o da tabela de
// parâmetros do Controle TI, se houver; senão o próprio código — ajustável depois em Cadastros).
// Os centros antigos de 4 dígitos (CC "puro") NÃO são tocados nem apagados: continuam ligados aos
// usuários até a fase de remapeamento. Centro que sumiu do SAP permanece no cadastro.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"fb_apu05/internal/sapodata"
)

// trocável nos testes
var lerCentrosCustoSAP = sapodata.LerCentrosCusto

// ResultadoCargaCC resume uma carga.
type ResultadoCargaCC struct {
	Lidos           int
	Criados         int
	Atualizados     int
	DivisoesCriadas int
	Ignorados       int
}

func (r ResultadoCargaCC) mensagem() string {
	m := fmt.Sprintf("carga ok — %d vigentes no SAP: %d criados, %d atualizados", r.Lidos, r.Criados, r.Atualizados)
	if r.DivisoesCriadas > 0 {
		m += fmt.Sprintf(", %d divisões (filiais) novas", r.DivisoesCriadas)
	}
	if r.Ignorados > 0 {
		m += fmt.Sprintf(", %d ignorados (código fora do padrão filial+CC ou sem nome)", r.Ignorados)
	}
	return m
}

// filialDoCodigo devolve a filial (código sem os 4 últimos dígitos) de um centro de custo SAP.
// ok=false se o código não for numérico com filial + 4 dígitos.
func filialDoCodigo(codigo string) (string, bool) {
	if len(codigo) < 5 || len(codigo) > 50 {
		return "", false
	}
	for _, r := range codigo {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	return CodigoDivisao4(codigo[:len(codigo)-4]), true // 4 caracteres: "81" → "0081"
}

// nomesDivisaoControleTI lê os nomes das filiais da tabela de parâmetros do Controle TI (opcional:
// qualquer erro só significa "sem nomes", a divisão nasce com o código como nome).
func nomesDivisaoControleTI(ctx context.Context, db *sql.DB) map[string]string {
	nomes := map[string]string{}
	rows, err := db.QueryContext(ctx, `SELECT divisao, descricao FROM ti_param_divisao`)
	if err != nil {
		return nomes
	}
	defer rows.Close()
	for rows.Next() {
		var d, n string
		if rows.Scan(&d, &n) == nil && strings.TrimSpace(n) != "" {
			nomes[strings.TrimSpace(d)] = strings.TrimSpace(n)
		}
	}
	return nomes
}

// carregarCentrosCustoSAP lê o SAP e grava divisões e centros_custo numa única transação.
func carregarCentrosCustoSAP(ctx context.Context, db *sql.DB, cfg sapodata.Config, agora time.Time) (ResultadoCargaCC, error) {
	var res ResultadoCargaCC
	ccs, err := lerCentrosCustoSAP(ctx, cfg, agora)
	if err != nil {
		return res, err
	}
	res.Lidos = len(ccs)
	if len(ccs) == 0 {
		// nunca trata "SAP devolveu zero" como sucesso: pode ser filtro/permissão quebrados
		return res, fmt.Errorf("o SAP não devolveu nenhum centro de custo vigente")
	}
	nomesTI := nomesDivisaoControleTI(ctx, db)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return res, fmt.Errorf("erro ao iniciar a gravação: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	divisoes := map[string]string{} // filial -> id
	rows, err := tx.QueryContext(ctx, `SELECT codigo, id FROM divisoes`)
	if err != nil {
		return res, fmt.Errorf("erro ao ler divisões: %w", err)
	}
	for rows.Next() {
		var cod, id string
		if err := rows.Scan(&cod, &id); err != nil {
			rows.Close()
			return res, err
		}
		divisoes[cod] = id
	}
	rows.Close()

	for _, cc := range ccs {
		filial, ok := filialDoCodigo(cc.Codigo)
		if !ok || cc.Nome == "" {
			res.Ignorados++
			continue
		}
		nome := cc.Nome
		if len(nome) > 255 {
			nome = nome[:255]
		}
		divID, existe := divisoes[filial]
		if !existe {
			nomeDiv := filial
			if n := nomesTI[filial]; n != "" {
				nomeDiv = n
			} else if n := nomesTI[strings.TrimLeft(filial, "0")]; n != "" {
				nomeDiv = n // o Controle TI pode ter o nome sem os zeros à esquerda
			}
			if err := tx.QueryRowContext(ctx, `INSERT INTO divisoes (codigo, nome) VALUES ($1, $2) RETURNING id`, filial, nomeDiv).Scan(&divID); err != nil {
				return res, fmt.Errorf("erro ao criar a divisão %s: %w", filial, err)
			}
			divisoes[filial] = divID
			res.DivisoesCriadas++
		}
		var inserida bool
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO centros_custo (codigo, nome, divisao_id, filial) VALUES ($1, $2, $3, $4)
			ON CONFLICT (codigo) DO UPDATE SET nome = EXCLUDED.nome, divisao_id = EXCLUDED.divisao_id,
				filial = COALESCE(centros_custo.filial, EXCLUDED.filial), updated_at = now()
			RETURNING (xmax = 0)`, cc.Codigo, nome, divID, filial).Scan(&inserida); err != nil {
			return res, fmt.Errorf("erro ao gravar o centro de custo %s: %w", cc.Codigo, err)
		}
		if inserida {
			res.Criados++
		} else {
			res.Atualizados++
		}
	}
	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("erro ao concluir a gravação: %w", err)
	}
	return res, nil
}
