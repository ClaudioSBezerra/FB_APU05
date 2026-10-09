package handlers

// finalizar.go — Story 4.5 (Finalizar solicitação, Epic 4, FR-14/AD-3).
// FinalizarSolicitacaoHandler é o único ponto HTTP que aciona
// internal/exportacao.ExportadorVBA.Finalizar — nunca monta o UPDATE de
// status/exportacoes_sap/obra_ordens aqui (Boundaries "Always" da spec,
// mesmo princípio AD-1 dos demais handlers de escrita deste Epic
// (handlers/pendencia.go, handlers/exportacao.go)).
//
// Registrado em main.go atrás de RequireAuth(withPrivilegedDB(...),
// "administrador") — a conexão PRIVILEGIADA é necessária porque
// internal/exportacao grava status/exportacoes_sap/obra_ordens (AD-4, mesma
// conexão de internal/fila/internal/exportacao.GerarLote*).

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"

	"fb_apu05/internal/exportacao"
)

// ordemCriadaRequest é 1 item de `ordens_criadas` no corpo de POST
// /api/solicitacoes/{id}/finalizar.
type ordemCriadaRequest struct {
	LinhaID     string `json:"linha_id"`
	NumeroOrdem string `json:"numero_ordem"`
}

// finalizarRequest é o corpo de POST /api/solicitacoes/{id}/finalizar.
// `administrador_id` NUNCA vem do corpo (Boundaries "Always" da spec:
// sempre de GetUserIDFromContext, mesmo princípio dos demais handlers de
// escrita deste Epic).
type finalizarRequest struct {
	Versao        int                  `json:"versao"`
	Resultado     string               `json:"resultado"`
	OrdensCriadas []ordemCriadaRequest `json:"ordens_criadas"`
}

// lerFinalizarRequest lê e valida estruturalmente o corpo (Tasks da
// spec): `versao>=1` (mesmo achado de revisão já replicado em
// lerPendenciaOuComentarioRequest); `resultado` ∈ {"sucesso","erro"}; cada
// `ordens_criadas[i].linha_id` em formato de UUID válido e `numero_ordem`
// não-vazio após TrimSpace; nenhum `linha_id` duplicado no corpo; 400 se
// `resultado=="erro"` e `ordens_criadas` não vazio (Boundaries "Never" da
// spec: nunca criar ordem real quando resultado='erro'). Nenhuma validação
// de elegibilidade/negócio aqui (ex.: se `ordens_criadas` corresponde
// mesmo às linhas 'CRIAR' da solicitação) — delegada inteiramente a
// internal/exportacao.Finalizar.
func lerFinalizarRequest(w http.ResponseWriter, r *http.Request) (versao int, resultado string, ordensCriadas []exportacao.OrdemCriada, ok bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	corpo, err := io.ReadAll(r.Body)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "não foi possível ler o corpo da requisição")
		return 0, "", nil, false
	}

	var req finalizarRequest
	if err := json.Unmarshal(corpo, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return 0, "", nil, false
	}

	if req.Versao < 1 {
		jsonErr(w, http.StatusBadRequest, "campo 'versao' é obrigatório e deve ser >= 1")
		return 0, "", nil, false
	}

	if req.Resultado != "sucesso" && req.Resultado != "erro" {
		jsonErr(w, http.StatusBadRequest, "campo 'resultado' deve ser 'sucesso' ou 'erro'")
		return 0, "", nil, false
	}

	if req.Resultado == "erro" && len(req.OrdensCriadas) > 0 {
		jsonErr(w, http.StatusBadRequest, "campo 'ordens_criadas' não pode ser informado quando 'resultado' é 'erro'")
		return 0, "", nil, false
	}

	vistos := make(map[string]bool, len(req.OrdensCriadas))
	ordens := make([]exportacao.OrdemCriada, 0, len(req.OrdensCriadas))
	for _, oc := range req.OrdensCriadas {
		if !uuidFormatRegexp.MatchString(oc.LinhaID) {
			jsonErr(w, http.StatusBadRequest, "campo 'ordens_criadas' contém um 'linha_id' em formato inválido")
			return 0, "", nil, false
		}
		if vistos[oc.LinhaID] {
			jsonErr(w, http.StatusBadRequest, "campo 'ordens_criadas' contém 'linha_id' duplicado")
			return 0, "", nil, false
		}
		vistos[oc.LinhaID] = true

		numeroOrdem := strings.TrimSpace(oc.NumeroOrdem)
		if numeroOrdem == "" {
			jsonErr(w, http.StatusBadRequest, "campo 'ordens_criadas' contém 'numero_ordem' vazio")
			return 0, "", nil, false
		}
		if len(numeroOrdem) > 50 {
			// `obra_ordens.numero_ordem` é VARCHAR(50) (migration 009) —
			// sem este limite, um valor maior chegaria ao INSERT e viraria
			// um erro opaco de Postgres (500) em vez de um 400 claro aqui.
			jsonErr(w, http.StatusBadRequest, "campo 'ordens_criadas' contém 'numero_ordem' com mais de 50 caracteres")
			return 0, "", nil, false
		}

		ordens = append(ordens, exportacao.OrdemCriada{LinhaID: oc.LinhaID, NumeroOrdem: numeroOrdem})
	}

	return req.Versao, req.Resultado, ordens, true
}

// tratarErroFinalizar converte os sentinelas de
// internal/exportacao.Finalizar no envelope HTTP correto (I/O Matrix da
// spec). Devolve `true` quando já escreveu uma resposta de erro (o
// chamador deve retornar imediatamente); `false` quando `err` não é
// nenhum sentinela conhecido (o chamador trata como 500) — mesmo padrão
// de tratarErroEscritaFila/tratarErroExportacao.
func tratarErroFinalizar(w http.ResponseWriter, err error) bool {
	var conflito *exportacao.ErrConflitoVersao
	if errors.As(err, &conflito) {
		jsonErrConflitoVersao(w, conflito.VersaoAtual)
		return true
	}
	switch {
	case errors.Is(err, exportacao.ErrNaoEncontrada):
		jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
	case errors.Is(err, exportacao.ErrNaoAutorizado):
		jsonErr(w, http.StatusForbidden, "você não tem permissão para esta ação nesta solicitação")
	case errors.Is(err, exportacao.ErrNaoExportada):
		jsonErr(w, http.StatusConflict, "a solicitação ainda não foi exportada")
	case errors.Is(err, exportacao.ErrOrdensCriadasInvalidas):
		jsonErr(w, http.StatusBadRequest, "campo 'ordens_criadas' não corresponde às linhas 'CRIAR' desta solicitação")
	case errors.Is(err, exportacao.ErrOrdemJaExiste):
		jsonErr(w, http.StatusConflict, "o número de ordem informado já existe")
	default:
		return false
	}
	return true
}

// FinalizarSolicitacaoHandler — POST /api/solicitacoes/{id}/finalizar.
func FinalizarSolicitacaoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		id := r.PathValue("id")
		if !uuidFormatRegexp.MatchString(id) {
			// Formato de UUID inválido nunca poderia casar com uma linha
			// real — mesma resposta de "não encontrada" do padrão já
			// estabelecido em fila.go/handlers/pendencia.go.
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}

		versao, resultado, ordensCriadas, ok := lerFinalizarRequest(w, r)
		if !ok {
			return
		}

		administradorID := GetUserIDFromContext(r)

		exportador := exportacao.ExportadorVBA{}
		resultadoFinal, err := exportador.Finalizar(db, id, versao, administradorID, resultado, ordensCriadas)
		if err != nil {
			if tratarErroFinalizar(w, err) {
				return
			}
			log.Printf("[Exportacao] Erro ao finalizar solicitação %s (administrador=%s): %v", id, administradorID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":               resultadoFinal.ID,
			"tipo_solicitacao": resultadoFinal.TipoSolicitacao,
			"status":           resultadoFinal.Status,
			"versao":           resultadoFinal.Versao,
			"administrador_id": resultadoFinal.AdministradorID,
		})
	}
}
