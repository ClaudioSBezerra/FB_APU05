package handlers

// colaboradores_test.go — cobre a I/O Matrix da Story 2.2 (Carga de
// colaborador × centro de custo): carga feliz, placeholder, e-mail ainda não
// logou, CC inexistente, cabeçalho errado, linha com número de colunas
// errado, reimportação idempotente (sem 409) e 403 sem perfil administrador.
// Mesmo padrão de cadastros_test.go (httptest + sqlmock, sequência exata de
// queries programada e checada com mock.ExpectationsWereMet).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
)

const testCentroCustoID = "33333333-3333-3333-3333-333333333333"

// newColaboradoresRequest monta uma requisição POST com claims de
// administrador já injetadas no contexto, do jeito que RequireAuth injetaria
// antes de chamar o handler.
func newColaboradoresRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/admin/colaboradores/carga", strings.NewReader(body))
	claims := jwt.MapClaims{"user_id": testAtorID, "perfil": "administrador"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	return req.WithContext(ctx)
}

// TestCarregarColaboradoresHandler_Sucesso cobre "Carga feliz": e-mail já
// existe em usuarios, centro_custo_codigo existe -> 200, cc_proprio_id
// atualizado, contagem em "atualizados".
func TestCarregarColaboradoresHandler_Sucesso(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "\xEF\xBB\xBFmatricula;nome;email;centro_custo_codigo\n123;Fulano;fulano@example.com;CC1\n"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo, id FROM centros_custo")).
		WillReturnRows(sqlmock.NewRows([]string{"codigo", "id"}).AddRow("CC1", testCentroCustoID))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE usuarios SET cc_proprio_id = $1 WHERE LOWER(email) = LOWER($2)")).
		WithArgs(testCentroCustoID, "fulano@example.com").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := CarregarColaboradoresHandler(db)
	req := newColaboradoresRequest(csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"atualizados":1`) {
		t.Fatalf("corpo não indica 1 atualizado: %s", rec.Body.String())
	}
	for _, campo := range []string{`"placeholders_ignorados":0`, `"aguardando_primeiro_login":0`, `"centro_custo_nao_encontrado":0`} {
		if !strings.Contains(rec.Body.String(), campo) {
			t.Fatalf("corpo não contém %s: %s", campo, rec.Body.String())
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestCarregarColaboradoresHandler_Placeholder cobre "Linha placeholder":
// email vazio -> linha ignorada, nenhuma escrita, contada em
// placeholders_ignorados, carga continua (centros_custo ainda é carregado uma
// única vez no início da carga, mas usuarios não é consultado/atualizado para
// essa linha).
func TestCarregarColaboradoresHandler_Placeholder(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "matricula;nome;email;centro_custo_codigo\n456;Cargo Vago;;CC1\n"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo, id FROM centros_custo")).
		WillReturnRows(sqlmock.NewRows([]string{"codigo", "id"}).AddRow("CC1", testCentroCustoID))
	mock.ExpectCommit()

	handler := CarregarColaboradoresHandler(db)
	req := newColaboradoresRequest(csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"placeholders_ignorados":1`) {
		t.Fatalf("corpo não indica 1 placeholder ignorado: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"atualizados":0`) {
		t.Fatalf("corpo não indica 0 atualizados: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestCarregarColaboradoresHandler_AguardandoPrimeiroLogin cobre "E-mail
// ainda não logou": email preenchido, CC existe, mas nenhum usuarios casa
// (RowsAffected=0) -> nenhuma escrita efetiva, contada em
// aguardando_primeiro_login, carga continua.
func TestCarregarColaboradoresHandler_AguardandoPrimeiroLogin(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "matricula;nome;email;centro_custo_codigo\n789;Fulano;naologou@example.com;CC1\n"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo, id FROM centros_custo")).
		WillReturnRows(sqlmock.NewRows([]string{"codigo", "id"}).AddRow("CC1", testCentroCustoID))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE usuarios SET cc_proprio_id = $1 WHERE LOWER(email) = LOWER($2)")).
		WithArgs(testCentroCustoID, "naologou@example.com").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	handler := CarregarColaboradoresHandler(db)
	req := newColaboradoresRequest(csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"aguardando_primeiro_login":1`) {
		t.Fatalf("corpo não indica 1 aguardando_primeiro_login: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"atualizados":0`) {
		t.Fatalf("corpo não indica 0 atualizados: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestCarregarColaboradoresHandler_CentroCustoNaoEncontrado cobre "CC
// inexistente": centro_custo_codigo não existe em centros_custo -> nenhuma
// escrita para a linha, contada em centro_custo_nao_encontrado, carga
// continua (sem sequer consultar usuarios para essa linha).
func TestCarregarColaboradoresHandler_CentroCustoNaoEncontrado(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "matricula;nome;email;centro_custo_codigo\n123;Fulano;fulano@example.com;CCINEXISTENTE\n"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo, id FROM centros_custo")).
		WillReturnRows(sqlmock.NewRows([]string{"codigo", "id"}).AddRow("CC1", testCentroCustoID))
	mock.ExpectCommit()

	handler := CarregarColaboradoresHandler(db)
	req := newColaboradoresRequest(csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"centro_custo_nao_encontrado":1`) {
		t.Fatalf("corpo não indica 1 centro_custo_nao_encontrado: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"atualizados":0`) {
		t.Fatalf("corpo não indica 0 atualizados: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestCarregarColaboradoresHandler_CentroCustoCodigoCaseInsensitive cobre um
// centro_custo_codigo do CSV que difere do valor gravado em centros_custo
// apenas em maiúsculas/minúsculas ("cc1" no CSV vs "CC1" no banco) -> ainda
// resolve e atualiza com sucesso, mesmo tratamento já dado ao e-mail
// (LOWER(email) = LOWER($2)).
func TestCarregarColaboradoresHandler_CentroCustoCodigoCaseInsensitive(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "matricula;nome;email;centro_custo_codigo\n123;Fulano;fulano@example.com;cc1\n"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo, id FROM centros_custo")).
		WillReturnRows(sqlmock.NewRows([]string{"codigo", "id"}).AddRow("CC1", testCentroCustoID))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE usuarios SET cc_proprio_id = $1 WHERE LOWER(email) = LOWER($2)")).
		WithArgs(testCentroCustoID, "fulano@example.com").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := CarregarColaboradoresHandler(db)
	req := newColaboradoresRequest(csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"atualizados":1`) {
		t.Fatalf("corpo não indica 1 atualizado: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"centro_custo_nao_encontrado":0`) {
		t.Fatalf("corpo não indica 0 centro_custo_nao_encontrado: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestCarregarColaboradoresHandler_CabecalhoInvalido cobre "Cabeçalho
// errado": nada é escrito, 400 citando as colunas esperadas.
func TestCarregarColaboradoresHandler_CabecalhoInvalido(t *testing.T) {
	db, mock := newSQLMock(t)

	handler := CarregarColaboradoresHandler(db)
	req := newColaboradoresRequest("matricula;nome;email\n123;Fulano;fulano@example.com\n")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "centro_custo_codigo") {
		t.Fatalf("corpo não cita as colunas esperadas: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nenhuma query deveria ter sido disparada): %v", err)
	}
}

// TestCarregarColaboradoresHandler_LinhaComNumeroDeColunasErrado cobre
// "Linha com número de colunas errado": arquivo inteiro rejeitado com 400
// citando a linha N, nada é escrito.
func TestCarregarColaboradoresHandler_LinhaComNumeroDeColunasErrado(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "matricula;nome;email;centro_custo_codigo\n123;Fulano;fulano@example.com;CC1;CAMPO_EXTRA\n"

	handler := CarregarColaboradoresHandler(db)
	req := newColaboradoresRequest(csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "linha 2") {
		t.Fatalf("corpo não cita a linha 2: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nenhuma query deveria ter sido disparada): %v", err)
	}
}

// TestCarregarColaboradoresHandler_Reimportacao cobre "Reimportação": dois
// uploads sucessivos com dados DIFERENTES (CC1 depois CC2) para o mesmo
// e-mail -- o cc_proprio_id final reflete o último upload, idempotente,
// nunca 409 em nenhum dos dois.
func TestCarregarColaboradoresHandler_Reimportacao(t *testing.T) {
	db, mock := newSQLMock(t)

	const testCentroCustoID2 = "55555555-5555-5555-5555-555555555555"
	csvs := []string{
		"matricula;nome;email;centro_custo_codigo\n123;Fulano;fulano@example.com;CC1\n",
		"matricula;nome;email;centro_custo_codigo\n123;Fulano;fulano@example.com;CC2\n",
	}
	codigos := []string{"CC1", "CC2"}
	ids := []string{testCentroCustoID, testCentroCustoID2}

	for i := 0; i < 2; i++ {
		mock.ExpectBegin()
		mock.ExpectQuery(regexp.QuoteMeta("SELECT codigo, id FROM centros_custo")).
			WillReturnRows(sqlmock.NewRows([]string{"codigo", "id"}).AddRow(codigos[i], ids[i]))
		mock.ExpectExec(regexp.QuoteMeta("UPDATE usuarios SET cc_proprio_id = $1 WHERE LOWER(email) = LOWER($2)")).
			WithArgs(ids[i], "fulano@example.com").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		handler := CarregarColaboradoresHandler(db)
		req := newColaboradoresRequest(csvs[i])
		rec := httptest.NewRecorder()
		handler(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("upload %d: esperado 200, obtido %d (body=%s)", i+1, rec.Code, rec.Body.String())
		}
		if rec.Code == http.StatusConflict || strings.Contains(rec.Body.String(), "409") {
			t.Fatalf("upload %d: reimportação nunca deve gerar 409: %s", i+1, rec.Body.String())
		}
	}
	// A 2a chamada programada no mock com o CC2/id2 comprova que o último
	// upload é o que vale -- se o handler tivesse algum guard de "já
	// possui dados" (409), a 2a iteração do loop acima já teria falhado
	// antes de chegar aqui.
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestColaboradoresCargaRoute_RequireAdministrador cobre "Acesso sem perfil
// administrador": token válido com perfil=solicitante -> 403, mesma
// RequireAuth usada nas demais rotas administrativas (registrada em
// main.go); o handler nunca é chamado, então db=nil é seguro.
func TestColaboradoresCargaRoute_RequireAdministrador(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-at-least-32-bytes-long!!")

	token, err := GenerateToken("user-789", "solicitante")
	if err != nil {
		t.Fatalf("GenerateToken falhou: %v", err)
	}

	called := false
	protected := RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}, "administrador")

	req := httptest.NewRequest(http.MethodPost, "/api/admin/colaboradores/carga", strings.NewReader(""))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	protected(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("esperado 403, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("handler de carga de colaboradores não deveria ser chamado com perfil insuficiente")
	}
}
