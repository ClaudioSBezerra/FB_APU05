package handlers

// centros_sap_sync_test.go — cobre a I/O Matrix de
// SincronizarCentrosSAPHandler: sem X-API-Key (401, nunca toca o banco),
// API key inválida (401), corpo vazio (400), e linha criada vs. atualizada
// vs. ignorada (contadores corretos) — mesmo padrão de
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

func newCentrosSyncRequest(body, apiKey string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/erp-bridge/centros/sincronizar", strings.NewReader(body))
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	return req.WithContext(context.Background())
}

func TestSincronizarCentrosSAPHandler_SemAPIKey(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := SincronizarCentrosSAPHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newCentrosSyncRequest(`{"centros":[]}`, ""))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nada deveria tocar o banco): %v", err)
	}
}

func TestSincronizarCentrosSAPHandler_APIKeyInvalida(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM erp_bridge_config WHERE api_key_hash")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	handler := SincronizarCentrosSAPHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newCentrosSyncRequest(`{"centros":[{"codigo":"C1","nome":"Centro 1"}]}`, "chave-errada"))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestSincronizarCentrosSAPHandler_CorpoVazio(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM erp_bridge_config WHERE api_key_hash")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	handler := SincronizarCentrosSAPHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newCentrosSyncRequest(`{"centros":[]}`, "chave-certa"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestSincronizarCentrosSAPHandler_CriaAtualizaEIgnora cobre as 3
// classificações possíveis: codigo novo -> criado (xmax=0), codigo já
// existente -> atualizado (xmax<>0), e linha sem codigo/nome -> ignorada
// sem tocar o banco para ela.
func TestSincronizarCentrosSAPHandler_CriaAtualizaEIgnora(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM erp_bridge_config WHERE api_key_hash")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO centros")).
		WithArgs("C1", "Centro Um").
		WillReturnRows(sqlmock.NewRows([]string{"inserida"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO centros")).
		WithArgs("C2", "Centro Dois Atualizado").
		WillReturnRows(sqlmock.NewRows([]string{"inserida"}).AddRow(false))
	mock.ExpectCommit()

	handler := SincronizarCentrosSAPHandler(db)
	rec := httptest.NewRecorder()
	body := `{"centros":[
		{"codigo":"C1","nome":"Centro Um"},
		{"codigo":"C2","nome":"Centro Dois Atualizado"},
		{"codigo":"","nome":"Sem código"}
	]}`
	handler(rec, newCentrosSyncRequest(body, "chave-certa"))

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
