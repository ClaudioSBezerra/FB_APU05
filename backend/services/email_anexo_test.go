package services

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

func TestBuildMessageWithAttachment(t *testing.T) {
	pdf := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte{0, 1, 2, 250, 255}, 100)...)
	raw := buildMessageWithAttachment("Cloud <c@x.com>", []string{"a@x.com", "b@x.com"}, "Resumo de despesas – ação", "<p>Olá</p>", "Resumo \"TI\"\r\n../x.pdf", "application/pdf", pdf)
	m, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := (&mime.WordDecoder{}).DecodeHeader(m.Header.Get("Subject")); got != "Resumo de despesas – ação" {
		t.Errorf("assunto: %q", got)
	}
	mt, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/mixed" {
		t.Fatalf("content-type: %v %v", mt, err)
	}
	mr := multipart.NewReader(m.Body, params["boundary"])
	p1, err := mr.NextPart()
	if err != nil || !strings.HasPrefix(p1.Header.Get("Content-Type"), "multipart/alternative") {
		t.Fatalf("1ª parte deve ser o corpo (multipart/alternative): %v", err)
	}
	_, ap, _ := mime.ParseMediaType(p1.Header.Get("Content-Type"))
	ar := multipart.NewReader(p1, ap["boundary"])
	txt, _ := ar.NextPart()
	if txt == nil || !strings.HasPrefix(txt.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("o corpo deve ter a versão texto")
	}
	if tb, _ := io.ReadAll(base64Decoder(txt)); strings.TrimSpace(string(tb)) != "Olá" { // lê antes da próxima parte
		t.Errorf("texto simples: %q", tb)
	}
	if htm, _ := ar.NextPart(); htm == nil || !strings.HasPrefix(htm.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("o corpo deve ter a versão HTML")
	}
	if m.Header.Get("Date") == "" || !strings.HasSuffix(strings.TrimSpace(m.Header.Get("Message-ID")), "@x.com>") {
		t.Errorf("faltam Date/Message-ID: %q %q", m.Header.Get("Date"), m.Header.Get("Message-ID"))
	}
	p2, err := mr.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	disp := p2.Header.Get("Content-Disposition")
	if !strings.Contains(disp, "attachment") || strings.ContainsAny(disp, "\r\n") {
		t.Errorf("disposição do anexo: %q", disp)
	}
	dados, _ := io.ReadAll(base64Decoder(p2))
	if !bytes.Equal(dados, pdf) {
		t.Errorf("o anexo decodificado difere do original (%d x %d bytes)", len(dados), len(pdf))
	}
	if _, err := mr.NextPart(); err != io.EOF {
		t.Errorf("esperava só 2 partes")
	}
}

func TestNomeAnexoSeguro(t *testing.T) {
	for in, quer := range map[string]string{"a.pdf": "a.pdf", "": "anexo.pdf", "..": "anexo.pdf", "x/y\\z.pdf": "x_y_z.pdf", "a\"b\r\nc.pdf": "a_b__c.pdf"} {
		if got := nomeAnexoSeguro(in); got != quer {
			t.Errorf("%q -> %q, esperava %q", in, got, quer)
		}
	}
}

func base64Decoder(p *multipart.Part) io.Reader {
	return base64.NewDecoder(base64.StdEncoding, p)
}
