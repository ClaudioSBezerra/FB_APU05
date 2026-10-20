package handlers

// fornecedores_oracle_sync_test.go — cobre a I/O Matrix de
// SincronizarFornecedoresOracleHandler: sem X-API-Key (401, nunca toca o
// banco), API key inválida (401), corpo vazio (400), e linha criada vs.
// atualizada vs. ignorada (contadores corretos) — mesmo padrão de
// colaboradores_oracle_sync_test.go.

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

func newFornecedoresSyncRequest(body, apiKey string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/erp-bridge/fornecedores/sincronizar", strings.NewReader(body))
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	return req.WithContext(context.Background())
}

func TestSincronizarFornecedoresOracleHandler_SemAPIKey(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := SincronizarFornecedoresOracleHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newFornecedoresSyncRequest(`{"fornecedores":[]}`, ""))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nada deveria tocar o banco): %v", err)
	}
}

func TestSincronizarFornecedoresOracleHandler_APIKeyInvalida(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM erp_bridge_config WHERE api_key_hash")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	handler := SincronizarFornecedoresOracleHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newFornecedoresSyncRequest(`{"fornecedores":[{"cgc":"11222333000181","razsoc":"Fulano Ltda","fantas":"Fulano","uf":"SP"}]}`, "chave-errada"))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestSincronizarFornecedoresOracleHandler_CorpoVazio(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM erp_bridge_config WHERE api_key_hash")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	handler := SincronizarFornecedoresOracleHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newFornecedoresSyncRequest(`{"fornecedores":[]}`, "chave-certa"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestSincronizarFornecedoresOracleHandler_CriaAtualizaEIgnora cobre as 3
// classificações possíveis: cgc novo -> criado (xmax=0 na linha devolvida),
// cgc já existente -> atualizado (xmax<>0), e linha sem cgc/razsoc ->
// ignorada sem tocar o banco para ela.
func TestSincronizarFornecedoresOracleHandler_CriaAtualizaEIgnora(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM erp_bridge_config WHERE api_key_hash")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO fornecedores")).
		WithArgs("11222333000181", "Fulano Ltda", "Fulano", "SP").
		WillReturnRows(sqlmock.NewRows([]string{"inserida"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO fornecedores")).
		WithArgs("99888777000166", "Ciclano S.A.", nil, "RJ").
		WillReturnRows(sqlmock.NewRows([]string{"inserida"}).AddRow(false))
	mock.ExpectCommit()

	handler := SincronizarFornecedoresOracleHandler(db)
	rec := httptest.NewRecorder()
	body := `{"fornecedores":[
		{"cgc":"11222333000181","razsoc":"Fulano Ltda","fantas":"Fulano","uf":"SP"},
		{"cgc":"99888777000166","razsoc":"Ciclano S.A.","fantas":"","uf":"RJ"},
		{"cgc":"","razsoc":"Sem CGC Ltda","fantas":"","uf":""}
	]}`
	handler(rec, newFornecedoresSyncRequest(body, "chave-certa"))

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
