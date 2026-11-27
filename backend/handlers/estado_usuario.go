package handlers

// estado_usuario.go — RequireAuth confere perfil e "ativo" no banco (27/11/2026, pré-pentest), com cache de 30 s.

import (
	"database/sql"
	"sync"
	"time"
)

var (
	authDBMu    sync.RWMutex
	authDB      *sql.DB
	estadoCache sync.Map // user_id -> estadoUsuario
)

type estadoUsuario struct {
	perfil string
	ativo  bool
	em     time.Time
}

// SetAuthDB liga a checagem (main.go, depois de conectar). Sem ela (testes), vale só o token.
func SetAuthDB(db *sql.DB) {
	authDBMu.Lock()
	authDB = db
	authDBMu.Unlock()
}

// esquecerEstadoUsuario: chamado quando o admin altera o usuário, para valer na hora.
func esquecerEstadoUsuario(id string) { estadoCache.Delete(id) }

func estadoUsuarioAtual(id string) (perfil string, ativo, conhecido bool) {
	if v, ok := estadoCache.Load(id); ok {
		e := v.(estadoUsuario)
		if time.Since(e.em) < 30*time.Second {
			return e.perfil, e.ativo, true
		}
	}
	authDBMu.RLock()
	db := authDB
	authDBMu.RUnlock()
	if db == nil {
		return "", false, false
	}
	var e estadoUsuario
	err := db.QueryRow(`SELECT perfil, ativo FROM usuarios WHERE id = $1`, id).Scan(&e.perfil, &e.ativo)
	if err == sql.ErrNoRows {
		e = estadoUsuario{ativo: false}
	} else if err != nil {
		return "", false, false // banco fora: não derruba a sessão por isso
	}
	e.em = time.Now()
	estadoCache.Store(id, e)
	return e.perfil, e.ativo, true
}
