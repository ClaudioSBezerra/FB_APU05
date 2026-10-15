package handlers

// exportacao_preview.go — prévia somente-leitura do conteúdo de um lote
// (Despesa ou Obra) como CSV, para demonstração enquanto o `.xlsm` real
// (EXPORT_TEMPLATE_LOTE_DESPESA/EXPORT_TEMPLATE_LOTE_OBRA) não está
// configurado (ver Parte 4 do manual de teste, 15/10). Usa
// internal/exportacao.PreviewLoteDespesa/PreviewLoteObra — mesma
// elegibilidade do lote real (em_atendimento, tipo correto, linhas CRIAR
// descartadas em Obras), mas nunca grava nada: por isso roda na conexão
// GERAL (withDB), não na privilegiada que GerarLoteDespesaHandler/
// GerarLoteObraHandler usam para escrever em `exportacoes_sap`.

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"fb_apu05/internal/exportacao"
)

type previewLoteRequest struct {
	SolicitacaoIDs []string `json:"solicitacao_ids"`
}

func lerPreviewLoteRequest(w http.ResponseWriter, r *http.Request) (previewLoteRequest, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	corpo, err := io.ReadAll(r.Body)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "não foi possível ler o corpo da requisição")
		return previewLoteRequest{}, false
	}
	var req previewLoteRequest
	if err := json.Unmarshal(corpo, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return previewLoteRequest{}, false
	}
	if len(req.SolicitacaoIDs) == 0 {
		jsonErr(w, http.StatusBadRequest, "campo 'solicitacao_ids' é obrigatório e não pode ser vazio")
		return previewLoteRequest{}, false
	}
	for _, id := range req.SolicitacaoIDs {
		if !uuidFormatRegexp.MatchString(id) {
			jsonErr(w, http.StatusBadRequest, "campo 'solicitacao_ids' contém um id em formato inválido")
			return previewLoteRequest{}, false
		}
	}
	return req, true
}

// PreviewLoteDespesaCSVHandler — POST /api/solicitacoes/lote-despesa/csv.
func PreviewLoteDespesaCSVHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		req, ok := lerPreviewLoteRequest(w, r)
		if !ok {
			return
		}

		administradorID := GetUserIDFromContext(r)
		linhas, err := exportacao.PreviewLoteDespesa(db, req.SolicitacaoIDs, administradorID)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="preview_lote_despesa.csv"`)
		escritor := csv.NewWriter(w)
		escritor.Comma = ';'
		_ = escritor.Write([]string{
			"tipo_solicitacao", "solicitacao_id", "solicitante_nome", "lado",
			"divisao_codigo", "centro_custo_codigo", "conta_ou_classe_codigo", "mes", "valor",
		})
		for _, l := range linhas {
			mes := ""
			if l.Mes.Valid {
				mes = l.Mes.Time.Format("2006-01-02")
			}
			_ = escritor.Write([]string{
				l.TipoSolicitacao, l.SolicitacaoID, l.SolicitanteNome, l.Lado,
				l.DivisaoCodigo, l.CentroCustoCodigo, l.ContaOuClasseCodigo, mes,
				strconv.FormatFloat(l.Valor, 'f', 2, 64),
			})
		}
		escritor.Flush()
	}
}

// PreviewLoteObraCSVHandler — POST /api/solicitacoes/lote-obra/csv.
func PreviewLoteObraCSVHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		req, ok := lerPreviewLoteRequest(w, r)
		if !ok {
			return
		}

		administradorID := GetUserIDFromContext(r)
		linhas, err := exportacao.PreviewLoteObra(db, req.SolicitacaoIDs, administradorID)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="preview_lote_obra.csv"`)
		escritor := csv.NewWriter(w)
		escritor.Comma = ';'
		_ = escritor.Write([]string{
			"tipo_solicitacao", "solicitacao_id", "solicitante_nome", "classificacao",
			"local_obra_codigo", "subgrupo_codigo", "ordem_investimento", "valor", "lado",
		})
		for _, l := range linhas {
			lado := ""
			if l.Lado.Valid {
				lado = l.Lado.String
			}
			_ = escritor.Write([]string{
				l.TipoSolicitacao, l.SolicitacaoID, l.SolicitanteNome, l.Classificacao,
				l.LocalObraCodigo, l.SubgrupoCodigo, l.OrdemInvestimento,
				strconv.FormatFloat(l.Valor, 'f', 2, 64), lado,
			})
		}
		escritor.Flush()
	}
}
