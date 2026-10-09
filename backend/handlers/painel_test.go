package handlers

// painel_test.go — cobre a I/O Matrix da Story 5.1 no nível do handler
// HTTP: painel consolidado com dados (200, agrupado por dimensao), painel
// consolidado sem dados (200, 3 arrays vazios, gerado_em nil), painel
// desconhecido (404). Mesmo padrão httptest+sqlmock de fila_test.go;
// autenticação em si (401) é responsabilidade do middleware RequireAuth
// (middleware_test.go), não deste handler.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
)

// newPainelRequest monta GET /api/paineis/{painel} com claims de um usuário
// autenticado qualquer (perfil "solicitante") — a rota é aberta a qualquer
// perfil (Boundaries "Always" da spec), diferente de ListarFilaHandler.
func newPainelRequest(painel string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/paineis/"+painel, nil)
	req.SetPathValue("painel", painel)
	claims := jwt.MapClaims{"user_id": "55555555-5555-5555-5555-555555555555", "perfil": "solicitante"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	return req.WithContext(ctx)
}

type painelResponse struct {
	PorTipo   []painelItem `json:"por_tipo"`
	PorStatus []painelItem `json:"por_status"`
	PorCC     []painelItem `json:"por_cc"`
	GeradoEm  *time.Time   `json:"gerado_em"`
}

// TestObterPainelHandler_ComDados cobre "Painel consolidado com dados" da
// I/O Matrix: linhas nas 3 dimensões agrupadas corretamente, gerado_em = o
// mais recente entre as linhas (não a última lida).
func TestObterPainelHandler_ComDados(t *testing.T) {
	db, mock := newSQLMock(t)

	maisAntigo := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	maisRecente := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta("FROM painel_snapshots")).
		WillReturnRows(sqlmock.NewRows([]string{"dimensao", "chave", "rotulo", "quantidade", "gerado_em"}).
			AddRow("cc", "1000", "CC Administrativo", 3, maisAntigo).
			AddRow("status", "aberta", "Aberta", 5, maisRecente).
			AddRow("tipo", "transferencia", "Transferência", 7, maisAntigo))

	handler := ObterPainelHandler(db)
	req := newPainelRequest("consolidado")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}

	var resp painelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}

	if len(resp.PorTipo) != 1 || resp.PorTipo[0] != (painelItem{Chave: "transferencia", Rotulo: "Transferência", Quantidade: 7}) {
		t.Fatalf("por_tipo = %+v, inesperado", resp.PorTipo)
	}
	if len(resp.PorStatus) != 1 || resp.PorStatus[0] != (painelItem{Chave: "aberta", Rotulo: "Aberta", Quantidade: 5}) {
		t.Fatalf("por_status = %+v, inesperado", resp.PorStatus)
	}
	if len(resp.PorCC) != 1 || resp.PorCC[0] != (painelItem{Chave: "1000", Rotulo: "CC Administrativo", Quantidade: 3}) {
		t.Fatalf("por_cc = %+v, inesperado", resp.PorCC)
	}
	if resp.GeradoEm == nil || !resp.GeradoEm.Equal(maisRecente) {
		t.Fatalf("gerado_em = %v, esperado %v (o mais recente entre as linhas)", resp.GeradoEm, maisRecente)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestObterPainelHandler_SemDados cobre "Painel consolidado sem dados" da
// I/O Matrix: painel_snapshots vazia -> 200, 3 arrays vazios, gerado_em
// null (nunca erro).
func TestObterPainelHandler_SemDados(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("FROM painel_snapshots")).
		WillReturnRows(sqlmock.NewRows([]string{"dimensao", "chave", "rotulo", "quantidade", "gerado_em"}))

	handler := ObterPainelHandler(db)
	req := newPainelRequest("consolidado")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, esperado 200 (body=%s)", rec.Code, rec.Body.String())
	}

	var resp painelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resposta não é JSON válido: %v", err)
	}
	if len(resp.PorTipo) != 0 || len(resp.PorStatus) != 0 || len(resp.PorCC) != 0 {
		t.Fatalf("esperado os 3 arrays vazios, obtido: %+v", resp)
	}
	if resp.GeradoEm != nil {
		t.Fatalf("gerado_em = %v, esperado null", resp.GeradoEm)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestObterPainelHandler_PainelDesconhecido cobre "Nome de painel
// desconhecido" da I/O Matrix: 404, sem tocar o banco.
func TestObterPainelHandler_PainelDesconhecido(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := ObterPainelHandler(db)
	req := newPainelRequest("qualquer-outro")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, esperado 404 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas (nada deveria tocar o banco): %v", err)
	}
}
