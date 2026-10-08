package handlers

// fila.go — Story 4.1 (Assumir solicitação da fila, Epic 4). Primeira story
// do FB_APU05 que faz UPDATE em `aprovador_snapshot`/`versao`/`status`/
// `administrador_id` de `solicitacoes` — toda essa escrita passa
// exclusivamente por internal/fila (nunca direto aqui, Boundaries "Always"
// da spec).
//
// ListarFilaHandler roda na conexão GERAL (withDB, só leitura — role
// fb_apu05_app) e decora cada item com `prazo_limite`/`atrasada`
// exclusivamente via internal/sla (nunca recalculado inline aqui).
// AssumirSolicitacaoHandler roda na conexão PRIVILEGIADA (withPrivilegedDB
// — role fb_apu05_privilegiado, única com GRANT UPDATE nessas colunas,
// AD-4) e delega inteiramente a internal/fila.Assumir.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"fb_apu05/internal/fila"
	"fb_apu05/internal/sla"
)

// ListarFilaHandler — GET /api/fila/solicitacoes?pagina=&tamanho=. Só
// `status='aberta'` (Boundaries "Always" da spec: não existe status
// 'aprovada' — toda solicitação persistida já é aprovada), ordenado por
// `created_at ASC` (mais antiga primeiro — quem está mais perto do SLA
// vencer aparece primeiro). Paginação no mesmo formato já estabelecido
// (`{items, pagina, tamanho}`, sem total/cursor).
func ListarFilaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		pagina, tamanho := paginacaoDaQuery(r)

		// Calendário carregado uma vez por requisição e reaproveitado para
		// decorar todas as linhas da página — internal/sla é a ÚNICA fonte
		// do cálculo (Boundaries "Always" da spec).
		calendario, err := sla.NovoCalendario(db)
		if err != nil {
			log.Printf("[Fila] Erro ao carregar calendário de SLA: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		rows, err := db.Query(`
			SELECT id, tipo_solicitacao, centro_custo_id, status, versao, created_at
			FROM solicitacoes
			WHERE status = 'aberta'
			ORDER BY created_at ASC, id ASC
			LIMIT $1 OFFSET $2
		`, tamanho, (pagina-1)*tamanho)
		if err != nil {
			log.Printf("[Fila] Erro ao listar fila: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer rows.Close()

		items := make([]map[string]interface{}, 0)
		for rows.Next() {
			var id, tipoSolicitacao, status string
			var centroCustoID sql.NullString
			var versao int
			var criadoEm time.Time
			if err := rows.Scan(&id, &tipoSolicitacao, &centroCustoID, &status, &versao, &criadoEm); err != nil {
				log.Printf("[Fila] Erro ao ler linha da fila: %v", err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}

			items = append(items, map[string]interface{}{
				"id":               id,
				"tipo_solicitacao": tipoSolicitacao,
				"centro_custo_id":  nullStringOuNil(centroCustoID),
				"status":           status,
				"versao":           versao,
				"created_at":       criadoEm,
				"prazo_limite":     calendario.PrazoLimite(criadoEm),
				"atrasada":         calendario.EstaAtrasado(criadoEm),
			})
		}
		if err := rows.Err(); err != nil {
			log.Printf("[Fila] Erro ao iterar fila: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"items": items, "pagina": pagina, "tamanho": tamanho,
		})
	}
}

// assumirSolicitacaoRequest é o corpo de POST /api/solicitacoes/{id}/assumir
// — só `versao` (a versão que o cliente leu na listagem); `administrador_id`
// NUNCA vem do corpo (Boundaries "Always" da spec: sempre de
// GetUserIDFromContext).
type assumirSolicitacaoRequest struct {
	Versao int `json:"versao"`
}

// AssumirSolicitacaoHandler — POST /api/solicitacoes/{id}/assumir. Registrado
// em main.go atrás de withPrivilegedDB (conexão com GRANT UPDATE nas
// colunas protegidas, AD-4) — delega inteiramente a internal/fila.Assumir,
// nunca monta o UPDATE aqui.
func AssumirSolicitacaoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		id := r.PathValue("id")
		if !uuidFormatRegexp.MatchString(id) {
			// Formato de UUID inválido nunca poderia casar com uma linha
			// real — mesma resposta de "não encontrada" (I/O Matrix da
			// spec), nunca um 400 que sugira que o formato em si é
			// relevante para o cliente.
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		corpo, err := io.ReadAll(r.Body)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, "não foi possível ler o corpo da requisição")
			return
		}

		var req assumirSolicitacaoRequest
		if err := json.Unmarshal(corpo, &req); err != nil {
			jsonErr(w, http.StatusBadRequest, "corpo da requisição inválido")
			return
		}

		// Sem esta checagem, um corpo sem `versao` (ou `versao:0`) decairia
		// para Versao=0, o UPDATE condicional falharia por não casar nenhuma
		// linha, e o cliente receberia 409 conflito_versao (mensagem de
		// conflito genuíno) em vez de um erro de validação claro sobre o
		// corpo malformado.
		if req.Versao < 1 {
			jsonErr(w, http.StatusBadRequest, "campo 'versao' é obrigatório e deve ser >= 1")
			return
		}

		administradorID := GetUserIDFromContext(r)

		resultado, err := fila.Assumir(db, id, req.Versao, administradorID)
		if err != nil {
			var conflito *fila.ErrConflitoVersao
			if errors.As(err, &conflito) {
				jsonErrConflitoVersao(w, conflito.VersaoAtual)
				return
			}
			if errors.Is(err, fila.ErrNaoEncontrada) {
				jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
				return
			}
			log.Printf("[Fila] Erro ao assumir solicitação %s: %v", id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		log.Printf("[Fila] %s assumiu solicitação %s (versao=%d)", administradorID, resultado.ID, resultado.Versao)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":               resultado.ID,
			"tipo_solicitacao": resultado.TipoSolicitacao,
			"status":           resultado.Status,
			"versao":           resultado.Versao,
			"administrador_id": resultado.AdministradorID,
		})
	}
}

// nullStringOuNil converte sql.NullString em interface{} para o encoder
// JSON devolver `null` (em vez de string vazia) quando a coluna é NULL —
// caso de `centro_custo_id` para solicitações de Obras (migration 009).
func nullStringOuNil(v sql.NullString) interface{} {
	if !v.Valid {
		return nil
	}
	return v.String
}
