package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNomeSeguro(t *testing.T) {
	for _, v := range []string{"Wanessa de Oliveira Rodrigues", "  João   D'Ávila-Silva ", "ANA PAULA"} {
		if _, err := NomeSeguro("nome", v, 120); err != nil {
			t.Errorf("%q deveria passar: %v", v, err)
		}
	}
	for _, v := range []string{"", "   ", "<script>alert(1)</script>", "<img src=x onerror=fetch('https://x')>", "a>b",
		"nul\x00byte", "bidi‮oculto", "zero​width", strings.Repeat("a", 121), "\xff\xfe"} {
		if _, err := NomeSeguro("nome", v, 120); err == nil {
			t.Errorf("%q deveria ser recusado", v)
		}
	}
}

func TestSanitizarNome(t *testing.T) {
	if got := SanitizarNome("<b>x</b>", 120); got != "bx/b" {
		t.Errorf("sanitizar: %q", got)
	}
	if got := SanitizarNome(strings.Repeat("é", 300), 120); len([]rune(got)) != 120 {
		t.Errorf("truncar: %d", len([]rune(got)))
	}
}

func TestGetClientIPSaltosConfiaveis(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "198.51.100.7, 172.16.1.1, 172.18.0.5")
	t.Setenv("TRUSTED_PROXY_HOPS", "")
	if got := GetClientIP(r); got != "172.18.0.5" {
		t.Errorf("padrão (comportamento antigo): %q", got)
	}
	t.Setenv("TRUSTED_PROXY_HOPS", "3")
	if got := GetClientIP(r); got != "198.51.100.7" {
		t.Errorf("3 saltos: %q", got)
	}
	r.Header.Set("X-Forwarded-For", "6.6.6.6, 198.51.100.7, 172.16.1.1, 172.18.0.5") // forjado à esquerda
	if got := GetClientIP(r); got != "198.51.100.7" {
		t.Errorf("forjado: %q", got)
	}
}

func TestPerfilPermitido(t *testing.T) {
	casos := []struct {
		perfil, exigido string
		quer            bool
	}{
		{"administrador", "administrador", true},
		{"atendente", "administrador", false},
		{"atendente", "administrador,atendente", true},
		{"administrador", "administrador,atendente", true},
		{"administrador", "administrador, atendente", true}, // espaço depois da vírgula
		{"solicitante", "administrador,atendente", false},
		{"", "administrador,atendente", false},
		{"", "", false},
	}
	for _, c := range casos {
		if got := perfilPermitido(c.perfil, c.exigido); got != c.quer {
			t.Errorf("perfilPermitido(%q, %q) = %v, quer %v", c.perfil, c.exigido, got, c.quer)
		}
	}
	for _, p := range []string{"administrador", "atendente", "solicitante"} {
		if !perfilValido(p) {
			t.Errorf("%q deveria ser perfil válido", p)
		}
	}
	for _, p := range []string{"", "root", "admin", "Administrador"} {
		if perfilValido(p) {
			t.Errorf("%q NÃO deveria ser perfil válido", p)
		}
	}
}
