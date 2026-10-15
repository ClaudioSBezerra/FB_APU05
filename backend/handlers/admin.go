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
	"strings"

	"github.com/lib/pq"
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
	// CodEmpresa/Numcad (migration 019): identidade em
	// SFC.SFC_VW_COLABORADORES — editável aqui independente de
	// perfil/ativo, principalmente para backfill de usuários provisionados
	// via SSO (que nunca passam pelo formulário de criação local onde esses
	// campos são obrigatórios). Sempre os dois juntos — um par parcial não
	// identifica ninguém de verdade.
	CodEmpresa *int `json:"cod_empresa,omitempty"`
	Numcad     *int `json:"numcad,omitempty"`
}

const errMsgUltimoAdministrador = "operação recusada: deixaria o sistema sem nenhum administrador ativo"

// adminMutationLockKey é a chave fixa do advisory lock que serializa toda
// transação de AdminUpdateUsuarioHandler (ver uso abaixo).
const adminMutationLockKey = 72190001

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
		if req.Perfil == nil && req.Ativo == nil && req.CodEmpresa == nil && req.Numcad == nil {
			jsonErr(w, http.StatusBadRequest, "informe ao menos 'perfil', 'ativo', 'cod_empresa' ou 'numcad'")
			return
		}
		if req.Perfil != nil && *req.Perfil != "administrador" && *req.Perfil != "solicitante" {
			jsonErr(w, http.StatusBadRequest, "perfil deve ser 'administrador' ou 'solicitante'")
			return
		}
		if (req.CodEmpresa == nil) != (req.Numcad == nil) {
			jsonErr(w, http.StatusBadRequest, "'cod_empresa' e 'numcad' devem ser enviados juntos")
			return
		}
		if req.CodEmpresa != nil && (*req.CodEmpresa <= 0 || *req.Numcad <= 0) {
			jsonErr(w, http.StatusBadRequest, "'cod_empresa' e 'numcad' devem ser positivos")
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

		// Serializa toda transação desta rota num advisory lock de chave fixa,
		// adquirido ANTES de qualquer SELECT ... FOR UPDATE. Sem isso, duas
		// requisições concorrentes mirando dois administradores-alvo
		// DIFERENTES podiam travar primeiro suas próprias linhas-alvo (em
		// ordens não relacionadas entre si) e só depois tentar travar o
		// conjunto de admins ativos com ORDER BY — a ordem das duas travas por
		// linha-alvo não é coberta por esse ORDER BY, então as duas
		// transações podiam se esperar mutuamente (deadlock), e uma delas
		// recebia um 500 genérico em vez do 200/409 esperado. Com o advisory
		// lock, a segunda requisição simplesmente espera a primeira
		// commitar/abortar antes de travar qualquer linha.
		if _, err = tx.Exec(`SELECT pg_advisory_xact_lock($1)`, adminMutationLockKey); err != nil {
			log.Printf("[Admin] Erro ao adquirir advisory lock: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		var perfilAnterior string
		var ativoAnterior bool
		var codEmpresaAnterior, numcadAnterior sql.NullInt64
		// FOR UPDATE: trava a linha-alvo pela duração da transação — ver
		// Design Notes da spec.
		err = tx.QueryRow(`
			SELECT perfil, ativo, cod_empresa, numcad FROM usuarios WHERE id = $1 FOR UPDATE
		`, alvoID).Scan(&perfilAnterior, &ativoAnterior, &codEmpresaAnterior, &numcadAnterior)
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
		codEmpresaNovo := codEmpresaAnterior
		numcadNovo := numcadAnterior
		if req.CodEmpresa != nil {
			codEmpresaNovo = sql.NullInt64{Int64: int64(*req.CodEmpresa), Valid: true}
			numcadNovo = sql.NullInt64{Int64: int64(*req.Numcad), Valid: true}
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
		identidadeMudou := codEmpresaNovo != codEmpresaAnterior || numcadNovo != numcadAnterior

		if !perfilMudou && !ativoMudou && !identidadeMudou {
			// Nada mudou: não é "uma mudança aplicada" (spec), então não grava
			// auditoria — só confirma o estado atual.
			if err := tx.Commit(); err != nil {
				log.Printf("[Admin] Erro ao commitar transação (no-op): %v", err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			writeUsuarioJSON(w, alvoID, perfilNovo, ativoNovo, codEmpresaNovo, numcadNovo)
			return
		}

		if _, err = tx.Exec(`
			UPDATE usuarios SET perfil = $1, ativo = $2, cod_empresa = $3, numcad = $4, updated_at = now() WHERE id = $5
		`, perfilNovo, ativoNovo, codEmpresaNovo, numcadNovo, alvoID); err != nil {
			if isUniqueViolationOnConstraint(err, "idx_usuarios_cod_empresa_numcad") {
				jsonErr(w, http.StatusConflict, "já existe um usuário com esse código de empresa + matrícula")
				return
			}
			log.Printf("[Admin] Erro ao atualizar usuário %s: %v", alvoID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		// usuarios_auditoria só registra perfil/ativo (FR-2) — mudança só de
		// identidade (cod_empresa/numcad) não grava evento auditável aqui,
		// mesmo princípio do "no-op": a auditoria desta tabela é sobre
		// concessão/revogação de acesso, não sobre dado cadastral.
		if perfilMudou || ativoMudou {
			if _, err = tx.Exec(`
				INSERT INTO usuarios_auditoria
					(ator_id, usuario_alvo_id, acao, perfil_anterior, perfil_novo, ativo_anterior, ativo_novo)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
			`, atorID, alvoID, auditAction(perfilMudou, ativoMudou), perfilAnterior, perfilNovo, ativoAnterior, ativoNovo); err != nil {
				log.Printf("[Admin] Erro ao gravar auditoria para %s: %v", alvoID, err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
		}

		if err := tx.Commit(); err != nil {
			log.Printf("[Admin] Erro ao commitar transação: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		log.Printf("[Admin] %s atualizou usuário %s: perfil %q->%q, ativo %v->%v",
			atorID, alvoID, perfilAnterior, perfilNovo, ativoAnterior, ativoNovo)

		writeUsuarioJSON(w, alvoID, perfilNovo, ativoNovo, codEmpresaNovo, numcadNovo)
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

func writeUsuarioJSON(w http.ResponseWriter, id, perfil string, ativo bool, codEmpresa, numcad sql.NullInt64) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":          id,
		"perfil":      perfil,
		"ativo":       ativo,
		"cod_empresa": nullInt64OuNil(codEmpresa),
		"numcad":      nullInt64OuNil(numcad),
	})
}

// nullInt64OuNil converte sql.NullInt64 em interface{} para o encoder JSON
// devolver `null` (em vez de 0) quando a coluna é NULL — mesmo princípio de
// nullStringOuNil (fila.go).
func nullInt64OuNil(v sql.NullInt64) interface{} {
	if !v.Valid {
		return nil
	}
	return v.Int64
}

// CreateUsuarioLocalRequest é o corpo de POST /api/admin/usuarios.
type CreateUsuarioLocalRequest struct {
	Email      string `json:"email"`
	Nome       string `json:"nome"`
	Senha      string `json:"senha"`
	Perfil     string `json:"perfil"`
	CodEmpresa int    `json:"cod_empresa"`
	Numcad     int    `json:"numcad"`
}

// CreateUsuarioLocalHandler cria uma conta local (e-mail+senha) — a única
// forma de provisionar um usuário quando SSO Keycloak não está configurado
// (sem IAM_BASE_URL, não existe auto-provisionamento via auth_sso.go).
// Decisão de escopo: só um administrador cria contas locais, não existe
// autocadastro público (diferente do RegisterHandler do FB_APU02) — por
// isso este handler fica atrás de RequireAuth(..., "administrador"), igual
// AdminUpdateUsuarioHandler.
func CreateUsuarioLocalHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var req CreateUsuarioLocalRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, http.StatusBadRequest, "corpo da requisição inválido")
			return
		}

		req.Email = strings.TrimSpace(req.Email)
		if req.Email == "" || !strings.Contains(req.Email, "@") {
			jsonErr(w, http.StatusBadRequest, "e-mail inválido")
			return
		}
		if req.Nome == "" {
			jsonErr(w, http.StatusBadRequest, "nome é obrigatório")
			return
		}
		// Mesma política de senha do FB_APU02 (RegisterHandler): mínimo 8
		// caracteres.
		if len(req.Senha) < 8 {
			jsonErr(w, http.StatusBadRequest, "a senha deve ter no mínimo 8 caracteres")
			return
		}
		if req.Perfil == "" {
			req.Perfil = "solicitante"
		}
		if req.Perfil != "administrador" && req.Perfil != "solicitante" {
			jsonErr(w, http.StatusBadRequest, "perfil deve ser 'administrador' ou 'solicitante'")
			return
		}
		// cod_empresa/numcad (migration 019): identidade do colaborador em
		// SFC.SFC_VW_COLABORADORES (Oracle, FCCORP) — sem isso, a
		// sincronização diária de CC via ERP Bridge nunca alcança este
		// usuário (a view não tem e-mail para casar de outro jeito).
		if req.CodEmpresa <= 0 || req.Numcad <= 0 {
			jsonErr(w, http.StatusBadRequest, "campos 'cod_empresa' e 'numcad' são obrigatórios e devem ser positivos")
			return
		}

		hash, err := HashPassword(req.Senha)
		if err != nil {
			log.Printf("[Admin] Erro ao gerar hash de senha: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
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

		var novoID string
		err = tx.QueryRow(`
			INSERT INTO usuarios (email, nome, perfil, password_hash, cod_empresa, numcad)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id
		`, req.Email, req.Nome, req.Perfil, hash, req.CodEmpresa, req.Numcad).Scan(&novoID)
		if err != nil {
			if isUniqueViolationOnConstraint(err, "idx_usuarios_cod_empresa_numcad") {
				jsonErr(w, http.StatusConflict, "já existe um usuário com esse código de empresa + matrícula")
				return
			}
			// Violação do índice único idx_usuarios_email_lower (migration 001)
			// — e-mail já cadastrado (local ou auto-provisionado via SSO).
			if isUniqueViolation(err) {
				jsonErr(w, http.StatusConflict, "já existe um usuário com esse e-mail")
				return
			}
			log.Printf("[Admin] Erro ao criar usuário local %s: %v", req.Email, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		if _, err = tx.Exec(`
			INSERT INTO usuarios_auditoria
				(ator_id, usuario_alvo_id, acao, perfil_novo, ativo_novo)
			VALUES ($1, $2, 'usuario_criado', $3, true)
		`, atorID, novoID, req.Perfil); err != nil {
			log.Printf("[Admin] Erro ao gravar auditoria de criação para %s: %v", novoID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		if err := tx.Commit(); err != nil {
			log.Printf("[Admin] Erro ao commitar criação de usuário: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		log.Printf("[Admin] %s criou usuário local %s (perfil=%s)", atorID, req.Email, req.Perfil)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":          novoID,
			"email":       req.Email,
			"nome":        req.Nome,
			"perfil":      req.Perfil,
			"ativo":       true,
			"cod_empresa": req.CodEmpresa,
			"numcad":      req.Numcad,
		})
	}
}

// isUniqueViolation reconhece violação de UNIQUE do Postgres — mesmo padrão
// de errorMsgAmigavel (cadastros.go): type-assert para *pq.Error e checa o
// nome do código SQLSTATE, nunca por texto da mensagem.
func isUniqueViolation(err error) bool {
	pqErr, ok := err.(*pq.Error)
	return ok && pqErr.Code.Name() == "unique_violation"
}

// isUniqueViolationOnConstraint identifica uma violação de UNIQUE num
// índice/constraint específico (pelo nome) — usado quando uma mesma
// instrução pode violar mais de um índice único (email OU
// idx_usuarios_cod_empresa_numcad), e cada um precisa de mensagem própria.
func isUniqueViolationOnConstraint(err error, constraintName string) bool {
	pqErr, ok := err.(*pq.Error)
	return ok && pqErr.Code.Name() == "unique_violation" && pqErr.Constraint == constraintName
}

// UsuarioListItem é um item de GET /api/admin/usuarios — nunca inclui
// password_hash.
type UsuarioListItem struct {
	ID         string `json:"id"`
	Email      string `json:"email"`
	Nome       string `json:"nome"`
	Perfil     string `json:"perfil"`
	Ativo      bool   `json:"ativo"`
	CreatedAt  string `json:"created_at"`
	CodEmpresa *int   `json:"cod_empresa"`
	Numcad     *int   `json:"numcad"`
}

// ListarUsuariosHandler — GET /api/admin/usuarios, paginado (pagina/tamanho,
// AD-14) — lista para a tela de gestão de usuários do frontend. Mesma
// convenção de paginação dos demais endpoints de listagem (paginacaoDaQuery,
// cadastros.go).
func ListarUsuariosHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		pagina, tamanho := paginacaoDaQuery(r)

		rows, err := db.Query(`
			SELECT id, email, nome, perfil, ativo, created_at, cod_empresa, numcad
			FROM usuarios ORDER BY created_at, id LIMIT $1 OFFSET $2
		`, tamanho, (pagina-1)*tamanho)
		if err != nil {
			log.Printf("[Admin] Erro ao listar usuários: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer rows.Close()

		items := make([]UsuarioListItem, 0)
		for rows.Next() {
			var item UsuarioListItem
			if err := rows.Scan(&item.ID, &item.Email, &item.Nome, &item.Perfil, &item.Ativo, &item.CreatedAt, &item.CodEmpresa, &item.Numcad); err != nil {
				log.Printf("[Admin] Erro ao ler usuário: %v", err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			log.Printf("[Admin] Erro ao iterar usuários: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"items": items, "pagina": pagina, "tamanho": tamanho,
		})
	}
}

// AdminLiberarAcessoHandler — POST /api/admin/usuarios/{id}/liberar-acesso.
// Envia (ou reenvia) o link de definição de senha para o e-mail da conta —
// mesmo mecanismo de "Esqueci minha senha" (gerarEEnviarTokenDeReset,
// auth.go), mas disparado pelo administrador em vez da própria pessoa.
// Cobre o caso de uma conta cuja senha é um valor aleatório descartado que
// ninguém conhece (criada via CreateUsuarioLocalHandler ou em lote,
// usuarios_carga.go) — sem isso, a pessoa nunca teria como entrar pela
// primeira vez. Funciona também para conta só-SSO (dá a ela um fallback de
// login local, mesmo espírito dual-auth do sistema) — ResetPasswordHandler
// já aceita definir password_hash pela primeira vez, não só "resetar" um
// já existente.
func AdminLiberarAcessoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")

		alvoID := r.PathValue("id")
		if !uuidFormatRegexp.MatchString(alvoID) {
			jsonErr(w, http.StatusBadRequest, "id inválido")
			return
		}

		var email string
		err := db.QueryRow(`SELECT email FROM usuarios WHERE id = $1`, alvoID).Scan(&email)
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusNotFound, "usuário não encontrado")
			return
		}
		if err != nil {
			log.Printf("[Admin] Erro ao buscar usuário %s para liberar acesso: %v", alvoID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		if err := gerarEEnviarTokenDeReset(db, alvoID, email); err != nil {
			log.Printf("[Admin] Erro ao liberar acesso para %s: %v", email, err)
			jsonErr(w, http.StatusInternalServerError, "Erro ao enviar e-mail de acesso")
			return
		}

		atorID := GetUserIDFromContext(r)
		log.Printf("[Admin] %s liberou acesso para %s", atorID, email)

		_ = json.NewEncoder(w).Encode(map[string]string{"message": "Link de acesso enviado para " + email})
	}
}
