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
