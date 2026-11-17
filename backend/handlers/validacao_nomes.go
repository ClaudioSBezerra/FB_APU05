package handlers

// validacao_nomes.go — validação de nomes informados por usuários (pessoa, empresa, etc.).
// pessoa). Achado de pentest (nov/2026, FB_APU04; mesma regra aplicada aqui): nomes sem
// validação viravam HTML/JS armazenado ("<script>…") em "Ambiente de …"/"Grupo de …".
// Não é SQL injection: todos os INSERT são parametrizados.

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxNome = 120

func caractereProibido(r rune) bool {
	// < e > (HTML), controles (incl. \x00) e formatação invisível/bidi (U+202E etc.)
	return r == '<' || r == '>' || unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
}

// NomeSeguro valida e normaliza (aparar e juntar espaços): 1..max caracteres, sem < >,
// controle nem invisíveis.
func NomeSeguro(campo, v string, max int) (string, error) {
	if max <= 0 {
		max = maxNome
	}
	v = strings.Join(strings.Fields(v), " ")
	if v == "" {
		return "", fmt.Errorf("%s é obrigatório", campo)
	}
	if !utf8.ValidString(v) {
		return "", fmt.Errorf("%s contém caracteres inválidos", campo)
	}
	if utf8.RuneCountInString(v) > max {
		return "", fmt.Errorf("%s deve ter no máximo %d caracteres", campo, max)
	}
	for _, r := range v {
		if caractereProibido(r) {
			return "", fmt.Errorf("%s contém caracteres não permitidos", campo)
		}
	}
	return v, nil
}

// SanitizarNome remove o que for proibido e trunca; para valores que não podem ser
// recusados (nome vindo do Keycloak no auto-provisionamento).
func SanitizarNome(v string, max int) string {
	if max <= 0 {
		max = maxNome
	}
	var b strings.Builder
	for _, r := range v {
		if unicode.IsSpace(r) { // quebra de linha e tab viram espaço (não juntam palavras)
			b.WriteRune(' ')
			continue
		}
		if r == utf8.RuneError || caractereProibido(r) {
			continue
		}
		b.WriteRune(r)
	}
	s := strings.Join(strings.Fields(b.String()), " ")
	if utf8.RuneCountInString(s) > max {
		s = string([]rune(s)[:max])
	}
	return s
}

// trustedProxyHops: quantos proxies confiáveis há entre a internet e a API (padrão 1).
// Atrás de ALB + nginx do servidor + nginx do frontend são 3.
func trustedProxyHops() int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("TRUSTED_PROXY_HOPS"))); err == nil && v >= 1 {
		return v
	}
	return 1
}
