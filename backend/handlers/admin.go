package handlers

// AdminUpdateUsuarioHandler — PATCH /api/admin/usuarios/{id} (Story 1.3,
// FR-2). Registrado em main.go sempre atrás de
// RequireAuth(..., "administrador") — nunca exposto sem essa proteção.
//
// Modelo mais próximo: PromoteUserHandler do FB_APU02
// (backend/handlers/admin.go:539-595) — mas lá não existe proteção de "último
// admin"; essa proteção é nova nesta story, não uma replicação literal (ver
// Intent da spec).
//
// Concede/revoga perfil `administrador` e ativa/desativa um usuário, com
// trilha de auditoria (`usuarios_auditoria`). Toda mudança que reduziria para
// zero o número de usuários com perfil='administrador' AND ativo=true é
// recusada com 409 — avaliada dentro da mesma transação que aplica o UPDATE,
// travando com `SELECT ... FOR UPDATE` tanto a linha-alvo quanto (quando a
// mudança sairia da condição "administrador ativo") todo o conjunto atual de
// administradores ativos, antes de recontar (ver Design Notes da spec): evita
// a corrida entre duas requisições concorrentes de desativação — inclusive
// mirando dois administradores diferentes — que lendo "ainda há 2 admins" ao
// mesmo tempo, poderiam ambas desativar e deixar zero.

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
)

// uuidFormatRegexp valida o formato (não a existência) de um UUID vindo do
// path — recusa com 400 antes de chegar ao banco; sem isso, um id malformado
// vira erro de sintaxe do Postgres na query `WHERE id = $1` (coluna UUID) e
// isso aparece como um 500 genérico.
var uuidFormatRegexp = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// AdminUpdateUsuarioRequest é o corpo de PATCH /api/admin/usuarios/{id}.
// Campos ponteiro: distinguem "não enviado" de "enviado como zero-value"
// (ex. `{"ativo": false}` precisa ser distinguível de "ativo não informado").
type AdminUpdateUsuarioRequest struct {
	Perfil *string `json:"perfil,omitempty"`
	Ativo  *bool   `json:"ativo,omitempty"`
}

const errMsgUltimoAdministrador = "operação recusada: deixaria o sistema sem nenhum administrador ativo"

func AdminUpdateUsuarioHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			w.Header().Set("Allow", http.MethodPatch)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		alvoID := r.PathValue("id")
		if alvoID == "" {
			jsonErr(w, http.StatusBadRequest, "id do usuário é obrigatório")
			return
		}
		if !uuidFormatRegexp.MatchString(alvoID) {
			jsonErr(w, http.StatusBadRequest, "id inválido")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var req AdminUpdateUsuarioRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, http.StatusBadRequest, "corpo da requisição inválido")
			return
		}
		if req.Perfil == nil && req.Ativo == nil {
			jsonErr(w, http.StatusBadRequest, "informe ao menos 'perfil' ou 'ativo'")
			return
		}
		if req.Perfil != nil && *req.Perfil != "administrador" && *req.Perfil != "solicitante" {
			jsonErr(w, http.StatusBadRequest, "perfil deve ser 'administrador' ou 'solicitante'")
			return
		}

		atorID := GetUserIDFromContext(r)

		tx, err := db.Begin()
		if err != nil {
			log.Printf("[Admin] Erro ao iniciar transação: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer func() { _ = tx.Rollback() }()

		var perfilAnterior string
		var ativoAnterior bool
		// FOR UPDATE: trava a linha-alvo pela duração da transação — ver
		// Design Notes da spec.
		err = tx.QueryRow(`
			SELECT perfil, ativo FROM usuarios WHERE id = $1 FOR UPDATE
		`, alvoID).Scan(&perfilAnterior, &ativoAnterior)
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusNotFound, "usuário não encontrado")
			return
		} else if err != nil {
			log.Printf("[Admin] Erro ao buscar usuário alvo %s: %v", alvoID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		perfilNovo := perfilAnterior
		if req.Perfil != nil {
			perfilNovo = *req.Perfil
		}
		ativoNovo := ativoAnterior
		if req.Ativo != nil {
			ativoNovo = *req.Ativo
		}

		// Proteção do último admin (FR-2): se esta mudança tira o usuário-alvo
		// da condição "administrador ativo" (rebaixamento, desativação, ou os
		// dois) e ele é hoje o único nessa condição, recusa sem aplicar nada.
		//
		// A recontagem trava TODAS as linhas atualmente
		// administrador+ativo=true com FOR UPDATE (não um COUNT(*) simples,
		// que o Postgres nem aceita combinado com FOR UPDATE, e que sozinho não
		// fecharia a corrida entre duas requisições concorrentes mirando dois
		// administradores DIFERENTES — travar só a linha-alvo de cada uma não
		// impede a outra de ler uma contagem desatualizada). Travando o
		// conjunto inteiro, a segunda requisição concorrente bloqueia até a
		// primeira commitar/abortar, e então recontaria corretamente — ver
		// Design Notes da spec.
		eraAdminAtivo := perfilAnterior == "administrador" && ativoAnterior
		seraAdminAtivo := perfilNovo == "administrador" && ativoNovo
		if eraAdminAtivo && !seraAdminAtivo {
			rows, err := tx.Query(`
				SELECT id FROM usuarios WHERE perfil = 'administrador' AND ativo = true ORDER BY id FOR UPDATE
			`)
			if err != nil {
				log.Printf("[Admin] Erro ao travar administradores ativos: %v", err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			totalAdminsAtivos := 0
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					log.Printf("[Admin] Erro ao ler administradores ativos: %v", err)
					jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
					return
				}
				totalAdminsAtivos++
			}
			rowsErr := rows.Err()
			rows.Close()
			if rowsErr != nil {
				log.Printf("[Admin] Erro ao iterar administradores ativos: %v", rowsErr)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			if totalAdminsAtivos <= 1 {
				jsonErr(w, http.StatusConflict, errMsgUltimoAdministrador)
				return
			}
		}

		perfilMudou := perfilNovo != perfilAnterior
		ativoMudou := ativoNovo != ativoAnterior

		if !perfilMudou && !ativoMudou {
			// Nada mudou: não é "uma mudança aplicada" (spec), então não grava
			// auditoria — só confirma o estado atual.
			if err := tx.Commit(); err != nil {
				log.Printf("[Admin] Erro ao commitar transação (no-op): %v", err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			writeUsuarioJSON(w, alvoID, perfilNovo, ativoNovo)
			return
		}

		if _, err = tx.Exec(`
			UPDATE usuarios SET perfil = $1, ativo = $2, updated_at = now() WHERE id = $3
		`, perfilNovo, ativoNovo, alvoID); err != nil {
			log.Printf("[Admin] Erro ao atualizar usuário %s: %v", alvoID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		if _, err = tx.Exec(`
			INSERT INTO usuarios_auditoria
				(ator_id, usuario_alvo_id, acao, perfil_anterior, perfil_novo, ativo_anterior, ativo_novo)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, atorID, alvoID, auditAction(perfilMudou, ativoMudou), perfilAnterior, perfilNovo, ativoAnterior, ativoNovo); err != nil {
			log.Printf("[Admin] Erro ao gravar auditoria para %s: %v", alvoID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		if err := tx.Commit(); err != nil {
			log.Printf("[Admin] Erro ao commitar transação: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		log.Printf("[Admin] %s atualizou usuário %s: perfil %q->%q, ativo %v->%v",
			atorID, alvoID, perfilAnterior, perfilNovo, ativoAnterior, ativoNovo)

		writeUsuarioJSON(w, alvoID, perfilNovo, ativoNovo)
	}
}

func auditAction(perfilMudou, ativoMudou bool) string {
	switch {
	case perfilMudou && ativoMudou:
		return "perfil_e_ativo_alterados"
	case perfilMudou:
		return "perfil_alterado"
	default:
		return "ativo_alterado"
	}
}

func writeUsuarioJSON(w http.ResponseWriter, id, perfil string, ativo bool) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":     id,
		"perfil": perfil,
		"ativo":  ativo,
	})
}
