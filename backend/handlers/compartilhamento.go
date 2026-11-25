package handlers

// compartilhamento.go — compartilhar chamado (somente leitura) com usuários já cadastrados. Só quem ABRIU o chamado
// compartilha e retira o acesso; quem recebe vê o andamento, o histórico e os anexos, e nada mais.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"fb_apu05/services"
)

var enviarEmailCompartilhamento = services.SendHTMLEmail

// podeVerSolicitacao: dono, atendente/administrador, administrador do chamado ou usuário com quem foi compartilhado.
func compartilhadoCom(db *sql.DB, solicitacaoID, usuarioID string) bool {
	var ok bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM solicitacao_compartilhamentos WHERE solicitacao_id = $1 AND usuario_id = $2)`, solicitacaoID, usuarioID).Scan(&ok); err != nil {
		log.Printf("[Compartilhamento] erro ao checar acesso: %v", err)
		return false
	}
	return ok
}

// donoDoChamado devolve o solicitante e o código do chamado; ok=false se não existe.
func donoDoChamado(db *sql.DB, id string) (solicitanteID string, numero int64, ok bool, err error) {
	err = db.QueryRow(`SELECT solicitante_id, numero FROM solicitacoes WHERE id = $1`, id).Scan(&solicitanteID, &numero)
	if err == sql.ErrNoRows {
		return "", 0, false, nil
	}
	return solicitanteID, numero, err == nil, err
}

// BuscarUsuariosHandler — GET /api/usuarios/busca?q= : nome e e-mail de usuários ativos, para escolher com quem compartilhar.
// Exige 3 caracteres e devolve no máximo 8, sem outros dados.
func BuscarUsuariosHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		out := []map[string]string{}
		if len([]rune(q)) >= 3 {
			p := "%" + escapaLike(q) + "%"
			rows, err := db.Query(`SELECT id, nome, email FROM usuarios WHERE ativo AND id <> $1 AND (nome ILIKE $2 OR email ILIKE $2) ORDER BY nome LIMIT 8`, GetUserIDFromContext(r), p)
			if err != nil {
				log.Printf("[Compartilhamento] busca de usuários: %v", err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
			defer rows.Close()
			for rows.Next() {
				var id, nome, email string
				if rows.Scan(&id, &nome, &email) == nil {
					out = append(out, map[string]string{"id": id, "nome": nome, "email": email})
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": out})
	}
}

func listarCompartilhamentos(db *sql.DB, id string) ([]map[string]string, error) {
	rows, err := db.Query(`SELECT u.id, u.nome, u.email FROM solicitacao_compartilhamentos c JOIN usuarios u ON u.id = c.usuario_id WHERE c.solicitacao_id = $1 ORDER BY c.created_at`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]string{}
	for rows.Next() {
		var uid, nome, email string
		if err := rows.Scan(&uid, &nome, &email); err != nil {
			return nil, err
		}
		out = append(out, map[string]string{"id": uid, "nome": nome, "email": email})
	}
	return out, rows.Err()
}

// CompartilhamentosHandler — GET /api/solicitacoes/{id}/compartilhamentos (só o dono).
func CompartilhamentosHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !uuidFormatRegexp.MatchString(id) {
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}
		dono, _, ok, err := donoDoChamado(db, id)
		if err != nil {
			log.Printf("[Compartilhamento] %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if !ok {
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}
		if dono != GetUserIDFromContext(r) {
			jsonErr(w, http.StatusForbidden, "Só quem abriu o chamado pode compartilhá-lo.")
			return
		}
		itens, err := listarCompartilhamentos(db, id)
		if err != nil {
			log.Printf("[Compartilhamento] listar: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": itens})
	}
}

// CompartilharHandler — POST /api/solicitacoes/{id}/compartilhamentos {"usuario_id": "..."} (só o dono).
func CompartilharHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !uuidFormatRegexp.MatchString(id) {
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
		corpo, _ := io.ReadAll(r.Body)
		var req struct {
			UsuarioID string `json:"usuario_id"`
		}
		if json.Unmarshal(corpo, &req) != nil || !uuidFormatRegexp.MatchString(req.UsuarioID) {
			jsonErr(w, http.StatusBadRequest, "Escolha a pessoa com quem compartilhar.")
			return
		}
		quem := GetUserIDFromContext(r)
		dono, numero, ok, err := donoDoChamado(db, id)
		if err != nil {
			log.Printf("[Compartilhamento] %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if !ok {
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}
		if dono != quem {
			jsonErr(w, http.StatusForbidden, "Só quem abriu o chamado pode compartilhá-lo.")
			return
		}
		if req.UsuarioID == quem {
			jsonErr(w, http.StatusBadRequest, "O chamado já é seu. Escolha outra pessoa.")
			return
		}
		var nome, email string
		var ativo bool
		if err := db.QueryRow(`SELECT nome, email, ativo FROM usuarios WHERE id = $1`, req.UsuarioID).Scan(&nome, &email, &ativo); err == sql.ErrNoRows || (err == nil && !ativo) {
			jsonErr(w, http.StatusBadRequest, "Usuário não encontrado ou inativo.")
			return
		} else if err != nil {
			log.Printf("[Compartilhamento] usuário: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		tx, err := db.Begin()
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer func() { _ = tx.Rollback() }()
		res, err := tx.Exec(`INSERT INTO solicitacao_compartilhamentos (solicitacao_id, usuario_id, compartilhado_por) VALUES ($1, $2, $3) ON CONFLICT (solicitacao_id, usuario_id) DO NOTHING`, id, req.UsuarioID, quem)
		if err != nil {
			log.Printf("[Compartilhamento] gravar: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		novo, _ := res.RowsAffected()
		if novo > 0 {
			if _, err := tx.Exec(`INSERT INTO solicitacao_comentarios (solicitacao_id, autor_id, tipo, texto) VALUES ($1, $2, 'evento', $3)`, id, quem, "Compartilhou o chamado (somente leitura) com "+nome+"."); err != nil {
				log.Printf("[Compartilhamento] histórico: %v", err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
		}
		if err := tx.Commit(); err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if novo > 0 {
			var quemNome string
			_ = db.QueryRow(`SELECT nome FROM usuarios WHERE id = $1`, quem).Scan(&quemNome)
			go avisarCompartilhamento(email, nome, quemNome, CodigoChamado(numero), id)
		}
		itens, _ := listarCompartilhamentos(db, id)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": itens})
	}
}

// RemoverCompartilhamentoHandler — DELETE /api/solicitacoes/{id}/compartilhamentos/{usuarioId} (só o dono).
func RemoverCompartilhamentoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, alvo := r.PathValue("id"), r.PathValue("usuarioId")
		if !uuidFormatRegexp.MatchString(id) || !uuidFormatRegexp.MatchString(alvo) {
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}
		quem := GetUserIDFromContext(r)
		dono, _, ok, err := donoDoChamado(db, id)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if !ok {
			jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			return
		}
		if dono != quem {
			jsonErr(w, http.StatusForbidden, "Só quem abriu o chamado pode retirar o acesso.")
			return
		}
		tx, err := db.Begin()
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer func() { _ = tx.Rollback() }()
		var nome string
		_ = tx.QueryRow(`SELECT nome FROM usuarios WHERE id = $1`, alvo).Scan(&nome)
		res, err := tx.Exec(`DELETE FROM solicitacao_compartilhamentos WHERE solicitacao_id = $1 AND usuario_id = $2`, id, alvo)
		if err != nil {
			log.Printf("[Compartilhamento] remover: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		if n, _ := res.RowsAffected(); n > 0 {
			if _, err := tx.Exec(`INSERT INTO solicitacao_comentarios (solicitacao_id, autor_id, tipo, texto) VALUES ($1, $2, 'evento', $3)`, id, quem, "Retirou o acesso de "+nome+" ao chamado."); err != nil {
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
				return
			}
		}
		if err := tx.Commit(); err != nil {
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		itens, _ := listarCompartilhamentos(db, id)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": itens})
	}
}

func avisarCompartilhamento(email, nome, quemNome, codigo, id string) {
	link := strings.TrimRight(os.Getenv("APP_URL"), "/") + "/solicitacoes/" + id
	corpo := fmt.Sprintf(`<p>Olá, %s.</p><p>%s compartilhou com você o chamado <strong>%s</strong> do Sistema de Orçamento. Você pode acompanhar o andamento (somente leitura):</p><p><a href="%s">Abrir o chamado %s</a></p>`,
		html.EscapeString(nome), html.EscapeString(quemNome), html.EscapeString(codigo), html.EscapeString(link), html.EscapeString(codigo))
	if err := enviarEmailCompartilhamento([]string{email}, "Chamado "+codigo+" compartilhado com você", corpo); err != nil {
		log.Printf("[Compartilhamento] e-mail não enviado para %s: %v", email, err)
	}
}
