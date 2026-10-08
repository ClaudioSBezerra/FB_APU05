// Package aprovacao é o núcleo de domínio isolado do motor de aprovação
// (Architecture Spine AD-1/AD-2) — sem dependência de net/http nem de
// nenhum driver de banco específico na sua superfície pública (só
// database/sql, a abstração padrão da linguagem); handlers nunca calculam
// aprovador diretamente, sempre delegam a este pacote.
package aprovacao

import "fmt"

// Tipo de Aprovador — "pessoa" quando ColaboradorID aponta para um
// colaborador específico, "cargo" quando o papel resolvido não tem (ou não
// precisa de) um titular único (papel colegiado sem titular, ou grupo
// colegiado de gerentes — ambos são "cargo", nunca uma lista de pessoas;
// Boundaries "Always" da spec).
const (
	TipoPessoa = "pessoa"
	TipoCargo  = "cargo"
)

// Aprovador é o resultado do cálculo de alçada — tipo único e explícito,
// nunca string livre (AD-2). ColaboradorID é nulo exatamente quando
// Tipo="cargo".
type Aprovador struct {
	Tipo          string // "pessoa" | "cargo"
	Nome          string
	ColaboradorID *string
	Motivo        string
	RegraID       string
	RegraVersao   int
}

// ErrSemAlcadaCadastrada é o erro sentinela devolvido quando nenhuma etapa da
// cadeia de resolução (regras especiais curadas -> matriz de alçadas ->
// janela do gerente -> fallback GR) cobre o caso — o resolver NUNCA
// sintetiza um Aprovador fictício para preencher a lacuna (AD-2, Boundaries
// "Never" da spec: "inventar aprovador fictício ou escalonamento acima de
// R$20.000"). É o chamador (handler HTTP) que traduz este erro em
// "sem alçada cadastrada" para o usuário, citando CentroCusto e Filial.
type ErrSemAlcadaCadastrada struct {
	CentroCusto string
	Filial      string
}

func (e *ErrSemAlcadaCadastrada) Error() string {
	return fmt.Sprintf("sem alçada cadastrada para o centro de custo %q (filial %q)", e.CentroCusto, e.Filial)
}

// ErrAutorizadorInvalido é o erro sentinela devolvido por AutorizadorNominal
// (Story 3.3, FR-6/FR-11) quando o autorizador_id escolhido pelo
// solicitante NÃO tem nenhuma linha ativa em `autorizadores_formulario`
// cobrindo o centro de custo e a faixa de valor da solicitação —
// AutorizadorNominal NUNCA sintetiza um Aprovador quando essa checagem falha
// (mesmo princípio AD-2 de ErrSemAlcadaCadastrada). É o chamador (handler
// HTTP) que traduz este erro em 400, não 422 — Design Notes da spec 3.3:
// diferente de ErrSemAlcadaCadastrada (lacuna de configuração num cálculo
// sem entrada do cliente), este erro sempre envolve um autorizador_id que o
// próprio cliente enviou (mesma natureza de "conta do plano errado"). A
// mensagem nunca distingue "ninguém cadastrado para este CC/faixa" de "esta
// pessoa não é a cadastrada" — evita expor a lista de autorizadores por CC
// via tentativa e erro.
type ErrAutorizadorInvalido struct {
	CentroCusto string
}

func (e *ErrAutorizadorInvalido) Error() string {
	return "autorizador selecionado não é válido para este centro de custo e faixa de valor"
}
