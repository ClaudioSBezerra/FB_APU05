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
	"github.com/lib/pq"
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

// TestImportarCadastroHandler_CentrosCusto_DivisaoNaoEncontrada cobre a
// rejeição de resolveDivisaoID quando divisao_codigo não existe em
// `divisoes` (Design Notes: "não encontrado -> rejeita a carga citando a
// linha") — só o caminho feliz dessa resolução tinha teste antes deste.
func TestImportarCadastroHandler_CentrosCusto_DivisaoNaoEncontrada(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "codigo;nome;divisao_codigo\nCC1;Centro Um;D-INEXISTENTE\n"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_centros-custo").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM centros_custo)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM divisoes WHERE codigo = $1")).
		WithArgs("D-INEXISTENTE").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "centros-custo", "", csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "linha 2") || !strings.Contains(rec.Body.String(), "não encontrada") {
		t.Fatalf("corpo não cita a linha/causa da rejeição: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nenhum INSERT deveria ter sido tentado): %v", err)
	}
}

// TestImportarCadastroHandler_Alcadas_FaixaInvertida cobre validarFaixaAlcada
// rejeitando valor_maximo < valor_minimo — o único teste de import de
// "alcadas" antes deste usava valor_maximo vazio, que nunca alcança essa
// comparação.
func TestImportarCadastroHandler_Alcadas_FaixaInvertida(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "filial;centro_custo_codigo;valor_minimo;valor_maximo;papel_aprovador;ativo\nF1;CC1;500;100;Gerente;true\n"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_alcadas").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM alcadas)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "alcadas", "", csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "linha 2") || !strings.Contains(rec.Body.String(), "valor_maximo") {
		t.Fatalf("corpo não cita a linha/causa da rejeição: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nenhum INSERT deveria ter sido tentado): %v", err)
	}
}

// TestImportarCadastroHandler_Contas_Success cobre o decode de "contas"
// (validarPlano + PK composta plano+codigo) — nenhum teste deste arquivo
// exercitava esse tipo antes deste.
func TestImportarCadastroHandler_Contas_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	const contaID = "77777777-7777-7777-7777-777777777777"
	csv := "plano;codigo;nome\nBIFC;1000;Caixa\n"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_contas").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM contas)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO contas (plano, codigo, nome) VALUES ($1, $2, $3) RETURNING id")).
		WithArgs("BIFC", "1000", "Caixa").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(contaID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cadastro_historico")).
		WithArgs("contas", contaID, `{"codigo":"1000","nome":"Caixa","plano":"BIFC"}`, testAtorID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "contas", "", csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestImportarCadastroHandler_Contas_PlanoInvalido cobre validarPlano
// rejeitando um "plano" fora de {BIFC, SFC} — a única regra de validação
// própria de "contas" não tinha nenhum teste antes deste.
func TestImportarCadastroHandler_Contas_PlanoInvalido(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "plano;codigo;nome\nXYZ;1000;Caixa\n"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_contas").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM contas)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "contas", "", csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "linha 2") || !strings.Contains(rec.Body.String(), "plano") {
		t.Fatalf("corpo não cita a linha/causa da rejeição: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestImportarCadastroHandler_ClassesImobilizado_Success cobre o decode de
// "classes-imobilizado" — nenhum teste deste arquivo exercitava esse tipo
// antes deste.
func TestImportarCadastroHandler_ClassesImobilizado_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	const classeID = "88888888-8888-8888-8888-888888888888"
	csv := "codigo;nome\nCI1;Veiculos\n"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_classes-imobilizado").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM classes_imobilizado)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO classes_imobilizado (codigo, nome) VALUES ($1, $2) RETURNING id")).
		WithArgs("CI1", "Veiculos").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(classeID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cadastro_historico")).
		WithArgs("classes-imobilizado", classeID, `{"codigo":"CI1","nome":"Veiculos"}`, testAtorID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "classes-imobilizado", "", csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestImportarCadastroHandler_PapelPessoa_Success cobre o decode de
// "papel-pessoa" — nenhum teste deste arquivo exercitava esse tipo antes
// deste.
func TestImportarCadastroHandler_PapelPessoa_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	const papelID = "99999999-9999-9999-9999-999999999999"
	csv := "papel;pessoa_nome\nGerente;Fulano de Tal\n"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_papel-pessoa").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM papel_pessoa)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO papel_pessoa (papel, pessoa_nome) VALUES ($1, $2) RETURNING id")).
		WithArgs("Gerente", "Fulano de Tal").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(papelID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cadastro_historico")).
		WithArgs("papel-pessoa", papelID, `{"papel":"Gerente","pessoa_nome":"Fulano de Tal"}`, testAtorID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "papel-pessoa", "", csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestImportarCadastroHandler_Feriados_Success cobre o decode de "feriados"
// (parseDataISO) — nenhum teste deste arquivo exercitava esse tipo antes
// deste.
func TestImportarCadastroHandler_Feriados_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	const feriadoID = "10101010-1010-1010-1010-101010101010"
	csv := "data;descricao\n2024-01-01;Confraternização Universal\n"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_feriados").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM feriados)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO feriados (data, descricao) VALUES ($1, $2) RETURNING id")).
		WithArgs("2024-01-01", "Confraternização Universal").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(feriadoID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cadastro_historico")).
		WithArgs("feriados", feriadoID, `{"data":"2024-01-01","descricao":"Confraternização Universal"}`, testAtorID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "feriados", "", csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d (body=%s)", rec.Code, rec.Body.String())
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

// TestListarCadastroHandler_Feriados_DataFormato cobre a listagem do único
// tipo com coluna colData ("feriados") — regressão do achado [high] da
// primeira passada de revisão (colData escaneado como *string saía em
// RFC3339Nano em vez de YYYY-MM-DD); nenhum teste deste arquivo exercitava
// esse tipo antes deste, então o fix ficava sem nenhuma rede de proteção.
func TestListarCadastroHandler_Feriados_DataFormato(t *testing.T) {
	db, mock := newSQLMock(t)

	agora := time.Now()
	dataFeriado := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, data, descricao, created_at, updated_at FROM feriados ORDER BY created_at, id LIMIT $1 OFFSET $2")).
		WithArgs(20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "data", "descricao", "created_at", "updated_at"}).
			AddRow(testCadastroID, dataFeriado, "Confraternização Universal", agora, agora))

	handler := ListarCadastroHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cadastros/feriados", nil)
	req.SetPathValue("tipo", "feriados")
	claims := jwt.MapClaims{"user_id": testAtorID, "perfil": "administrador"}
	req = req.WithContext(context.WithValue(req.Context(), ClaimsContextKey, claims))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"data":"2024-01-02"`) {
		t.Fatalf("corpo não contém a data no formato YYYY-MM-DD: %s", body)
	}
	if strings.Contains(body, "T00:00:00Z") {
		t.Fatalf("corpo vazou formato RFC3339Nano em vez de YYYY-MM-DD: %s", body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestListarCadastroHandler_PaginaExtrema cobre o clamp de "pagina" (ao
// contrário de "tamanho", que já tinha teste) — sem o clamp, um valor de
// pagina suficientemente grande overflowaria (pagina-1)*tamanho num OFFSET
// negativo e o Postgres devolveria 500 em vez de um 400/200 previsível.
func TestListarCadastroHandler_PaginaExtrema(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, codigo, nome, created_at, updated_at FROM divisoes ORDER BY created_at, id LIMIT $1 OFFSET $2")).
		WithArgs(20, 19999980).
		WillReturnRows(sqlmock.NewRows([]string{"id", "codigo", "nome", "created_at", "updated_at"}))

	handler := ListarCadastroHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/cadastros/divisoes?pagina=5000000", nil)
	req.SetPathValue("tipo", "divisoes")
	claims := jwt.MapClaims{"user_id": testAtorID, "perfil": "administrador"}
	req = req.WithContext(context.WithValue(req.Context(), ClaimsContextKey, claims))
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"pagina":1000000`) {
		t.Fatalf("corpo não contém pagina clampada para 1000000: %s", rec.Body.String())
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

// TestAtualizarCadastroHandler_Alcadas_ValorMaximoAusente cobre
// extractFloatOpcional exigindo a chave "valor_maximo" presente no corpo de
// PUT (ausência = erro de digitação do cliente, não "sem teto" silencioso)
// — nenhum teste deste arquivo exercitava PUT de "alcadas" antes deste.
func TestAtualizarCadastroHandler_Alcadas_ValorMaximoAusente(t *testing.T) {
	handler := AtualizarCadastroHandler(nil)
	body := `{"filial":"F1","centro_custo_codigo":"CC1","valor_minimo":100,"papel_aprovador":"Gerente","ativo":true}`
	req := newCadastroRequest(http.MethodPut, "alcadas", testCadastroID, body)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "valor_maximo") {
		t.Fatalf("corpo não cita o campo ausente: %s", rec.Body.String())
	}
}

// TestAtualizarCadastroHandler_Alcadas_ValorMaximoNuloExplicito cobre o
// caminho feliz de "sem teto" via PUT (chave presente, valor `null`) —
// complementa o teste de ausência acima.
func TestAtualizarCadastroHandler_Alcadas_ValorMaximoNuloExplicito(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM alcadas WHERE id = $1 FOR UPDATE")).
		WithArgs(testCadastroID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testCadastroID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 0) + 1 FROM cadastro_historico WHERE tipo_cadastro = $1 AND registro_id = $2")).
		WithArgs("alcadas", testCadastroID).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(2))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE alcadas SET filial = $1, centro_custo_codigo = $2, valor_minimo = $3, valor_maximo = $4, papel_aprovador = $5, ativo = $6, updated_at = now() WHERE id = $7")).
		WithArgs("F1", "CC1", 100.0, nil, "Gerente", true, testCadastroID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cadastro_historico")).
		WithArgs("alcadas", testCadastroID, 2, `{"ativo":true,"centro_custo_codigo":"CC1","filial":"F1","papel_aprovador":"Gerente","valor_maximo":null,"valor_minimo":100}`, testAtorID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := AtualizarCadastroHandler(db)
	body := `{"filial":"F1","centro_custo_codigo":"CC1","valor_minimo":100,"valor_maximo":null,"papel_aprovador":"Gerente","ativo":true}`
	req := newCadastroRequest(http.MethodPut, "alcadas", testCadastroID, body)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
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

// TestRestaurarCadastroHandler_ViolacaoConstraint cobre uma violação de
// constraint (ex. UNIQUE) no UPDATE de restauração — diferente de
// Atualizar/Importar, essa rota não mapeava violação de constraint para 400
// antes deste fix, devolvendo 500 para qualquer erro de UPDATE.
func TestRestaurarCadastroHandler_ViolacaoConstraint(t *testing.T) {
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
		WillReturnError(&pq.Error{Code: "23505"})
	mock.ExpectRollback()

	handler := RestaurarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "divisoes", testCadastroID, `{"versao":1}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "registro duplicado") {
		t.Fatalf("corpo não contém a mensagem amigável de violação de constraint: %s", rec.Body.String())
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
