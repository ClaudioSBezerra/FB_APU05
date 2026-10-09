package handlers

// exportacao.go — Story 4.3 (Gerar lote Despesa, Epic 4, FR-15/AD-3).
// GerarLoteDespesaHandler é o único ponto HTTP que aciona
// internal/exportacao.ExportadorVBA.GerarLoteDespesa — nunca recalcula
// elegibilidade, monta o `.xlsm` ou grava `exportacoes_sap` aqui
// (Boundaries "Always" da spec, mesmo princípio AD-1 de
// handlers/fila.go/handlers/pendencia.go sobre internal/fila).
//
// Registrado em main.go atrás de
// RequireAuth(withPrivilegedDB(...), "administrador") — a conexão
// PRIVILEGIADA é necessária porque internal/exportacao grava
// `exportacoes_sap`/`solicitacao_comentarios` (AD-4, mesma conexão de
// internal/fila).
import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"fb_apu05/internal/exportacao"
)

// exportTemplateLoteDespesaDefault é o caminho padrão do template `.xlsm`
// (AD-12) quando EXPORT_TEMPLATE_LOTE_DESPESA não está definida — Code Map
// da spec. O arquivo real do robô é um ativo de negócio externo e
// DELIBERADAMENTE não existe neste repositório (Intent da spec); sem ele
// neste caminho (nem em outro apontado pela env var), GerarLoteDespesa
// devolve exportacao.ErrTemplateAusente -> 503.
const exportTemplateLoteDespesaDefault = "internal/exportacao/templates/lote_despesa.xlsm"

// gerarLoteDespesaRequest é o corpo de POST /api/solicitacoes/lote-despesa.
// `administrador_id` NUNCA vem do corpo (Boundaries "Always" da spec:
// sempre de GetUserIDFromContext, mesmo princípio dos demais handlers de
// escrita deste Epic).
type gerarLoteDespesaRequest struct {
	SolicitacaoIDs             []string `json:"solicitacao_ids"`
	ChaveIdempotencia          string   `json:"chave_idempotencia"`
	IgnorarAvisoVerbaDuplicada bool     `json:"ignorar_aviso_verba_duplicada"`
}

// exportTemplateLoteDespesaPath resolve o caminho do template a partir de
// EXPORT_TEMPLATE_LOTE_DESPESA, com o default acima quando a env var está
// vazia/ausente.
func exportTemplateLoteDespesaPath() string {
	if caminho := os.Getenv("EXPORT_TEMPLATE_LOTE_DESPESA"); caminho != "" {
		return caminho
	}
	return exportTemplateLoteDespesaDefault
}

// lerGerarLoteDespesaRequest lê e valida estruturalmente o corpo (400 se
// `solicitacao_ids` vazio, `chave_idempotencia` vazia, ou algum id não é
// UUID) — nenhuma validação de elegibilidade/negócio aqui, delegada
// inteiramente a internal/exportacao.
func lerGerarLoteDespesaRequest(w http.ResponseWriter, r *http.Request) (gerarLoteDespesaRequest, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	corpo, err := io.ReadAll(r.Body)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "não foi possível ler o corpo da requisição")
		return gerarLoteDespesaRequest{}, false
	}

	var req gerarLoteDespesaRequest
	if err := json.Unmarshal(corpo, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return gerarLoteDespesaRequest{}, false
	}

	if len(req.SolicitacaoIDs) == 0 {
		jsonErr(w, http.StatusBadRequest, "campo 'solicitacao_ids' é obrigatório e não pode ser vazio")
		return gerarLoteDespesaRequest{}, false
	}
	for _, id := range req.SolicitacaoIDs {
		if !uuidFormatRegexp.MatchString(id) {
			jsonErr(w, http.StatusBadRequest, "campo 'solicitacao_ids' contém um id em formato inválido")
			return gerarLoteDespesaRequest{}, false
		}
	}
	if strings.TrimSpace(req.ChaveIdempotencia) == "" {
		jsonErr(w, http.StatusBadRequest, "campo 'chave_idempotencia' é obrigatório e não pode ser vazio")
		return gerarLoteDespesaRequest{}, false
	}

	return req, true
}

// tratarErroExportacao converte os sentinelas de internal/exportacao no
// envelope HTTP correto (I/O Matrix da spec). Devolve `true` quando já
// escreveu uma resposta de erro (o chamador deve retornar imediatamente);
// `false` quando `err` não é nenhum sentinela conhecido (o chamador trata
// como 500) — mesmo padrão de tratarErroEscritaFila (handlers/pendencia.go).
func tratarErroExportacao(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, exportacao.ErrNaoEncontrada):
		jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
	case errors.Is(err, exportacao.ErrNaoAutorizado):
		jsonErr(w, http.StatusForbidden, "você não tem permissão para esta ação nesta solicitação")
	case errors.Is(err, exportacao.ErrElegibilidadeInvalida):
		jsonErr(w, http.StatusConflict, "a solicitação não está em atendimento")
	case errors.Is(err, exportacao.ErrChaveIdempotenciaConflitante):
		jsonErr(w, http.StatusConflict, "esta chave de idempotência já foi usada com um conjunto diferente de solicitações")
	case errors.Is(err, exportacao.ErrTipoNaoElegivel):
		jsonErr(w, http.StatusBadRequest, "solicitações do tipo 'obras' não fazem parte do lote de despesa")
	case errors.Is(err, exportacao.ErrExercicioMisto):
		jsonErr(w, http.StatusBadRequest, "as solicitações selecionadas têm exercícios orçamentários diferentes")
	case errors.Is(err, exportacao.ErrTemplateAusente):
		jsonErr(w, http.StatusServiceUnavailable, "modelo de exportação indisponível — contate o administrador do sistema")
	default:
		return false
	}
	return true
}

// GerarLoteDespesaHandler — POST /api/solicitacoes/lote-despesa.
func GerarLoteDespesaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		req, ok := lerGerarLoteDespesaRequest(w, r)
		if !ok {
			return
		}

		administradorID := GetUserIDFromContext(r)

		exportador := exportacao.ExportadorVBA{TemplatePath: exportTemplateLoteDespesaPath()}
		lote, aviso, err := exportador.GerarLoteDespesa(db, req.SolicitacaoIDs, administradorID, req.ChaveIdempotencia, req.IgnorarAvisoVerbaDuplicada)
		if err != nil {
			if tratarErroExportacao(w, err) {
				return
			}
			log.Printf("[Exportacao] Erro ao gerar lote de despesa (administrador=%s): %v", administradorID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		w.Header().Set("Content-Type", "application/json")

		if aviso != nil {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"aviso": map[string]interface{}{
					"codigo":                aviso.Codigo,
					"solicitacoes_em_risco": aviso.SolicitacoesEmRisco,
				},
			})
			return
		}

		log.Printf("[Exportacao] %s gerou lote de despesa %s (%d solicitações)", administradorID, lote.LoteID, len(lote.SolicitacoesIncluidas))

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"lote_id":                lote.LoteID,
			"arquivo_nome":           "lote_despesa_" + lote.LoteID + ".xlsm",
			"arquivo_base64":         base64.StdEncoding.EncodeToString(lote.ArquivoBytes),
			"solicitacoes_incluidas": lote.SolicitacoesIncluidas,
		})
	}
}
