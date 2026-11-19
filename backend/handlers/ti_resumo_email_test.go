package handlers

import (
	"bytes"
	"database/sql"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

const sqlDestinatariosTI = "FROM usuarios u"

func formResumo(t *testing.T, campos map[string]string, nome, conteudo string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range campos {
		_ = w.WriteField(k, v)
	}
	if nome != "" {
		fw, _ := w.CreateFormFile("arquivo", nome)
		_, _ = fw.Write([]byte(conteudo))
	}
	_ = w.Close()
	return &buf, w.FormDataContentType()
}

func esperaDestinatarios(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(regexp.QuoteMeta(sqlDestinatariosTI)).WillReturnRows(sqlmock.NewRows([]string{"nome", "email", "controle_ti", "ccs"}).
		AddRow("Claudio", "claudio.bezerra@ferreiracosta.com.br", true, "").
		AddRow("Marlos", "marlos.waquim@ferreiracosta.com.br", false, "6710"))
}

func TestEnviarResumo_Sucesso(t *testing.T) {
	db, mock := newSQLMock(t)
	orig := enviarPDFPorEmail
	t.Cleanup(func() { enviarPDFPorEmail = orig })
	var para []string
	var nome, tipo string
	var tam int
	var corpo string
	enviarPDFPorEmail = func(to []string, assunto, c, n, ct string, d []byte) error {
		para, nome, tipo, tam, corpo = to, n, ct, len(d), c
		return nil
	}
	esperaPermissaoTI(mock, true)
	esperaDestinatarios(mock)
	mock.ExpectExec("INSERT INTO ti_envios_resumo").WithArgs(testTIUsuarioID, "claudio.bezerra@ferreiracosta.com.br", "Resumo TI", "Resumo.pdf", 13, true, "").WillReturnResult(sqlmock.NewResult(1, 1))

	corp, ct := formResumo(t, map[string]string{"destinatario": " Claudio.Bezerra@FerreiraCosta.com.br ", "assunto": "Resumo TI", "mensagem": "Olá <b>time</b>\nsegue"}, "Resumo", "%PDF-1.4\nxxxx")
	rec := httptest.NewRecorder()
	TIEnviarResumoHandler(db)(rec, reqTI(http.MethodPost, "/api/ti/enviar-resumo", corp, ct))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d corpo=%s", rec.Code, rec.Body.String())
	}
	if len(para) != 1 || para[0] != "claudio.bezerra@ferreiracosta.com.br" || nome != "Resumo.pdf" || tipo != "application/pdf" || tam != 13 {
		t.Errorf("envio: para=%v nome=%q tipo=%q tam=%d", para, nome, tipo, tam)
	}
	if strings.Contains(corpo, "<b>time</b>") || !strings.Contains(corpo, "&lt;b&gt;time&lt;/b&gt;") {
		t.Errorf("a mensagem digitada deve ir escapada: %s", corpo)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestEnviarResumo_Recusas(t *testing.T) {
	pdf := "%PDF-1.4\nxxxx"
	for _, c := range []struct {
		nome   string
		campos map[string]string
		arq    string
		conte  string
		trecho string
		banco  bool // chega a consultar os destinatários
	}{
		{"destinatário fora do módulo", map[string]string{"destinatario": "estranho@gmail.com"}, "r.pdf", pdf, "não faz parte", true},
		{"não é PDF", map[string]string{"destinatario": "marlos.waquim@ferreiracosta.com.br"}, "r.pdf", "isto não é um pdf", "não é um PDF", true},
		{"sem anexo", map[string]string{"destinatario": "marlos.waquim@ferreiracosta.com.br"}, "", "", "anexe o PDF", true},
		{"assunto com quebra de linha", map[string]string{"destinatario": "marlos.waquim@ferreiracosta.com.br", "assunto": "a\r\nBcc: x@y.com"}, "r.pdf", pdf, "assunto inválido", false},
	} {
		t.Run(c.nome, func(t *testing.T) {
			db, mock := newSQLMock(t)
			orig := enviarPDFPorEmail
			t.Cleanup(func() { enviarPDFPorEmail = orig })
			enviarPDFPorEmail = func([]string, string, string, string, string, []byte) error {
				t.Error("não deveria enviar")
				return nil
			}
			esperaPermissaoTI(mock, true)
			if c.banco {
				esperaDestinatarios(mock)
			}
			corp, ct := formResumo(t, c.campos, c.arq, c.conte)
			rec := httptest.NewRecorder()
			TIEnviarResumoHandler(db)(rec, reqTI(http.MethodPost, "/api/ti/enviar-resumo", corp, ct))
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), c.trecho) {
				t.Fatalf("status=%d corpo=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestEnviarResumo_FalhaDoSMTPEhRegistrada(t *testing.T) {
	db, mock := newSQLMock(t)
	orig := enviarPDFPorEmail
	t.Cleanup(func() { enviarPDFPorEmail = orig })
	enviarPDFPorEmail = func([]string, string, string, string, string, []byte) error { return errors.New("smtp fora") }
	esperaPermissaoTI(mock, true)
	esperaDestinatarios(mock)
	mock.ExpectExec("INSERT INTO ti_envios_resumo").WithArgs(testTIUsuarioID, "marlos.waquim@ferreiracosta.com.br", "Resumo de despesas de TI", "r.pdf", 13, false, "smtp fora").WillReturnResult(sqlmock.NewResult(1, 1))
	corp, ct := formResumo(t, map[string]string{"destinatario": "marlos.waquim@ferreiracosta.com.br"}, "r.pdf", "%PDF-1.4\nxxxx")
	rec := httptest.NewRecorder()
	TIEnviarResumoHandler(db)(rec, reqTI(http.MethodPost, "/api/ti/enviar-resumo", corp, ct))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("esperado 502, obtido %d (%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestEnviarResumo_SoADM(t *testing.T) {
	db, mock := newSQLMock(t)
	esperaPermissaoTI(mock, false) // gestor/sem interruptor
	corp, ct := formResumo(t, map[string]string{"destinatario": "x@y.com"}, "r.pdf", "%PDF-1.4\nxxxx")
	rec := httptest.NewRecorder()
	TIEnviarResumoHandler(db)(rec, reqTI(http.MethodPost, "/api/ti/enviar-resumo", corp, ct))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("esperado 403, obtido %d", rec.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
	var _ sql.DB
}
