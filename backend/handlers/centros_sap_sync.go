package handlers

// centros_sap_sync.go — carga de centros (SAP: Plant/Werk, filial/local
// físico) a partir do SAP S/4HANA da Ferreira Costa, pedido do Marlos
// (21/10). Diferente do ERP Bridge Oracle (fornecedores_oracle_sync.go,
// colaboradores_oracle_sync.go — conector Python nosso, que PUXA do
// Oracle/FCCORP): aqui é o lado SAP (ABAP, Maria Eduarda/Duda) que EMPURRA
// os dados pra cá — mesma autenticação X-API-Key (erpBridgeAPIKeyValida),
// mesmo endpoint de credenciais, só muda quem chama. Ver
// docs/referencia/03-INTEGRACAO-SAP-S4HANA.md para a especificação
// completa enviada à Duda.
//
// `codigo` é a chave de negócio — UNIQUE, alvo do upsert. Linha sem
// codigo/nome é ignorada (best-effort, mesmo padrão de
// fornecedores_oracle_sync.go — não aborta a sincronização inteira por
// uma linha ruim).
//
// POST /api/erp-bridge/centros/sincronizar

import (
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

type centroSAPLinha struct {
	Codigo string `json:"codigo"`
	Nome   string `json:"nome"`
}

type sincronizarCentrosRequest struct {
	Centros []centroSAPLinha `json:"centros"`
}

// SincronizarCentrosSAPHandler — ver cabeçalho do arquivo.
func SincronizarCentrosSAPHandler(db *sql.DB) http.HandlerFunc {
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

		var req sincronizarCentrosRequest
		if err := json.Unmarshal(corpo, &req); err != nil {
			jsonErr(w, http.StatusBadRequest, "corpo da requisição inválido")
			return
		}
		if len(req.Centros) == 0 {
			jsonErr(w, http.StatusBadRequest, "campo 'centros' não pode ser vazio")
			return
		}

		tx, err := db.Begin()
		if err != nil {
			log.Printf("[CentrosSAPSync] Erro ao iniciar transação: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer func() { _ = tx.Rollback() }()

		criados := 0
		atualizados := 0
		ignorados := 0
		for _, linha := range req.Centros {
			codigo := strings.TrimSpace(linha.Codigo)
			nome := strings.TrimSpace(linha.Nome)
			if codigo == "" || nome == "" {
				ignorados++
				continue
			}

			var inserida bool
			err := tx.QueryRow(`
				INSERT INTO centros (codigo, nome)
				VALUES ($1, $2)
				ON CONFLICT (codigo) DO UPDATE SET
					nome = EXCLUDED.nome, updated_at = now()
				RETURNING (xmax = 0)
			`, codigo, nome).Scan(&inserida)
			if err != nil {
				log.Printf("[CentrosSAPSync] Erro ao sincronizar codigo=%q: %v", codigo, err)
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
			log.Printf("[CentrosSAPSync] Erro ao commitar sincronização: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		log.Printf("[CentrosSAPSync] Sincronização concluída: %d criados, %d atualizados, %d ignorados (total %d)",
			criados, atualizados, ignorados, len(req.Centros))

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"criados":     criados,
			"atualizados": atualizados,
			"ignorados":   ignorados,
			"total":       len(req.Centros),
		})
	}
}
