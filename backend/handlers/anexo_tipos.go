package handlers

import (
	"bytes"
	"path/filepath"
	"strings"
)

// Validação de anexo pelo CONTEÚDO (retorno da homologação, item 19): a extensão escolhe o tipo e os primeiros
// bytes do arquivo precisam bater com ele; o tipo declarado pelo navegador é ignorado (vem vazio para .msg/.eml).
// O content_type gravado é sempre o canônico da extensão.

type assinatura int

const (
	assPDF assinatura = iota
	assJPEG
	assPNG
	assOLE2 // .doc, .xls, .msg (Compound File)
	assZIP  // .docx, .xlsx
	assEML  // texto com cabeçalhos de e-mail
)

var tiposAnexo = map[string]struct {
	ct  string
	ass assinatura
}{
	".pdf":  {"application/pdf", assPDF},
	".jpg":  {"image/jpeg", assJPEG},
	".jpeg": {"image/jpeg", assJPEG},
	".png":  {"image/png", assPNG},
	".doc":  {"application/msword", assOLE2},
	".xls":  {"application/vnd.ms-excel", assOLE2},
	".msg":  {"application/vnd.ms-outlook", assOLE2},
	".docx": {"application/vnd.openxmlformats-officedocument.wordprocessingml.document", assZIP},
	".xlsx": {"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", assZIP},
	".eml":  {"message/rfc822", assEML},
}

const tiposAnexoAceitos = "PDF, imagem (JPG/PNG), Word, Excel e e-mail (.msg ou .eml)"

// tipoDoAnexo devolve o content_type canônico se a extensão é aceita e o conteúdo é daquele formato.
func tipoDoAnexo(nome string, dados []byte) (string, bool) {
	t, ok := tiposAnexo[strings.ToLower(filepath.Ext(strings.TrimSpace(nome)))]
	if !ok {
		return "", false
	}
	var bateu bool
	switch t.ass {
	case assPDF:
		// o PDF pode ter lixo antes do cabeçalho (até 1024 bytes, como os leitores toleram)
		n := len(dados)
		if n > 1024 {
			n = 1024
		}
		bateu = bytes.Contains(dados[:n], []byte("%PDF-"))
	case assJPEG:
		bateu = bytes.HasPrefix(dados, []byte{0xFF, 0xD8, 0xFF})
	case assPNG:
		bateu = bytes.HasPrefix(dados, []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
	case assOLE2:
		bateu = bytes.HasPrefix(dados, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1})
	case assZIP:
		bateu = bytes.HasPrefix(dados, []byte("PK\x03\x04"))
	case assEML:
		bateu = pareceEmail(dados)
	}
	return t.ct, bateu
}

// pareceEmail: texto (sem bytes nulos) cujas primeiras linhas têm cabeçalhos de e-mail (From:, To:, Subject:, ...).
func pareceEmail(dados []byte) bool {
	n := len(dados)
	if n > 8192 {
		n = 8192
	}
	cab := dados[:n]
	if bytes.IndexByte(cab, 0) >= 0 {
		return false
	}
	achou := 0
	for _, linha := range strings.Split(strings.ReplaceAll(string(cab), "\r\n", "\n"), "\n") {
		if linha == "" {
			break // fim do cabeçalho
		}
		l := strings.ToLower(linha)
		for _, h := range []string{"from:", "to:", "subject:", "date:", "received:", "message-id:", "mime-version:", "content-type:"} {
			if strings.HasPrefix(l, h) {
				achou++
			}
		}
	}
	return achou >= 2
}
