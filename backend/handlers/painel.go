package handlers

// painel.go — Story 5.1 (Visualizar painel consolidado de solicitações,
// Epic 5, FR-17). Único handler de "Painéis e Indicadores" (Architecture
// Spine 4.8): lê EXCLUSIVAMENTE de `painel_snapshots` (AD-1 — "Handlers de
// Painéis leem exclusivamente de painel_snapshots"), nunca agrega
// `solicitacoes`/tabelas relacionadas diretamente.
//
// A resposta é um objeto único pré-calculado (`por_tipo`/`por_status`/
// `por_cc`/`gerado_em`) — nunca o contrato paginado `{items,pagina,tamanho}`
// usado pelas demais listagens (AD-14: os dois formatos não devem ser
// confundidos). O job que calcula/grava `painel_snapshots` a partir das
// tabelas transacionais fica fora desta story (Boundaries "Never" da spec,
// PRD §7.3/Questão Aberta 14) — enquanto não existir, `painel_snapshots`
// fica vazia e a rota devolve 200 com os 3 arrays vazios e `gerado_em: null`
// (estado válido, nunca erro).

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// painelItem é um item de agrupamento ({chave, rotulo, quantidade}),
// idêntico para as 3 dimensões (tipo/status/cc) — os valores vêm exatamente
// como gravados em `painel_snapshots`, sem nenhuma transformação aqui.
type painelItem struct {
	Chave      string `json:"chave"`
	Rotulo     string `json:"rotulo"`
	Quantidade int    `json:"quantidade"`
}

// ObterPainelHandler — GET /api/paineis/{painel}. Hoje só `painel`
// "consolidado" tem suporte (404 para qualquer outro valor, I/O Matrix da
// spec); os outros painéis citados pelo contrato de API original ficam
// para iteração futura, fora do escopo mínimo desta story.
func ObterPainelHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		painel := r.PathValue("painel")
		if painel != "consolidado" {
			jsonErr(w, http.StatusNotFound, "painel não encontrado")
			return
		}

		rows, err := db.Query(`
			SELECT dimensao, chave, rotulo, quantidade, gerado_em
			FROM painel_snapshots
			WHERE painel = 'consolidado'
			ORDER BY dimensao, chave
		`)
		if err != nil {
			log.Printf("[Painel] Erro ao consultar painel_snapshots: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer rows.Close()

		porTipo := make([]painelItem, 0)
		porStatus := make([]painelItem, 0)
		porCC := make([]painelItem, 0)
		var geradoEm *time.Time

		for rows.Next() {
			var dimensao string
			var item painelItem
			var linhaGeradoEm time.Time
			if err := rows.Scan(&dimensao, &item.Chave, &item.Rotulo, &item.Quantidade, &linhaGeradoEm); err != nil {
				log.Printf("[Painel] Erro ao ler linha de painel_snapshots: %v", err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}

			// gerado_em da resposta = o mais recente entre todas as linhas
			// lidas, não o da linha mais recentemente escaneada (Tasks &
			// Acceptance da spec).
			if geradoEm == nil || linhaGeradoEm.After(*geradoEm) {
				geradoEm = &linhaGeradoEm
			}

			switch dimensao {
			case "tipo":
				porTipo = append(porTipo, item)
			case "status":
				porStatus = append(porStatus, item)
			case "cc":
				porCC = append(porCC, item)
			}
		}
		if err := rows.Err(); err != nil {
			log.Printf("[Painel] Erro ao iterar painel_snapshots: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"por_tipo":   porTipo,
			"por_status": porStatus,
			"por_cc":     porCC,
			"gerado_em":  geradoEm,
		})
	}
}
