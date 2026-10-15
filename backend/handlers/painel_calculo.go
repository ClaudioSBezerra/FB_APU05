package handlers

// painel_calculo.go — job de cálculo de `painel_snapshots` (Epic 5, FR-17),
// deixado deliberadamente fora da Story 5.1 (ver comentário em painel.go,
// PRD §7.3/Questão Aberta 14: escopo de indicadores ainda não fechado com o
// negócio). Esta é a primeira (e única) rotina que ESCREVE nessa tabela —
// ObterPainelHandler (painel.go) continua só leitura (AD-1).
//
// Substitui a tabela inteira (DELETE + INSERT, mesma transação) a cada
// rodada — nunca deixa `painel_snapshots` com uma mistura de contagem
// antiga e nova de uma rodada anterior interrompida. Chamada tanto pelo job
// periódico (main.go, a cada 5 minutos) quanto pelo endpoint de recálculo
// manual abaixo — mesmo caminho de código, sem duplicar a lógica.
//
// Escopo fixo nas 3 dimensões já definidas pela Story 5.1 (tipo/status/cc):
// nenhum indicador novo é adicionado aqui, só o cálculo dos que já existem.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// rotulosTipoSolicitacao/rotulosStatusSolicitacao traduzem os códigos
// internos (como gravados em `solicitacoes`) para o texto que a tela do
// painel exibe — resolvido uma única vez aqui na gravação, nunca no
// caminho de leitura (mesmo princípio de "rotulo denormalizado" já
// documentado na migration de painel_snapshots).
var rotulosTipoSolicitacao = map[string]string{
	"transferencia": "Transferência",
	"inclusao":      "Inclusão",
	"inclusao_sfc":  "Inclusão SFC",
	"imobilizado":   "Imobilizado",
	"obras":         "Obras",
}

var rotulosStatusSolicitacao = map[string]string{
	"aberta":             "Aberta",
	"em_atendimento":     "Em atendimento",
	"pendente":           "Pendente",
	"finalizada_sucesso": "Finalizada (sucesso)",
	"finalizada_erro":    "Finalizada (erro)",
}

// rotuloOuChave devolve a chave crua quando não há tradução conhecida (ex.
// um status novo introduzido por uma story futura) — nunca quebra o
// recálculo por um rótulo ausente.
func rotuloOuChave(mapa map[string]string, chave string) string {
	if rotulo, ok := mapa[chave]; ok {
		return rotulo
	}
	return chave
}

type contagemPainel struct {
	Chave      string
	Rotulo     string
	Quantidade int
}

// contarAgrupado roda `query` e devolve as linhas já lidas e com `rows`
// fechado ANTES de qualquer outra query rodar na mesma transação — rodar um
// novo Exec/Query com um *sql.Rows ainda aberto na MESMA *sql.Tx (mesma
// conexão) é um erro clássico do database/sql (a conexão só processa um
// comando por vez).
func contarAgrupado(tx *sql.Tx, query string, comRotulo bool, rotulos map[string]string) ([]contagemPainel, error) {
	rows, err := tx.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contagens []contagemPainel
	for rows.Next() {
		var c contagemPainel
		if comRotulo {
			if err := rows.Scan(&c.Chave, &c.Rotulo, &c.Quantidade); err != nil {
				return nil, err
			}
		} else {
			if err := rows.Scan(&c.Chave, &c.Quantidade); err != nil {
				return nil, err
			}
			c.Rotulo = rotuloOuChave(rotulos, c.Chave)
		}
		contagens = append(contagens, c)
	}
	return contagens, rows.Err()
}

// RecalcularPainelConsolidado recalcula as 3 dimensões (tipo/status/cc) do
// painel "consolidado" a partir de `solicitacoes` (nunca lido pelo handler
// de leitura — AD-1). "por cc" exclui Obras de propósito: `centro_custo_id`
// é NULL para tipo_solicitacao='obras' (migration 009), e o INNER JOIN
// abaixo já as deixa de fora sem precisar de um WHERE explícito.
func RecalcularPainelConsolidado(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("painel: iniciar transação: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	porTipo, err := contarAgrupado(tx, `SELECT tipo_solicitacao, COUNT(*) FROM solicitacoes GROUP BY tipo_solicitacao`, false, rotulosTipoSolicitacao)
	if err != nil {
		return fmt.Errorf("painel: contar por tipo: %w", err)
	}
	porStatus, err := contarAgrupado(tx, `SELECT status, COUNT(*) FROM solicitacoes GROUP BY status`, false, rotulosStatusSolicitacao)
	if err != nil {
		return fmt.Errorf("painel: contar por status: %w", err)
	}
	porCC, err := contarAgrupado(tx, `
		SELECT cc.codigo, cc.nome, COUNT(*)
		FROM solicitacoes s
		JOIN centros_custo cc ON cc.id = s.centro_custo_id
		GROUP BY cc.codigo, cc.nome
	`, true, nil)
	if err != nil {
		return fmt.Errorf("painel: contar por cc: %w", err)
	}

	if _, err := tx.Exec(`DELETE FROM painel_snapshots WHERE painel = 'consolidado'`); err != nil {
		return fmt.Errorf("painel: limpar snapshots antigos: %w", err)
	}

	geradoEm := time.Now()
	gravar := func(dimensao string, contagens []contagemPainel) error {
		for _, c := range contagens {
			if _, err := tx.Exec(`
				INSERT INTO painel_snapshots (painel, dimensao, chave, rotulo, quantidade, gerado_em)
				VALUES ('consolidado', $1, $2, $3, $4, $5)
			`, dimensao, c.Chave, c.Rotulo, c.Quantidade, geradoEm); err != nil {
				return err
			}
		}
		return nil
	}

	if err := gravar("tipo", porTipo); err != nil {
		return fmt.Errorf("painel: gravar snapshot por tipo: %w", err)
	}
	if err := gravar("status", porStatus); err != nil {
		return fmt.Errorf("painel: gravar snapshot por status: %w", err)
	}
	if err := gravar("cc", porCC); err != nil {
		return fmt.Errorf("painel: gravar snapshot por cc: %w", err)
	}

	return tx.Commit()
}

// RecalcularPainelHandler — POST /api/admin/paineis/{painel}/recalcular.
// Dispara a mesma rotina do job periódico (main.go) sob demanda — para
// validar o painel sem esperar o próximo ciclo de 5 minutos. Só
// "consolidado" existe hoje (mesmo guard de ObterPainelHandler).
func RecalcularPainelHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		painel := r.PathValue("painel")
		if painel != "consolidado" {
			jsonErr(w, http.StatusNotFound, "painel não encontrado")
			return
		}

		if err := RecalcularPainelConsolidado(db); err != nil {
			log.Printf("[Painel] Erro ao recalcular: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"recalculado": true})
	}
}
