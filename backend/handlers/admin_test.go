package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
)

// --- sqlmock helpers ---
//
// Os testes abaixo exercitam de verdade a transação do
// AdminUpdateUsuarioHandler (os testes acima usam db=nil e só cobrem
// validação de entrada, que retorna antes de qualquer acesso ao banco). Cada
// teste monta um *sql.DB fake via sqlmock, programa a sequência exata de
// queries que o handler executa (ver admin.go) e confere que nenhuma query
// extra/faltante acontece (mock.ExpectationsWereMet).

const (
	testAlvoID = "11111111-1111-1111-1111-111111111111"
	testAtorID = "22222222-2222-2222-2222-222222222222"
)

// newAdminRequest monta uma requisição PATCH com claims no contexto, do
// mesmo jeito que RequireAuth (middleware.go) injeta antes de chamar o
// handler protegido — GetUserIDFromContext lê daqui.
func newAdminRequest(alvoID, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPatch, "/api/admin/usuarios/"+alvoID, strings.NewReader(body))
	req.SetPathValue("id", alvoID)
	claims := jwt.MapClaims{"user_id": testAtorID, "perfil": "administrador"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	return req.WithContext(ctx)
}

func newSQLMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New falhou: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, mock
}

// TestAdminUpdateUsuarioHandler_LastAdminProtection_Rejects garante que a
// proteção do último administrador (FR-2) recusa com 409 QUANDO de fato
// chega a banco: trava a linha-alvo, trava o conjunto de admins ativos,
// encontra só 1 (o próprio alvo), e recusa sem nenhum UPDATE/INSERT —
// só ROLLBACK (via defer).
func TestAdminUpdateUsuarioHandler_LastAdminProtection_Rejects(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock($1)")).
		WithArgs(adminMutationLockKey).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT perfil, ativo FROM usuarios WHERE id = $1 FOR UPDATE")).
		WithArgs(testAlvoID).
		WillReturnRows(sqlmock.NewRows([]string{"perfil", "ativo"}).AddRow("administrador", true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE perfil = 'administrador' AND ativo = true ORDER BY id FOR UPDATE")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testAlvoID))
	mock.ExpectRollback()

	handler := AdminUpdateUsuarioHandler(db)
	req := newAdminRequest(testAlvoID, `{"ativo":false}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("esperado 409, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), errMsgUltimoAdministrador) {
		t.Fatalf("corpo não contém a mensagem esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestAdminUpdateUsuarioHandler_Success_UpdatesAndAudits garante o caminho
// feliz: com 2+ admins ativos, a mudança é aceita, o UPDATE e o INSERT de
// auditoria acontecem com os valores before/after corretos, e a transação
// comita.
func TestAdminUpdateUsuarioHandler_Success_UpdatesAndAudits(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock($1)")).
		WithArgs(adminMutationLockKey).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT perfil, ativo FROM usuarios WHERE id = $1 FOR UPDATE")).
		WithArgs(testAlvoID).
		WillReturnRows(sqlmock.NewRows([]string{"perfil", "ativo"}).AddRow("administrador", true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE perfil = 'administrador' AND ativo = true ORDER BY id FOR UPDATE")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testAlvoID).AddRow("33333333-3333-3333-3333-333333333333"))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE usuarios SET perfil = $1, ativo = $2, updated_at = now() WHERE id = $3")).
		WithArgs("administrador", false, testAlvoID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO usuarios_auditoria")).
		WithArgs(testAtorID, testAlvoID, "ativo_alterado", "administrador", "administrador", true, false).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	handler := AdminUpdateUsuarioHandler(db)
	req := newAdminRequest(testAlvoID, `{"ativo":false}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestAdminUpdateUsuarioHandler_NoOp_CommitsWithoutAuditing garante que uma
// requisição que não muda nada (perfil e ativo iguais aos valores atuais)
// não grava UPDATE nem auditoria — só confirma o estado atual com COMMIT.
func TestAdminUpdateUsuarioHandler_NoOp_CommitsWithoutAuditing(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock($1)")).
		WithArgs(adminMutationLockKey).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT perfil, ativo FROM usuarios WHERE id = $1 FOR UPDATE")).
		WithArgs(testAlvoID).
		WillReturnRows(sqlmock.NewRows([]string{"perfil", "ativo"}).AddRow("administrador", true))
	mock.ExpectCommit()

	handler := AdminUpdateUsuarioHandler(db)
	req := newAdminRequest(testAlvoID, `{"perfil":"administrador","ativo":true}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (UPDATE/INSERT não deveriam ter sido chamados): %v", err)
	}
}

// TestAdminUpdateUsuarioHandler_RejectsEmptyBody garante 400 quando nem
// `perfil` nem `ativo` são enviados — usa um id de formato válido para que a
// requisição de fato alcance essa validação (e não a de "id inválido"); db=nil
// é seguro: o handler retorna antes de qualquer acesso ao banco.
func TestAdminUpdateUsuarioHandler_RejectsEmptyBody(t *testing.T) {
	handler := AdminUpdateUsuarioHandler(nil)

	req := newAdminRequest(testAlvoID, `{}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "informe ao menos") {
		t.Fatalf("corpo não indica a causa esperada (campo ausente): %s", rec.Body.String())
	}
}

// TestAdminUpdateUsuarioHandler_RejectsInvalidPerfil garante 400 para um
// valor de `perfil` fora do CHECK constraint da coluna (só
// 'administrador'/'solicitante') — usa um id de formato válido pela mesma
// razão do teste acima.
func TestAdminUpdateUsuarioHandler_RejectsInvalidPerfil(t *testing.T) {
	handler := AdminUpdateUsuarioHandler(nil)

	req := newAdminRequest(testAlvoID, `{"perfil":"super-admin"}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "perfil deve ser") {
		t.Fatalf("corpo não indica a causa esperada (perfil inválido): %s", rec.Body.String())
	}
}

// TestAdminUpdateUsuarioHandler_RejectsInvalidID garante 400 quando o {id} do
// path não tem formato de UUID — precisa recusar antes de chegar ao banco
// (um id malformado na query `WHERE id = $1` contra coluna UUID vira erro de
// sintaxe do Postgres, um 500 genérico em vez de um 400 claro).
func TestAdminUpdateUsuarioHandler_RejectsInvalidID(t *testing.T) {
	handler := AdminUpdateUsuarioHandler(nil)

	req := newAdminRequest("abc", `{"ativo":false}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "id inválido") {
		t.Fatalf("corpo não indica a causa esperada (id inválido): %s", rec.Body.String())
	}
}

// TestAdminUpdateUsuarioHandler_RejectsMissingID garante 400 quando o
// {id} do path não resolve (path vazio).
func TestAdminUpdateUsuarioHandler_RejectsMissingID(t *testing.T) {
	handler := AdminUpdateUsuarioHandler(nil)

	req := httptest.NewRequest(http.MethodPatch, "/api/admin/usuarios/", strings.NewReader(`{"ativo":false}`))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestAdminUpdateUsuarioHandler_RejectsWrongMethod garante 405 para métodos
// diferentes de PATCH (defesa redundante à do ServeMux de main.go, útil para
// quem chamar o handler diretamente).
func TestAdminUpdateUsuarioHandler_RejectsWrongMethod(t *testing.T) {
	handler := AdminUpdateUsuarioHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/usuarios/abc", nil)
	req.SetPathValue("id", "abc")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("esperado 405, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestAuditAction cobre o mapeamento de auditAction usado para preencher
// usuarios_auditoria.acao.
func TestAuditAction(t *testing.T) {
	cases := []struct {
		perfilMudou, ativoMudou bool
		want                    string
	}{
		{true, true, "perfil_e_ativo_alterados"},
		{true, false, "perfil_alterado"},
		{false, true, "ativo_alterado"},
	}
	for _, c := range cases {
		got := auditAction(c.perfilMudou, c.ativoMudou)
		if got != c.want {
			t.Errorf("auditAction(%v, %v) = %q, want %q", c.perfilMudou, c.ativoMudou, got, c.want)
		}
	}
}
