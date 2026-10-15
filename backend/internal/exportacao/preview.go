package exportacao

// preview.go — pré-visualização somente-leitura do conteúdo que um lote
// (Despesa ou Obra) conteria, para demonstração/validação enquanto o
// arquivo `.xlsm` real (EXPORT_TEMPLATE_LOTE_DESPESA/
// EXPORT_TEMPLATE_LOTE_OBRA) não está configurado — ver Parte 4 do manual
// de teste, 15/10. Reaproveita a MESMA elegibilidade de
// GerarLoteDespesa/GerarLoteObra (em_atendimento, tipo correto, linhas
// `ordem_investimento='CRIAR'` descartadas em Obras) — a prévia é fiel ao
// que o lote real incluiria — mas nunca grava nada: sem
// `exportacoes_sap`, sem checagem de idempotência/verba duplicada, sem
// precisar do template.
//
// Diferente de GerarLoteDespesa/GerarLoteObra, um id problemático (não
// encontrado, de outro administrador, fora de elegibilidade) é
// SILENCIOSAMENTE ignorado em vez de recusar a prévia inteira — é uma
// prévia de demonstração, não uma operação que precisa de uma resposta de
// erro estruturada por id.

import "database/sql"

// LinhaPreview é uma linha de prévia do lote Despesa, já com os códigos de
// negócio (nunca UUIDs internos) — mesmo layout de linhaExportacao
// (vba.go), exportado para o pacote handlers formatar como CSV.
type LinhaPreview struct {
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

// LinhaPreviewObra é o equivalente de LinhaPreview para o lote Obra.
type LinhaPreviewObra struct {
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

// PreviewLoteDespesa devolve as linhas que GerarLoteDespesa incluiria no
// lote para os `solicitacaoIDs` elegíveis de `administradorID`.
func PreviewLoteDespesa(db *sql.DB, solicitacaoIDs []string, administradorID string) ([]LinhaPreview, error) {
	var linhas []LinhaPreview
	for _, id := range solicitacaoIDs {
		info, err := buscarSolicitacaoParaExportacao(db, id)
		if err != nil {
			if err == ErrNaoEncontrada {
				continue
			}
			return nil, err
		}
		if info.AdministradorID.Valid && info.AdministradorID.String != administradorID {
			continue
		}
		if info.Status != "em_atendimento" || info.TipoSolicitacao == "obras" {
			continue
		}

		linhasSolicitacao, err := buscarLancamentosParaExportacao(db, id)
		if err != nil {
			return nil, err
		}
		for _, l := range linhasSolicitacao {
			linhas = append(linhas, LinhaPreview{
				TipoSolicitacao:     info.TipoSolicitacao,
				SolicitacaoID:       id,
				SolicitanteNome:     info.SolicitanteNome,
				Lado:                l.Lado,
				DivisaoCodigo:       l.DivisaoCodigo,
				CentroCustoCodigo:   l.CentroCustoCodigo,
				ContaOuClasseCodigo: l.ContaOuClasseCodigo,
				Mes:                 l.Mes,
				Valor:               l.Valor,
			})
		}
	}
	return linhas, nil
}

// PreviewLoteObra devolve as linhas que GerarLoteObra incluiria no lote
// (já descartando linhas com `ordem_investimento='CRIAR'`, mesma regra de
// GerarLoteObra) para os `solicitacaoIDs` elegíveis de `administradorID`.
func PreviewLoteObra(db *sql.DB, solicitacaoIDs []string, administradorID string) ([]LinhaPreviewObra, error) {
	var linhas []LinhaPreviewObra
	for _, id := range solicitacaoIDs {
		info, err := buscarSolicitacaoObraParaExportacao(db, id)
		if err != nil {
			if err == ErrNaoEncontrada {
				continue
			}
			return nil, err
		}
		if info.AdministradorID.Valid && info.AdministradorID.String != administradorID {
			continue
		}
		if info.Status != "em_atendimento" || info.TipoSolicitacao != "obras" {
			continue
		}

		linhasSolicitacao, err := buscarLinhasObraParaExportacao(db, id)
		if err != nil {
			return nil, err
		}
		for _, l := range linhasSolicitacao {
			if l.OrdemInvestimento == "CRIAR" {
				continue
			}
			linhas = append(linhas, LinhaPreviewObra{
				TipoSolicitacao:   "obras",
				SolicitacaoID:     id,
				SolicitanteNome:   info.SolicitanteNome,
				Classificacao:     info.Classificacao.String,
				LocalObraCodigo:   l.LocalObraCodigo,
				SubgrupoCodigo:    l.SubgrupoCodigo,
				OrdemInvestimento: l.OrdemInvestimento,
				Valor:             l.Valor,
				Lado:              l.Lado,
			})
		}
	}
	return linhas, nil
}
