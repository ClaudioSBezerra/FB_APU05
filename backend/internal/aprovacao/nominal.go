package aprovacao

// nominal.go — Story 3.3 (Abrir Inclusão com autorizador nominal,
// FR-6/FR-11): 2ª implementação de Resolver (AD-2). Diferente de
// ResolverCalculado (cadeia de etapas calculadas sem entrada do cliente),
// AutorizadorNominal valida um `autorizador_id` que o PRÓPRIO solicitante
// escolheu contra a lista fixa `autorizadores_formulario` (CC/faixa de
// valor) — sempre pessoa, nunca cargo/colegiado (Boundaries "Always" da
// spec: FR-11 só descreve "pessoa escolhida pelo solicitante").

import (
	"database/sql"
	"fmt"
)

// AutorizadorNominal implementa Resolver para tipo_solicitacao="inclusao".
// Resolve NUNCA sintetiza um Aprovador quando a validação falha — devolve
// ErrAutorizadorInvalido (mesmo princípio AD-2 de ErrSemAlcadaCadastrada em
// ResolverCalculado).
type AutorizadorNominal struct {
	db DBTX
}

// NovoAutorizadorNominal constrói um AutorizadorNominal sobre a conexão/
// transação informada — mesmo padrão de NovoResolverCalculado (tipicamente a
// MESMA transação que o handler usa para gravar
// `solicitacoes`/`solicitacao_lancamentos`, tudo atômico).
func NovoAutorizadorNominal(db DBTX) *AutorizadorNominal {
	return &AutorizadorNominal{db: db}
}

// Resolve implementa Resolver. Consulta `autorizadores_formulario` por uma
// linha ativa que case `centro_custo_codigo` da solicitação,
// `colaborador_id = autorizador_id` escolhido pelo solicitante, e
// `valor_minimo <= valor <= valor_maximo` (valor_maximo nulo = sem teto).
// Sem match -> ErrAutorizadorInvalido; com match -> Aprovador Tipo="pessoa"
// com ColaboradorID preenchido.
func (a *AutorizadorNominal) Resolve(s Solicitacao) (Aprovador, error) {
	var id string
	err := a.db.QueryRow(`
		SELECT id
		FROM autorizadores_formulario
		WHERE ativo = true
		  AND centro_custo_codigo = $1
		  AND colaborador_id = $2
		  AND valor_minimo <= $3
		  AND (valor_maximo IS NULL OR valor_maximo >= $3)
		ORDER BY valor_minimo DESC, id
		LIMIT 1
	`, s.CentroCustoCodigo, s.AutorizadorEscolhidoID, s.Valor).Scan(&id)
	if err == sql.ErrNoRows {
		return Aprovador{}, &ErrAutorizadorInvalido{CentroCusto: s.CentroCustoCodigo}
	}
	if err != nil {
		return Aprovador{}, fmt.Errorf("aprovacao: autorizadores_formulario: %w", err)
	}

	versao, err := versaoDoCadastro(a.db, "autorizadores-formulario", id)
	if err != nil {
		return Aprovador{}, err
	}
	nome, err := nomeColaborador(a.db, s.AutorizadorEscolhidoID)
	if err != nil {
		return Aprovador{}, err
	}

	cid := s.AutorizadorEscolhidoID
	return Aprovador{
		Tipo:          TipoPessoa,
		Nome:          nome,
		ColaboradorID: &cid,
		Motivo:        "autorizador nominal validado para o CC/faixa de valor",
		RegraID:       id,
		RegraVersao:   versao,
	}, nil
}
