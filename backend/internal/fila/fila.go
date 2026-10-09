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

// ErrNaoAutorizado (Story 4.2) é devolvido pelo SELECT de fallback de
// MarcarPendencia/Comentar quando a solicitação EXISTE e a versão até pode
// bater, mas quem chamou não é o dono esperado daquela ação
// (administrador_id para MarcarPendencia, solicitante_id para Comentar) —
// 403, mesmo padrão de jsonErr(w, http.StatusForbidden, ...) já usado em
// handlers/solicitacoes.go:289 (Boundaries "Always" da spec).
var ErrNaoAutorizado = errors.New("não autorizado")

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

// MarcarPendencia (Story 4.2) executa, na MESMA transação (Boundaries
// "Always" da spec: comentário nunca existe sem a transição de status
// correspondente, e vice-versa), `UPDATE solicitacoes SET status='pendente',
// versao=versao+1 WHERE id=$1 AND versao=$2 AND status='em_atendimento' AND
// administrador_id=$3` seguido de `INSERT INTO solicitacao_comentarios
// (..., tipo='pendencia', ...)` — pela conexão PRIVILEGIADA `db` (AD-4,
// mesmo princípio de Assumir). administradorID já deve vir de
// GetUserIDFromContext — este pacote não lê contexto HTTP (AD-1).
//
// 0 linhas afetadas: SELECT de fallback distingue, nesta ordem exata
// (Boundaries "Always" da spec), 404 (id inexistente) -> 403
// (administrador_id da linha != administradorID) -> 409 (versão obsoleta,
// ou status já não é 'em_atendimento').
func MarcarPendencia(db *sql.DB, id string, versaoLida int, administradorID string, comentario string) (Solicitacao, error) {
	tx, err := db.Begin()
	if err != nil {
		return Solicitacao{}, fmt.Errorf("iniciar transação para marcar pendência de %s: %w", id, err)
	}
	defer func() { _ = tx.Rollback() }()

	var s Solicitacao
	err = tx.QueryRow(`
		UPDATE solicitacoes
		SET status = 'pendente', versao = versao + 1
		WHERE id = $1 AND versao = $2 AND status = 'em_atendimento' AND administrador_id = $3
		RETURNING id, tipo_solicitacao, status, versao, administrador_id
	`, id, versaoLida, administradorID).Scan(&s.ID, &s.TipoSolicitacao, &s.Status, &s.Versao, &s.AdministradorID)
	if err != nil {
		if err != sql.ErrNoRows {
			return Solicitacao{}, fmt.Errorf("marcar pendência de %s: %w", id, err)
		}
		// UPDATE não afetou nenhuma linha — nada foi escrito. Encerra esta
		// transação explicitamente ANTES do SELECT de fallback (que roda na
		// conexão `db` diretamente, fora da tx): sem isso, a conexão da tx
		// continuaria aberta/reservada enquanto o fallback precisa de uma
		// segunda conexão do pool só para uma leitura — desperdício sem
		// benefício, já que não há nada para reverter além do próprio
		// SELECT que falhou (mesmo princípio de Assumir: sem retry
		// automático, nenhum risco de corrida adicional).
		_ = tx.Rollback()
		return Solicitacao{}, fallbackMarcarPendencia(db, id, administradorID)
	}

	if _, err := tx.Exec(`
		INSERT INTO solicitacao_comentarios (solicitacao_id, autor_id, tipo, texto)
		VALUES ($1, $2, 'pendencia', $3)
	`, id, administradorID, comentario); err != nil {
		return Solicitacao{}, fmt.Errorf("inserir comentário de pendência em %s: %w", id, err)
	}

	if err := tx.Commit(); err != nil {
		return Solicitacao{}, fmt.Errorf("confirmar transação de pendência de %s: %w", id, err)
	}
	return s, nil
}

// fallbackMarcarPendencia distingue 404/403/409 quando o UPDATE de
// MarcarPendencia afeta 0 linhas — mesma ordem documentada ali.
// `administrador_id` é nulável (toda solicitação nasce sem administrador,
// migration 010) por isso `sql.NullString`: uma linha NULL (nunca assumida,
// ex. status='aberta') nunca é "outro administrador" — só um
// administrador_id PREENCHIDO e diferente do chamador é 403; NULL cai no
// mesmo 409 de qualquer outro status incompatível (I/O Matrix da spec:
// "Pendência com status/versão incompatível" exige 409, não 403, para
// status=aberta).
func fallbackMarcarPendencia(db *sql.DB, id string, administradorID string) error {
	var donoID sql.NullString
	var status string
	var versaoAtual int
	err := db.QueryRow(`
		SELECT administrador_id, status, versao FROM solicitacoes WHERE id = $1
	`, id).Scan(&donoID, &status, &versaoAtual)
	if err == sql.ErrNoRows {
		return ErrNaoEncontrada
	}
	if err != nil {
		return fmt.Errorf("consultar solicitação %s para fallback de pendência: %w", id, err)
	}
	if donoID.Valid && donoID.String != administradorID {
		return ErrNaoAutorizado
	}
	return &ErrConflitoVersao{VersaoAtual: versaoAtual}
}

// Comentar (Story 4.2) executa, na MESMA transação, `UPDATE solicitacoes SET
// status = CASE WHEN status IN ('aberta','em_atendimento') THEN status ELSE
// 'em_atendimento' END, versao=versao+1 WHERE id=$1 AND versao=$2 AND
// solicitante_id=$3` seguido de `INSERT INTO solicitacao_comentarios (...,
// tipo='comentario', ...)` — pela conexão PRIVILEGIADA `db` (AD-4).
//
// A reabertura é por EXCLUSÃO, nunca por um literal 'finalizada' que não
// existe no código (Boundaries "Never" da spec, Design Notes): o mesmo CASE
// cobre tanto "solicitante responde a uma pendência" (pendente ->
// em_atendimento) quanto "comentário reabre uma solicitação finalizada"
// (qualquer outro status -> em_atendimento), sem ramificação extra.
// `Comentar` NÃO checa `status` no WHERE (Boundaries "Always" da spec:
// precisa funcionar em qualquer status) — só id+versao+solicitante_id.
//
// 0 linhas afetadas: SELECT de fallback distingue 404 (id inexistente) ->
// 403 (solicitante_id da linha != solicitanteID) -> 409 (versão obsoleta) —
// sem checagem de status aqui (não há motivo de status para o UPDATE
// falhar, já que nenhum status é excluído do WHERE).
func Comentar(db *sql.DB, id string, versaoLida int, solicitanteID string, comentario string) (Solicitacao, error) {
	tx, err := db.Begin()
	if err != nil {
		return Solicitacao{}, fmt.Errorf("iniciar transação para comentar em %s: %w", id, err)
	}
	defer func() { _ = tx.Rollback() }()

	// `administrador_id` pode ser NULL aqui (ao contrário de Assumir e
	// MarcarPendencia): o WHERE de Comentar não exige nenhum administrador
	// específico (Boundaries "Always" da spec), então o solicitante pode
	// comentar numa solicitação `status='aberta'` que nenhum administrador
	// assumiu ainda — sql.NullString evita o erro de Scan
	// "converting NULL to string is unsupported" nesse caso.
	var s Solicitacao
	var administradorID sql.NullString
	err = tx.QueryRow(`
		UPDATE solicitacoes
		SET status = CASE WHEN status IN ('aberta', 'em_atendimento') THEN status ELSE 'em_atendimento' END,
		    versao = versao + 1
		WHERE id = $1 AND versao = $2 AND solicitante_id = $3
		RETURNING id, tipo_solicitacao, status, versao, administrador_id
	`, id, versaoLida, solicitanteID).Scan(&s.ID, &s.TipoSolicitacao, &s.Status, &s.Versao, &administradorID)
	s.AdministradorID = administradorID.String
	if err != nil {
		if err != sql.ErrNoRows {
			return Solicitacao{}, fmt.Errorf("comentar em %s: %w", id, err)
		}
		// Mesmo motivo do Rollback explícito em MarcarPendencia: encerra a
		// transação ANTES do SELECT de fallback, que roda fora dela.
		_ = tx.Rollback()
		return Solicitacao{}, fallbackComentar(db, id, solicitanteID)
	}

	if _, err := tx.Exec(`
		INSERT INTO solicitacao_comentarios (solicitacao_id, autor_id, tipo, texto)
		VALUES ($1, $2, 'comentario', $3)
	`, id, solicitanteID, comentario); err != nil {
		return Solicitacao{}, fmt.Errorf("inserir comentário em %s: %w", id, err)
	}

	if err := tx.Commit(); err != nil {
		return Solicitacao{}, fmt.Errorf("confirmar transação de comentário em %s: %w", id, err)
	}
	return s, nil
}

// fallbackComentar distingue 404/403/409 quando o UPDATE de Comentar afeta 0
// linhas — mesma ordem documentada ali. `solicitante_id` é NOT NULL
// (migration 006), então nunca precisa de sql.NullString como em
// fallbackMarcarPendencia.
func fallbackComentar(db *sql.DB, id string, solicitanteID string) error {
	var donoID string
	var versaoAtual int
	err := db.QueryRow(`
		SELECT solicitante_id, versao FROM solicitacoes WHERE id = $1
	`, id).Scan(&donoID, &versaoAtual)
	if err == sql.ErrNoRows {
		return ErrNaoEncontrada
	}
	if err != nil {
		return fmt.Errorf("consultar solicitação %s para fallback de comentário: %w", id, err)
	}
	if donoID != solicitanteID {
		return ErrNaoAutorizado
	}
	return &ErrConflitoVersao{VersaoAtual: versaoAtual}
}
