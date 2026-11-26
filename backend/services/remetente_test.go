package services

import (
	"net/mail"
	"strings"
	"testing"
)

func TestComNomeRemetente_TrocaSoONome(t *testing.T) {
	t.Setenv("SMTP_FROM_NAME", "")
	got := comNomeRemetente("Cloud <contato@fortesbezerra.com.br>")
	a, err := mail.ParseAddress(got)
	if err != nil || a.Name != "Orçamento FC" || a.Address != "contato@fortesbezerra.com.br" {
		t.Fatalf("%q -> %+v %v", got, a, err)
	}
	if strings.Contains(got, "ç") {
		t.Errorf("o acento precisa ir codificado no cabeçalho: %q", got)
	}
	t.Setenv("SMTP_FROM_NAME", "Controladoria FC")
	if a, _ := mail.ParseAddress(comNomeRemetente("x@fc.com")); a.Name != "Controladoria FC" {
		t.Errorf("SMTP_FROM_NAME deveria valer: %+v", a)
	}
}
