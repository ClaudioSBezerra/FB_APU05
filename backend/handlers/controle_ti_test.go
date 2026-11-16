package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
	"github.com/xuri/excelize/v2"
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

// ---- importação em lote (Forecast + KSB1 + KSB2) ----

func xlsxBytes(t *testing.T, linhas [][]any) []byte {
	t.Helper()
	f := excelize.NewFile()
	for i, linha := range linhas {
		for j, v := range linha {
			cel, _ := excelize.CoordinatesToCellName(j+1, i+1)
			if err := f.SetCellValue("Sheet1", cel, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func multipartVarios(t *testing.T, arquivos map[string][]byte, ordem []string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, nome := range ordem {
		fw, err := w.CreateFormFile("arquivo", nome)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(arquivos[nome])
	}
	_ = w.Close()
	return &buf, w.FormDataContentType()
}

func ksb2Valida(t *testing.T) []byte {
	return xlsxBytes(t, [][]any{
		{"Classe de custo", "Descr.classe custo", "Centro custo", "Valor/MR", "Quantidade total", "Nº doc.de referência", "Data do documento", "Item referência", "Fornecedor", "Texto breve material"},
		{"4220300006", "ALUGUEL", "10026070", 1026.63, 1, "4522289396", "2026-01-21", "10", "", "LOCACAO"},
		{nil, nil, nil, 1026.63, nil, "4522289396", nil, nil, nil, nil}, // linha de total: descartada
	})
}

type respImportacao struct {
	Resultados []struct {
		Arquivo string `json:"arquivo"`
		Tipo    string `json:"tipo"`
		Ok      bool   `json:"ok"`
		Erro    string `json:"erro"`
	} `json:"resultados"`
	Error string `json:"error"`
}

func TestTIImportar_ArquivoNaoReconhecido(t *testing.T) {
	desconhecido := xlsxBytes(t, [][]any{{"A", "B"}, {1, 2}})
	corpo, ct := multipartVarios(t, map[string][]byte{"qualquer.xlsx": desconhecido}, []string{"qualquer.xlsx"})
	db, mock := newSQLMock(t)
	esperaPermissaoTI(mock, true)
	rec := httptest.NewRecorder()
	TIImportarHandler(db)(rec, reqTI(http.MethodPost, "/api/ti/importar", corpo, ct))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (%s)", rec.Code, rec.Body.String())
	}
	var r respImportacao
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if len(r.Resultados) != 1 || r.Resultados[0].Ok || !strings.Contains(r.Resultados[0].Erro, "não reconhecido") {
		t.Fatalf("resultado: %+v", r)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("nada deveria ser gravado: %v", err)
	}
}

func TestTIImportar_Ksb2ValidaJuntoDeArquivoRuim_UmNaoImpedeOOutro(t *testing.T) {
	corpo, ct := multipartVarios(t,
		map[string][]byte{"ruim.xlsx": []byte("isto não é planilha"), "ksb2.xlsx": ksb2Valida(t)},
		[]string{"ruim.xlsx", "ksb2.xlsx"})
	db, mock := newSQLMock(t)
	esperaPermissaoTI(mock, true)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO ti_cargas_sap")).
		WithArgs("ksb2.xlsx", 2, 1, testTIUsuarioID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(7)))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM ti_ksb2")).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO ti_ksb2")).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	rec := httptest.NewRecorder()
	TIImportarHandler(db)(rec, reqTI(http.MethodPost, "/api/ti/importar", corpo, ct))
	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200 (um arquivo entrou), obtido %d (%s)", rec.Code, rec.Body.String())
	}
	var r respImportacao
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Resultados) != 2 {
		t.Fatalf("resultados: %+v", r)
	}
	for _, x := range r.Resultados {
		switch x.Arquivo {
		case "ruim.xlsx":
			if x.Ok || x.Erro == "" {
				t.Errorf("arquivo ruim deveria falhar com a causa: %+v", x)
			}
		case "ksb2.xlsx":
			if !x.Ok || x.Tipo != "ksb2" {
				t.Errorf("KSB2 válida deveria entrar: %+v", x)
			}
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("a transação da KSB2 não aconteceu como esperado: %v", err)
	}
}

func TestTIImportar_ErroDeBancoNaoVazaDetalhe(t *testing.T) {
	corpo, ct := multipartVarios(t, map[string][]byte{"ksb2.xlsx": ksb2Valida(t)}, []string{"ksb2.xlsx"})
	db, mock := newSQLMock(t)
	esperaPermissaoTI(mock, true)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO ti_cargas_sap")).WillReturnError(errors.New("pq: relation \"ti_cargas_sap\" does not exist (senha=segredo)"))
	mock.ExpectRollback()
	rec := httptest.NewRecorder()
	TIImportarHandler(db)(rec, reqTI(http.MethodPost, "/api/ti/importar", corpo, ct))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("todos falharam: esperado 400, obtido %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "segredo") || strings.Contains(rec.Body.String(), "relation") {
		t.Fatalf("detalhe de banco vazou para a resposta: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "nada foi alterado") {
		t.Errorf("mensagem genérica esperada: %s", rec.Body.String())
	}
}

func TestTIImportar_LimiteDeArquivos(t *testing.T) {
	arqs := map[string][]byte{}
	var ordem []string
	for i := 0; i < limiteArquivosImportacao+1; i++ {
		n := fmt.Sprintf("a%d.xlsx", i)
		arqs[n] = []byte("x")
		ordem = append(ordem, n)
	}
	corpo, ct := multipartVarios(t, arqs, ordem)
	db, mock := newSQLMock(t)
	esperaPermissaoTI(mock, true)
	rec := httptest.NewRecorder()
	TIImportarHandler(db)(rec, reqTI(http.MethodPost, "/api/ti/importar", corpo, ct))
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "no máximo") {
		t.Fatalf("status=%d corpo=%s", rec.Code, rec.Body.String())
	}
}

func TestTIConferencia_SemInterruptor_403(t *testing.T) {
	db, mock := newSQLMock(t)
	esperaPermissaoTI(mock, false)
	rec := httptest.NewRecorder()
	TIConferenciaHandler(db)(rec, reqTI(http.MethodGet, "/api/ti/conferencia", nil, ""))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("esperado 403, obtido %d", rec.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("a conferência leu dados sem permissão: %v", err)
	}
}
