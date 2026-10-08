package handlers

import (
	"encoding/json"
	"net/http"
	"sync"
)

// HealthResponse é o payload de GET /api/health.
type HealthResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

var (
	dbErrMu sync.RWMutex
	dbErrs  = map[string]error{}
)

// SetDBError registra o último erro de conexão (ou nil quando conectado)
// para a conexão identificada por `name`, para que HealthHandler possa
// refletir o estado real — chamado pelo ciclo de vida de CADA conexão em
// main.go (initDBAsync, connectWithRetry para dbApp/dbPrivileged). Uma
// entrada por conexão (nunca uma flag única compartilhada): main.go sobe 3
// pools de conexão em goroutines independentes, e uma flag única deixaria
// o sucesso de uma mascarar a falha de outra, quebrando o gate de deploy
// do AD-10 (/api/health precisa refletir falha de QUALQUER uma das 3).
func SetDBError(name string, err error) {
	dbErrMu.Lock()
	defer dbErrMu.Unlock()
	if err == nil {
		delete(dbErrs, name)
	} else {
		dbErrs[name] = err
	}
}

func getDBError() error {
	dbErrMu.RLock()
	defer dbErrMu.RUnlock()
	for _, err := range dbErrs {
		if err != nil {
			return err
		}
	}
	return nil
}

// HealthHandler responde GET /api/health — health-check de processo usado
// pelo deploy (AD-10) para liberar tráfego só depois do serviço subir.
// 200 {"status":"ok"} quando o banco está conectado; 503 com o erro quando
// não está.
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := getDBError(); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(HealthResponse{Status: "error", Error: err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(HealthResponse{Status: "ok"})
}
