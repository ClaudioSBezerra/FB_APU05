package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
	"github.com/lib/pq"
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
	mock.ExpectQuery(regexp.QuoteMeta("SELECT perfil, ativo, cod_empresa, numcad, nome, email, cc_proprio_id, aprovador FROM usuarios WHERE id = $1 FOR UPDATE")).
		WithArgs(testAlvoID).
		WillReturnRows(sqlmock.NewRows([]string{"perfil", "ativo", "cod_empresa", "numcad", "nome", "email", "cc_proprio_id", "aprovador"}).
			AddRow("administrador", true, nil, nil, "Fulano", "fulano@fortesbezerra.com.br", nil, false))
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
	mock.ExpectQuery(regexp.QuoteMeta("SELECT perfil, ativo, cod_empresa, numcad, nome, email, cc_proprio_id, aprovador FROM usuarios WHERE id = $1 FOR UPDATE")).
		WithArgs(testAlvoID).
		WillReturnRows(sqlmock.NewRows([]string{"perfil", "ativo", "cod_empresa", "numcad", "nome", "email", "cc_proprio_id", "aprovador"}).
			AddRow("administrador", true, nil, nil, "Fulano", "fulano@fortesbezerra.com.br", nil, false))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE perfil = 'administrador' AND ativo = true ORDER BY id FOR UPDATE")).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testAlvoID).AddRow("33333333-3333-3333-3333-333333333333"))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE usuarios SET")).
		WithArgs("administrador", false, nil, nil, "Fulano", "fulano@fortesbezerra.com.br", nil, false, testAlvoID).
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
	mock.ExpectQuery(regexp.QuoteMeta("SELECT perfil, ativo, cod_empresa, numcad, nome, email, cc_proprio_id, aprovador FROM usuarios WHERE id = $1 FOR UPDATE")).
		WithArgs(testAlvoID).
		WillReturnRows(sqlmock.NewRows([]string{"perfil", "ativo", "cod_empresa", "numcad", "nome", "email", "cc_proprio_id", "aprovador"}).
			AddRow("administrador", true, nil, nil, "Fulano", "fulano@fortesbezerra.com.br", nil, false))
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

// TestAdminUpdateUsuarioHandler_RejectsCodEmpresaSemNumcad garante 400
// quando só um dos dois campos de identidade (migration 019) é enviado — um
// par parcial não identifica ninguém de verdade.
func TestAdminUpdateUsuarioHandler_RejectsCodEmpresaSemNumcad(t *testing.T) {
	handler := AdminUpdateUsuarioHandler(nil)

	req := newAdminRequest(testAlvoID, `{"cod_empresa":1}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "juntos") {
		t.Fatalf("corpo não indica a causa esperada (campos juntos): %s", rec.Body.String())
	}
}

// TestAdminUpdateUsuarioHandler_AtualizaIdentidadeSemAfetarAuditoria garante
// que editar só cod_empresa/numcad (sem mudar perfil/ativo) grava o UPDATE
// mas NUNCA um evento em usuarios_auditoria (essa tabela é sobre
// concessão/revogação de acesso, não sobre dado cadastral).
func TestAdminUpdateUsuarioHandler_AtualizaIdentidadeSemAfetarAuditoria(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock($1)")).
		WithArgs(adminMutationLockKey).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT perfil, ativo, cod_empresa, numcad, nome, email, cc_proprio_id, aprovador FROM usuarios WHERE id = $1 FOR UPDATE")).
		WithArgs(testAlvoID).
		WillReturnRows(sqlmock.NewRows([]string{"perfil", "ativo", "cod_empresa", "numcad", "nome", "email", "cc_proprio_id", "aprovador"}).
			AddRow("solicitante", true, nil, nil, "Fulano", "fulano@fortesbezerra.com.br", nil, false))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE usuarios SET")).
		WithArgs("solicitante", true, 1, 12345, "Fulano", "fulano@fortesbezerra.com.br", nil, false, testAlvoID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := AdminUpdateUsuarioHandler(db)
	req := newAdminRequest(testAlvoID, `{"cod_empresa":1,"numcad":12345}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (INSERT INTO usuarios_auditoria não deveria ter sido chamado): %v", err)
	}
}

// TestAdminUpdateUsuarioHandler_RejectsInvalidID garante 400 quando o {id} do
// path não tem formato de UUID — precisa recusar antes de chegar ao banco
// (um id malformado na query `WHERE id = $1` contra coluna UUID vira erro de
// sintaxe do Postgres, um 500 genérico em vez de um 400 claro).
// TestAdminUpdateUsuarioHandler_AtualizaNomeEmailCentroCusto cobre a edição
// completa pedida na tela (nome/e-mail/centro de custo), além de
// perfil/ativo/identidade já cobertos acima — tudo num único UPDATE.
func TestAdminUpdateUsuarioHandler_AtualizaNomeEmailCentroCusto(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock($1)")).
		WithArgs(adminMutationLockKey).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT perfil, ativo, cod_empresa, numcad, nome, email, cc_proprio_id, aprovador FROM usuarios WHERE id = $1 FOR UPDATE")).
		WithArgs(testAlvoID).
		WillReturnRows(sqlmock.NewRows([]string{"perfil", "ativo", "cod_empresa", "numcad", "nome", "email", "cc_proprio_id", "aprovador"}).
			AddRow("solicitante", true, nil, nil, "Nome Antigo", "antigo@fortesbezerra.com.br", nil, false))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE usuarios SET")).
		WithArgs("solicitante", true, nil, nil, "Nome Novo", "novo@fortesbezerra.com.br", testCentroCustoID, false, testAlvoID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := AdminUpdateUsuarioHandler(db)
	body := fmt.Sprintf(`{"nome":"Nome Novo","email":"novo@fortesbezerra.com.br","centro_custo_id":"%s"}`, testCentroCustoID)
	req := newAdminRequest(testAlvoID, body)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestAdminUpdateUsuarioHandler_EmailDuplicado_Returns409 garante que
// violar idx_usuarios_email_lower vira 409 com mensagem específica — nunca
// o 500 genérico.
func TestAdminUpdateUsuarioHandler_EmailDuplicado_Returns409(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock($1)")).
		WithArgs(adminMutationLockKey).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT perfil, ativo, cod_empresa, numcad, nome, email, cc_proprio_id, aprovador FROM usuarios WHERE id = $1 FOR UPDATE")).
		WithArgs(testAlvoID).
		WillReturnRows(sqlmock.NewRows([]string{"perfil", "ativo", "cod_empresa", "numcad", "nome", "email", "cc_proprio_id", "aprovador"}).
			AddRow("solicitante", true, nil, nil, "Fulano", "fulano@fortesbezerra.com.br", nil, false))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE usuarios SET")).
		WithArgs("solicitante", true, nil, nil, "Fulano", "ja-existe@fortesbezerra.com.br", nil, false, testAlvoID).
		WillReturnError(&pq.Error{Code: "23505", Constraint: "idx_usuarios_email_lower"})
	mock.ExpectRollback()

	handler := AdminUpdateUsuarioHandler(db)
	req := newAdminRequest(testAlvoID, `{"email":"ja-existe@fortesbezerra.com.br"}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("esperado 409, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "e-mail") {
		t.Fatalf("corpo não menciona e-mail duplicado: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

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
