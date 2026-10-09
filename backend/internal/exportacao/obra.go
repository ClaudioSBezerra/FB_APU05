package exportacao

// obra.go — ExportadorVBA.GerarLoteObra (Story 4.4). Mesmo princípio de
// isolamento/recálculo de GerarLoteDespesa (vba.go): nunca confia em
// status/tipo/dono vindos do cliente além do próprio `solicitacao_id`.
// Diferença central: uma falha de dono/status/tipo (403/409/400) ainda
// aborta o LOTE INTEIRO (igual à Story 4.3), mas a ausência de linha
// válida numa solicitação (todas com `ordem_investimento='CRIAR'`) NUNCA é
// erro — essa solicitação entra em `[]Bloqueio` com motivo e as demais do
// lote seguem normalmente (Boundaries "Always" da spec 4.4).
//
// Ordem de validação por solicitação (mesma ordem 404->403->409->400 da
// Story 4.3, só a direção do 400 muda): não encontrada (404) -> outro
// administrador (403) -> não em_atendimento (409) -> tipo != 'obras'
// (400). Só depois disso as linhas da solicitação são lidas e filtradas.
import (
	"database/sql"
	"fmt"
	"os"
)

// linhaExportacaoObra é uma linha de `solicitacao_obras_linhas` já
// resolvida para os códigos de negócio (nunca UUIDs internos) que entram
// na planilha "SAP Export" do lote Obra — layout melhor-esforço (Design
// Notes da spec 4.4): tipo_solicitacao/solicitacao_id/classificacao/
// local_obra_codigo/subgrupo_codigo/ordem_investimento/valor/lado, mais o
// nome do solicitante para rastreabilidade humana no arquivo (mesmo
// raciocínio de SolicitanteNome em linhaExportacao, vba.go).
type linhaExportacaoObra struct {
	TipoSolicitacao   string
	SolicitacaoID     string
	SolicitanteNome   string
	Classificacao     string
	LocalObraCodigo   string
	SubgrupoCodigo    string
	OrdemInvestimento string
	Valor             float64
	Lado              sql.NullString
}

// infoSolicitacaoObra é a projeção mínima de `solicitacoes`+`usuarios`
// usada para recalcular elegibilidade (Boundaries "Always" da spec 4.4).
// Classificacao é sql.NullString (não string) de propósito: uma
// solicitação com `tipo_solicitacao != 'obras'` tem `classificacao` NULL
// (migration 009, CHECK chk_solicitacao_obras_cc_xor_classificacao) e o
// Scan falharia antes do 400 (ErrTipoNaoElegivel) ser decidido — Code Map
// da spec.
type infoSolicitacaoObra struct {
	TipoSolicitacao string
	Status          string
	AdministradorID sql.NullString
	Classificacao   sql.NullString
	SolicitanteNome string
}

// motivoBloqueioSemLinhaValida é o motivo fixo gravado em Bloqueio quando
// nenhuma linha de uma solicitação sobra após descartar as com
// `ordem_investimento='CRIAR'` (I/O Matrix da spec 4.4: "Solicitação só
// com linha(s) CRIAR").
const motivoBloqueioSemLinhaValida = "nenhuma linha com ordem de investimento já existente no SAP"

// GerarLoteObra implementa ExportadorSAP.GerarLoteObra — ver ordem de
// validação no cabeçalho do arquivo.
func (e ExportadorVBA) GerarLoteObra(db *sql.DB, solicitacaoIDs []string, administradorID, chaveIdempotencia string) (Lote, []Bloqueio, error) {
	if len(solicitacaoIDs) == 0 {
		return Lote{}, nil, fmt.Errorf("exportacao: lista de solicitacao_ids vazia")
	}

	var linhas []linhaExportacaoObra
	var incluidos []string
	var bloqueados []Bloqueio

	for _, id := range solicitacaoIDs {
		info, err := buscarSolicitacaoObraParaExportacao(db, id)
		if err != nil {
			return Lote{}, nil, err
		}
		if info.AdministradorID.Valid && info.AdministradorID.String != administradorID {
			return Lote{}, nil, ErrNaoAutorizado
		}
		if info.Status != "em_atendimento" {
			return Lote{}, nil, ErrElegibilidadeInvalida
		}
		if info.TipoSolicitacao != "obras" {
			return Lote{}, nil, ErrTipoNaoElegivel
		}

		linhasSolicitacao, err := buscarLinhasObraParaExportacao(db, id)
		if err != nil {
			return Lote{}, nil, err
		}

		var linhasValidas []linhaExportacaoObra
		for _, l := range linhasSolicitacao {
			// Descarta individualmente as linhas CRIAR (Intent da spec) —
			// valor<=0 já é inatingível por CHECK (valor > 0), migration
			// 009 (Design Notes da spec), então ordem_investimento='CRIAR'
			// é, na prática, o único critério de descarte de linha.
			if l.OrdemInvestimento == "CRIAR" {
				continue
			}
			l.TipoSolicitacao = "obras"
			l.SolicitacaoID = id
			l.Classificacao = info.Classificacao.String
			l.SolicitanteNome = info.SolicitanteNome
			linhasValidas = append(linhasValidas, l)
		}

		if len(linhasValidas) == 0 {
			bloqueados = append(bloqueados, Bloqueio{SolicitacaoID: id, Motivo: motivoBloqueioSemLinhaValida})
			continue
		}

		incluidos = append(incluidos, id)
		linhas = append(linhas, linhasValidas...)
	}

	if len(incluidos) == 0 {
		// Todo o lote bloqueado — devolve sem ler o template (Code Map da
		// spec 4.4), nada é gravado em exportacoes_sap.
		return Lote{}, bloqueados, nil
	}

	templateBytes, err := os.ReadFile(e.TemplatePathObra)
	if err != nil {
		return Lote{}, nil, ErrTemplateAusente
	}

	arquivo, err := montarXLSMComLinhas(templateBytes, montarSheetDataXMLObra(linhas))
	if err != nil {
		return Lote{}, nil, fmt.Errorf("exportacao: montar .xlsm: %w", err)
	}

	loteID, err := gravarLoteObra(db, incluidos, administradorID, chaveIdempotencia)
	if err != nil {
		return Lote{}, nil, err
	}

	return Lote{LoteID: loteID, ArquivoBytes: arquivo, SolicitacoesIncluidas: incluidos}, bloqueados, nil
}

// buscarSolicitacaoObraParaExportacao lê o estado ATUAL de uma solicitação
// (nunca confia no que o cliente mandou) + o nome do solicitante (join em
// `usuarios`) — mesmo padrão de buscarSolicitacaoParaExportacao (vba.go),
// com `classificacao` adicional (sql.NullString, ver infoSolicitacaoObra).
func buscarSolicitacaoObraParaExportacao(db *sql.DB, id string) (infoSolicitacaoObra, error) {
	var info infoSolicitacaoObra
	err := db.QueryRow(`
		SELECT s.tipo_solicitacao, s.status, s.administrador_id, s.classificacao, u.nome
		FROM solicitacoes s
		JOIN usuarios u ON u.id = s.solicitante_id
		WHERE s.id = $1
	`, id).Scan(&info.TipoSolicitacao, &info.Status, &info.AdministradorID, &info.Classificacao, &info.SolicitanteNome)
	if err == sql.ErrNoRows {
		return infoSolicitacaoObra{}, ErrNaoEncontrada
	}
	if err != nil {
		return infoSolicitacaoObra{}, fmt.Errorf("exportacao: consultar solicitação de obra %s: %w", id, err)
	}
	return info, nil
}

// buscarLinhasObraParaExportacao resolve cada linha de
// `solicitacao_obras_linhas` para os códigos de negócio correspondentes
// (local de obra + subgrupo de despesa, nunca os UUIDs internos — Design
// Notes da spec 4.4) — inclui as linhas CRIAR aqui; o descarte delas é
// feito pelo chamador (GerarLoteObra), não nesta consulta.
func buscarLinhasObraParaExportacao(db *sql.DB, solicitacaoID string) ([]linhaExportacaoObra, error) {
	rows, err := db.Query(`
		SELECT sol.lado, lo.codigo, sd.codigo, sol.ordem_investimento, sol.valor
		FROM solicitacao_obras_linhas sol
		JOIN locais_obra lo ON lo.id = sol.local_obra_id
		JOIN subgrupos_despesa sd ON sd.id = sol.subgrupo_despesa_id
		WHERE sol.solicitacao_id = $1
		ORDER BY sol.created_at ASC, sol.id ASC
	`, solicitacaoID)
	if err != nil {
		return nil, fmt.Errorf("exportacao: consultar linhas de obra de %s: %w", solicitacaoID, err)
	}
	defer rows.Close()

	var linhas []linhaExportacaoObra
	for rows.Next() {
		var lado sql.NullString
		var localObraCodigo, subgrupoCodigo, ordemInvestimento string
		var valor float64
		if err := rows.Scan(&lado, &localObraCodigo, &subgrupoCodigo, &ordemInvestimento, &valor); err != nil {
			return nil, fmt.Errorf("exportacao: ler linha de obra de %s: %w", solicitacaoID, err)
		}
		linhas = append(linhas, linhaExportacaoObra{
			Lado:              lado,
			LocalObraCodigo:   localObraCodigo,
			SubgrupoCodigo:    subgrupoCodigo,
			OrdemInvestimento: ordemInvestimento,
			Valor:             valor,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("exportacao: iterar linhas de obra de %s: %w", solicitacaoID, err)
	}
	return linhas, nil
}

// gravarLoteObra grava 1 linha de `exportacoes_sap` por solicitação
// EFETIVAMENTE incluída (não bloqueada) — idempotente via ON CONFLICT DO
// NOTHING na mesma UNIQUE (administrador_id, chave_idempotencia,
// solicitacao_id) da Story 4.3 (migration 012) — mesmo padrão de
// gravarLote (vba.go), sem a parte de aviso/comentário de verba duplicada
// (Design Notes da spec 4.4: exclusivo da Story 4.3). `lote_id` é resolvido
// uma única vez dentro da transação via loteIDExistenteOuNovo — reaproveita
// o lote_id já gravado para (administrador_id, chave_idempotencia) quando
// existir (replay com um subconjunto diferente de solicitações incluíveis
// NUNCA pode produzir dois lote_id distintos sob a mesma chave) e só gera
// um novo (gen_random_uuid()) na primeira gravação — e relido no final,
// mesmo raciocínio de gravarLote.
func gravarLoteObra(db *sql.DB, solicitacaoIDs []string, administradorID, chaveIdempotencia string) (string, error) {
	tx, err := db.Begin()
	if err != nil {
		return "", fmt.Errorf("exportacao: iniciar transação do lote obra: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	loteCandidato, err := loteIDExistenteOuNovo(tx, administradorID, chaveIdempotencia)
	if err != nil {
		return "", err
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

	var loteFinal string
	if err := tx.QueryRow(`
		SELECT lote_id FROM exportacoes_sap WHERE administrador_id = $1 AND chave_idempotencia = $2 LIMIT 1
	`, administradorID, chaveIdempotencia).Scan(&loteFinal); err != nil {
		return "", fmt.Errorf("exportacao: reler lote_id: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("exportacao: confirmar transação do lote obra: %w", err)
	}

	return loteFinal, nil
}

// loteIDExistenteOuNovo busca, dentro da MESMA transação de gravarLoteObra,
// se já existe um lote_id gravado para (administrador_id,
// chave_idempotencia). Quando existe (replay desta chave, mesmo que o
// conjunto de solicitações efetivamente incluíveis tenha mudado — ex.: uma
// solicitação antes bloqueada agora tem linha válida), reaproveita esse
// mesmo lote_id para todo INSERT desta chamada; só gera um novo via
// gen_random_uuid() quando é a primeira gravação desta chave. Isso garante
// que toda linha jamais gravada sob a mesma chave compartilhe exatamente 1
// lote_id — sem isso, o SELECT final (sem ORDER BY) poderia devolver um
// lote_id diferente do efetivamente usado nas linhas desta chamada.
func loteIDExistenteOuNovo(tx *sql.Tx, administradorID, chaveIdempotencia string) (string, error) {
	var loteID string
	err := tx.QueryRow(`
		SELECT lote_id FROM exportacoes_sap WHERE administrador_id = $1 AND chave_idempotencia = $2 LIMIT 1
	`, administradorID, chaveIdempotencia).Scan(&loteID)
	if err == nil {
		return loteID, nil
	}
	if err != sql.ErrNoRows {
		return "", fmt.Errorf("exportacao: verificar lote_id existente: %w", err)
	}

	if err := tx.QueryRow(`SELECT gen_random_uuid()`).Scan(&loteID); err != nil {
		return "", fmt.Errorf("exportacao: gerar lote_id: %w", err)
	}
	return loteID, nil
}
