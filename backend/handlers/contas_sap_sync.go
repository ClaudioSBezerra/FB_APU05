package handlers

// contas_sap_sync.go — carga de contas (plano de contas BIFC/SFC) a partir
// do SAP S/4HANA da Ferreira Costa (pedido do Marlos, 21/10) — mesmo
// princípio de centros_sap_sync.go (é o lado SAP/ABAP que empurra pra cá).
// Ver docs/referencia/03-INTEGRACAO-SAP-S4HANA.md para a especificação
// completa enviada à Duda.
//
// `(plano, codigo)` é a chave de negócio (UNIQUE composta, migration 003)
// — alvo do upsert. `plano` precisa ser exatamente 'BIFC' ou 'SFC'
// (validarPlano, cadastros.go); linha com plano/codigo/nome inválido é
// ignorada (best-effort, mesmo padrão de fornecedores_oracle_sync.go).
//
// POST /api/erp-bridge/contas/sincronizar

import (
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

type contaSAPLinha struct {
	Plano  string `json:"plano"`
	Codigo string `json:"codigo"`
	Nome   string `json:"nome"`
}

type sincronizarContasRequest struct {
	Contas []contaSAPLinha `json:"contas"`
}

// SincronizarContasSAPHandler — ver cabeçalho do arquivo.
func SincronizarContasSAPHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")

		if !erpBridgeAPIKeyValida(db, r) {
			jsonErr(w, http.StatusUnauthorized, "X-API-Key ausente ou inválida")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
		corpo, err := io.ReadAll(r.Body)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, "não foi possível ler o corpo da requisição")
			return
		}

		var req sincronizarContasRequest
		if err := json.Unmarshal(corpo, &req); err != nil {
			jsonErr(w, http.StatusBadRequest, "corpo da requisição inválido")
			return
		}
		if len(req.Contas) == 0 {
			jsonErr(w, http.StatusBadRequest, "campo 'contas' não pode ser vazio")
			return
		}

		tx, err := db.Begin()
		if err != nil {
			log.Printf("[ContasSAPSync] Erro ao iniciar transação: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer func() { _ = tx.Rollback() }()

		criados := 0
		atualizados := 0
		ignorados := 0
		for _, linha := range req.Contas {
			plano, err := validarPlano(linha.Plano)
			if err != nil {
				ignorados++
				continue
			}
			codigo := strings.TrimSpace(linha.Codigo)
			nome := strings.TrimSpace(linha.Nome)
			if codigo == "" || nome == "" {
				ignorados++
				continue
			}

			var inserida bool
			err = tx.QueryRow(`
				INSERT INTO contas (plano, codigo, nome)
				VALUES ($1, $2, $3)
				ON CONFLICT (plano, codigo) DO UPDATE SET
					nome = EXCLUDED.nome, updated_at = now()
				RETURNING (xmax = 0)
			`, plano, codigo, nome).Scan(&inserida)
			if err != nil {
				log.Printf("[ContasSAPSync] Erro ao sincronizar plano=%q codigo=%q: %v", plano, codigo, err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			if inserida {
				criados++
			} else {
				atualizados++
			}
		}

		if err := tx.Commit(); err != nil {
			log.Printf("[ContasSAPSync] Erro ao commitar sincronização: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		log.Printf("[ContasSAPSync] Sincronização concluída: %d criados, %d atualizados, %d ignorados (total %d)",
			criados, atualizados, ignorados, len(req.Contas))

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"criados":     criados,
			"atualizados": atualizados,
			"ignorados":   ignorados,
			"total":       len(req.Contas),
		})
	}
}
