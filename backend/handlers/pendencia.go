package handlers

// pendencia.go — Story 4.2 (Pendência e reabertura, Epic 4). Continua o
// Writer path único de internal/fila aberto pela Story 4.1: duas novas
// escritas em `status`/`versao` de `solicitacoes` (MarcarPendenciaHandler,
// ComentarSolicitacaoHandler) — mesma linha/lock do AD-4, mesma conexão
// PRIVILEGIADA (withPrivilegedDB). ObterSolicitacaoHandler é a única rota
// nova desta story que é só leitura (withDB) — expõe o histórico de
// `solicitacao_comentarios` e a `versao` atual a quem é dono da
// solicitação (solicitante ou administrador), para o cliente montar o
// próximo `{"versao":N,...}` de MarcarPendencia/Comentar/Assumir.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"fb_apu05/internal/fila"
)

// pendenciaOuComentarioRequest é o corpo compartilhado por POST
// /api/solicitacoes/{id}/pendencia e POST /api/solicitacoes/{id}/comentarios
// — `administrador_id`/`solicitante_id` (autor) NUNCA vem do corpo
// (Boundaries "Always" da spec: sempre de GetUserIDFromContext, mesmo
// princípio de assumirSolicitacaoRequest).
type pendenciaOuComentarioRequest struct {
	Versao     int    `json:"versao"`
	Comentario string `json:"comentario"`
}

// lerPendenciaOuComentarioRequest lê e valida o corpo compartilhado pelos
// 2 handlers de escrita desta story: `versao>=1` (mesmo achado de revisão
// da Story 4.1 — sem isso, versao=0 decairia para 409 conflito_versao em
// vez de um 400 de validação claro) e `comentario` não-vazio após
// TrimSpace (Boundaries "Never" da spec: sem CHECK de tamanho no banco, só
// esta validação no handler). Devolve o comentário já com TrimSpace
// aplicado — nunca o texto bruto do corpo.
func lerPendenciaOuComentarioRequest(w http.ResponseWriter, r *http.Request) (versao int, comentario string, ok bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	corpo, err := io.ReadAll(r.Body)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "não foi possível ler o corpo da requisição")
		return 0, "", false
	}

	var req pendenciaOuComentarioRequest
	if err := json.Unmarshal(corpo, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return 0, "", false
	}

	if req.Versao < 1 {
		jsonErr(w, http.StatusBadRequest, "campo 'versao' é obrigatório e deve ser >= 1")
		return 0, "", false
	}

	comentarioLimpo := strings.TrimSpace(req.Comentario)
	if comentarioLimpo == "" {
		jsonErr(w, http.StatusBadRequest, "campo 'comentario' é obrigatório e não pode ser vazio")
		return 0, "", false
	}

	return req.Versao, comentarioLimpo, true
}

// responderSolicitacaoFila escreve o mesmo formato de resposta 200 já
// usado por AssumirSolicitacaoHandler (Story 4.1) — reaproveitado por
// MarcarPendenciaHandler e ComentarSolicitacaoHandler, já que os 3
// devolvem a mesma projeção fila.Solicitacao. `administrador_id` vira JSON
// `null` quando vazio — só Comentar pode devolver isso (solicitante
// comentando numa solicitação `aberta` que nenhum administrador assumiu
// ainda, internal/fila.Comentar); Assumir/MarcarPendencia sempre devolvem
// um administrador_id preenchido, então o comportamento deles não muda.
func responderSolicitacaoFila(w http.ResponseWriter, resultado fila.Solicitacao) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":               resultado.ID,
		"tipo_solicitacao": resultado.TipoSolicitacao,
		"status":           resultado.Status,
		"versao":           resultado.Versao,
		"administrador_id": administradorIDOuNil(resultado.AdministradorID),
	})
}

// administradorIDOuNil converte a string-sentinela "" (sem administrador
// atribuído — só possível em Comentar, ver fila.Comentar) em `nil`, para o
// encoder JSON devolver `null` em vez de string vazia — delega a
// nullStringOuNil (fila.go), já que "" vazio equivale a um
// sql.NullString{Valid: false}.
func administradorIDOuNil(administradorID string) interface{} {
	return nullStringOuNil(sql.NullString{String: administradorID, Valid: administradorID != ""})
}

// tratarErroEscritaFila converte os 3 sentinelas de internal/fila
// (ErrNaoAutorizado, ErrConflitoVersao, ErrNaoEncontrada) no envelope HTTP
// correto — compartilhado por MarcarPendenciaHandler e
// ComentarSolicitacaoHandler. Devolve `true` quando já escreveu uma
// resposta de erro (o chamador deve retornar imediatamente); `false` quando
// `err` não é nenhum dos sentinelas conhecidos (o chamador trata como 500).
func tratarErroEscritaFila(w http.ResponseWriter, err error) bool {
	if errors.Is(err, fila.ErrNaoAutorizado) {
		jsonErr(w, http.StatusForbidden, "você não tem permissão para esta ação nesta solicitação")
		return true
	}
	var conflito *fila.ErrConflitoVersao
	if errors.As(err, &conflito) {
		jsonErrConflitoVersao(w, conflito.VersaoAtual)
		return true
	}
	if errors.Is(err, fila.ErrNaoEncontrada) {
		jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
		return true
	}
	return false
}

// MarcarPendenciaHandler — POST /api/solicitacoes/{id}/pendencia. Registrado
// em main.go atrás de RequireAuth(withPrivilegedDB(...), "administrador") —
// delega inteiramente a internal/fila.MarcarPendencia, nunca monta o UPDATE
// aqui (Boundaries "Always" da spec, mesmo princípio de
// AssumirSolicitacaoHandler).
func MarcarPendenciaHandler(db *sql.DB) http.HandlerFunc {
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
			// estabelecido em fila.go:126 (I/O Matrix da spec).
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}

		versao, comentario, ok := lerPendenciaOuComentarioRequest(w, r)
		if !ok {
			return
		}

		administradorID := GetUserIDFromContext(r)

		resultado, err := fila.MarcarPendencia(db, id, versao, administradorID, comentario)
		if err != nil {
			if tratarErroEscritaFila(w, err) {
				return
			}
			log.Printf("[Fila] Erro ao marcar pendência em %s: %v", id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		log.Printf("[Fila] %s marcou pendência em %s (versao=%d)", administradorID, resultado.ID, resultado.Versao)
		responderSolicitacaoFila(w, resultado)
	}
}

// ComentarSolicitacaoHandler — POST /api/solicitacoes/{id}/comentarios.
// Registrado em main.go atrás de RequireAuth(withPrivilegedDB(...), "") —
// qualquer perfil autenticado pode chamar a rota, mas só o solicitante dono
// da linha (`solicitante_id`) tem o comentário aceito (fila.ErrNaoAutorizado
// -> 403 para qualquer outro, Boundaries "Always" da spec) — delega
// inteiramente a internal/fila.Comentar.
func ComentarSolicitacaoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		id := r.PathValue("id")
		if !uuidFormatRegexp.MatchString(id) {
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}

		versao, comentario, ok := lerPendenciaOuComentarioRequest(w, r)
		if !ok {
			return
		}

		solicitanteID := GetUserIDFromContext(r)

		resultado, err := fila.Comentar(db, id, versao, solicitanteID, comentario)
		if err != nil {
			if tratarErroEscritaFila(w, err) {
				return
			}
			log.Printf("[Fila] Erro ao comentar em %s: %v", id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		log.Printf("[Fila] %s comentou em %s (versao=%d, status=%s)", solicitanteID, resultado.ID, resultado.Versao, resultado.Status)
		responderSolicitacaoFila(w, resultado)
	}
}

// comentarioDetalhe é um item do histórico devolvido por
// ObterSolicitacaoHandler — projeção mínima de `solicitacao_comentarios`
// (sem `solicitacao_id`: já implícito no recurso pai).
type comentarioDetalhe struct {
	ID        string    `json:"id"`
	AutorID   string    `json:"autor_id"`
	Tipo      string    `json:"tipo"`
	Texto     string    `json:"texto"`
	CreatedAt time.Time `json:"created_at"`
}

// ObterSolicitacaoHandler — GET /api/solicitacoes/{id}. Registrado em
// main.go atrás de RequireAuth(withDB(...), "") — só leitura (conexão
// GERAL, fb_apu05_app), qualquer perfil autenticado pode chamar a rota, mas
// só quem é `solicitante_id` OU `administrador_id` da linha recebe 200
// (403 para qualquer outro). SELECT direto de `solicitacoes` +
// `solicitacao_comentarios` (nenhuma escrita aqui — Boundaries "Always" da
// spec: escritas sempre por internal/fila).
func ObterSolicitacaoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		id := r.PathValue("id")
		if !uuidFormatRegexp.MatchString(id) {
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}

		var tipoSolicitacao, status, solicitanteID string
		var administradorID sql.NullString
		var versao int
		err := db.QueryRow(`
			SELECT tipo_solicitacao, status, versao, solicitante_id, administrador_id
			FROM solicitacoes WHERE id = $1
		`, id).Scan(&tipoSolicitacao, &status, &versao, &solicitanteID, &administradorID)
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}
		if err != nil {
			log.Printf("[Fila] Erro ao buscar solicitação %s: %v", id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		chamadorID := GetUserIDFromContext(r)
		souAdministrador := administradorID.Valid && administradorID.String == chamadorID
		if chamadorID != solicitanteID && !souAdministrador {
			// Mesmo padrão de jsonErr(w, http.StatusForbidden, ...) de
			// handlers/solicitacoes.go:289 — terceiro sem relação com a
			// solicitação nunca vê nem o histórico.
			jsonErr(w, http.StatusForbidden, "você não tem permissão para ver esta solicitação")
			return
		}

		rows, err := db.Query(`
			SELECT id, autor_id, tipo, texto, created_at
			FROM solicitacao_comentarios
			WHERE solicitacao_id = $1
			ORDER BY created_at ASC, id ASC
		`, id)
		if err != nil {
			log.Printf("[Fila] Erro ao buscar comentários de %s: %v", id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer rows.Close()

		comentarios := make([]comentarioDetalhe, 0)
		for rows.Next() {
			var c comentarioDetalhe
			if err := rows.Scan(&c.ID, &c.AutorID, &c.Tipo, &c.Texto, &c.CreatedAt); err != nil {
				log.Printf("[Fila] Erro ao ler comentário de %s: %v", id, err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			comentarios = append(comentarios, c)
		}
		if err := rows.Err(); err != nil {
			log.Printf("[Fila] Erro ao iterar comentários de %s: %v", id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":               id,
			"tipo_solicitacao": tipoSolicitacao,
			"status":           status,
			"versao":           versao,
			"solicitante_id":   solicitanteID,
			"administrador_id": nullStringOuNil(administradorID),
			"comentarios":      comentarios,
		})
	}
}
