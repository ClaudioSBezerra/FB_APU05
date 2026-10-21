package handlers

// centros_custo_sap_sync.go — carga de centros_custo a partir do SAP
// S/4HANA da Ferreira Costa (pedido do Marlos, 21/10) — mesmo princípio de
// centros_sap_sync.go (é o lado SAP/ABAP que empurra pra cá, não um
// conector nosso puxando de lá). Ver
// docs/referencia/03-INTEGRACAO-SAP-S4HANA.md para a especificação
// completa enviada à Duda.
//
// `divisao_codigo` resolve para `divisao_id` (FK NOT NULL em centros_custo,
// migration 003) na MESMA transação, igual à carga CSV manual
// (decodeCSVCentrosCusto/resolveDivisaoID, cadastros_csv.go) — se a
// divisão ainda não existir, a linha é ignorada (best-effort, mesmo padrão
// de fornecedores_oracle_sync.go: uma linha ruim não aborta a
// sincronização inteira). `filial` é opcional, mesmo padrão da carga CSV.
//
// POST /api/erp-bridge/centros-custo/sincronizar

import (
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

type centroCustoSAPLinha struct {
	Codigo        string `json:"codigo"`
	Nome          string `json:"nome"`
	DivisaoCodigo string `json:"divisao_codigo"`
	Filial        string `json:"filial"`
}

type sincronizarCentrosCustoRequest struct {
	CentrosCusto []centroCustoSAPLinha `json:"centros_custo"`
}

// SincronizarCentrosCustoSAPHandler — ver cabeçalho do arquivo.
func SincronizarCentrosCustoSAPHandler(db *sql.DB) http.HandlerFunc {
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

		var req sincronizarCentrosCustoRequest
		if err := json.Unmarshal(corpo, &req); err != nil {
			jsonErr(w, http.StatusBadRequest, "corpo da requisição inválido")
			return
		}
		if len(req.CentrosCusto) == 0 {
			jsonErr(w, http.StatusBadRequest, "campo 'centros_custo' não pode ser vazio")
			return
		}

		tx, err := db.Begin()
		if err != nil {
			log.Printf("[CentrosCustoSAPSync] Erro ao iniciar transação: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer func() { _ = tx.Rollback() }()

		criados := 0
		atualizados := 0
		ignorados := 0
		for _, linha := range req.CentrosCusto {
			codigo := strings.TrimSpace(linha.Codigo)
			nome := strings.TrimSpace(linha.Nome)
			divisaoCodigo := strings.TrimSpace(linha.DivisaoCodigo)
			if codigo == "" || nome == "" || divisaoCodigo == "" {
				ignorados++
				continue
			}

			divisaoID, err := resolveDivisaoID(tx, divisaoCodigo)
			if err != nil {
				ignorados++
				continue
			}

			filial := parseStringOpcional(linha.Filial)
			var inserida bool
			err = tx.QueryRow(`
				INSERT INTO centros_custo (codigo, nome, divisao_id, filial)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (codigo) DO UPDATE SET
					nome = EXCLUDED.nome, divisao_id = EXCLUDED.divisao_id, filial = EXCLUDED.filial, updated_at = now()
				RETURNING (xmax = 0)
			`, codigo, nome, divisaoID, filial).Scan(&inserida)
			if err != nil {
				log.Printf("[CentrosCustoSAPSync] Erro ao sincronizar codigo=%q: %v", codigo, err)
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
			log.Printf("[CentrosCustoSAPSync] Erro ao commitar sincronização: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		log.Printf("[CentrosCustoSAPSync] Sincronização concluída: %d criados, %d atualizados, %d ignorados (total %d)",
			criados, atualizados, ignorados, len(req.CentrosCusto))

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"criados":     criados,
			"atualizados": atualizados,
			"ignorados":   ignorados,
			"total":       len(req.CentrosCusto),
		})
	}
}
