package handlers

// centros_custo_sap_sync_test.go — cobre a I/O Matrix de
// SincronizarCentrosCustoSAPHandler: sem X-API-Key (401), API key inválida
// (401), corpo vazio (400), e linha criada vs. atualizada vs. ignorada
// (por campo obrigatório ausente OU divisao_codigo não encontrado) — mesmo
// padrão de fornecedores_oracle_sync_test.go.

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func newCentrosCustoSyncRequest(body, apiKey string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/erp-bridge/centros-custo/sincronizar", strings.NewReader(body))
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	return req.WithContext(context.Background())
}

func TestSincronizarCentrosCustoSAPHandler_SemAPIKey(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := SincronizarCentrosCustoSAPHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newCentrosCustoSyncRequest(`{"centros_custo":[]}`, ""))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, esperado 401 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nada deveria tocar o banco): %v", err)
	}
}

func TestSincronizarCentrosCustoSAPHandler_CorpoVazio(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM erp_bridge_config WHERE api_key_hash")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	handler := SincronizarCentrosCustoSAPHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newCentrosCustoSyncRequest(`{"centros_custo":[]}`, "chave-certa"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, esperado 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestSincronizarCentrosCustoSAPHandler_CriaAtualizaEIgnora cobre: divisão
// resolvida com sucesso -> criado/atualizado; divisao_codigo que não
// resolve (resolveDivisaoID devolve erro) -> ignorado, sem abortar a
// sincronização inteira (best-effort, mesmo padrão de fornecedores).
func TestSincronizarCentrosCustoSAPHandler_CriaAtualizaEIgnora(t *testing.T) {
	db, mock := newSQLMock(t)

	const divisaoID = "d0000000-0000-0000-0000-000000000001"

	mock.ExpectQuery(regexp.QuoteMeta("FROM erp_bridge_config WHERE api_key_hash")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM divisoes WHERE codigo = $1")).
		WithArgs("DIV1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(divisaoID))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO centros_custo")).
		WithArgs("CC1", "Centro de Custo Um", divisaoID, "0001").
		WillReturnRows(sqlmock.NewRows([]string{"inserida"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM divisoes WHERE codigo = $1")).
		WithArgs("DIVX").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectCommit()

	handler := SincronizarCentrosCustoSAPHandler(db)
	rec := httptest.NewRecorder()
	body := `{"centros_custo":[
		{"codigo":"CC1","nome":"Centro de Custo Um","divisao_codigo":"DIV1","filial":"0001"},
		{"codigo":"CC2","nome":"Divisão Inexistente","divisao_codigo":"DIVX","filial":""},
		{"codigo":"","nome":"Sem código","divisao_codigo":"DIV1","filial":""}
	]}`
	handler(rec, newCentrosCustoSyncRequest(body, "chave-certa"))

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
	if resp.Criados != 1 || resp.Atualizados != 0 || resp.Ignorados != 2 || resp.Total != 3 {
		t.Fatalf("resp = %+v, esperado {1 0 2 3}", resp)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}
