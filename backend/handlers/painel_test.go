package handlers

// painel_test.go — painel refeito em 26/11 (F5): conta ao vivo, com recortes e visibilidade no servidor. Os casos de
// visibilidade com dados reais (grupo, cópia, escopo de CC) estão em pg_integration_test.go.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
)

func reqPainel(t *testing.T, url, usuario, perfil string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.SetPathValue("painel", "consolidado")
	return req.WithContext(context.WithValue(req.Context(), ClaimsContextKey, jwt.MapClaims{"user_id": usuario, "perfil": perfil}))
}

func TestObterPainelHandler_AtendenteVeTudoComPeriodo(t *testing.T) {
	db, mock := newSQLMock(t)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, COUNT(*) FROM solicitacoes s WHERE s.created_at >= $1 AND s.created_at < $2")).
		WillReturnRows(sqlmock.NewRows([]string{"t", "n"}).AddRow("transferencia", 3))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.status, COUNT(*)")).WillReturnRows(sqlmock.NewRows([]string{"s", "n"}).AddRow("cancelada", 1).AddRow("finalizada_sucesso", 2))
	mock.ExpectQuery(regexp.QuoteMeta("JOIN centros_custo cc ON cc.id = s.centro_custo_id")).WillReturnRows(sqlmock.NewRows([]string{"c", "n", "q"}).AddRow("10016000", "TI", 3))
	mock.ExpectQuery(regexp.QuoteMeta("JOIN divisoes d ON d.id = cc.divisao_id")).
		WillReturnRows(sqlmock.NewRows([]string{"d", "r", "t", "i", "n"}).AddRow("1001", "FERREIRA COSTA — Recife/PE", 1500.0, 0.0, 3))
	rec := httptest.NewRecorder()
	ObterPainelHandler(db)(rec, reqPainel(t, "/api/paineis/consolidado?de=2026-11-01&ate=2026-11-30", "a1", "atendente"))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var r struct {
		Escopo       string         `json:"escopo"`
		PorStatus    []painelItem   `json:"por_status"`
		PorFilial    []painelFilial `json:"por_filial"`
		Solicitantes []pessoaFiltro `json:"solicitantes"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if r.Escopo != "geral" || len(r.PorFilial) != 1 || r.PorFilial[0].Transferido != 1500 || r.PorStatus[0].Rotulo != "Cancelada" || len(r.Solicitantes) != 0 {
		t.Fatalf("resposta: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestObterPainelHandler_SolicitanteRecortadoNoServidor(t *testing.T) {
	db, mock := newSQLMock(t)
	const eu = "55555555-5555-5555-5555-555555555555"
	mock.MatchExpectationsInOrder(true)
	for i := 0; i < 4; i++ {
		rows := sqlmock.NewRows([]string{"a", "b"})
		switch i {
		case 2:
			rows = sqlmock.NewRows([]string{"a", "b", "c"})
		case 3:
			rows = sqlmock.NewRows([]string{"d", "r", "t", "i", "n"})
		}
		mock.ExpectQuery(regexp.QuoteMeta("WHERE (s.solicitante_id = $1 OR")).WithArgs(eu).WillReturnRows(rows)
	}
	// só ela: o filtro "Quem abriu" não aparece
	mock.ExpectQuery(regexp.QuoteMeta("SELECT DISTINCT u.id, u.nome")).WithArgs(eu).WillReturnRows(sqlmock.NewRows([]string{"id", "nome"}).AddRow(eu, "Wanessa"))
	rec := httptest.NewRecorder()
	ObterPainelHandler(db)(rec, reqPainel(t, "/api/paineis/consolidado", eu, "solicitante"))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"escopo":"meus"`) || !strings.Contains(rec.Body.String(), `"solicitantes":[]`) {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestObterPainelHandler_PainelDesconhecido(t *testing.T) {
	db, _ := newSQLMock(t)
	req := httptest.NewRequest(http.MethodGet, "/api/paineis/x", nil)
	req.SetPathValue("painel", "x")
	rec := httptest.NewRecorder()
	ObterPainelHandler(db)(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("esperado 404, veio %d", rec.Code)
	}
}
