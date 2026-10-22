package handlers

// Rate limiter em memória, mesmo padrão do FB_APU02 (handlers/middleware.go)
// — adotado aqui especificamente para os fluxos de senha local (login,
// esqueci/redefinir senha, trocar senha), os únicos endpoints desta story
// que aceitam um segredo fornecido pelo cliente e por isso são alvo de
// brute-force. Nenhum outro handler do FB_APU05 usa isto.

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

type rateLimiter struct {
	mu       sync.Mutex
	requests map[string][]time.Time
	max      int
	window   time.Duration
}

func newRateLimiter(max int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		requests: make(map[string][]time.Time),
		max:      max,
		window:   window,
	}
}

// Allow checa E registra uma tentativa. Devolve false se o limite já foi
// atingido (a tentativa corrente ainda assim é descartada, não contada).
func (rl *rateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	existing := rl.requests[key]
	valid := existing[:0]
	for _, t := range existing {
		if now.Sub(t) < rl.window {
			valid = append(valid, t)
		}
	}
	if len(valid) >= rl.max {
		rl.requests[key] = valid
		return false
	}
	rl.requests[key] = append(valid, now)
	return true
}

var (
	LoginRL = newRateLimiter(5, 15*time.Minute)
	// LoginByAccountRL limita tentativas de login por conta (e-mail), além do
	// LoginRL por IP acima — sem isto, um atacante distribuído por várias IPs
	// consegue tentar senha contra uma única conta-alvo sem nunca esbarrar no
	// limite por IP.
	LoginByAccountRL = newRateLimiter(5, 15*time.Minute)
	ForgotPasswordRL = newRateLimiter(3, time.Hour)
	ResetPasswordRL  = newRateLimiter(5, time.Hour)
	ChangePasswordRL = newRateLimiter(5, 15*time.Minute)
)

// GetClientIP extrai o IP real do cliente atrás do proxy reverso (nginx,
// ver frontend/nginx.conf, que já envia X-Forwarded-For/X-Real-IP) — usa o
// ÚLTIMO IP de X-Forwarded-For (anexado pelo nosso próprio proxy, não
// forjável pelo cliente), mesmo padrão do FB_APU02.
func GetClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[len(parts)-1])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx > 0 {
		return addr[:idx]
	}
	return addr
}
