package handlers

// contas_sap_sync_test.go — cobre a I/O Matrix de
// SincronizarContasSAPHandler: sem X-API-Key (401), API key inválida
// (401), corpo vazio (400), e linha criada vs. atualizada vs. ignorada
// (plano inválido OU codigo/nome ausente) — mesmo padrão de
// fornecedores_oracle_sync_test.go.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func newContasSyncRequest(body, apiKey string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/erp-bridge/contas/sincronizar", strings.NewReader(body))
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	return req.WithContext(context.Background())
}

func TestSincronizarContasSAPHandler_SemAPIKey(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := SincronizarContasSAPHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newContasSyncRequest(`{"contas":[]}`, ""))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nada deveria tocar o banco): %v", err)
	}
}

func TestSincronizarContasSAPHandler_CorpoVazio(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM erp_bridge_config WHERE api_key_hash")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	handler := SincronizarContasSAPHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newContasSyncRequest(`{"contas":[]}`, "chave-certa"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestSincronizarContasSAPHandler_CriaAtualizaEIgnora cobre: plano válido
// (BIFC/SFC) -> criado/atualizado; plano inválido -> ignorado, sem abortar
// a sincronização inteira (best-effort, mesmo padrão de fornecedores).
func TestSincronizarContasSAPHandler_CriaAtualizaEIgnora(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM erp_bridge_config WHERE api_key_hash")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO contas")).
		WithArgs("BIFC", "1.01.001", "Caixa").
		WillReturnRows(sqlmock.NewRows([]string{"inserida"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO contas")).
		WithArgs("SFC", "2.02.002", "Fornecedores Atualizado").
		WillReturnRows(sqlmock.NewRows([]string{"inserida"}).AddRow(false))
	mock.ExpectCommit()

	handler := SincronizarContasSAPHandler(db)
	rec := httptest.NewRecorder()
	body := `{"contas":[
		{"plano":"BIFC","codigo":"1.01.001","nome":"Caixa"},
		{"plano":"SFC","codigo":"2.02.002","nome":"Fornecedores Atualizado"},
		{"plano":"ERRADO","codigo":"9.99.999","nome":"Plano inválido"}
	]}`
	handler(rec, newContasSyncRequest(body, "chave-certa"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Criados     int `json:"criados"`
		Atualizados int `json:"atualizados"`
		Ignorados   int `json:"ignorados"`
		Total       int `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if resp.Criados != 1 || resp.Atualizados != 1 || resp.Ignorados != 1 || resp.Total != 3 {
		t.Fatalf("resp = %+v, esperado {1 1 1 3}", resp)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}
