package fila

import (
	"database/sql"
	"fmt"
)

// Decidir aprova ('aberta', entra na fila do atendente) ou rejeita ('rejeitada') uma solicitação que
// está 'aguardando_aprovacao'. Mesma conexão PRIVILEGIADA e mesmo controle de versão do restante do
// pacote (AD-4). versaoLida 0 = não conferir a versão (decisão por link, que não conhece a versão).
func Decidir(db *sql.DB, id string, versaoLida int, aprovada bool) (Solicitacao, error) {
	novo := "rejeitada"
	if aprovada {
		novo = "aberta"
	}
	var s Solicitacao
	var admin sql.NullString
	err := db.QueryRow(`
		UPDATE solicitacoes
		SET status = $1, versao = versao + 1
		WHERE id = $2 AND status = 'aguardando_aprovacao' AND ($3 = 0 OR versao = $3)
		RETURNING id, tipo_solicitacao, status, versao, administrador_id
	`, novo, id, versaoLida).Scan(&s.ID, &s.TipoSolicitacao, &s.Status, &s.Versao, &admin)
	if err == nil {
		s.AdministradorID = admin.String
		return s, nil
	}
	if err != sql.ErrNoRows {
		return Solicitacao{}, fmt.Errorf("decidir solicitação %s: %w", id, err)
	}
	var versaoAtual int
	if e := db.QueryRow(`SELECT versao FROM solicitacoes WHERE id = $1`, id).Scan(&versaoAtual); e == sql.ErrNoRows {
		return Solicitacao{}, ErrNaoEncontrada
	} else if e != nil {
		return Solicitacao{}, fmt.Errorf("consultar versão atual de %s: %w", id, e)
	}
	return Solicitacao{}, &ErrConflitoVersao{VersaoAtual: versaoAtual}
}
