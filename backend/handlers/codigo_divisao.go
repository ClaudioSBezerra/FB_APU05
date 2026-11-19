package handlers

import (
	"regexp"
	"strings"
)

var reSoDigitosDiv = regexp.MustCompile(`^[0-9]+$`)

// CodigoDivisao4 padroniza o código de divisão (filial) em 4 caracteres: código só com dígitos e menor
// que 4 ganha zeros à esquerda ("91" → "0091", "501" → "0501"). Como o SAP escreve a filial. Código
// com letras (ex.: SEM-DIVISAO) ou já com 4 ou mais caracteres não muda.
func CodigoDivisao4(codigo string) string {
	c := strings.TrimSpace(codigo)
	if reSoDigitosDiv.MatchString(c) && len(c) < 4 {
		return strings.Repeat("0", 4-len(c)) + c
	}
	return c
}

// normalizaFilialOpcional aplica CodigoDivisao4 ao valor opcional de filial (nil continua nil).
func normalizaFilialOpcional(v interface{}) interface{} {
	if s, ok := v.(string); ok {
		return CodigoDivisao4(s)
	}
	return v
}
