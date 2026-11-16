package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
)

const testTIUsuarioID = "77777777-7777-7777-7777-777777777777"

const sqlPermissaoTI = "SELECT controle_ti AND ativo FROM usuarios WHERE id = $1"

func reqTI(method, url string, corpo *bytes.Buffer, contentType string) *http.Request {
	var req *http.Request
	if corpo != nil {
		req = httptest.NewRequest(method, url, corpo)
	} else {
		req = httptest.NewRequest(method, url, nil)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	claims := jwt.MapClaims{"user_id": testTIUsuarioID, "perfil": "solicitante"}
	return req.WithContext(context.WithValue(req.Context(), ClaimsContextKey, claims))
}

func esperaPermissaoTI(mock sqlmock.Sqlmock, liberado bool) {
	mock.ExpectQuery(regexp.QuoteMeta(sqlPermissaoTI)).
		WithArgs(testTIUsuarioID).
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(liberado))
}

func multipartComArquivo(t *testing.T, campo, nome, conteudo string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if campo != "" {
		fw, err := w.CreateFormFile(campo, nome)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write([]byte(conteudo))
	}
	_ = w.Close()
	return &buf, w.FormDataContentType()
}

func TestPermissoesHandler(t *testing.T) {
	for _, c := range []struct {
		nome     string
		liberado bool
		esperado string
	}{
		{"liberado", true, `"controle_ti":true`},
		{"não liberado", false, `"controle_ti":false`},
	} {
		t.Run(c.nome, func(t *testing.T) {
			db, mock := newSQLMock(t)
			esperaPermissaoTI(mock, c.liberado)
			rec := httptest.NewRecorder()
			PermissoesHandler(db)(rec, reqTI(http.MethodGet, "/api/me/permissoes", nil, ""))
			if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), c.esperado) {
				t.Fatalf("status=%d corpo=%s", rec.Code, rec.Body.String())
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPermissoesHandler_UsuarioInexistenteNaoLibera(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectQuery(regexp.QuoteMeta(sqlPermissaoTI)).WithArgs(testTIUsuarioID).WillReturnError(sql.ErrNoRows)
	rec := httptest.NewRecorder()
	PermissoesHandler(db)(rec, reqTI(http.MethodGet, "/api/me/permissoes", nil, ""))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"controle_ti":false`) {
		t.Fatalf("status=%d corpo=%s", rec.Code, rec.Body.String())
	}
}

// Sem o interruptor: 403 e NENHUMA outra consulta (o sqlmock falharia numa
// consulta não esperada, então ExpectationsWereMet + 403 provam que o modelo
// não foi lido).
func TestRotasTI_SemInterruptor_403_ENaoLeNada(t *testing.T) {
	corpo, ct := multipartComArquivo(t, "arquivo", "x.xlsx", "PK")
	for _, c := range []struct {
		nome string
		h    func(*sql.DB) http.HandlerFunc
		req  *http.Request
	}{
		{"modelo", TIModeloHandler, reqTI(http.MethodGet, "/api/ti/modelo", nil, "")},
		{"importar", TIImportarHandler, reqTI(http.MethodPost, "/api/ti/importar", corpo, ct)},
	} {
		t.Run(c.nome, func(t *testing.T) {
			db, mock := newSQLMock(t)
			esperaPermissaoTI(mock, false)
			rec := httptest.NewRecorder()
			c.h(db)(rec, c.req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("esperado 403, obtido %d (%s)", rec.Code, rec.Body.String())
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("o handler tocou no banco além da checagem de permissão: %v", err)
			}
		})
	}
}

func TestTIImportar_Rejeita_SemTocarNoBanco(t *testing.T) {
	semCampo, ctSemCampo := multipartComArquivo(t, "", "", "")
	naoPlanilha, ctNao := multipartComArquivo(t, "arquivo", "dados.xlsx", "isto é só texto, não uma planilha")
	zipQuebrado, ctZip := multipartComArquivo(t, "arquivo", "quebrado.xlsx", "PK\x03\x04lixo")
	for _, c := range []struct {
		nome   string
		corpo  *bytes.Buffer
		ct     string
		trecho string
	}{
		{"sem o campo arquivo", semCampo, ctSemCampo, "arquivo"},
		{"texto puro", naoPlanilha, ctNao, ".xlsx"},
		{"zip quebrado", zipQuebrado, ctZip, "planilha"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			db, mock := newSQLMock(t)
			esperaPermissaoTI(mock, true)
			rec := httptest.NewRecorder()
			TIImportarHandler(db)(rec, reqTI(http.MethodPost, "/api/ti/importar", c.corpo, c.ct))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("esperado 400, obtido %d (%s)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), c.trecho) {
				t.Fatalf("mensagem sem %q: %s", c.trecho, rec.Body.String())
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("nada deveria ter sido gravado: %v", err)
			}
		})
	}
}

func TestTIImportar_NaoMultipart_400(t *testing.T) {
	db, mock := newSQLMock(t)
	esperaPermissaoTI(mock, true)
	rec := httptest.NewRecorder()
	TIImportarHandler(db)(rec, reqTI(http.MethodPost, "/api/ti/importar", bytes.NewBufferString(`{"a":1}`), "application/json"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (%s)", rec.Code, rec.Body.String())
	}
}
