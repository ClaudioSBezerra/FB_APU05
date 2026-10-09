package handlers

// pendencia_test.go — cobre a I/O Matrix da Story 4.2 no nível do handler
// HTTP: pendência com sucesso/admin errado/status incompatível, comentário
// que reabre de pendente/de status "encerrado" simulado/mantém status
// ativo/rejeitado por dono errado/corpo sem texto, e o GET de detalhe por
// dono (solicitante ou administrador) vs. terceiro. Mesmo padrão
// httptest+sqlmock de fila_test.go/admin_test.go.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
)

const (
	testPendenciaSolicitacaoID   = "77777777-7777-7777-7777-777777777777"
	testPendenciaAdministradorID = "88888888-8888-8888-8888-888888888888"
	testPendenciaSolicitanteID   = "99999999-9999-9999-9999-999999999999"
	testPendenciaOutroID         = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
)

// testTimeFixo é um horário fixo e determinístico usado para popular
// `created_at` nas linhas de solicitacao_comentarios simuladas — o teste só
// confere ORDEM/conteúdo, nunca o valor absoluto do timestamp.
var testTimeFixo = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

// newPendenciaRequest monta uma requisição POST/GET com claims de
// `userID`/`perfil` já injetadas no contexto — mesmo padrão de
// newAssumirRequest, parametrizado pelo método/caminho/corpo.
func newPendenciaRequest(method, caminho, id, userID, perfil, corpo string) *http.Request {
	var req *http.Request
	if corpo == "" && method == http.MethodGet {
		req = httptest.NewRequest(method, caminho, nil)
	} else {
		req = httptest.NewRequest(method, caminho, strings.NewReader(corpo))
	}
	req.SetPathValue("id", id)
	claims := jwt.MapClaims{"user_id": userID}
	if perfil != "" {
		claims["perfil"] = perfil
	}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	return req.WithContext(ctx)
}

// --- MarcarPendenciaHandler ---

// TestMarcarPendenciaHandler_Sucesso cobre "Pendência com sucesso": 200,
// status=pendente, versao=N+1, administrador_id inalterado.
func TestMarcarPendenciaHandler_Sucesso(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs(testPendenciaSolicitacaoID, 1, testPendenciaAdministradorID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tipo_solicitacao", "status", "versao", "administrador_id"}).
			AddRow(testPendenciaSolicitacaoID, "transferencia", "pendente", 2, testPendenciaAdministradorID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO solicitacao_comentarios")).
		WithArgs(testPendenciaSolicitacaoID, testPendenciaAdministradorID, "falta nota fiscal").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := MarcarPendenciaHandler(db)
	req := newPendenciaRequest(http.MethodPost, "/api/solicitacoes/"+testPendenciaSolicitacaoID+"/pendencia",
		testPendenciaSolicitacaoID, testPendenciaAdministradorID, "administrador", `{"versao":1,"comentario":"falta nota fiscal"}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if resp["status"] != "pendente" || resp["versao"].(float64) != 2 || resp["administrador_id"] != testPendenciaAdministradorID {
		t.Fatalf("resposta inesperada: %+v", resp)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestMarcarPendenciaHandler_AdminErrado cobre "Pendência por admin
// errado": fila.ErrNaoAutorizado -> 403.
func TestMarcarPendenciaHandler_AdminErrado(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs(testPendenciaSolicitacaoID, 1, testPendenciaAdministradorID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT administrador_id, status, versao FROM solicitacoes WHERE id = $1")).
		WithArgs(testPendenciaSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"administrador_id", "status", "versao"}).
			AddRow(testPendenciaOutroID, "em_atendimento", 1))

	handler := MarcarPendenciaHandler(db)
	req := newPendenciaRequest(http.MethodPost, "/api/solicitacoes/"+testPendenciaSolicitacaoID+"/pendencia",
		testPendenciaSolicitacaoID, testPendenciaAdministradorID, "administrador", `{"versao":1,"comentario":"texto"}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestMarcarPendenciaHandler_StatusIncompativel cobre "Pendência com
// status/versão incompatível": 409 conflito_versao com versao_atual.
func TestMarcarPendenciaHandler_StatusIncompativel(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs(testPendenciaSolicitacaoID, 1, testPendenciaAdministradorID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT administrador_id, status, versao FROM solicitacoes WHERE id = $1")).
		WithArgs(testPendenciaSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"administrador_id", "status", "versao"}).
			AddRow(testPendenciaAdministradorID, "aberta", 3))

	handler := MarcarPendenciaHandler(db)
	req := newPendenciaRequest(http.MethodPost, "/api/solicitacoes/"+testPendenciaSolicitacaoID+"/pendencia",
		testPendenciaSolicitacaoID, testPendenciaAdministradorID, "administrador", `{"versao":1,"comentario":"texto"}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, esperado 409 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Erro struct {
			Codigo      string `json:"codigo"`
			VersaoAtual int    `json:"versao_atual"`
		} `json:"erro"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if resp.Erro.Codigo != "conflito_versao" || resp.Erro.VersaoAtual != 3 {
		t.Fatalf("resposta inesperada: %+v", resp)
	}
}

// TestMarcarPendenciaHandler_NaoEncontrada cobre a solicitação inexistente
// -> 404.
func TestMarcarPendenciaHandler_NaoEncontrada(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs(testPendenciaSolicitacaoID, 1, testPendenciaAdministradorID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT administrador_id, status, versao FROM solicitacoes WHERE id = $1")).
		WithArgs(testPendenciaSolicitacaoID).
		WillReturnError(sql.ErrNoRows)

	handler := MarcarPendenciaHandler(db)
	req := newPendenciaRequest(http.MethodPost, "/api/solicitacoes/"+testPendenciaSolicitacaoID+"/pendencia",
		testPendenciaSolicitacaoID, testPendenciaAdministradorID, "administrador", `{"versao":1,"comentario":"texto"}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, esperado 404 (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestMarcarPendenciaHandler_IDFormatoInvalido garante o mesmo 404 (nunca
// 400) para um {id} malformado, sem tocar o banco.
func TestMarcarPendenciaHandler_IDFormatoInvalido(t *testing.T) {
	db, _ := newSQLMock(t)

	handler := MarcarPendenciaHandler(db)
	req := newPendenciaRequest(http.MethodPost, "/api/solicitacoes/nao-e-uuid/pendencia",
		"nao-e-uuid", testPendenciaAdministradorID, "administrador", `{"versao":1,"comentario":"texto"}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, esperado 404 (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestMarcarPendenciaHandler_VersaoAusente cobre o corpo sem `versao` -> 400,
// nunca toca o banco.
func TestMarcarPendenciaHandler_VersaoAusente(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := MarcarPendenciaHandler(db)
	req := newPendenciaRequest(http.MethodPost, "/api/solicitacoes/"+testPendenciaSolicitacaoID+"/pendencia",
		testPendenciaSolicitacaoID, testPendenciaAdministradorID, "administrador", `{"comentario":"texto"}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nada deveria tocar o banco): %v", err)
	}
}

// TestMarcarPendenciaHandler_ComentarioVazio cobre "Comentário sem texto
// (vazio/whitespace)" da I/O Matrix -> 400, nunca toca o banco.
func TestMarcarPendenciaHandler_ComentarioVazio(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := MarcarPendenciaHandler(db)
	req := newPendenciaRequest(http.MethodPost, "/api/solicitacoes/"+testPendenciaSolicitacaoID+"/pendencia",
		testPendenciaSolicitacaoID, testPendenciaAdministradorID, "administrador", `{"versao":1,"comentario":"   "}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nada deveria tocar o banco): %v", err)
	}
}

// --- ComentarSolicitacaoHandler ---

// TestComentarSolicitacaoHandler_ReabreDePendente cobre "Comentário reabre
// de pendente": 200, status=em_atendimento, versao=N+1.
func TestComentarSolicitacaoHandler_ReabreDePendente(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs(testPendenciaSolicitacaoID, 1, testPendenciaSolicitanteID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tipo_solicitacao", "status", "versao", "administrador_id"}).
			AddRow(testPendenciaSolicitacaoID, "transferencia", "em_atendimento", 2, testPendenciaAdministradorID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO solicitacao_comentarios")).
		WithArgs(testPendenciaSolicitacaoID, testPendenciaSolicitanteID, "ja resolvi").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := ComentarSolicitacaoHandler(db)
	req := newPendenciaRequest(http.MethodPost, "/api/solicitacoes/"+testPendenciaSolicitacaoID+"/comentarios",
		testPendenciaSolicitacaoID, testPendenciaSolicitanteID, "solicitante", `{"versao":1,"comentario":"ja resolvi"}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if resp["status"] != "em_atendimento" || resp["versao"].(float64) != 2 {
		t.Fatalf("resposta inesperada: %+v", resp)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestComentarSolicitacaoHandler_ReabreDeEncerradoSimulado cobre
// "Comentário reabre de status 'encerrado' simulado": o mock devolve
// status=em_atendimento (o CASE já resolvido no banco) a partir de um
// status fora de {aberta, em_atendimento} — aqui só confirmamos que o
// handler aceita e devolve o resultado tal como veio, sem reinterpretar
// nada (a decisão é 100% de internal/fila.Comentar, já testada em
// internal/fila/fila_test.go).
func TestComentarSolicitacaoHandler_ReabreDeEncerradoSimulado(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs(testPendenciaSolicitacaoID, 5, testPendenciaSolicitanteID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tipo_solicitacao", "status", "versao", "administrador_id"}).
			AddRow(testPendenciaSolicitacaoID, "transferencia", "em_atendimento", 6, testPendenciaAdministradorID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO solicitacao_comentarios")).
		WithArgs(testPendenciaSolicitacaoID, testPendenciaSolicitanteID, "reabrindo").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := ComentarSolicitacaoHandler(db)
	req := newPendenciaRequest(http.MethodPost, "/api/solicitacoes/"+testPendenciaSolicitacaoID+"/comentarios",
		testPendenciaSolicitacaoID, testPendenciaSolicitanteID, "solicitante", `{"versao":5,"comentario":"reabrindo"}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if resp["status"] != "em_atendimento" || resp["versao"].(float64) != 6 {
		t.Fatalf("resposta inesperada: %+v", resp)
	}
}

// TestComentarSolicitacaoHandler_SolicitanteErrado cobre "Comentário por
// quem não é o solicitante dono" -> 403.
func TestComentarSolicitacaoHandler_SolicitanteErrado(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs(testPendenciaSolicitacaoID, 1, testPendenciaSolicitanteID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT solicitante_id, versao FROM solicitacoes WHERE id = $1")).
		WithArgs(testPendenciaSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"solicitante_id", "versao"}).AddRow(testPendenciaOutroID, 1))

	handler := ComentarSolicitacaoHandler(db)
	req := newPendenciaRequest(http.MethodPost, "/api/solicitacoes/"+testPendenciaSolicitacaoID+"/comentarios",
		testPendenciaSolicitacaoID, testPendenciaSolicitanteID, "solicitante", `{"versao":1,"comentario":"texto"}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestComentarSolicitacaoHandler_ComentarioVazio cobre o corpo
// `{"versao":N,"comentario":"  "}` -> 400, nunca toca o banco.
func TestComentarSolicitacaoHandler_ComentarioVazio(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := ComentarSolicitacaoHandler(db)
	req := newPendenciaRequest(http.MethodPost, "/api/solicitacoes/"+testPendenciaSolicitacaoID+"/comentarios",
		testPendenciaSolicitacaoID, testPendenciaSolicitanteID, "solicitante", `{"versao":1,"comentario":"  "}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nada deveria tocar o banco): %v", err)
	}
}

// TestComentarSolicitacaoHandler_NaoEncontrada cobre a solicitação
// inexistente -> 404.
func TestComentarSolicitacaoHandler_NaoEncontrada(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs(testPendenciaSolicitacaoID, 1, testPendenciaSolicitanteID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT solicitante_id, versao FROM solicitacoes WHERE id = $1")).
		WithArgs(testPendenciaSolicitacaoID).
		WillReturnError(sql.ErrNoRows)

	handler := ComentarSolicitacaoHandler(db)
	req := newPendenciaRequest(http.MethodPost, "/api/solicitacoes/"+testPendenciaSolicitacaoID+"/comentarios",
		testPendenciaSolicitacaoID, testPendenciaSolicitanteID, "solicitante", `{"versao":1,"comentario":"texto"}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, esperado 404 (body=%s)", rec.Code, rec.Body.String())
	}
}

// --- ObterSolicitacaoHandler ---

// TestObterSolicitacaoHandler_PorSolicitanteDono cobre "GET detalhe por
// dono (solicitante)": 200 com o histórico ordenado por created_at ASC.
func TestObterSolicitacaoHandler_PorSolicitanteDono(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT tipo_solicitacao, status, versao, solicitante_id, administrador_id")).
		WithArgs(testPendenciaSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "status", "versao", "solicitante_id", "administrador_id"}).
			AddRow("transferencia", "pendente", 2, testPendenciaSolicitanteID, testPendenciaAdministradorID))
	mock.ExpectQuery(regexp.QuoteMeta("FROM solicitacao_comentarios")).
		WithArgs(testPendenciaSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "autor_id", "tipo", "texto", "created_at"}).
			AddRow("c1111111-1111-1111-1111-111111111111", testPendenciaAdministradorID, "pendencia", "falta nota", testTimeFixo).
			AddRow("c2222222-2222-2222-2222-222222222222", testPendenciaSolicitanteID, "comentario", "anexei", testTimeFixo.Add(1)))

	handler := ObterSolicitacaoHandler(db)
	req := newPendenciaRequest(http.MethodGet, "/api/solicitacoes/"+testPendenciaSolicitacaoID,
		testPendenciaSolicitacaoID, testPendenciaSolicitanteID, "solicitante", "")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID              string                   `json:"id"`
		Status          string                   `json:"status"`
		Versao          int                      `json:"versao"`
		SolicitanteID   string                   `json:"solicitante_id"`
		AdministradorID string                   `json:"administrador_id"`
		Comentarios     []map[string]interface{} `json:"comentarios"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if resp.Status != "pendente" || resp.Versao != 2 || resp.SolicitanteID != testPendenciaSolicitanteID {
		t.Fatalf("resposta inesperada: %+v", resp)
	}
	if len(resp.Comentarios) != 2 {
		t.Fatalf("len(comentarios) = %d, esperado 2", len(resp.Comentarios))
	}
	if resp.Comentarios[0]["tipo"] != "pendencia" || resp.Comentarios[1]["tipo"] != "comentario" {
		t.Fatalf("ordem/tipo dos comentários inesperada: %+v", resp.Comentarios)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestObterSolicitacaoHandler_PorAdministradorDono cobre "GET detalhe por
// dono (administrador)": 200, mesmo quando o chamador não é o solicitante.
func TestObterSolicitacaoHandler_PorAdministradorDono(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT tipo_solicitacao, status, versao, solicitante_id, administrador_id")).
		WithArgs(testPendenciaSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "status", "versao", "solicitante_id", "administrador_id"}).
			AddRow("transferencia", "em_atendimento", 1, testPendenciaSolicitanteID, testPendenciaAdministradorID))
	mock.ExpectQuery(regexp.QuoteMeta("FROM solicitacao_comentarios")).
		WithArgs(testPendenciaSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "autor_id", "tipo", "texto", "created_at"}))

	handler := ObterSolicitacaoHandler(db)
	req := newPendenciaRequest(http.MethodGet, "/api/solicitacoes/"+testPendenciaSolicitacaoID,
		testPendenciaSolicitacaoID, testPendenciaAdministradorID, "administrador", "")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestObterSolicitacaoHandler_Terceiro cobre "GET detalhe por terceiro" ->
// 403, nunca chega a consultar o histórico de comentários.
func TestObterSolicitacaoHandler_Terceiro(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT tipo_solicitacao, status, versao, solicitante_id, administrador_id")).
		WithArgs(testPendenciaSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "status", "versao", "solicitante_id", "administrador_id"}).
			AddRow("transferencia", "em_atendimento", 1, testPendenciaSolicitanteID, testPendenciaAdministradorID))

	handler := ObterSolicitacaoHandler(db)
	req := newPendenciaRequest(http.MethodGet, "/api/solicitacoes/"+testPendenciaSolicitacaoID,
		testPendenciaSolicitacaoID, testPendenciaOutroID, "solicitante", "")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, esperado 403 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nunca deveria buscar comentários): %v", err)
	}
}

// TestObterSolicitacaoHandler_NaoEncontrada cobre o id inexistente -> 404.
func TestObterSolicitacaoHandler_NaoEncontrada(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT tipo_solicitacao, status, versao, solicitante_id, administrador_id")).
		WithArgs(testPendenciaSolicitacaoID).
		WillReturnError(sql.ErrNoRows)

	handler := ObterSolicitacaoHandler(db)
	req := newPendenciaRequest(http.MethodGet, "/api/solicitacoes/"+testPendenciaSolicitacaoID,
		testPendenciaSolicitacaoID, testPendenciaSolicitanteID, "solicitante", "")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, esperado 404 (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestObterSolicitacaoHandler_IDFormatoInvalido garante o mesmo 404 (nunca
// 400) para um {id} malformado, sem tocar o banco.
func TestObterSolicitacaoHandler_IDFormatoInvalido(t *testing.T) {
	db, _ := newSQLMock(t)

	handler := ObterSolicitacaoHandler(db)
	req := newPendenciaRequest(http.MethodGet, "/api/solicitacoes/nao-e-uuid",
		"nao-e-uuid", testPendenciaSolicitanteID, "solicitante", "")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, esperado 404 (body=%s)", rec.Code, rec.Body.String())
	}
}
