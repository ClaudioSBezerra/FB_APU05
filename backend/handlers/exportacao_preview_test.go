package handlers

// exportacao_preview_test.go — cobre PreviewLoteDespesaCSVHandler/
// PreviewLoteObraCSVHandler no nível HTTP: corpo inválido, id malformado,
// e o caminho feliz devolvendo um CSV com Content-Type/Content-Disposition
// corretos e as linhas esperadas.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
)

const testSolicitacaoPreviewID = "11111111-1111-1111-1111-111111111111"

func newPreviewLoteRequest(path, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	claims := jwt.MapClaims{"user_id": testAtorID, "perfil": "administrador"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	return req.WithContext(ctx)
}

func TestPreviewLoteDespesaCSVHandler_RejectsWrongMethod(t *testing.T) {
	handler := PreviewLoteDespesaCSVHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/solicitacoes/lote-despesa/csv", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("esperado 405, obtido %d", rec.Code)
	}
}

func TestPreviewLoteDespesaCSVHandler_RejectsEmptyIDs(t *testing.T) {
	handler := PreviewLoteDespesaCSVHandler(nil)
	rec := httptest.NewRecorder()
	handler(rec, newPreviewLoteRequest("/api/solicitacoes/lote-despesa/csv", `{"solicitacao_ids":[]}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestPreviewLoteDespesaCSVHandler_RejectsInvalidID(t *testing.T) {
	handler := PreviewLoteDespesaCSVHandler(nil)
	rec := httptest.NewRecorder()
	handler(rec, newPreviewLoteRequest("/api/solicitacoes/lote-despesa/csv", `{"solicitacao_ids":["nao-e-um-uuid"]}`))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
}

func TestPreviewLoteDespesaCSVHandler_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, s.status, s.administrador_id, u.nome")).
		WithArgs(testSolicitacaoPreviewID).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "status", "administrador_id", "nome"}).
			AddRow("transferencia", "em_atendimento", testAtorID, "Fulano de Tal"))
	mock.ExpectQuery(regexp.QuoteMeta("FROM solicitacao_lancamentos sl")).
		WithArgs(testSolicitacaoPreviewID).
		WillReturnRows(sqlmock.NewRows([]string{"lado", "divisao_codigo", "cc_codigo", "conta_plano", "conta_codigo", "classe_codigo", "mes", "valor"}).
			AddRow("origem", "D1", "CC1", "BIFC", "1000", nil, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), 1000.0))

	handler := PreviewLoteDespesaCSVHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newPreviewLoteRequest("/api/solicitacoes/lote-despesa/csv", `{"solicitacao_ids":["`+testSolicitacaoPreviewID+`"]}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Fatalf("Content-Type inesperado: %s", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "preview_lote_despesa.csv") {
		t.Fatalf("Content-Disposition inesperado: %s", cd)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "transferencia") || !strings.Contains(body, "BIFC-1000") || !strings.Contains(body, "1000.00") {
		t.Fatalf("corpo CSV não contém os dados esperados: %s", body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestPreviewLoteObraCSVHandler_RejectsWrongMethod(t *testing.T) {
	handler := PreviewLoteObraCSVHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/solicitacoes/lote-obra/csv", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("esperado 405, obtido %d", rec.Code)
	}
}

func TestPreviewLoteObraCSVHandler_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, s.status, s.administrador_id, s.classificacao, u.nome")).
		WithArgs(testSolicitacaoPreviewID).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "status", "administrador_id", "classificacao", "nome"}).
			AddRow("obras", "em_atendimento", testAtorID, "inclusao", "Fulano de Tal"))
	mock.ExpectQuery(regexp.QuoteMeta("FROM solicitacao_obras_linhas sol")).
		WithArgs(testSolicitacaoPreviewID).
		WillReturnRows(sqlmock.NewRows([]string{"lado", "local_obra_codigo", "subgrupo_codigo", "ordem_investimento", "valor"}).
			AddRow(nil, "LO1", "SD1", "9999", 200.0))

	handler := PreviewLoteObraCSVHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newPreviewLoteRequest("/api/solicitacoes/lote-obra/csv", `{"solicitacao_ids":["`+testSolicitacaoPreviewID+`"]}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "9999") || !strings.Contains(body, "200.00") {
		t.Fatalf("corpo CSV não contém os dados esperados: %s", body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}
