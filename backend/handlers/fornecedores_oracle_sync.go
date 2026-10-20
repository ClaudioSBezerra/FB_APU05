package handlers

// fornecedores_oracle_sync.go — carga de fornecedores a partir da tabela
// FORN (Oracle, servidor FCCORP), via o mesmo conector ERP Bridge usado por
// colaboradores_oracle_sync.go (handlers/erp_bridge.go fornece as
// credenciais Oracle; este arquivo é o destino do que o conector lê de lá e
// envia de volta). Query de origem acordada com o negócio:
//
//	SELECT f.cgc, f.razsoc, f.fantas, f.uf FROM forn f
//
// Diferente de colaboradores_oracle_sync.go (que só faz UPDATE num usuário
// já existente): aqui `fornecedores` é uma tabela nova — um `cgc` novo vira
// um registro novo (INSERT), um `cgc` já conhecido tem seus dados
// atualizados (UPDATE). Desde 20/10, "fornecedores" também está no
// cadastroRegistry genérico (handlers/cadastros.go) — admin pode fazer
// carga inicial via CSV e correções pontuais via "Editar"; este handler
// continua sendo o caminho de sincronização CONTÍNUA (upsert por cgc, sem
// o teto de "tabela vazia" que ImportarCadastroHandler aplica à carga CSV).
//
// POST /api/erp-bridge/fornecedores/sincronizar — autenticado via
// X-API-Key (erpBridgeAPIKeyValida), mesmo padrão de
// SincronizarColaboradoresOracleHandler — é o conector quem chama, nunca o
// navegador.

import (
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

type fornecedorOracleLinha struct {
	CGC    string `json:"cgc"`
	Razsoc string `json:"razsoc"`
	Fantas string `json:"fantas"`
	UF     string `json:"uf"`
}

type sincronizarFornecedoresRequest struct {
	Fornecedores []fornecedorOracleLinha `json:"fornecedores"`
}

// SincronizarFornecedoresOracleHandler — ver cabeçalho do arquivo.
func SincronizarFornecedoresOracleHandler(db *sql.DB) http.HandlerFunc {
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

		var req sincronizarFornecedoresRequest
		if err := json.Unmarshal(corpo, &req); err != nil {
			jsonErr(w, http.StatusBadRequest, "corpo da requisição inválido")
			return
		}
		if len(req.Fornecedores) == 0 {
			jsonErr(w, http.StatusBadRequest, "campo 'fornecedores' não pode ser vazio")
			return
		}

		tx, err := db.Begin()
		if err != nil {
			log.Printf("[FornecedoresOracleSync] Erro ao iniciar transação: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer func() { _ = tx.Rollback() }()

		criados := 0
		atualizados := 0
		ignorados := 0
		for _, linha := range req.Fornecedores {
			cgc := strings.TrimSpace(linha.CGC)
			razsoc := strings.TrimSpace(linha.Razsoc)
			if cgc == "" || razsoc == "" {
				ignorados++
				continue
			}
			fantas := parseStringOpcional(linha.Fantas)
			uf := parseStringOpcional(linha.UF)

			// `xmax = 0` é um truque específico do Postgres: verdadeiro só na
			// linha que acabou de ser INSERIDA por esta instrução, falso
			// quando o caminho ON CONFLICT DO UPDATE foi tomado — dá para
			// contar criados/atualizados num único INSERT...ON CONFLICT, sem
			// precisar de um SELECT de existência antes.
			var inserida bool
			err := tx.QueryRow(`
				INSERT INTO fornecedores (cgc, razsoc, fantas, uf)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (cgc) DO UPDATE SET
					razsoc = EXCLUDED.razsoc, fantas = EXCLUDED.fantas, uf = EXCLUDED.uf, updated_at = now()
				RETURNING (xmax = 0)
			`, cgc, razsoc, fantas, uf).Scan(&inserida)
			if err != nil {
				log.Printf("[FornecedoresOracleSync] Erro ao sincronizar cgc=%q: %v", cgc, err)
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
			log.Printf("[FornecedoresOracleSync] Erro ao commitar sincronização: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		log.Printf("[FornecedoresOracleSync] Sincronização concluída: %d criados, %d atualizados, %d ignorados (total %d)",
			criados, atualizados, ignorados, len(req.Fornecedores))

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"criados":     criados,
			"atualizados": atualizados,
			"ignorados":   ignorados,
			"total":       len(req.Fornecedores),
		})
	}
}
