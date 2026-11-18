package handlers

// centros_custo_sap_carga.go — carga diária (06:00) dos centros de custo lidos do SAP pelo gateway
// (internal/sapodata.LerCentrosCusto) para a tabela centros_custo. Cadastro: o SAP manda — nome e
// divisão são sobrescritos a cada carga; `filial` e os demais campos do orçamento não são tocados.
//
// Divisão: centros_custo.divisao_id é obrigatório, e o SAP informa BusinessArea (ex.: "0081"). O
// BusinessArea é casado com divisoes.codigo (exato ou sem zeros à esquerda). Centro de custo NOVO
// sem divisão correspondente não é criado (fica contado em "sem_divisao" e listado no log), e um
// centro já existente só tem o nome atualizado. Nada é apagado: centro que sumiu do SAP permanece.

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"fb_apu05/internal/sapodata"
)

// trocável nos testes
var lerCentrosCustoSAP = sapodata.LerCentrosCusto

// ResultadoCargaCC resume uma carga.
type ResultadoCargaCC struct {
	Lidos       int
	Criados     int
	Atualizados int
	SemDivisao  int
	Ignorados   int
}

func (r ResultadoCargaCC) mensagem() string {
	m := fmt.Sprintf("carga ok — %d vigentes no SAP: %d criados, %d atualizados", r.Lidos, r.Criados, r.Atualizados)
	if r.SemDivisao > 0 {
		m += fmt.Sprintf(", %d novos sem divisão correspondente (não criados)", r.SemDivisao)
	}
	if r.Ignorados > 0 {
		m += fmt.Sprintf(", %d ignorados (sem nome)", r.Ignorados)
	}
	return m
}

func chaveDivisao(codigo string) string {
	c := strings.TrimSpace(codigo)
	if t := strings.TrimLeft(c, "0"); t != "" {
		return t
	}
	return c
}

// carregarCentrosCustoSAP lê o SAP e grava em centros_custo numa única transação.
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

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return res, fmt.Errorf("erro ao iniciar a gravação: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	divisoes := map[string]string{}
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
		if _, dup := divisoes[chaveDivisao(cod)]; !dup {
			divisoes[chaveDivisao(cod)] = id
		}
	}
	rows.Close()

	semDivisao := map[string]int{}
	for _, cc := range ccs {
		if len(cc.Codigo) > 50 || cc.Nome == "" {
			res.Ignorados++
			continue
		}
		nome := cc.Nome
		if len(nome) > 255 {
			nome = nome[:255]
		}
		divID, achou := divisoes[cc.AreaNegocio]
		if !achou {
			divID, achou = divisoes[chaveDivisao(cc.AreaNegocio)]
		}
		if achou {
			var inserida bool
			if err := tx.QueryRowContext(ctx, `
				INSERT INTO centros_custo (codigo, nome, divisao_id) VALUES ($1, $2, $3)
				ON CONFLICT (codigo) DO UPDATE SET nome = EXCLUDED.nome, divisao_id = EXCLUDED.divisao_id, updated_at = now()
				RETURNING (xmax = 0)`, cc.Codigo, nome, divID).Scan(&inserida); err != nil {
				return res, fmt.Errorf("erro ao gravar o centro de custo %s: %w", cc.Codigo, err)
			}
			if inserida {
				res.Criados++
			} else {
				res.Atualizados++
			}
			continue
		}
		r, err := tx.ExecContext(ctx, `UPDATE centros_custo SET nome = $2, updated_at = now() WHERE codigo = $1`, cc.Codigo, nome)
		if err != nil {
			return res, fmt.Errorf("erro ao atualizar o centro de custo %s: %w", cc.Codigo, err)
		}
		if n, _ := r.RowsAffected(); n > 0 {
			res.Atualizados++
		} else {
			res.SemDivisao++
			semDivisao[cc.AreaNegocio]++
		}
	}
	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("erro ao concluir a gravação: %w", err)
	}
	if len(semDivisao) > 0 {
		log.Printf("[SAP] Centros de custo novos sem divisão (BusinessArea → quantidade): %v", semDivisao)
	}
	return res, nil
}
