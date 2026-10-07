package aprovacao

import (
	"database/sql"
	"fmt"
)

// ResolverCalculado implementa Resolver para os tipos que usam o motor de
// alçada calculada (Transferência nesta story; Inclusão SFC reaproveita o
// MESMO motor na Story 3.2 — Cross-Story Dependencies do epic-3-context, não
// reimplementa a cadeia). Encadeia, em ordem (Epic 3 context / Boundaries
// "Always" da spec):
//
//  1. regras especiais curadas (`regras_aprovacao`) — precedência mais alta
//     que a matriz de alçadas quando uma linha casa;
//  2. matriz de alçadas SAP (`alcadas`);
//  3. janela do gerente do centro de custo (`gerentes_aprovacao`, linha por
//     `centro_custo_id`);
//  4. fallback GR da divisão (`gerentes_aprovacao`, linha por `divisao_id`,
//     `centro_custo_id` nulo);
//  5. fallback GR global (`gerentes_aprovacao`, os dois nulos);
//  6. ErrSemAlcadaCadastrada — nunca sintetiza um Aprovador fictício.
type ResolverCalculado struct {
	db DBTX
}

// NovoResolverCalculado constrói um ResolverCalculado sobre a conexão/
// transação informada — tipicamente a MESMA transação que o handler usa para
// gravar `solicitacoes`/`solicitacao_lancamentos` (tudo atômico).
func NovoResolverCalculado(db DBTX) *ResolverCalculado {
	return &ResolverCalculado{db: db}
}

// Resolve implementa Resolver.
func (r *ResolverCalculado) Resolve(s Solicitacao) (Aprovador, error) {
	etapas := []func(Solicitacao) (Aprovador, bool, error){
		r.regraCurada,
		r.alcada,
		r.janelaGerenteCC,
		r.fallbackDivisao,
		r.fallbackGlobal,
	}

	for _, etapa := range etapas {
		aprovador, ok, err := etapa(s)
		if err != nil {
			return Aprovador{}, err
		}
		if ok {
			return aprovador, nil
		}
	}

	return Aprovador{}, &ErrSemAlcadaCadastrada{CentroCusto: s.CentroCustoCodigo, Filial: s.Filial}
}

// regraCurada consulta `regras_aprovacao`: filial+centro_custo_codigo+valor
// casam, ativo=true. Quando mais de uma linha casa, a de menor `precedencia`
// vence (critério de desempate entre regras curadas concorrentes).
func (r *ResolverCalculado) regraCurada(s Solicitacao) (Aprovador, bool, error) {
	var id string
	var colaboradorID, papelAprovador sql.NullString
	err := r.db.QueryRow(`
		SELECT id, colaborador_id, papel_aprovador
		FROM regras_aprovacao
		WHERE ativo = true
		  AND filial = $1
		  AND centro_custo_codigo = $2
		  AND valor_minimo <= $3
		  AND (valor_maximo IS NULL OR valor_maximo >= $3)
		ORDER BY precedencia ASC, id
		LIMIT 1
	`, s.Filial, s.CentroCustoCodigo, s.Valor).Scan(&id, &colaboradorID, &papelAprovador)
	if err == sql.ErrNoRows {
		return Aprovador{}, false, nil
	}
	if err != nil {
		return Aprovador{}, false, fmt.Errorf("aprovacao: regras_aprovacao: %w", err)
	}

	aprovador, err := r.montarAprovador(id, "regras-aprovacao", "regra especial curada", colaboradorID, papelAprovador)
	if err != nil {
		return Aprovador{}, false, err
	}
	return aprovador, true, nil
}

// alcada consulta a matriz de alçadas SAP (`alcadas`, já existe desde a
// Story 2.1) — só tem `papel_aprovador` (sem `colaborador_id`), então todo
// match aqui é sempre Tipo="cargo".
func (r *ResolverCalculado) alcada(s Solicitacao) (Aprovador, bool, error) {
	var id string
	var papelAprovador sql.NullString
	err := r.db.QueryRow(`
		SELECT id, papel_aprovador
		FROM alcadas
		WHERE ativo = true
		  AND filial = $1
		  AND centro_custo_codigo = $2
		  AND valor_minimo <= $3
		  AND (valor_maximo IS NULL OR valor_maximo >= $3)
		ORDER BY valor_minimo DESC, id
		LIMIT 1
	`, s.Filial, s.CentroCustoCodigo, s.Valor).Scan(&id, &papelAprovador)
	if err == sql.ErrNoRows {
		return Aprovador{}, false, nil
	}
	if err != nil {
		return Aprovador{}, false, fmt.Errorf("aprovacao: alcadas: %w", err)
	}

	aprovador, err := r.montarAprovador(id, "alcadas", "matriz de alçadas", sql.NullString{}, papelAprovador)
	if err != nil {
		return Aprovador{}, false, err
	}
	return aprovador, true, nil
}

// As 3 queries de `gerentes_aprovacao` abaixo são literais fixos (nunca
// montados por concatenação/fmt.Sprintf) — a condição WHERE extra nunca vem
// de entrada do usuário, mas um literal fixo por chamador evita que uma
// futura extensão (ex. mais um degrau de fallback) introduza concatenação de
// algo que não seja uma string fixa no módulo de maior risco do produto.
const (
	queryGerentePorCC = `
		SELECT id, colaborador_id, papel_aprovador
		FROM gerentes_aprovacao
		WHERE ativo = true
		  AND teto >= $1
		  AND centro_custo_id = $2
		ORDER BY teto ASC, id
		LIMIT 1
	`
	queryGerentePorDivisao = `
		SELECT id, colaborador_id, papel_aprovador
		FROM gerentes_aprovacao
		WHERE ativo = true
		  AND teto >= $1
		  AND centro_custo_id IS NULL AND divisao_id = $2
		ORDER BY teto ASC, id
		LIMIT 1
	`
	queryGerenteGlobal = `
		SELECT id, colaborador_id, papel_aprovador
		FROM gerentes_aprovacao
		WHERE ativo = true
		  AND teto >= $1
		  AND centro_custo_id IS NULL AND divisao_id IS NULL
		ORDER BY teto ASC, id
		LIMIT 1
	`
)

// janelaGerenteCC consulta `gerentes_aprovacao` pela linha específica do
// centro de custo (teto >= valor — FR-10: janela do gerente vale até
// R$20.000 por padrão, mas o teto é por linha, não uma constante).
func (r *ResolverCalculado) janelaGerenteCC(s Solicitacao) (Aprovador, bool, error) {
	return r.gerente(queryGerentePorCC, s.CentroCustoID, s.Valor, "janela do gerente do centro de custo")
}

// fallbackDivisao consulta `gerentes_aprovacao` pela linha de divisão
// (centro_custo_id nulo, divisao_id preenchido) — "sobe para o GR mais
// próximo acima" quando não há linha específica do CC.
func (r *ResolverCalculado) fallbackDivisao(s Solicitacao) (Aprovador, bool, error) {
	return r.gerente(queryGerentePorDivisao, s.DivisaoID, s.Valor, "fallback do GR da divisão")
}

// fallbackGlobal consulta `gerentes_aprovacao` pela linha global (os dois
// nulos) — último degrau antes de ErrSemAlcadaCadastrada.
func (r *ResolverCalculado) fallbackGlobal(s Solicitacao) (Aprovador, bool, error) {
	return r.gerente(queryGerenteGlobal, nil, s.Valor, "fallback do GR global")
}

// gerente é o helper compartilhado pelas 3 consultas acima a
// `gerentes_aprovacao` — recebe a query literal já fixa (nunca montada por
// concatenação) e só varia o argumento posicional, quando houver.
func (r *ResolverCalculado) gerente(query string, arg interface{}, valor float64, motivo string) (Aprovador, bool, error) {
	var row *sql.Row
	if arg == nil {
		row = r.db.QueryRow(query, valor)
	} else {
		row = r.db.QueryRow(query, valor, arg)
	}

	var id string
	var colaboradorID, papelAprovador sql.NullString
	err := row.Scan(&id, &colaboradorID, &papelAprovador)
	if err == sql.ErrNoRows {
		return Aprovador{}, false, nil
	}
	if err != nil {
		return Aprovador{}, false, fmt.Errorf("aprovacao: gerentes_aprovacao: %w", err)
	}

	aprovador, err := r.montarAprovador(id, "gerentes-aprovacao", motivo, colaboradorID, papelAprovador)
	if err != nil {
		return Aprovador{}, false, err
	}
	return aprovador, true, nil
}

// montarAprovador monta o Aprovador final a partir de uma linha casada de
// regras_aprovacao/alcadas/gerentes_aprovacao: Tipo="pessoa" quando
// colaborador_id está presente (busca o nome em `usuarios`), Tipo="cargo"
// quando só papel_aprovador está presente — cobre tanto papel colegiado sem
// titular quanto grupo colegiado de gerentes (Design Notes da spec).
// RegraVersao vem de MAX(cadastro_historico.versao) para (tipoCadastro, id)
// — reaproveita o histórico já existente em vez de versionar em paralelo
// (Code Map da spec).
func (r *ResolverCalculado) montarAprovador(id, tipoCadastro, motivo string, colaboradorID, papelAprovador sql.NullString) (Aprovador, error) {
	versao, err := r.versaoDoCadastro(tipoCadastro, id)
	if err != nil {
		return Aprovador{}, err
	}

	if colaboradorID.Valid {
		nome, err := r.nomeColaborador(colaboradorID.String)
		if err != nil {
			return Aprovador{}, err
		}
		cid := colaboradorID.String
		return Aprovador{
			Tipo:          TipoPessoa,
			Nome:          nome,
			ColaboradorID: &cid,
			Motivo:        motivo,
			RegraID:       id,
			RegraVersao:   versao,
		}, nil
	}

	nome := ""
	if papelAprovador.Valid {
		nome = papelAprovador.String
	}
	return Aprovador{
		Tipo:        TipoCargo,
		Nome:        nome,
		Motivo:      motivo,
		RegraID:     id,
		RegraVersao: versao,
	}, nil
}

func (r *ResolverCalculado) versaoDoCadastro(tipoCadastro, registroID string) (int, error) {
	var versao int
	err := r.db.QueryRow(`
		SELECT COALESCE(MAX(versao), 1) FROM cadastro_historico WHERE tipo_cadastro = $1 AND registro_id = $2
	`, tipoCadastro, registroID).Scan(&versao)
	if err != nil {
		return 0, fmt.Errorf("aprovacao: cadastro_historico (%s/%s): %w", tipoCadastro, registroID, err)
	}
	return versao, nil
}

func (r *ResolverCalculado) nomeColaborador(colaboradorID string) (string, error) {
	var nome string
	err := r.db.QueryRow(`SELECT nome FROM usuarios WHERE id = $1`, colaboradorID).Scan(&nome)
	if err != nil {
		return "", fmt.Errorf("aprovacao: usuarios (%s): %w", colaboradorID, err)
	}
	return nome, nil
}
