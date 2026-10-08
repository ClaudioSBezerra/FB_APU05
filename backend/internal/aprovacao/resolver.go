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
// "inclusao_sfc" (3.2), "inclusao" (3.3) e "imobilizado" (3.4) (Boundaries
// "Never" da spec 3.4: "obras" ainda não tem Resolver implementado; a
// história 3.5 estende este mapa, nunca reimplementa o dispatch
// tipo->resolver).
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
	case "inclusao", "imobilizado":
		// Story 3.3: Inclusão (plain) usa o motor de autorizador nominal —
		// 2ª implementação de Resolver (AD-2). Story 3.4: Imobilizado
		// reaproveita a MESMA instância/tabela (`autorizadores_formulario`)
		// — FR-11 agrupa inclusão+imobilizado+obras sob o mesmo mecanismo
		// nominal; `nominal.go` já é agnóstico de tipo (consulta só por
		// CC/colaborador/faixa de valor), nenhuma parametrização nova.
		return NovoAutorizadorNominal(db), nil
	default:
		return nil, ErrTipoNaoSuportado
	}
}
