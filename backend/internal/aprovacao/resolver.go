package aprovacao

import (
	"database/sql"
	"errors"
)

// Solicitacao é a entrada mínima que um Resolver precisa para calcular o
// aprovador — não é o modelo de persistência completo de `solicitacoes`, só
// os campos que alimentam a cadeia de resolução (FR-10). O handler HTTP
// monta esse valor a partir do centro de custo já validado/autorizado da
// solicitação (CentroCustoCodigo/Filial/DivisaoID vêm de `centros_custo`);
// o resolver nunca lê HTTP nem decide autorização de CC — isso é
// responsabilidade do handler (AD-1).
type Solicitacao struct {
	Filial            string
	CentroCustoID     string
	CentroCustoCodigo string
	DivisaoID         string
	Valor             float64
	// AutorizadorEscolhidoID é o colaborador_id escolhido pelo solicitante
	// no campo `autorizador_id` da requisição — só consumido por
	// AutorizadorNominal (tipo_solicitacao="inclusao", Story 3.3); vazio e
	// ignorado por ResolverCalculado (Code Map da spec 3.3).
	AutorizadorEscolhidoID string
	// TipoSolicitacao (Story 3.5) é só consumido por AutorizadorNominal, para
	// decidir entre `autorizadores_formulario` (CC/faixa de valor) e
	// `aprovadores_obra` (teto individual, sem CC/faixa) — mesmo precedente
	// de AutorizadorEscolhidoID. Vazio e ignorado por ResolverCalculado.
	TipoSolicitacao string
}

// Resolver é a porta única do motor de aprovação (AD-2): todo handler de
// solicitação chama exclusivamente Resolve(solicitacao) -> (Aprovador,
// error); nenhum handler calcula aprovador por conta própria (AD-1).
type Resolver interface {
	Resolve(s Solicitacao) (Aprovador, error)
}

// DBTX é o subconjunto de *sql.DB/*sql.Tx que ResolverCalculado precisa para
// consultar as tabelas de alçada. Permite ao handler passar a MESMA
// transação que grava `solicitacoes`/`solicitacao_lancamentos` (tudo
// atômico - Boundaries "Always" da spec), sem este pacote depender de um
// driver de banco específico (AD-1) — só do pacote padrão database/sql.
type DBTX interface {
	QueryRow(query string, args ...interface{}) *sql.Row
	Query(query string, args ...interface{}) (*sql.Rows, error)
}

// ErrTipoNaoSuportado é devolvido por ResolverParaTipo para qualquer
// tipo_solicitacao fora do mapeado até agora — "transferencia" (3.1),
// "inclusao_sfc" (3.2), "inclusao" (3.3), "imobilizado" (3.4) e "obras"
// (3.5) são todos mapeados; só um tipo_solicitacao fora do Glossário
// alcançaria este sentinela.
var ErrTipoNaoSuportado = errors.New("tipo de solicitação não suportado ainda")

// ResolverParaTipo é a ÚNICA função de dispatch tipo->resolver do sistema
// (AD-2) — nenhum handler reimplementa este mapeamento.
func ResolverParaTipo(tipo string, db DBTX) (Resolver, error) {
	switch tipo {
	case "transferencia", "inclusao_sfc":
		// Story 3.2: Inclusão SFC reaproveita o MESMO ResolverCalculado de
		// Transferência (Epic 3 Cross-Story Dependencies) — nenhuma lógica
		// nova neste pacote.
		return NovoResolverCalculado(db), nil
	case "inclusao", "imobilizado", "obras":
		// Story 3.3: Inclusão (plain) usa o motor de autorizador nominal —
		// 2ª implementação de Resolver (AD-2). Story 3.4: Imobilizado
		// reaproveita a MESMA instância/tabela (`autorizadores_formulario`).
		// Story 3.5: Obras reaproveita a MESMA instância/tipo concreto
		// *AutorizadorNominal (AD-2 continua com exatamente 2
		// implementações de Resolver, Design Notes da spec 3.5) — nunca um
		// 3º tipo de Resolver; `nominal.go` decide internamente, por um
		// branch tipo-aware em `s.TipoSolicitacao`, entre
		// `autorizadores_formulario` (CC/faixa) e `aprovadores_obra` (teto
		// individual).
		return NovoAutorizadorNominal(db), nil
	default:
		return nil, ErrTipoNaoSuportado
	}
}
