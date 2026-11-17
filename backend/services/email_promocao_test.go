package services

import (
	"mime"
	"strings"
	"testing"
)

func TestMontarEmailPromocao_EscapaTudoEMantemAssuntoEmUmaLinha(t *testing.T) {
	p := PedidoPromocao{
		Nome:             `<img src=x onerror=alert(1)> Maria` + "\r\nBcc: vitima@x.com",
		Email:            `m@fc.com.br"><script>x</script>`,
		PerfilAtual:      "solicitante",
		PerfilSolicitado: "atendente",
		Mensagem:         `<script>fetch('https://evil')</script>`,
	}
	assunto, corpo := montarEmailPromocao("https://orcamento.ferreiracosta.com.br", p)

	for _, perigoso := range []string{"<img src=x", "<script>", "onerror=alert(1)>"} {
		if strings.Contains(corpo, perigoso) {
			t.Errorf("corpo contém trecho não escapado %q", perigoso)
		}
	}
	if !strings.Contains(corpo, "&lt;script&gt;") {
		t.Error("mensagem deveria aparecer escapada")
	}
	if strings.ContainsAny(assunto, "\r\n") {
		t.Errorf("assunto com quebra de linha (injeção de cabeçalho): %q", assunto)
	}
	dec, err := new(mime.WordDecoder).DecodeHeader(assunto)
	if err != nil || strings.ContainsAny(dec, "\r\n") {
		t.Errorf("assunto decodificado inválido: %q (%v)", dec, err)
	}
	if !strings.Contains(corpo, "https://orcamento.ferreiracosta.com.br/admin/usuarios") {
		t.Error("link para Gestão de usuários ausente")
	}
}

func TestSendPromotionRequestEmail_SemSMTP(t *testing.T) {
	t.Setenv("SMTP_PASSWORD", "")
	if n, err := SendPromotionRequestEmail([]string{"a@fc.com.br"}, PedidoPromocao{Nome: "X"}); err == nil || n != 0 {
		t.Fatalf("sem SMTP deveria falhar: n=%d err=%v", n, err)
	}
}
