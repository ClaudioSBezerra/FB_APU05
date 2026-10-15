package handlers

// colaboradores_oracle_sync_test.go — cobre a I/O Matrix de
// SincronizarColaboradoresOracleHandler: sem X-API-Key (401, nunca toca o
// banco), API key inválida (401), linha atualizada vs. ignorada (contadores
// corretos), e corpo vazio (400).

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

func newColaboradoresSyncRequest(body, apiKey string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/erp-bridge/colaboradores/sincronizar", strings.NewReader(body))
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	return req.WithContext(context.Background())
}

func TestSincronizarColaboradoresOracleHandler_SemAPIKey(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := SincronizarColaboradoresOracleHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newColaboradoresSyncRequest(`{"colaboradores":[]}`, ""))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nada deveria tocar o banco): %v", err)
	}
}

func TestSincronizarColaboradoresOracleHandler_APIKeyInvalida(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM erp_bridge_config WHERE api_key_hash")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	handler := SincronizarColaboradoresOracleHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newColaboradoresSyncRequest(`{"colaboradores":[{"cod_empresa":1,"centro_custo":"1000","numcad":123,"nomfun":"Fulano"}]}`, "chave-errada"))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestSincronizarColaboradoresOracleHandler_CorpoVazio(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM erp_bridge_config WHERE api_key_hash")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	handler := SincronizarColaboradoresOracleHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newColaboradoresSyncRequest(`{"colaboradores":[]}`, "chave-certa"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestSincronizarColaboradoresOracleHandler_AtualizaEIgnora(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM erp_bridge_config WHERE api_key_hash")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	mock.ExpectBegin()
	// Linha 1: casa com um usuário e CC existentes — 1 linha afetada.
	mock.ExpectExec(regexp.QuoteMeta("UPDATE usuarios u")).
		WithArgs(1, 12345, "1000").
		WillReturnResult(sqlmock.NewResult(0, 1))
	// Linha 2: ninguém com esse (cod_empresa, numcad) — 0 linhas afetadas.
	mock.ExpectExec(regexp.QuoteMeta("UPDATE usuarios u")).
		WithArgs(1, 99999, "2000").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	handler := SincronizarColaboradoresOracleHandler(db)
	rec := httptest.NewRecorder()
	body := `{"colaboradores":[
		{"cod_empresa":1,"centro_custo":"1000","numcad":12345,"nomfun":"Fulano de Tal"},
		{"cod_empresa":1,"centro_custo":"2000","numcad":99999,"nomfun":"Ciclano de Tal"}
	]}`
	handler(rec, newColaboradoresSyncRequest(body, "chave-certa"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Atualizados int `json:"atualizados"`
		Ignorados   int `json:"ignorados"`
		Total       int `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if resp.Atualizados != 1 || resp.Ignorados != 1 || resp.Total != 2 {
		t.Fatalf("resp = %+v, esperado {1 1 2}", resp)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}
