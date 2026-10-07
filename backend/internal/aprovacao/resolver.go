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
// tipo_solicitacao fora do mapeado até agora — "transferencia" (3.1) e
// "inclusao_sfc" (3.2), ambos via ResolverCalculado (Boundaries "Never" da
// spec: os outros 3 tipos ainda não têm Resolver implementado; histórias
// 3.3-3.5 estendem este mapa, nunca reimplementam o dispatch tipo->resolver).
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
	default:
		return nil, ErrTipoNaoSuportado
	}
}
