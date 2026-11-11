package exportacao

// lote_comum.go — salvaguardas compartilhadas por GerarLoteDespesa (vba.go)
// e GerarLoteObra (obra.go).
//
// DW-12: um solicitacao_id repetido no pedido era lido/validado 2x e entrava
// 2x nas linhas do .xlsm (verba duplicada no SAP). deduplicarIDs remove as
// repetições preservando a ordem da primeira ocorrência.
//
// DW-13: a validação de dono/status roda fora de transação; entre ela e a
// gravação, outra requisição podia finalizar a solicitação ou ela mudar de
// administrador (janela TOCTOU). revalidarSobLock relê status/dono DENTRO da
// transação de gravação com SELECT ... FOR UPDATE, que segura as linhas até
// o commit — mesmos erros (403/409) da validação original.

import (
	"database/sql"
	"fmt"
	"sort"
)

func deduplicarIDs(ids []string) []string {
	vistos := make(map[string]struct{}, len(ids))
	unicos := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := vistos[id]; ok {
			continue
		}
		vistos[id] = struct{}{}
		unicos = append(unicos, id)
	}
	return unicos
}

// revalidarSobLock trava as solicitações em ordem crescente de id (ordem
// fixa evita deadlock entre dois lotes concorrentes com ids em comum) e
// confere que continuam em_atendimento com o mesmo administrador.
func revalidarSobLock(tx *sql.Tx, solicitacaoIDs []string, administradorID string) error {
	ordenados := append([]string(nil), solicitacaoIDs...)
	sort.Strings(ordenados)

	for _, id := range ordenados {
		var status string
		var adminAtual sql.NullString
		err := tx.QueryRow(`
			SELECT status, administrador_id FROM solicitacoes WHERE id = $1 FOR UPDATE
		`, id).Scan(&status, &adminAtual)
		if err == sql.ErrNoRows {
			return ErrNaoEncontrada
		}
		if err != nil {
			return fmt.Errorf("exportacao: revalidar solicitação %s: %w", id, err)
		}
		if adminAtual.Valid && adminAtual.String != administradorID {
			return ErrNaoAutorizado
		}
		if status != "em_atendimento" {
			return ErrElegibilidadeInvalida
		}
	}
	return nil
}
