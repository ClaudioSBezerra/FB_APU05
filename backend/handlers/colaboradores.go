package handlers

// colaboradores.go — Story 2.2 (FR-3, AD-8): carga de colaborador × centro de
// custo. Endpoint único de upload CSV que grava SOMENTE
// usuarios.cc_proprio_id, casando cada linha por e-mail contra um `usuarios`
// já existente (nunca cria usuário novo — a identidade nasce exclusivamente
// do primeiro login SSO, Story 1.2; ver auth_sso.go).
//
// Diferente de ImportarCadastroHandler (Story 2.1): aqui não há guard de
// "tabela já tem dados" (reimportar é sempre permitido, nunca 409) nem
// gravação em cadastro_historico (Boundaries "Never" da spec desta story) —
// é um UPDATE best-effort recorrente, e cada linha sem efeito aplicável vira
// contagem no retorno em vez de abortar a carga inteira.

import (
	"database/sql"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

// cabecalhoColaboradoresCSV é o cabeçalho exato exigido pelo CSV de carga de
// colaboradores (Boundaries "Always" da spec). `matricula`/`nome` são lidos
// do arquivo (exigidos pelo formato de origem) mas nunca gravados em lugar
// nenhum — AD-8 ("escopo estrito"): esta carga só escreve
// usuarios.cc_proprio_id.
var cabecalhoColaboradoresCSV = []string{"matricula", "nome", "email", "centro_custo_codigo"}

// CarregarColaboradoresHandler — POST /api/admin/colaboradores/carga (corpo =
// CSV cru, UTF-8 BOM opcional, separador ';'). Sempre registrado atrás de
// RequireAuth(..., "administrador") em main.go.
func CarregarColaboradoresHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// 20MB: mesma margem generosa de ImportarCadastroHandler (Story 2.1).
		r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
		corpo, err := io.ReadAll(r.Body)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, "não foi possível ler o corpo da requisição")
			return
		}

		// Cabeçalho errado ou linha com número de colunas errado rejeita o
		// arquivo inteiro com 400 (mesmo padrão de leitura da Story 2.1) —
		// nada é escrito.
		linhas, err := lerCadastroCSV(corpo, cabecalhoColaboradoresCSV)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}

		tx, err := db.Begin()
		if err != nil {
			log.Printf("[Colaboradores] Erro ao iniciar transação: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer func() { _ = tx.Rollback() }()

		// Resolve todos os centros_custo de uma vez (em vez de uma query por
		// linha do CSV, que repetiria a mesma SELECT centenas/milhares de
		// vezes numa carga grande). Chave normalizada em upper-case para
		// casar o código do CSV sem diferenciar maiúsculas/minúsculas — mesmo
		// tratamento já dado ao e-mail (LOWER(email) = LOWER($2)) abaixo.
		centrosCusto := make(map[string]string)
		rows, err := tx.Query(`SELECT codigo, id FROM centros_custo`)
		if err != nil {
			log.Printf("[Colaboradores] Erro ao carregar centros_custo: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		for rows.Next() {
			var codigo, id string
			if err := rows.Scan(&codigo, &id); err != nil {
				rows.Close()
				log.Printf("[Colaboradores] Erro ao ler centros_custo: %v", err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			centrosCusto[strings.ToUpper(strings.TrimSpace(codigo))] = id
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			log.Printf("[Colaboradores] Erro ao iterar centros_custo: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		rows.Close()

		var atualizados, placeholdersIgnorados, aguardandoPrimeiroLogin, centroCustoNaoEncontrado int

		for _, linha := range linhas {
			// Campos[0]=matricula, Campos[1]=nome: deliberadamente não lidos
			// além do parse de coluna — AD-8 proíbe esta carga de escrever
			// qualquer coluna de usuarios além de cc_proprio_id.
			email, err := campoObrigatorio(linha.Campos[2], "email")
			if err != nil {
				// Linha-placeholder (cargo sem titular, sem pessoa física —
				// FR-3): ignorada sem falhar a carga.
				placeholdersIgnorados++
				continue
			}

			centroCustoCodigo := strings.ToUpper(strings.TrimSpace(linha.Campos[3]))

			centroCustoID, ok := centrosCusto[centroCustoCodigo]
			if !ok {
				centroCustoNaoEncontrado++
				continue
			}

			result, err := tx.Exec(`
				UPDATE usuarios SET cc_proprio_id = $1 WHERE LOWER(email) = LOWER($2)
			`, centroCustoID, email)
			if err != nil {
				log.Printf("[Colaboradores] Erro ao atualizar cc_proprio_id (linha %d): %v", linha.Numero, err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			linhasAfetadas, err := result.RowsAffected()
			if err != nil {
				log.Printf("[Colaboradores] Erro ao checar RowsAffected (linha %d): %v", linha.Numero, err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			if linhasAfetadas == 0 {
				// E-mail do CSV ainda sem usuários correspondente — ainda não
				// fez o primeiro login via SSO. Não é erro: fica para a
				// próxima carga (Design Notes da spec).
				aguardandoPrimeiroLogin++
				continue
			}
			atualizados++
		}

		if err := tx.Commit(); err != nil {
			log.Printf("[Colaboradores] Erro ao commitar carga de colaboradores: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		atorID := GetUserIDFromContext(r)
		log.Printf("[Colaboradores] %s carregou colaboradores: %d atualizados, %d placeholders_ignorados, %d aguardando_primeiro_login, %d centro_custo_nao_encontrado",
			atorID, atualizados, placeholdersIgnorados, aguardandoPrimeiroLogin, centroCustoNaoEncontrado)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"atualizados":                 atualizados,
			"placeholders_ignorados":      placeholdersIgnorados,
			"aguardando_primeiro_login":   aguardandoPrimeiroLogin,
			"centro_custo_nao_encontrado": centroCustoNaoEncontrado,
		})
	}
}
