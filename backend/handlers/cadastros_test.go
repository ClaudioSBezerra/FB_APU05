package handlers

// cadastros_test.go — cobre a I/O Matrix da Story 2.1 (Carga inicial e
// cadastros administráveis): import feliz/409/400×2/404-tipo, update
// feliz/404, restore feliz/404, 403. Mesmo padrão de admin_test.go
// (httptest + sqlmock, sequência exata de queries programada e checada com
// mock.ExpectationsWereMet).
//
// Todos os testes usam o {tipo} "divisoes" (2 colunas simples, sem
// resolução de FK) — suficiente para exercitar o caminho genérico comum aos
// 7 tipos; a resolução de FK de centros-custo tem cobertura própria no
// pacote de parsing (decodeCSVCentrosCusto não precisa de sqlmock além da
// consulta a divisoes, já coberta indiretamente aqui pelo mesmo padrão de
// `tx.QueryRow`).

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/golang-jwt/jwt/v5"
)

const testCadastroID = "44444444-4444-4444-4444-444444444444"

// newCadastroRequest monta uma requisição com claims de administrador já
// injetadas no contexto (do jeito que RequireAuth injetaria antes de chamar
// o handler) e os PathValue que o ServeMux (Go 1.22+) resolveria a partir do
// padrão de rota (`{tipo}`, `{id}`).
func newCadastroRequest(method, tipo, id, body string) *http.Request {
	path := "/api/admin/cadastros/" + tipo
	if id != "" {
		path += "/" + id
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.SetPathValue("tipo", tipo)
	if id != "" {
		req.SetPathValue("id", id)
	}
	claims := jwt.MapClaims{"user_id": testAtorID, "perfil": "administrador"}
	ctx := context.WithValue(req.Context(), ClaimsContextKey, claims)
	return req.WithContext(ctx)
}

// --- Importar ---

// TestImportarCadastroHandler_Success cobre "Import feliz" da I/O Matrix:
// CSV válido com BOM UTF-8 e separador ';', tabela vazia -> 201
// {"importados": N}, N linhas em cadastro_historico (acao=criado, versao=1).
func TestImportarCadastroHandler_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "\xEF\xBB\xBFcodigo;nome\nD1;Divisao Um\nD2;Divisao Dois\n"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_divisoes").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM divisoes)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO divisoes (codigo, nome) VALUES ($1, $2) RETURNING id")).
		WithArgs("D1", "Divisao Um").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("11111111-1111-1111-1111-111111111111"))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cadastro_historico")).
		WithArgs("divisoes", "11111111-1111-1111-1111-111111111111", `{"codigo":"D1","nome":"Divisao Um"}`, testAtorID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO divisoes (codigo, nome) VALUES ($1, $2) RETURNING id")).
		WithArgs("D2", "Divisao Dois").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("22222222-2222-2222-2222-222222222222"))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cadastro_historico")).
		WithArgs("divisoes", "22222222-2222-2222-2222-222222222222", `{"codigo":"D2","nome":"Divisao Dois"}`, testAtorID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectCommit()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "divisoes", "", csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"importados":2`) {
		t.Fatalf("corpo não indica 2 importados: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestImportarCadastroHandler_TabelaNaoVazia cobre "Import com tabela
// não-vazia": nada é escrito, 409 "cadastro já possui dados".
func TestImportarCadastroHandler_TabelaNaoVazia(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_divisoes").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM divisoes)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectRollback()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "divisoes", "", "codigo;nome\nD1;Divisao Um\n")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("esperado 409, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "cadastro já possui dados") {
		t.Fatalf("corpo não contém a mensagem esperada: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestImportarCadastroHandler_CabecalhoInvalido cobre "Import com cabeçalho
// errado": nada é escrito, 400 citando as colunas esperadas.
func TestImportarCadastroHandler_CabecalhoInvalido(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_divisoes").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM divisoes)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "divisoes", "", "codigo;nomee\nD1;Divisao Um\n")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "codigo;nome") {
		t.Fatalf("corpo não cita as colunas esperadas: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestImportarCadastroHandler_LinhaInvalida cobre "Import com linha
// inválida": nada é escrito (transação inteira desfeita), 400 citando a
// linha N.
func TestImportarCadastroHandler_LinhaInvalida(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_divisoes").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM divisoes)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	handler := ImportarCadastroHandler(db)
	// Linha 2 (primeira linha de dado) tem codigo vazio -> rejeita citando
	// a linha, nenhum INSERT deveria ter sido tentado.
	req := newCadastroRequest(http.MethodPost, "divisoes", "", "codigo;nome\n;Divisao Um\n")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "linha 2") {
		t.Fatalf("corpo não cita a linha inválida: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nenhum INSERT deveria ter sido tentado): %v", err)
	}
}

// TestImportarCadastroHandler_TipoDesconhecido cobre "{tipo} desconhecido":
// 404 "tipo de cadastro desconhecido" — db=nil é seguro, o handler retorna
// antes de qualquer acesso ao banco.
func TestImportarCadastroHandler_TipoDesconhecido(t *testing.T) {
	handler := ImportarCadastroHandler(nil)
	req := newCadastroRequest(http.MethodPost, "tipo-inexistente", "", "codigo;nome\n")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("esperado 404, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "tipo de cadastro desconhecido") {
		t.Fatalf("corpo não contém a mensagem esperada: %s", rec.Body.String())
	}
}

// TestImportarCadastroHandler_CentrosCusto_Success cobre a resolução de FK
// divisao_codigo -> divisao_id (decodeCSVCentrosCusto/resolveDivisaoID)
// dentro da MESMA transação do import — hoje só "divisoes" é exercitado
// pelos demais testes deste arquivo, então uma regressão nessa resolução
// passaria despercebida sem este teste.
func TestImportarCadastroHandler_CentrosCusto_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	const divisaoID = "33333333-3333-3333-3333-333333333333"
	const centroCustoID = "55555555-5555-5555-5555-555555555555"

	csv := "codigo;nome;divisao_codigo\nCC1;Centro Um;D1\n"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_centros-custo").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM centros_custo)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM divisoes WHERE codigo = $1")).
		WithArgs("D1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(divisaoID))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO centros_custo (codigo, nome, divisao_id) VALUES ($1, $2, $3) RETURNING id")).
		WithArgs("CC1", "Centro Um", divisaoID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(centroCustoID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cadastro_historico")).
		WithArgs("centros-custo", centroCustoID, `{"codigo":"CC1","divisao_id":"33333333-3333-3333-3333-333333333333","nome":"Centro Um"}`, testAtorID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectCommit()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "centros-custo", "", csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"importados":1`) {
		t.Fatalf("corpo não indica 1 importado: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestImportarCadastroHandler_Alcadas_Success cobre o decode de "alcadas":
// valor_maximo vazio no CSV vira NULL ("sem teto") e "ativo" é parseado como
// booleano — nenhum outro teste deste arquivo exercita esses dois caminhos.
func TestImportarCadastroHandler_Alcadas_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	const alcadaID = "66666666-6666-6666-6666-666666666666"

	csv := "filial;centro_custo_codigo;valor_minimo;valor_maximo;papel_aprovador;ativo\nF1;CC1;100;;Gerente;true\n"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_alcadas").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM alcadas)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO alcadas (filial, centro_custo_codigo, valor_minimo, valor_maximo, papel_aprovador, ativo) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id")).
		WithArgs("F1", "CC1", 100.0, nil, "Gerente", true).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(alcadaID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cadastro_historico")).
		WithArgs("alcadas", alcadaID, `{"ativo":true,"centro_custo_codigo":"CC1","filial":"F1","papel_aprovador":"Gerente","valor_maximo":null,"valor_minimo":100}`, testAtorID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectCommit()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "alcadas", "", csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"importados":1`) {
		t.Fatalf("corpo não indica 1 importado: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// --- Listar ---

// TestListarCadastroHandler_Success cobre o envelope {items, pagina, tamanho}
// (AD-14) e o clamp de "tamanho" quando um valor fora do intervalo 1–100 é
// pedido — ListarCadastroHandler não tinha nenhum teste antes deste.
func TestListarCadastroHandler_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	agora := time.Now()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, codigo, nome, created_at, updated_at FROM divisoes ORDER BY created_at, id LIMIT $1 OFFSET $2")).
		WithArgs(100, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "codigo", "nome", "created_at", "updated_at"}).
			AddRow(testCadastroID, "D1", "Divisao Um", agora, agora))

	handler := ListarCadastroHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cadastros/divisoes?tamanho=500", nil)
	req.SetPathValue("tipo", "divisoes")
	claims := jwt.MapClaims{"user_id": testAtorID, "perfil": "administrador"}
	req = req.WithContext(context.WithValue(req.Context(), ClaimsContextKey, claims))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"pagina":1`) {
		t.Fatalf("corpo não contém pagina=1: %s", body)
	}
	if !strings.Contains(body, `"tamanho":100`) {
		t.Fatalf("corpo não contém tamanho clampado para 100: %s", body)
	}
	if !strings.Contains(body, `"codigo":"D1"`) {
		t.Fatalf("corpo não contém o item esperado: %s", body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// --- Histórico ---

// TestHistoricoCadastroHandler_Success cobre o envelope paginado do
// histórico de um registro existente — HistoricoCadastroHandler não tinha
// nenhum teste antes deste.
func TestHistoricoCadastroHandler_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	agora := time.Now()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM divisoes WHERE id = $1)")).
		WithArgs(testCadastroID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT versao, acao, dados, ator_id, created_at FROM cadastro_historico WHERE tipo_cadastro = $1 AND registro_id = $2 ORDER BY versao DESC LIMIT $3 OFFSET $4")).
		WithArgs("divisoes", testCadastroID, 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"versao", "acao", "dados", "ator_id", "created_at"}).
			AddRow(1, "criado", []byte(`{"codigo":"D1","nome":"Divisao Um"}`), testAtorID, agora))

	handler := HistoricoCadastroHandler(db)
	req := newCadastroRequest(http.MethodGet, "divisoes", testCadastroID, "")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"pagina":1`) || !strings.Contains(body, `"tamanho":20`) {
		t.Fatalf("corpo não contém o envelope de paginação esperado: %s", body)
	}
	if !strings.Contains(body, `"versao":1`) || !strings.Contains(body, `"acao":"criado"`) {
		t.Fatalf("corpo não contém a linha de histórico esperada: %s", body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestHistoricoCadastroHandler_RegistroInexistente cobre 404 quando o
// registro-alvo não existe — nenhuma query de histórico chega a ser
// executada.
func TestHistoricoCadastroHandler_RegistroInexistente(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM divisoes WHERE id = $1)")).
		WithArgs(testCadastroID).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	handler := HistoricoCadastroHandler(db)
	req := newCadastroRequest(http.MethodGet, "divisoes", testCadastroID, "")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("esperado 404, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// --- Atualizar ---

// TestAtualizarCadastroHandler_Success cobre "Edição feliz": PUT em registro
// existente com campos válidos -> 200, linha atualizada, nova linha
// (versao+1, atualizado) no histórico.
func TestAtualizarCadastroHandler_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM divisoes WHERE id = $1 FOR UPDATE")).
		WithArgs(testCadastroID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testCadastroID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 0) + 1 FROM cadastro_historico WHERE tipo_cadastro = $1 AND registro_id = $2")).
		WithArgs("divisoes", testCadastroID).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(2))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE divisoes SET codigo = $1, nome = $2, updated_at = now() WHERE id = $3")).
		WithArgs("D1-novo", "Divisao Renomeada", testCadastroID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cadastro_historico")).
		WithArgs("divisoes", testCadastroID, 2, `{"codigo":"D1-novo","nome":"Divisao Renomeada"}`, testAtorID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := AtualizarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPut, "divisoes", testCadastroID, `{"codigo":"D1-novo","nome":"Divisao Renomeada"}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "D1-novo") {
		t.Fatalf("corpo não reflete o valor atualizado: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestAtualizarCadastroHandler_RegistroInexistente cobre "Edição de id
// inexistente": 404, nenhuma escrita.
func TestAtualizarCadastroHandler_RegistroInexistente(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM divisoes WHERE id = $1 FOR UPDATE")).
		WithArgs(testCadastroID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	handler := AtualizarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPut, "divisoes", testCadastroID, `{"codigo":"D1","nome":"Divisao Um"}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("esperado 404, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// --- Restaurar ---

// TestRestaurarCadastroHandler_Success cobre "Restauração feliz": campos
// voltam aos valores da versão alvo, nova linha (versao=max+1, restaurado).
func TestRestaurarCadastroHandler_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	dadosVersao1 := `{"codigo":"D1","nome":"Divisao Original"}`

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM divisoes WHERE id = $1 FOR UPDATE")).
		WithArgs(testCadastroID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testCadastroID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT dados FROM cadastro_historico WHERE tipo_cadastro = $1 AND registro_id = $2 AND versao = $3")).
		WithArgs("divisoes", testCadastroID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"dados"}).AddRow([]byte(dadosVersao1)))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 0) + 1 FROM cadastro_historico WHERE tipo_cadastro = $1 AND registro_id = $2")).
		WithArgs("divisoes", testCadastroID).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(2))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE divisoes SET codigo = $1, nome = $2, updated_at = now() WHERE id = $3")).
		WithArgs("D1", "Divisao Original", testCadastroID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cadastro_historico")).
		WithArgs("divisoes", testCadastroID, 2, dadosVersao1, testAtorID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := RestaurarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "divisoes", testCadastroID, `{"versao":1}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Divisao Original") {
		t.Fatalf("corpo não reflete os valores restaurados: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestRestaurarCadastroHandler_VersaoInexistente cobre "Restauração de
// versão inexistente": 404, nenhuma escrita.
func TestRestaurarCadastroHandler_VersaoInexistente(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM divisoes WHERE id = $1 FOR UPDATE")).
		WithArgs(testCadastroID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testCadastroID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT dados FROM cadastro_historico WHERE tipo_cadastro = $1 AND registro_id = $2 AND versao = $3")).
		WithArgs("divisoes", testCadastroID, 99).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	handler := RestaurarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "divisoes", testCadastroID, `{"versao":99}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("esperado 404, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// --- Autorização ---

// TestCadastroRoutes_RequireAdministrador cobre "Acesso sem perfil
// administrador": token válido com perfil=solicitante -> 403, mesma
// RequireAuth usada nas 5 rotas (registrada em main.go); o handler nunca é
// chamado, então db=nil é seguro.
func TestCadastroRoutes_RequireAdministrador(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-at-least-32-bytes-long!!")

	token, err := GenerateToken("user-789", "solicitante")
	if err != nil {
		t.Fatalf("GenerateToken falhou: %v", err)
	}

	called := false
	protected := RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}, "administrador")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/cadastros/divisoes", nil)
	req.SetPathValue("tipo", "divisoes")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	protected(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("esperado 403, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if called {
		t.Fatal("handler de cadastro não deveria ser chamado com perfil insuficiente")
	}
}
