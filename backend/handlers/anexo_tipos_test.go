package handlers

import (
	"encoding/base64"
	"strings"
	"testing"
)

func anexoDe(nome string, conteudo []byte) []anexoRequest {
	return []anexoRequest{{NomeArquivo: nome, ContentType: "", ConteudoBase64: base64.StdEncoding.EncodeToString(conteudo)}}
}

func TestAnexo_TextoRenomeadoParaPDFRecusado(t *testing.T) {
	if _, msg := validarAnexos(anexoDe("falso.pdf", []byte("isto é só texto")), true); msg == "" {
		t.Error("texto renomeado para .pdf deveria ser recusado")
	}
}

func TestAnexo_EmailMsgEEml(t *testing.T) {
	ole := append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, make([]byte, 64)...)
	if got, msg := validarAnexos(anexoDe("autorizacao.msg", ole), true); msg != "" || got[0].ContentType != "application/vnd.ms-outlook" {
		t.Errorf(".msg real deveria passar: %q", msg)
	}
	eml := []byte("From: a@x.com\r\nTo: b@x.com\r\nSubject: ok\r\n\r\ncorpo")
	if got, msg := validarAnexos(anexoDe("autorizacao.eml", eml), true); msg != "" || got[0].ContentType != "message/rfc822" {
		t.Errorf(".eml real deveria passar: %q", msg)
	}
	if _, msg := validarAnexos(anexoDe("falso.eml", []byte("olá, não sou e-mail")), true); msg == "" {
		t.Error("texto sem cabeçalhos de e-mail não pode ser .eml")
	}
	if _, msg := validarAnexos(anexoDe("falso.msg", []byte("texto")), true); msg == "" {
		t.Error(".msg sem assinatura OLE2 deve ser recusado")
	}
}

func TestAnexo_TipoDeclaradoIgnorado(t *testing.T) {
	// o navegador manda tipo vazio ou genérico: vale a extensão + conteúdo, e o tipo gravado é o canônico
	req := []anexoRequest{{NomeArquivo: "a.pdf", ContentType: "application/octet-stream", ConteudoBase64: base64.StdEncoding.EncodeToString([]byte("%PDF-1.7 x"))}}
	got, msg := validarAnexos(req, true)
	if msg != "" || got[0].ContentType != "application/pdf" {
		t.Errorf("msg=%q", msg)
	}
	if _, msg := validarAnexos(anexoDe("x.exe", []byte("MZ")), true); !strings.Contains(msg, "Aceitos") {
		t.Errorf("exe deve ser recusado listando os tipos aceitos: %q", msg)
	}
}
