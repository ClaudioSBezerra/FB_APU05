package exportacao

// finalizar.go — ExportadorVBA.Finalizar (Story 4.5, Epic 4, FR-14). Fecha
// o ciclo deixado em aberto por GerarLoteDespesa/GerarLoteObra (vba.go/
// obra.go, Stories 4.3/4.4): nenhum dos dois muda `status` de
// `solicitacoes` — Finalizar é a única escrita que faz isso. Numa ÚNICA
// transação pela conexão PRIVILEGIADA `db` (AD-4/AD-5, mesma linha/lock de
// `solicitacoes` que a exportação já usa):
//
//  1. UPDATE condicional por versão/dono/status em `solicitacoes`
//     (lock otimista, mesmo padrão de fila.MarcarPendencia) — 0 linhas
//     afetadas: fallback 404->403->409 (fallbackFinalizar, replica
//     fila.fallbackMarcarPendencia).
//  2. UPDATE de TODAS as linhas `exportacoes_sap` da solicitação de
//     'GERADO' para 'FINALIZADO' — 0 linhas afetadas vira ErrNaoExportada
//     (a solicitação nunca foi incluída num lote gerado, ou já foi
//     finalizada antes).
//  3. Só quando `resultado=="sucesso"`: resolve cada linha de Obras
//     `ordem_investimento='CRIAR'` da solicitação, criando a ordem real em
//     `obra_ordens` a partir do `numero_ordem` de `ordensCriadas`
//     (resolverLinhasCriar) — qualquer divergência 1:1 entre as linhas
//     CRIAR e `ordensCriadas` aborta a transação inteira
//     (ErrOrdensCriadasInvalidas); `numero_ordem` duplicado também aborta
//     (ErrOrdemJaExiste, violação de unicidade do Postgres).
//
// Qualquer erro em qualquer etapa faz rollback da transação inteira —
// nenhuma escrita parcial (Boundaries "Always" da spec).
import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

// Finalizar implementa ExportadorSAP.Finalizar — ver ordem de operações no
// cabeçalho do arquivo. `administradorID` já deve vir de
// GetUserIDFromContext (Boundaries "Always" da spec) — este pacote não lê
// contexto HTTP (AD-1, mesmo princípio de internal/fila).
func (e ExportadorVBA) Finalizar(db *sql.DB, solicitacaoID string, versaoLida int, administradorID, resultado string, ordensCriadas []OrdemCriada) (Solicitacao, error) {
	if resultado != "sucesso" && resultado != "erro" {
		// Guarda defensiva (AD-1: este pacote nunca confia no chamador) —
		// sem isso, qualquer valor fora de {"sucesso","erro"} cairia
		// silenciosamente em `finalizada_erro` (o `if resultado == "sucesso"`
		// abaixo só reconhece o literal certo). O handler HTTP já valida
		// isto antes de chamar Finalizar, mas o domínio não deve depender
		// só disso.
		return Solicitacao{}, fmt.Errorf("exportacao: resultado inválido %q (esperado 'sucesso' ou 'erro')", resultado)
	}

	tx, err := db.Begin()
	if err != nil {
		return Solicitacao{}, fmt.Errorf("exportacao: iniciar transação para finalizar %s: %w", solicitacaoID, err)
	}
	defer func() { _ = tx.Rollback() }()

	// Literais novos, nunca vistos em `status` até esta story (`status` é
	// VARCHAR(30) sem CHECK, Intent da spec) — a reabertura por EXCLUSÃO de
	// fila.Comentar (Design Notes da spec 4.5) já trata qualquer status
	// fora de {aberta,em_atendimento} como "encerrado", então nenhuma
	// mudança é necessária em fila.go para esses dois literais existirem.
	novoStatus := "finalizada_erro"
	if resultado == "sucesso" {
		novoStatus = "finalizada_sucesso"
	}

	var s Solicitacao
	err = tx.QueryRow(`
		UPDATE solicitacoes
		SET status = $1, versao = versao + 1
		WHERE id = $2 AND versao = $3 AND administrador_id = $4 AND status = 'em_atendimento'
		RETURNING id, tipo_solicitacao, status, versao, administrador_id
	`, novoStatus, solicitacaoID, versaoLida, administradorID).Scan(&s.ID, &s.TipoSolicitacao, &s.Status, &s.Versao, &s.AdministradorID)
	if err != nil {
		if err != sql.ErrNoRows {
			return Solicitacao{}, fmt.Errorf("exportacao: finalizar solicitação %s: %w", solicitacaoID, err)
		}
		// 0 linhas afetadas: nada foi escrito ainda nesta transação —
		// encerra explicitamente ANTES do SELECT de fallback (que roda na
		// conexão `db` diretamente, fora da tx), mesmo princípio de
		// fila.MarcarPendencia.
		_ = tx.Rollback()
		return Solicitacao{}, fallbackFinalizar(db, solicitacaoID, administradorID)
	}

	resExportacoes, err := tx.Exec(`
		UPDATE exportacoes_sap SET status = 'FINALIZADO'
		WHERE solicitacao_id = $1 AND status = 'GERADO'
	`, solicitacaoID)
	if err != nil {
		return Solicitacao{}, fmt.Errorf("exportacao: migrar exportacoes_sap da solicitação %s: %w", solicitacaoID, err)
	}
	linhasExportacoesMigradas, err := resExportacoes.RowsAffected()
	if err != nil {
		return Solicitacao{}, fmt.Errorf("exportacao: contar exportacoes_sap migradas da solicitação %s: %w", solicitacaoID, err)
	}
	if linhasExportacoesMigradas == 0 {
		// A solicitação existe, está em_atendimento e o lock otimista bateu
		// — mas nenhuma linha `exportacoes_sap` GERADO existe para ela
		// (nunca foi exportada, ou já foi finalizada antes). Recusa a
		// operação INTEIRA (rollback via defer) — nenhuma escrita parcial.
		return Solicitacao{}, ErrNaoExportada
	}

	if resultado == "sucesso" {
		if err := resolverLinhasCriar(tx, solicitacaoID, ordensCriadas); err != nil {
			return Solicitacao{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return Solicitacao{}, fmt.Errorf("exportacao: confirmar transação de finalização de %s: %w", solicitacaoID, err)
	}
	return s, nil
}

// fallbackFinalizar distingue 404/403/409 quando o UPDATE de Finalizar
// afeta 0 linhas — mesma ordem/motivo de fila.fallbackMarcarPendencia:
// `administrador_id` nulável (toda solicitação nasce sem administrador,
// migration 010) por isso sql.NullString — uma linha NULL nunca é "outro
// administrador" (403), só um administrador_id PREENCHIDO e diferente do
// chamador é; NULL cai no mesmo 409 de qualquer outro status/versão
// incompatível.
func fallbackFinalizar(db *sql.DB, id string, administradorID string) error {
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
		return fmt.Errorf("exportacao: consultar solicitação %s para fallback de finalização: %w", id, err)
	}
	if donoID.Valid && donoID.String != administradorID {
		return ErrNaoAutorizado
	}
	return &ErrConflitoVersao{VersaoAtual: versaoAtual}
}

// linhaCriarPendente é a projeção mínima de uma linha
// `solicitacao_obras_linhas` com `ordem_investimento='CRIAR'` ainda não
// resolvida — só os campos necessários para gravar `obra_ordens`.
type linhaCriarPendente struct {
	ID                string
	LocalObraID       string
	SubgrupoDespesaID string
}

// resolverLinhasCriar lê, na MESMA transação `tx`, todas as linhas
// `solicitacao_obras_linhas` com `ordem_investimento='CRIAR'` da
// solicitação, confere 1:1 contra `ordensCriadas` (qualquer divergência —
// falta linha CRIAR, sobra `linha_id` extra, `linha_id` duplicado em
// `ordensCriadas`, ou que não é CRIAR/não é desta solicitação — vira
// ErrOrdensCriadasInvalidas) e, por linha, INSERT em `obra_ordens` +
// UPDATE de `ordem_investimento` em `solicitacao_obras_linhas` (Code Map da
// spec 4.5). Só chamada quando `resultado=="sucesso"` (Boundaries "Always"
// da spec: nunca criar ordem real quando resultado='erro').
func resolverLinhasCriar(tx *sql.Tx, solicitacaoID string, ordensCriadas []OrdemCriada) error {
	rows, err := tx.Query(`
		SELECT id, local_obra_id, subgrupo_despesa_id
		FROM solicitacao_obras_linhas
		WHERE solicitacao_id = $1 AND ordem_investimento = 'CRIAR'
	`, solicitacaoID)
	if err != nil {
		return fmt.Errorf("exportacao: consultar linhas 'CRIAR' da solicitação %s: %w", solicitacaoID, err)
	}

	var linhas []linhaCriarPendente
	for rows.Next() {
		var l linhaCriarPendente
		if err := rows.Scan(&l.ID, &l.LocalObraID, &l.SubgrupoDespesaID); err != nil {
			rows.Close()
			return fmt.Errorf("exportacao: ler linha 'CRIAR' da solicitação %s: %w", solicitacaoID, err)
		}
		linhas = append(linhas, l)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("exportacao: iterar linhas 'CRIAR' da solicitação %s: %w", solicitacaoID, err)
	}
	rows.Close()

	// Divergência de contagem já basta para recusar — cobre tanto "falta
	// linha CRIAR" quanto "sobra linha_id extra" sem precisar de uma
	// segunda passada (I/O Matrix da spec).
	if len(linhas) != len(ordensCriadas) {
		return ErrOrdensCriadasInvalidas
	}

	// Chave em minúsculas: o Postgres devolve colunas `uuid` já
	// canonicalizadas em lowercase, mas `handlers.uuidFormatRegexp` aceita
	// `linha_id` em qualquer caixa e o devolve sem normalizar — sem isto,
	// um `linha_id` sintaticamente válido só com letras maiúsculas seria
	// rejeitado aqui por engano (ErrOrdensCriadasInvalidas).
	porID := make(map[string]linhaCriarPendente, len(linhas))
	for _, l := range linhas {
		porID[strings.ToLower(l.ID)] = l
	}

	for _, oc := range ordensCriadas {
		l, ok := porID[strings.ToLower(oc.LinhaID)]
		if !ok {
			// `linha_id` não é uma linha CRIAR desta solicitação — ou não
			// existe, ou já foi consumido por um item anterior de
			// `ordensCriadas` (linha_id duplicado: defesa redundante à
			// validação de corpo do handler, nunca confiada por este
			// pacote — AD-1).
			return ErrOrdensCriadasInvalidas
		}
		delete(porID, strings.ToLower(oc.LinhaID))

		numeroOrdem := strings.TrimSpace(oc.NumeroOrdem)
		if numeroOrdem == "" {
			// Mesma defesa redundante de LinhaID acima — o handler HTTP já
			// rejeita `numero_ordem` vazio após TrimSpace, mas este pacote
			// nunca confia só nisso (AD-1).
			return ErrOrdensCriadasInvalidas
		}

		if _, err := tx.Exec(`
			INSERT INTO obra_ordens (numero_ordem, local_obra_id, subgrupo_despesa_id)
			VALUES ($1, $2, $3)
		`, numeroOrdem, l.LocalObraID, l.SubgrupoDespesaID); err != nil {
			if ehViolacaoDeUnicidade(err) {
				return ErrOrdemJaExiste
			}
			return fmt.Errorf("exportacao: inserir ordem %s para a linha %s: %w", numeroOrdem, l.ID, err)
		}

		if _, err := tx.Exec(`
			UPDATE solicitacao_obras_linhas SET ordem_investimento = $1 WHERE id = $2
		`, numeroOrdem, l.ID); err != nil {
			return fmt.Errorf("exportacao: atualizar ordem_investimento da linha %s: %w", l.ID, err)
		}
	}

	return nil
}

// ehViolacaoDeUnicidade detecta `UNIQUE obra_ordens.numero_ordem`
// (migration 009) — mesmo padrão de ehViolacaoDeConstraint
// (handlers/cadastros.go), reimplementado aqui porque internal/exportacao
// não importa `handlers` (AD-1: domínio isolado, sem dependência da camada
// HTTP) e ainda não importava `github.com/lib/pq` em nenhum outro arquivo
// deste pacote (Code Map da spec 4.5).
func ehViolacaoDeUnicidade(err error) bool {
	pqErr, ok := err.(*pq.Error)
	return ok && pqErr.Code.Name() == "unique_violation"
}
