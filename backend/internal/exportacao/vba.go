package exportacao

// vba.go — ExportadorVBA.GerarLoteDespesa (Story 4.3). Única implementação
// de ExportadorSAP.GerarLoteDespesa nesta story — recalcula elegibilidade
// de cada solicitação a partir do banco (nunca confia em status/tipo/dono
// vindos do cliente, Boundaries "Always" da spec), valida exercício único
// e risco de "verba duplicada", e só então monta o `.xlsm` (xlsm.go) e
// grava `exportacoes_sap` pela conexão PRIVILEGIADA passada em `db`.
//
// Ordem de validação (replica o padrão 404->403->409 já estabelecido em
// internal/fila.fallbackMarcarPendencia, estendido com os 2 códigos
// próprios desta story):
//  1. Conflito de chave de idempotência (409) — checagem independente de
//     qualquer solicitação específica, feita primeiro.
//  2. Por solicitação, nesta ordem: não encontrada (404) -> outro
//     administrador (403) -> não em_atendimento (409) -> tipo 'obras' (400).
//  3. Exercício misto entre as solicitações do lote (400).
//  4. Risco de "verba duplicada": se houver e `ignorarAvisoVerbaDuplicada`
//     for false, devolve *Aviso sem gravar nada.
//  5. Template ausente (503) — só checado depois de todas as validações
//     acima, e só por isso nenhuma linha é gravada quando falta (nunca
//     chegou a tentar).
import (
	"database/sql"
	"fmt"
	"os"
)

// ExportadorVBA é a implementação v1 de ExportadorSAP (AD-3) — manipula o
// `.xlsm` como arquivo ZIP/XML (xlsm.go, AD-12), nunca via biblioteca
// externa de OOXML. TemplatePath aponta para o `.xlsm` base (AD-12) —
// resolvido pelo chamador (handlers/exportacao.go) a partir de
// EXPORT_TEMPLATE_LOTE_DESPESA.
type ExportadorVBA struct {
	TemplatePath string
}

// linhaExportacao é uma linha de `solicitacao_lancamentos` já resolvida
// para os códigos de negócio (nunca UUIDs internos) que entram na planilha
// "SAP Export" (xlsm.go) — layout melhor-esforço (Design Notes da spec):
// tipo/solicitação/divisão/CC/conta-ou-classe/mês/valor/lado, mais o nome
// do solicitante (SolicitanteNome) para rastreabilidade humana no arquivo
// — extensão deste código além da lista literal do Design Notes,
// justificada pelo GRANT SELECT em `usuarios` concedido a
// fb_apu05_privilegiado pela migration 012 (sem outro consumidor óbvio
// desse GRANT nesta story).
type linhaExportacao struct {
	TipoSolicitacao     string
	SolicitacaoID       string
	SolicitanteNome     string
	Lado                string
	DivisaoCodigo       string
	CentroCustoCodigo   string
	ContaOuClasseCodigo string
	Mes                 sql.NullTime
	Valor               float64
}

// infoSolicitacao é a projeção mínima de `solicitacoes`+`usuarios` usada
// para recalcular elegibilidade (Boundaries "Always" da spec).
type infoSolicitacao struct {
	TipoSolicitacao string
	Status          string
	AdministradorID sql.NullString
	SolicitanteNome string
}

// GerarLoteDespesa implementa ExportadorSAP.GerarLoteDespesa — ver ordem de
// validação no cabeçalho do arquivo.
func (e ExportadorVBA) GerarLoteDespesa(db *sql.DB, solicitacaoIDs []string, administradorID, chaveIdempotencia string, ignorarAvisoVerbaDuplicada bool) (Lote, *Aviso, error) {
	if len(solicitacaoIDs) == 0 {
		return Lote{}, nil, fmt.Errorf("exportacao: lista de solicitacao_ids vazia")
	}

	if err := verificarConflitoIdempotencia(db, administradorID, chaveIdempotencia, solicitacaoIDs); err != nil {
		return Lote{}, nil, err
	}

	var linhas []linhaExportacao
	anoExercicio := -1
	for _, id := range solicitacaoIDs {
		info, err := buscarSolicitacaoParaExportacao(db, id)
		if err != nil {
			return Lote{}, nil, err
		}
		if info.AdministradorID.Valid && info.AdministradorID.String != administradorID {
			return Lote{}, nil, ErrNaoAutorizado
		}
		if info.Status != "em_atendimento" {
			return Lote{}, nil, ErrElegibilidadeInvalida
		}
		if info.TipoSolicitacao == "obras" {
			return Lote{}, nil, ErrTipoNaoElegivel
		}

		linhasSolicitacao, err := buscarLancamentosParaExportacao(db, id)
		if err != nil {
			return Lote{}, nil, err
		}
		for i := range linhasSolicitacao {
			linhasSolicitacao[i].TipoSolicitacao = info.TipoSolicitacao
			linhasSolicitacao[i].SolicitacaoID = id
			linhasSolicitacao[i].SolicitanteNome = info.SolicitanteNome

			// Exercício único: só sobre linhas com `mes` preenchido —
			// linhas de Imobilizado não têm `mes` (migration 008) e ficam
			// fora desta checagem (Design Notes da spec).
			if linhasSolicitacao[i].Mes.Valid {
				ano := linhasSolicitacao[i].Mes.Time.Year()
				if anoExercicio == -1 {
					anoExercicio = ano
				} else if ano != anoExercicio {
					return Lote{}, nil, ErrExercicioMisto
				}
			}
		}
		linhas = append(linhas, linhasSolicitacao...)
	}

	solicitacoesEmRisco, err := detectarVerbaDuplicada(db, solicitacaoIDs, chaveIdempotencia)
	if err != nil {
		return Lote{}, nil, err
	}
	if len(solicitacoesEmRisco) > 0 && !ignorarAvisoVerbaDuplicada {
		return Lote{}, &Aviso{Codigo: "verba_duplicada", SolicitacoesEmRisco: solicitacoesEmRisco}, nil
	}

	templateBytes, err := os.ReadFile(e.TemplatePath)
	if err != nil {
		return Lote{}, nil, ErrTemplateAusente
	}

	arquivo, err := montarXLSM(templateBytes, linhas)
	if err != nil {
		return Lote{}, nil, fmt.Errorf("exportacao: montar .xlsm: %w", err)
	}

	loteID, err := gravarLote(db, solicitacaoIDs, administradorID, chaveIdempotencia, solicitacoesEmRisco, ignorarAvisoVerbaDuplicada)
	if err != nil {
		return Lote{}, nil, err
	}

	return Lote{LoteID: loteID, ArquivoBytes: arquivo, SolicitacoesIncluidas: solicitacaoIDs}, nil, nil
}

// verificarConflitoIdempotencia checa se `chaveIdempotencia` (para
// `administradorID`) já foi usada com um conjunto de `solicitacaoIDs`
// diferente do atual (Boundaries "Always" da spec) -> 409
// ErrChaveIdempotenciaConflitante. Nenhum uso anterior (conjunto vazio) ou
// o MESMO conjunto (replay idempotente) não é erro.
func verificarConflitoIdempotencia(db *sql.DB, administradorID, chaveIdempotencia string, solicitacaoIDs []string) error {
	rows, err := db.Query(`
		SELECT solicitacao_id FROM exportacoes_sap WHERE administrador_id = $1 AND chave_idempotencia = $2
	`, administradorID, chaveIdempotencia)
	if err != nil {
		return fmt.Errorf("exportacao: consultar uso anterior da chave de idempotência: %w", err)
	}
	defer rows.Close()

	existentes := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("exportacao: ler uso anterior da chave de idempotência: %w", err)
		}
		existentes[id] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("exportacao: iterar uso anterior da chave de idempotência: %w", err)
	}

	if len(existentes) == 0 {
		return nil
	}

	solicitados := make(map[string]bool, len(solicitacaoIDs))
	for _, id := range solicitacaoIDs {
		solicitados[id] = true
	}

	if len(existentes) != len(solicitados) {
		return ErrChaveIdempotenciaConflitante
	}
	for id := range solicitados {
		if !existentes[id] {
			return ErrChaveIdempotenciaConflitante
		}
	}
	return nil
}

// buscarSolicitacaoParaExportacao lê o estado ATUAL de uma solicitação
// (nunca confia no que o cliente mandou) + o nome do solicitante (join em
// `usuarios`, usado no conteúdo do arquivo — ver linhaExportacao).
func buscarSolicitacaoParaExportacao(db *sql.DB, id string) (infoSolicitacao, error) {
	var info infoSolicitacao
	err := db.QueryRow(`
		SELECT s.tipo_solicitacao, s.status, s.administrador_id, u.nome
		FROM solicitacoes s
		JOIN usuarios u ON u.id = s.solicitante_id
		WHERE s.id = $1
	`, id).Scan(&info.TipoSolicitacao, &info.Status, &info.AdministradorID, &info.SolicitanteNome)
	if err == sql.ErrNoRows {
		return infoSolicitacao{}, ErrNaoEncontrada
	}
	if err != nil {
		return infoSolicitacao{}, fmt.Errorf("exportacao: consultar solicitação %s: %w", id, err)
	}
	return info, nil
}

// buscarLancamentosParaExportacao resolve cada linha de
// `solicitacao_lancamentos` para os códigos de negócio correspondentes
// (divisão/CC sempre; conta OU classe de imobilizado, conforme o tipo —
// migration 008, XOR de schema) — nunca os UUIDs internos (Design Notes da
// spec: layout melhor-esforço da planilha "SAP Export").
func buscarLancamentosParaExportacao(db *sql.DB, solicitacaoID string) ([]linhaExportacao, error) {
	rows, err := db.Query(`
		SELECT sl.lado, d.codigo, cc.codigo, c.plano, c.codigo, ci.codigo, sl.mes, sl.valor
		FROM solicitacao_lancamentos sl
		JOIN divisoes d ON d.id = sl.divisao_id
		JOIN centros_custo cc ON cc.id = sl.centro_custo_id
		LEFT JOIN contas c ON c.id = sl.conta_id
		LEFT JOIN classes_imobilizado ci ON ci.id = sl.classe_imobilizado_id
		WHERE sl.solicitacao_id = $1
		ORDER BY sl.created_at ASC, sl.id ASC
	`, solicitacaoID)
	if err != nil {
		return nil, fmt.Errorf("exportacao: consultar lançamentos de %s: %w", solicitacaoID, err)
	}
	defer rows.Close()

	var linhas []linhaExportacao
	for rows.Next() {
		var lado, divisaoCodigo, centroCustoCodigo string
		var contaPlano, contaCodigo, classeCodigo sql.NullString
		var mes sql.NullTime
		var valor float64
		if err := rows.Scan(&lado, &divisaoCodigo, &centroCustoCodigo, &contaPlano, &contaCodigo, &classeCodigo, &mes, &valor); err != nil {
			return nil, fmt.Errorf("exportacao: ler lançamento de %s: %w", solicitacaoID, err)
		}

		contaOuClasse := classeCodigo.String
		if contaPlano.Valid && contaCodigo.Valid {
			contaOuClasse = contaPlano.String + "-" + contaCodigo.String
		}

		linhas = append(linhas, linhaExportacao{
			Lado:                lado,
			DivisaoCodigo:       divisaoCodigo,
			CentroCustoCodigo:   centroCustoCodigo,
			ContaOuClasseCodigo: contaOuClasse,
			Mes:                 mes,
			Valor:               valor,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("exportacao: iterar lançamentos de %s: %w", solicitacaoID, err)
	}
	return linhas, nil
}

// detectarVerbaDuplicada devolve os `solicitacao_ids` do lote que já têm
// uma linha `exportacoes_sap` com `status='GERADO'` de uma
// `chave_idempotencia` DIFERENTE da atual (Boundaries "Always" da spec) —
// exclui deliberadamente a própria chave atual, para um replay idempotente
// (mesma chave, mesmos IDs) nunca se auto-marcar como risco.
func detectarVerbaDuplicada(db *sql.DB, solicitacaoIDs []string, chaveIdempotencia string) ([]string, error) {
	var emRisco []string
	for _, id := range solicitacaoIDs {
		var existe bool
		err := db.QueryRow(`
			SELECT EXISTS(
				SELECT 1 FROM exportacoes_sap
				WHERE solicitacao_id = $1 AND status = 'GERADO' AND chave_idempotencia != $2
			)
		`, id, chaveIdempotencia).Scan(&existe)
		if err != nil {
			return nil, fmt.Errorf("exportacao: checar verba duplicada de %s: %w", id, err)
		}
		if existe {
			emRisco = append(emRisco, id)
		}
	}
	return emRisco, nil
}

// gravarLote grava 1 linha de `exportacoes_sap` por solicitação (idempotente
// via ON CONFLICT DO NOTHING na UNIQUE (administrador_id,
// chave_idempotencia, solicitacao_id), migration 012) + 1
// `solicitacao_comentarios` (tipo='aviso_verba_duplicada') por solicitação
// em risco quando o aviso foi ignorado — tudo na MESMA transação (mesmo
// princípio de internal/fila.MarcarPendencia: nunca uma escrita sem a
// outra). `lote_id` é gerado uma única vez (gen_random_uuid() dentro da
// transação, não um DEFAULT de coluna) e reusado em todos os INSERTs deste
// lote; a releitura final (`SELECT lote_id ... LIMIT 1`) devolve o valor
// já gravado em qualquer retry (replay idempotente) em vez do candidato
// gerado nesta chamada, que é descartado quando o ON CONFLICT não insere
// nada.
func gravarLote(db *sql.DB, solicitacaoIDs []string, administradorID, chaveIdempotencia string, solicitacoesEmRisco []string, ignorarAvisoVerbaDuplicada bool) (string, error) {
	tx, err := db.Begin()
	if err != nil {
		return "", fmt.Errorf("exportacao: iniciar transação do lote: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var loteCandidato string
	if err := tx.QueryRow(`SELECT gen_random_uuid()`).Scan(&loteCandidato); err != nil {
		return "", fmt.Errorf("exportacao: gerar lote_id: %w", err)
	}

	for _, id := range solicitacaoIDs {
		if _, err := tx.Exec(`
			INSERT INTO exportacoes_sap (lote_id, solicitacao_id, administrador_id, chave_idempotencia)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (administrador_id, chave_idempotencia, solicitacao_id) DO NOTHING
		`, loteCandidato, id, administradorID, chaveIdempotencia); err != nil {
			return "", fmt.Errorf("exportacao: gravar exportacoes_sap de %s: %w", id, err)
		}
	}

	if ignorarAvisoVerbaDuplicada {
		for _, id := range solicitacoesEmRisco {
			if _, err := tx.Exec(`
				INSERT INTO solicitacao_comentarios (solicitacao_id, autor_id, tipo, texto)
				VALUES ($1, $2, 'aviso_verba_duplicada', $3)
			`, id, administradorID, "risco de verba duplicada ignorado ao gerar o lote de despesa"); err != nil {
				return "", fmt.Errorf("exportacao: gravar aviso de verba duplicada de %s: %w", id, err)
			}
		}
	}

	var loteFinal string
	if err := tx.QueryRow(`
		SELECT lote_id FROM exportacoes_sap WHERE administrador_id = $1 AND chave_idempotencia = $2 LIMIT 1
	`, administradorID, chaveIdempotencia).Scan(&loteFinal); err != nil {
		return "", fmt.Errorf("exportacao: reler lote_id: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("exportacao: confirmar transação do lote: %w", err)
	}

	return loteFinal, nil
}
