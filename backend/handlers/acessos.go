package handlers

// acessos.go — registro de acessos (26/11/2026): a cada login (senha ou login corporativo) grava o último acesso do
// usuário e uma linha no histórico (quando, por onde, IP). A Gestão de usuários mostra o último acesso e o histórico.

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

// registrarAcesso é trocável nos testes (os de sqlmock do login não simulam estas escritas).
var registrarAcesso = func(db *sql.DB, r *http.Request, usuarioID string) {
	via := "senha"
	if strings.Contains(r.URL.Path, "sso") {
		via = "sso"
	}
	ip := GetClientIP(r)
	if len(ip) > 64 {
		ip = ip[:64]
	}
	if _, err := db.Exec(`UPDATE usuarios SET ultimo_acesso = now() WHERE id = $1`, usuarioID); err != nil {
		log.Printf("[Acessos] último acesso de %s: %v", usuarioID, err)
	}
	if _, err := db.Exec(`INSERT INTO usuarios_acessos (usuario_id, via, ip) VALUES ($1, $2, NULLIF($3, ''))`, usuarioID, via, ip); err != nil {
		log.Printf("[Acessos] histórico de %s: %v", usuarioID, err)
	}
}

// ultimosAcessos: id → último acesso (para a lista da Gestão de usuários). Erro = mapa vazio.
func ultimosAcessos(db *sql.DB) map[string]time.Time {
	out := map[string]time.Time{}
	rows, err := db.Query(`SELECT id, ultimo_acesso FROM usuarios WHERE ultimo_acesso IS NOT NULL`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var t time.Time
		if rows.Scan(&id, &t) == nil {
			out[id] = t
		}
	}
	return out
}

// AcessosUsuarioHandler — GET /api/admin/usuarios/{id}/acessos (administrador): os 100 logins mais recentes.
func AcessosUsuarioHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !uuidFormatRegexp.MatchString(id) {
			jsonErr(w, http.StatusNotFound, "Usuário não encontrado.")
			return
		}
		rows, err := db.Query(`SELECT quando, via, COALESCE(ip, '') FROM usuarios_acessos WHERE usuario_id = $1 ORDER BY quando DESC LIMIT 100`, id)
		if err != nil {
			log.Printf("[Acessos] listar %s: %v", id, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		defer rows.Close()
		out := []map[string]any{}
		for rows.Next() {
			var quando time.Time
			var via, ip string
			if rows.Scan(&quando, &via, &ip) == nil {
				out = append(out, map[string]any{"quando": quando, "via": via, "ip": ip})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": out})
	}
}
