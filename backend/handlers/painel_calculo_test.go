package handlers

// painel_calculo_test.go — cobre RecalcularPainelConsolidado (as 3 queries
// de contagem + regravação de painel_snapshots) e RecalcularPainelHandler
// (POST /api/admin/paineis/{painel}/recalcular). Mesmo padrão sqlmock dos
// demais testes deste pacote.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
)

func newRecalcularPainelRequest(painel string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/admin/paineis/"+painel+"/recalcular", nil)
	req.SetPathValue("painel", painel)
	claims := jwt.MapClaims{"user_id": testAtorID, "perfil": "administrador"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	return req.WithContext(ctx)
}

// TestRecalcularPainelConsolidado_Success cobre o caminho feliz: 3 queries
// de contagem, limpeza da tabela, e 1 INSERT por linha contada — inclusive
// tradução de código para rótulo legível (tipo/status) e uso direto do
// rótulo já vindo do banco (cc, via JOIN com centros_custo).
func TestRecalcularPainelConsolidado_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT tipo_solicitacao, COUNT(*) FROM solicitacoes GROUP BY tipo_solicitacao")).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "count"}).
			AddRow("transferencia", 7).
			AddRow("obras", 2))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT status, COUNT(*) FROM solicitacoes GROUP BY status")).
		WillReturnRows(sqlmock.NewRows([]string{"status", "count"}).
			AddRow("aberta", 5))
	mock.ExpectQuery(regexp.QuoteMeta("FROM solicitacoes s")).
		WillReturnRows(sqlmock.NewRows([]string{"codigo", "nome", "count"}).
			AddRow("1000", "CC Administrativo", 3))

	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM painel_snapshots WHERE painel = 'consolidado'")).
		WillReturnResult(sqlmock.NewResult(0, 0))

	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO painel_snapshots")).
		WithArgs("tipo", "transferencia", "Transferência", 7, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO painel_snapshots")).
		WithArgs("tipo", "obras", "Obras", 2, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO painel_snapshots")).
		WithArgs("status", "aberta", "Aberta", 5, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO painel_snapshots")).
		WithArgs("cc", "1000", "CC Administrativo", 3, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := RecalcularPainelConsolidado(db); err != nil {
		t.Fatalf("esperado sucesso, obtido erro: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestRecalcularPainelConsolidado_RotuloDesconhecido cobre um código sem
// tradução conhecida (ex. um status novo que uma story futura introduza) —
// nunca quebra o recálculo, só usa a própria chave como rótulo.
func TestRecalcularPainelConsolidado_RotuloDesconhecido(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT tipo_solicitacao, COUNT(*) FROM solicitacoes GROUP BY tipo_solicitacao")).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "count"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT status, COUNT(*) FROM solicitacoes GROUP BY status")).
		WillReturnRows(sqlmock.NewRows([]string{"status", "count"}).
			AddRow("status_novo_desconhecido", 1))
	mock.ExpectQuery(regexp.QuoteMeta("FROM solicitacoes s")).
		WillReturnRows(sqlmock.NewRows([]string{"codigo", "nome", "count"}))

	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM painel_snapshots WHERE painel = 'consolidado'")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO painel_snapshots")).
		WithArgs("status", "status_novo_desconhecido", "status_novo_desconhecido", 1, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := RecalcularPainelConsolidado(db); err != nil {
		t.Fatalf("esperado sucesso, obtido erro: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestRecalcularPainelHandler_RejectsWrongMethod(t *testing.T) {
	handler := RecalcularPainelHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/paineis/consolidado/recalcular", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("esperado 405, obtido %d", rec.Code)
	}
}

func TestRecalcularPainelHandler_PainelDesconhecido(t *testing.T) {
	handler := RecalcularPainelHandler(nil)
	rec := httptest.NewRecorder()
	handler(rec, newRecalcularPainelRequest("outro"))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("esperado 404, obtido %d", rec.Code)
	}
}

func TestRecalcularPainelHandler_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT tipo_solicitacao, COUNT(*) FROM solicitacoes GROUP BY tipo_solicitacao")).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "count"}))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT status, COUNT(*) FROM solicitacoes GROUP BY status")).
		WillReturnRows(sqlmock.NewRows([]string{"status", "count"}))
	mock.ExpectQuery(regexp.QuoteMeta("FROM solicitacoes s")).
		WillReturnRows(sqlmock.NewRows([]string{"codigo", "nome", "count"}))
	mock.ExpectExec(regexp.QuoteMeta("DELETE FROM painel_snapshots WHERE painel = 'consolidado'")).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	handler := RecalcularPainelHandler(db)
	rec := httptest.NewRecorder()
	handler(rec, newRecalcularPainelRequest("consolidado"))

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}
