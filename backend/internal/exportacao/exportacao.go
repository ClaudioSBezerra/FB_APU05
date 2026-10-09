// Package exportacao é a porta única de exportação SAP (Architecture
// Spine AD-3) — hoje só `GerarLoteDespesa` (Story 4.3); `GerarLoteObra`
// fica para a Story 4.4 (Boundaries "Never" da spec 4.3, Code Map da
// própria spec do Lote-Obra). Mesmo princípio de isolamento de
// internal/aprovacao/internal/fila: sem dependência de net/http na
// superfície pública — só database/sql.
//
// `ExportadorVBA` (vba.go) é a implementação v1 (AD-3): recalcula
// elegibilidade a partir do banco (nunca confia no que vem do cliente além
// dos próprios IDs), grava `exportacoes_sap` pela conexão PRIVILEGIADA
// (AD-4, mesmo princípio de internal/fila) e monta o `.xlsm` (xlsm.go,
// AD-12) reescrevendo um template fixo lido de disco — nunca gerando um
// arquivo do zero.
package exportacao

import (
	"database/sql"
	"errors"
)

// ExportadorSAP é a interface única de exportação SAP (AD-3). v1 só
// implementa GerarLoteDespesa — a assinatura aqui já inclui os parâmetros
// de idempotência/aviso de verba duplicada próprios desta story (não é
// literalmente igual ao esqueleto `GerarLoteDespesa(solicitacaoIDs
// []string) (Lote, error)` do Architecture Spine, que antecede esta story:
// idempotência e aviso de verba duplicada são requisitos descobertos na
// elicitação da Story 4.3, Boundaries "Always" da spec).
type ExportadorSAP interface {
	GerarLoteDespesa(db *sql.DB, solicitacaoIDs []string, administradorID, chaveIdempotencia string, ignorarAvisoVerbaDuplicada bool) (Lote, *Aviso, error)
}

// Lote é o resultado de uma geração bem-sucedida (sem aviso pendente) —
// ArquivoBytes é o `.xlsm` já montado (xlsm.go), pronto para o handler
// devolver em base64 (AD-3: a própria implementação de ExportadorSAP grava
// `exportacoes_sap`, nunca o handler).
type Lote struct {
	LoteID                string
	ArquivoBytes          []byte
	SolicitacoesIncluidas []string
}

// Aviso é devolvido em vez de Lote quando o lote tem risco de "verba
// duplicada" (Boundaries "Always" da spec) e o chamador não pediu
// `ignorarAvisoVerbaDuplicada=true` — nada é gravado nesse caso (nem
// `exportacoes_sap`, nem `solicitacao_comentarios`).
type Aviso struct {
	Codigo              string
	SolicitacoesEmRisco []string
}

// Sentinelas traduzidos pelo handler HTTP (handlers/exportacao.go) nos
// códigos da I/O Matrix da spec — mesmo padrão de internal/fila
// (ErrNaoAutorizado/ErrNaoEncontrada/ErrConflitoVersao).
var (
	// ErrTemplateAusente -> 503: `EXPORT_TEMPLATE_LOTE_DESPESA` não aponta
	// para um `.xlsm` legível. Nenhuma linha é gravada quando este erro
	// ocorre (Boundaries "Always" da spec).
	ErrTemplateAusente = errors.New("exportacao: template .xlsm ausente ou ilegível")

	// ErrNaoAutorizado -> 403: alguma solicitação do lote pertence a um
	// administrador diferente do chamador.
	ErrNaoAutorizado = errors.New("exportacao: não autorizado")

	// ErrNaoEncontrada -> 404: algum `solicitacao_id` do lote não existe.
	ErrNaoEncontrada = errors.New("exportacao: solicitação não encontrada")

	// ErrElegibilidadeInvalida -> 409: alguma solicitação do lote não está
	// `status='em_atendimento'` (ex.: 'aberta', 'pendente', ou qualquer
	// outro status).
	ErrElegibilidadeInvalida = errors.New("exportacao: solicitação não está em atendimento")

	// ErrChaveIdempotenciaConflitante -> 409: a mesma `chave_idempotencia`
	// (para o mesmo `administrador_id`) já foi usada com um conjunto de
	// `solicitacao_ids` diferente do atual.
	ErrChaveIdempotenciaConflitante = errors.New("exportacao: chave de idempotência já usada com um conjunto diferente de solicitações")

	// ErrTipoNaoElegivel -> 400: alguma solicitação do lote tem
	// `tipo_solicitacao='obras'` (Lote-Obra é a Story 4.4, Boundaries
	// "Never" desta spec).
	ErrTipoNaoElegivel = errors.New("exportacao: tipo de solicitação não é elegível para o lote de despesa")

	// ErrExercicioMisto -> 400: as linhas das solicitações do lote (que têm
	// `mes` preenchido) caem em mais de um exercício orçamentário (ano).
	ErrExercicioMisto = errors.New("exportacao: solicitações com exercícios orçamentários diferentes")
)
