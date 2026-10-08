// Package fila é o único caminho de escrita para `aprovador_snapshot`/
// `versao`/`status`/`administrador_id` de `solicitacoes` (Boundaries
// "Always" da Story 4.1 spec: "nunca direto em handler"). Mesmo princípio
// de internal/aprovacao — pacote de domínio isolado, sem net/http na sua
// superfície pública.
//
// Assumir faz o UPDATE condicional por `versao` (lock otimista) através da
// conexão PRIVILEGIADA (AD-4: só ela tem GRANT UPDATE nessas colunas no
// banco — ver migration 010); zero linhas afetadas nunca dispara retry
// automático, só o sentinela ErrConflitoVersao (ou ErrNaoEncontrada, quando
// o id simplesmente não existe).
package fila

import (
	"database/sql"
	"errors"
	"fmt"
)

// Solicitacao é a projeção mínima devolvida por Assumir — só os campos que
// o handler HTTP precisa para montar a resposta (mesmo princípio de
// aprovacao.Solicitacao: não é o modelo de persistência completo).
type Solicitacao struct {
	ID              string
	TipoSolicitacao string
	Status          string
	Versao          int
	AdministradorID string
}

// ErrNaoEncontrada é o sentinela devolvido quando `id` não existe em
// `solicitacoes` (I/O Matrix da spec: 404, nunca confundido com conflito de
// versão).
var ErrNaoEncontrada = errors.New("solicitação não encontrada")

// ErrConflitoVersao é devolvido quando o UPDATE condicional afeta 0 linhas
// mas a solicitação EXISTE — ou porque outro administrador já consumiu essa
// `versao`, ou porque o status já não é 'aberta' (já em atendimento) —
// I/O Matrix da spec: os dois casos recebem a MESMA resposta 409
// conflito_versao, sem distinguir o motivo.
type ErrConflitoVersao struct {
	VersaoAtual int
}

func (e *ErrConflitoVersao) Error() string {
	return fmt.Sprintf("conflito de versão: versão atual é %d", e.VersaoAtual)
}

// Assumir executa `UPDATE solicitacoes SET status='em_atendimento',
// administrador_id=$1, versao=versao+1 WHERE id=$2 AND versao=$3 AND
// status='aberta'` pela conexão privilegiada `db` (Boundaries "Always" da
// spec). administradorID já deve vir de GetUserIDFromContext — este pacote
// não lê contexto HTTP nem decide de onde esse valor vem (AD-1, mesmo
// princípio de internal/aprovacao).
func Assumir(db *sql.DB, id string, versaoLida int, administradorID string) (Solicitacao, error) {
	var s Solicitacao
	err := db.QueryRow(`
		UPDATE solicitacoes
		SET status = 'em_atendimento', administrador_id = $1, versao = versao + 1
		WHERE id = $2 AND versao = $3 AND status = 'aberta'
		RETURNING id, tipo_solicitacao, status, versao, administrador_id
	`, administradorID, id, versaoLida).Scan(&s.ID, &s.TipoSolicitacao, &s.Status, &s.Versao, &s.AdministradorID)
	if err == nil {
		return s, nil
	}
	if err != sql.ErrNoRows {
		return Solicitacao{}, fmt.Errorf("assumir solicitação %s: %w", id, err)
	}

	// 0 linhas afetadas: SELECT de fallback (sem condição de versao/status)
	// distingue "id inexistente" (404) de "conflito de versão" (409) — o
	// UPDATE acima já tentou e falhou por completo, então este SELECT
	// nunca corre risco de corrida adicional (não há retry automático,
	// Boundaries "Always" da spec).
	var versaoAtual int
	errFallback := db.QueryRow(`SELECT versao FROM solicitacoes WHERE id = $1`, id).Scan(&versaoAtual)
	if errFallback == sql.ErrNoRows {
		return Solicitacao{}, ErrNaoEncontrada
	}
	if errFallback != nil {
		return Solicitacao{}, fmt.Errorf("consultar versão atual de %s: %w", id, errFallback)
	}
	return Solicitacao{}, &ErrConflitoVersao{VersaoAtual: versaoAtual}
}
