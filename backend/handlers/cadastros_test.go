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

	csv := "codigo;nome;divisao_codigo;filial\nCC1;Centro Um;D1;F1\n"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_centros-custo").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM centros_custo)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM divisoes WHERE codigo = $1")).
		WithArgs("D1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(divisaoID))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO centros_custo (codigo, nome, divisao_id, filial) VALUES ($1, $2, $3, $4) RETURNING id")).
		WithArgs("CC1", "Centro Um", divisaoID, "F1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(centroCustoID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cadastro_historico")).
		WithArgs("centros-custo", centroCustoID, `{"codigo":"CC1","divisao_id":"33333333-3333-3333-3333-333333333333","filial":"F1","nome":"Centro Um"}`, testAtorID).
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

	csv := "codigo;nome;divisao_codigo;filial\nCC1;Centro Um;D-INEXISTENTE;\n"

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

// --- cc-excecao (Story 2.3, FR-4) ---
//
// Oitavo {tipo} no MESMO registry/Handler Factory da Story 2.1 — cobre a
// I/O Matrix própria desta story: import feliz/409/400×3 (e-mail inexistente,
// CC inexistente, par duplicado), PUT feliz, restaurar feliz, 403. Mesmo
// padrão httptest+sqlmock de TestImportarCadastroHandler_CentrosCusto_*.

const (
	testCcExcecaoColaboradorID = "c0000000-0000-0000-0000-000000000001"
	testCcExcecaoCentroCustoID = "c0000000-0000-0000-0000-000000000002"
	testCcExcecaoRegistroID    = "c0000000-0000-0000-0000-000000000003"
)

// TestImportarCadastroHandler_CcExcecao_Success cobre "Import feliz": CSV
// válido, e-mail já existe em usuarios, centro_custo_codigo existe -> 201,
// linha nasce em cc_excecao, histórico 'criado'. Resolução de ambas as FKs
// (colaborador_email -> usuarios.id, centro_custo_codigo -> centros_custo.id)
// é case-insensitive (LOWER/UPPER), mesma convenção de colaboradores.go.
func TestImportarCadastroHandler_CcExcecao_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "colaborador_email;centro_custo_codigo\nFulano@Exemplo.com;cc1\n"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_cc-excecao").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM cc_excecao)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("Fulano@Exemplo.com").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testCcExcecaoColaboradorID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM centros_custo WHERE UPPER(codigo) = UPPER($1)")).
		WithArgs("cc1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testCcExcecaoCentroCustoID))

	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO cc_excecao (colaborador_id, centro_custo_id) VALUES ($1, $2) RETURNING id")).
		WithArgs(testCcExcecaoColaboradorID, testCcExcecaoCentroCustoID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testCcExcecaoRegistroID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cadastro_historico")).
		WithArgs("cc-excecao", testCcExcecaoRegistroID,
			`{"centro_custo_id":"`+testCcExcecaoCentroCustoID+`","colaborador_id":"`+testCcExcecaoColaboradorID+`"}`,
			testAtorID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectCommit()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "cc-excecao", "", csv)
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

// TestImportarCadastroHandler_CcExcecao_TabelaNaoVazia cobre "Import com
// tabela não-vazia": nada é escrito, 409 "cadastro já possui dados" — mesmo
// padrão atômico dos 7 tipos da Story 2.1, não o best-effort de
// colaboradores.go (Design Notes da spec).
func TestImportarCadastroHandler_CcExcecao_TabelaNaoVazia(t *testing.T) {
	db, mock := newSQLMock(t)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_cc-excecao").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM cc_excecao)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectRollback()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "cc-excecao", "", "colaborador_email;centro_custo_codigo\nfulano@exemplo.com;CC1\n")
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

// TestImportarCadastroHandler_CcExcecao_EmailNaoEncontrado cobre "E-mail
// inexistente": colaborador_email sem usuarios correspondente -> nada é
// escrito, 400 citando a linha.
func TestImportarCadastroHandler_CcExcecao_EmailNaoEncontrado(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "colaborador_email;centro_custo_codigo\nfulano@exemplo.com;CC1\n"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_cc-excecao").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM cc_excecao)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("fulano@exemplo.com").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "cc-excecao", "", csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "linha 2") || !strings.Contains(rec.Body.String(), "não encontrado") {
		t.Fatalf("corpo não cita a linha/causa da rejeição: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nenhum INSERT deveria ter sido tentado): %v", err)
	}
}

// TestImportarCadastroHandler_CcExcecao_EmailVazio cobre decodeCSVCcExcecao
// rejeitando colaborador_email vazio -> nada é escrito, 400 citando a linha
// (campoObrigatorio falha antes de qualquer resolução em usuarios).
func TestImportarCadastroHandler_CcExcecao_EmailVazio(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "colaborador_email;centro_custo_codigo\n;CC1\n"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_cc-excecao").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM cc_excecao)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "cc-excecao", "", csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "linha 2") || !strings.Contains(rec.Body.String(), "colaborador_email") {
		t.Fatalf("corpo não cita a linha/causa da rejeição: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nenhuma resolução/INSERT deveria ter sido tentada): %v", err)
	}
}

// TestImportarCadastroHandler_CcExcecao_CentroCustoNaoEncontrado cobre "CC
// inexistente": centro_custo_codigo não existe -> nada é escrito, 400
// citando a linha.
func TestImportarCadastroHandler_CcExcecao_CentroCustoNaoEncontrado(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "colaborador_email;centro_custo_codigo\nfulano@exemplo.com;CC-INEXISTENTE\n"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_cc-excecao").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM cc_excecao)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("fulano@exemplo.com").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testCcExcecaoColaboradorID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM centros_custo WHERE UPPER(codigo) = UPPER($1)")).
		WithArgs("CC-INEXISTENTE").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "cc-excecao", "", csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "linha 2") || !strings.Contains(rec.Body.String(), "não encontrado") {
		t.Fatalf("corpo não cita a linha/causa da rejeição: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nenhum INSERT deveria ter sido tentado): %v", err)
	}
}

// TestImportarCadastroHandler_CcExcecao_ParDuplicado cobre "Mesmo par
// duplicado": CSV com 2 linhas colaborador+CC idênticas -> nada é escrito
// (transação inteira desfeita), 400 (violação UNIQUE) citando a linha.
func TestImportarCadastroHandler_CcExcecao_ParDuplicado(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "colaborador_email;centro_custo_codigo\nfulano@exemplo.com;CC1\nfulano@exemplo.com;CC1\n"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_cc-excecao").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM cc_excecao)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	// Linha 2 — insere com sucesso.
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("fulano@exemplo.com").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testCcExcecaoColaboradorID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM centros_custo WHERE UPPER(codigo) = UPPER($1)")).
		WithArgs("CC1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testCcExcecaoCentroCustoID))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO cc_excecao (colaborador_id, centro_custo_id) VALUES ($1, $2) RETURNING id")).
		WithArgs(testCcExcecaoColaboradorID, testCcExcecaoCentroCustoID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testCcExcecaoRegistroID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cadastro_historico")).
		WithArgs("cc-excecao", testCcExcecaoRegistroID,
			`{"centro_custo_id":"`+testCcExcecaoCentroCustoID+`","colaborador_id":"`+testCcExcecaoColaboradorID+`"}`,
			testAtorID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	// Linha 3 — mesmo par, violação UNIQUE no INSERT.
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("fulano@exemplo.com").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testCcExcecaoColaboradorID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM centros_custo WHERE UPPER(codigo) = UPPER($1)")).
		WithArgs("CC1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testCcExcecaoCentroCustoID))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO cc_excecao (colaborador_id, centro_custo_id) VALUES ($1, $2) RETURNING id")).
		WithArgs(testCcExcecaoColaboradorID, testCcExcecaoCentroCustoID).
		WillReturnError(&pq.Error{Code: "23505"})
	mock.ExpectRollback()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "cc-excecao", "", csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "linha 3") || !strings.Contains(rec.Body.String(), "registro duplicado") {
		t.Fatalf("corpo não cita a linha/causa da rejeição: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestAtualizarCadastroHandler_CentrosCusto_FilialAusente cobre
// extractStringOpcional exigindo a chave "filial" presente no corpo de PUT
// de "centros-custo" (Story 3.1 adicionou esta coluna opcional) — mesmo
// padrão de TestAtualizarCadastroHandler_Alcadas_ValorMaximoAusente. Antes
// desta story, decodeJSONCentrosCusto só exigia codigo/nome/divisao_id;
// qualquer chamador pré-existente que não envie "filial" agora recebe 400,
// então este teste fixa esse contrato (em vez de deixá-lo sem nenhuma
// cobertura, como estava).
func TestAtualizarCadastroHandler_CentrosCusto_FilialAusente(t *testing.T) {
	handler := AtualizarCadastroHandler(nil)
	body := `{"codigo":"CC1","nome":"Centro Um","divisao_id":"33333333-3333-3333-3333-333333333333"}`
	req := newCadastroRequest(http.MethodPut, "centros-custo", testCadastroID, body)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "filial") {
		t.Fatalf("corpo não cita o campo ausente: %s", rec.Body.String())
	}
}

// TestAtualizarCadastroHandler_CentrosCusto_FilialPreenchida cobre o
// caminho feliz com a chave "filial" presente e preenchida — complementa o
// teste de ausência acima e fixa que o tipo "centros-custo" continua
// editável por PUT depois da Story 3.1.
func TestAtualizarCadastroHandler_CentrosCusto_FilialPreenchida(t *testing.T) {
	db, mock := newSQLMock(t)

	const divisaoID = "33333333-3333-3333-3333-333333333333"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM centros_custo WHERE id = $1 FOR UPDATE")).
		WithArgs(testCadastroID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testCadastroID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 0) + 1 FROM cadastro_historico WHERE tipo_cadastro = $1 AND registro_id = $2")).
		WithArgs("centros-custo", testCadastroID).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(2))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE centros_custo SET codigo = $1, nome = $2, divisao_id = $3, filial = $4, updated_at = now() WHERE id = $5")).
		WithArgs("CC1-novo", "Centro Um", divisaoID, "F2", testCadastroID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cadastro_historico")).
		WithArgs("centros-custo", testCadastroID, 2, `{"codigo":"CC1-novo","divisao_id":"33333333-3333-3333-3333-333333333333","filial":"F2","nome":"Centro Um"}`, testAtorID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := AtualizarCadastroHandler(db)
	body := `{"codigo":"CC1-novo","nome":"Centro Um","divisao_id":"` + divisaoID + `","filial":"F2"}`
	req := newCadastroRequest(http.MethodPut, "centros-custo", testCadastroID, body)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestAtualizarCadastroHandler_RegrasAprovacao_AutorExclusivo cobre
// validarAutorExclusivo: uma linha de "regras-aprovacao" com
// colaborador_id E papel_aprovador preenchidos ao mesmo tempo é 400 — sem
// esta checagem, montarAprovador prioriza colaborador_id silenciosamente,
// descartando o papel_aprovador que o administrador também curou.
func TestAtualizarCadastroHandler_RegrasAprovacao_AutorExclusivo(t *testing.T) {
	handler := AtualizarCadastroHandler(nil)
	body := `{"precedencia":1,"filial":"F1","centro_custo_codigo":"CC1","valor_minimo":100,"valor_maximo":null,"colaborador_id":"11111111-1111-1111-1111-111111111111","papel_aprovador":"Gerente","ativo":true}`
	req := newCadastroRequest(http.MethodPut, "regras-aprovacao", testCadastroID, body)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no máximo um") {
		t.Fatalf("corpo não cita a exclusividade esperada: %s", rec.Body.String())
	}
}

// TestAtualizarCadastroHandler_GerentesAprovacao_AutorExclusivo cobre a
// mesma checagem para "gerentes-aprovacao".
func TestAtualizarCadastroHandler_GerentesAprovacao_AutorExclusivo(t *testing.T) {
	handler := AtualizarCadastroHandler(nil)
	body := `{"centro_custo_id":"cc000000-0000-0000-0000-000000000001","divisao_id":null,"colaborador_id":"11111111-1111-1111-1111-111111111111","papel_aprovador":"Gerente","teto":20000,"ativo":true}`
	req := newCadastroRequest(http.MethodPut, "gerentes-aprovacao", testCadastroID, body)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no máximo um") {
		t.Fatalf("corpo não cita a exclusividade esperada: %s", rec.Body.String())
	}
}

// TestAtualizarCadastroHandler_CcExcecao_Success cobre "Edição feliz": PUT
// com novo centro_custo_id válido (já resolvido, UUID) -> 200, registro
// atualizado, nova versão em histórico.
func TestAtualizarCadastroHandler_CcExcecao_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	const novoCentroCustoID = "c0000000-0000-0000-0000-000000000099"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM cc_excecao WHERE id = $1 FOR UPDATE")).
		WithArgs(testCcExcecaoRegistroID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testCcExcecaoRegistroID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 0) + 1 FROM cadastro_historico WHERE tipo_cadastro = $1 AND registro_id = $2")).
		WithArgs("cc-excecao", testCcExcecaoRegistroID).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(2))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE cc_excecao SET colaborador_id = $1, centro_custo_id = $2, updated_at = now() WHERE id = $3")).
		WithArgs(testCcExcecaoColaboradorID, novoCentroCustoID, testCcExcecaoRegistroID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cadastro_historico")).
		WithArgs("cc-excecao", testCcExcecaoRegistroID, 2,
			`{"centro_custo_id":"`+novoCentroCustoID+`","colaborador_id":"`+testCcExcecaoColaboradorID+`"}`,
			testAtorID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := AtualizarCadastroHandler(db)
	body := `{"colaborador_id":"` + testCcExcecaoColaboradorID + `","centro_custo_id":"` + novoCentroCustoID + `"}`
	req := newCadastroRequest(http.MethodPut, "cc-excecao", testCcExcecaoRegistroID, body)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), novoCentroCustoID) {
		t.Fatalf("corpo não reflete o novo centro_custo_id: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestAtualizarCadastroHandler_CcExcecao_CampoAusente cobre decodeJSONCcExcecao
// rejeitando corpo sem 'centro_custo_id' -> 400, nenhuma query tentada
// (decode acontece antes de db.Begin).
func TestAtualizarCadastroHandler_CcExcecao_CampoAusente(t *testing.T) {
	handler := AtualizarCadastroHandler(nil)
	body := `{"colaborador_id":"` + testCcExcecaoColaboradorID + `"}`
	req := newCadastroRequest(http.MethodPut, "cc-excecao", testCcExcecaoRegistroID, body)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "centro_custo_id") {
		t.Fatalf("corpo não cita o campo ausente: %s", rec.Body.String())
	}
}

// TestAtualizarCadastroHandler_CcExcecao_UUIDInvalido cobre decodeJSONCcExcecao
// rejeitando 'colaborador_id' que não bate com o formato UUID -> 400.
func TestAtualizarCadastroHandler_CcExcecao_UUIDInvalido(t *testing.T) {
	handler := AtualizarCadastroHandler(nil)
	body := `{"colaborador_id":"nao-e-um-uuid","centro_custo_id":"` + testCcExcecaoCentroCustoID + `"}`
	req := newCadastroRequest(http.MethodPut, "cc-excecao", testCcExcecaoRegistroID, body)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "colaborador_id") {
		t.Fatalf("corpo não cita o campo inválido: %s", rec.Body.String())
	}
}

// TestRestaurarCadastroHandler_CcExcecao_Success cobre "Restauração feliz":
// campos voltam aos valores da versão alvo, nova versão 'restaurado'.
func TestRestaurarCadastroHandler_CcExcecao_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	dadosVersao1 := `{"centro_custo_id":"` + testCcExcecaoCentroCustoID + `","colaborador_id":"` + testCcExcecaoColaboradorID + `"}`

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM cc_excecao WHERE id = $1 FOR UPDATE")).
		WithArgs(testCcExcecaoRegistroID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(testCcExcecaoRegistroID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT dados FROM cadastro_historico WHERE tipo_cadastro = $1 AND registro_id = $2 AND versao = $3")).
		WithArgs("cc-excecao", testCcExcecaoRegistroID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"dados"}).AddRow([]byte(dadosVersao1)))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 0) + 1 FROM cadastro_historico WHERE tipo_cadastro = $1 AND registro_id = $2")).
		WithArgs("cc-excecao", testCcExcecaoRegistroID).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(2))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE cc_excecao SET colaborador_id = $1, centro_custo_id = $2, updated_at = now() WHERE id = $3")).
		WithArgs(testCcExcecaoColaboradorID, testCcExcecaoCentroCustoID, testCcExcecaoRegistroID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cadastro_historico")).
		WithArgs("cc-excecao", testCcExcecaoRegistroID, 2, dadosVersao1, testAtorID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	handler := RestaurarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "cc-excecao", testCcExcecaoRegistroID, `{"versao":1}`)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("esperado 200, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), testCcExcecaoCentroCustoID) {
		t.Fatalf("corpo não reflete os valores restaurados: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestCadastroRoutes_CcExcecao_RequireAdministrador cobre "Acesso sem perfil
// administrador" especificamente para {tipo}=cc-excecao -- 403, handler nunca
// chamado.
func TestCadastroRoutes_CcExcecao_RequireAdministrador(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret-at-least-32-bytes-long!!")

	token, err := GenerateToken("user-790", "solicitante")
	if err != nil {
		t.Fatalf("GenerateToken falhou: %v", err)
	}

	called := false
	protected := RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}, "administrador")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/cadastros/cc-excecao", nil)
	req.SetPathValue("tipo", "cc-excecao")
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

// --- regras-aprovacao / gerentes-aprovacao (Story 3.1) ---

// TestImportarCadastroHandler_RegrasAprovacao_Success cobre o caminho feliz
// de "regras-aprovacao": colaborador_email vazio (linha identificada por
// papel_aprovador, não por pessoa) -> colaborador_id grava NULL sem
// consultar `usuarios`.
func TestImportarCadastroHandler_RegrasAprovacao_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	const regraID = "77777777-7777-7777-7777-777777777777"
	csv := "precedencia;filial;centro_custo_codigo;valor_minimo;valor_maximo;colaborador_email;papel_aprovador;ativo\n" +
		"1;F1;CC1;1000;;;Gerente Financeiro;true\n"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_regras-aprovacao").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM regras_aprovacao)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	mock.ExpectQuery(regexp.QuoteMeta(
		"INSERT INTO regras_aprovacao (precedencia, filial, centro_custo_codigo, valor_minimo, valor_maximo, colaborador_id, papel_aprovador, ativo) VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id",
	)).
		WithArgs(1.0, "F1", "CC1", 1000.0, nil, nil, "Gerente Financeiro", true).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(regraID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cadastro_historico")).
		WithArgs(
			"regras-aprovacao", regraID,
			`{"ativo":true,"centro_custo_codigo":"CC1","colaborador_id":null,"filial":"F1","papel_aprovador":"Gerente Financeiro","precedencia":1,"valor_maximo":null,"valor_minimo":1000}`,
			testAtorID,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectCommit()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "regras-aprovacao", "", csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestImportarCadastroHandler_RegrasAprovacao_SemAprovador cobre
// validarAutorRegra: colaborador_email E papel_aprovador vazios na mesma
// linha -> 400, nada gravado (uma linha assim casaria no resolver e
// produziria um Aprovador sem nome nem titular).
func TestImportarCadastroHandler_RegrasAprovacao_SemAprovador(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "precedencia;filial;centro_custo_codigo;valor_minimo;valor_maximo;colaborador_email;papel_aprovador;ativo\n" +
		"1;F1;CC1;1000;;;;true\n"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_regras-aprovacao").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM regras_aprovacao)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "regras-aprovacao", "", csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nenhum INSERT deveria ter sido tentado): %v", err)
	}
}

// TestImportarCadastroHandler_GerentesAprovacao_FallbackDivisao cobre a
// resolução de FK divisao_codigo -> divisao_id com centro_custo_codigo
// vazio (linha de fallback por divisão, Design Notes da spec) e "teto"
// vazio assumindo o default 20000.
func TestImportarCadastroHandler_GerentesAprovacao_FallbackDivisao(t *testing.T) {
	db, mock := newSQLMock(t)

	const divisaoID = "88888888-8888-8888-8888-888888888888"
	const gerenteID = "99999999-9999-9999-9999-999999999999"
	csv := "centro_custo_codigo;divisao_codigo;colaborador_email;papel_aprovador;teto;ativo\n" +
		";D1;;Gerentes Regionais de Logística;;true\n"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_gerentes-aprovacao").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM gerentes_aprovacao)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM divisoes WHERE codigo = $1")).
		WithArgs("D1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(divisaoID))

	mock.ExpectQuery(regexp.QuoteMeta(
		"INSERT INTO gerentes_aprovacao (centro_custo_id, divisao_id, colaborador_id, papel_aprovador, teto, ativo) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id",
	)).
		WithArgs(nil, divisaoID, nil, "Gerentes Regionais de Logística", 20000.0, true).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(gerenteID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cadastro_historico")).
		WithArgs(
			"gerentes-aprovacao", gerenteID,
			`{"ativo":true,"centro_custo_id":null,"colaborador_id":null,"divisao_id":"88888888-8888-8888-8888-888888888888","papel_aprovador":"Gerentes Regionais de Logística","teto":20000}`,
			testAtorID,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectCommit()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "gerentes-aprovacao", "", csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("esperado 201, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestImportarCadastroHandler_GerentesAprovacao_CCEDivisaoAoMesmoTempo
// cobre validarCCXorDivisao: centro_custo_codigo E divisao_codigo
// preenchidos na mesma linha -> 400, nada gravado.
func TestImportarCadastroHandler_GerentesAprovacao_CCEDivisaoAoMesmoTempo(t *testing.T) {
	db, mock := newSQLMock(t)

	const centroCustoID = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	const divisaoID = "88888888-8888-8888-8888-888888888888"
	csv := "centro_custo_codigo;divisao_codigo;colaborador_email;papel_aprovador;teto;ativo\n" +
		"CC1;D1;;Gerente;;true\n"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_gerentes-aprovacao").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM gerentes_aprovacao)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM centros_custo WHERE UPPER(codigo) = UPPER($1)")).
		WithArgs("CC1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(centroCustoID))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM divisoes WHERE codigo = $1")).
		WithArgs("D1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(divisaoID))
	mock.ExpectRollback()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "gerentes-aprovacao", "", csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nenhum INSERT deveria ter sido tentado): %v", err)
	}
}

// --- Story 3.3 — "autorizadores-formulario" (FR-6/FR-11) ---
//
// Mesmo padrão de "regras-aprovacao": colaborador_email resolvido para
// colaborador_id dentro da MESMA transação do import — mas aqui
// OBRIGATÓRIO (resolveColaboradorIDPorEmail, não ...Opcional), já que
// autorizadores_formulario é SEMPRE pessoa, nunca papel_aprovador
// (Boundaries "Always" da spec 3.3).

// TestImportarCadastroHandler_AutorizadoresFormulario_Success cobre o
// caminho feliz: colaborador_email resolvido -> colaborador_id, ativo=true.
func TestImportarCadastroHandler_AutorizadoresFormulario_Success(t *testing.T) {
	db, mock := newSQLMock(t)

	const colaboradorID = "d0000000-0000-0000-0000-000000000001"
	const registroID = "d0000000-0000-0000-0000-000000000002"
	csv := "centro_custo_codigo;valor_minimo;valor_maximo;colaborador_email;ativo\n" +
		"CC1;1000;5000;fulano@exemplo.com;true\n"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_autorizadores-formulario").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM autorizadores_formulario)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("fulano@exemplo.com").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(colaboradorID))

	mock.ExpectQuery(regexp.QuoteMeta(
		"INSERT INTO autorizadores_formulario (centro_custo_codigo, valor_minimo, valor_maximo, colaborador_id, ativo) VALUES ($1, $2, $3, $4, $5) RETURNING id",
	)).
		WithArgs("CC1", 1000.0, 5000.0, colaboradorID, true).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(registroID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO cadastro_historico")).
		WithArgs(
			"autorizadores-formulario", registroID,
			`{"ativo":true,"centro_custo_codigo":"CC1","colaborador_id":"`+colaboradorID+`","valor_maximo":5000,"valor_minimo":1000}`,
			testAtorID,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	mock.ExpectCommit()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "autorizadores-formulario", "", csv)
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

// TestImportarCadastroHandler_AutorizadoresFormulario_EmailNaoEncontrado
// cobre "E-mail inexistente" (mesmo padrão de cc-excecao/regras-aprovacao):
// colaborador_email sem usuarios correspondente -> nada é escrito, 400
// citando a linha.
func TestImportarCadastroHandler_AutorizadoresFormulario_EmailNaoEncontrado(t *testing.T) {
	db, mock := newSQLMock(t)

	csv := "centro_custo_codigo;valor_minimo;valor_maximo;colaborador_email;ativo\n" +
		"CC1;1000;5000;fulano@exemplo.com;true\n"

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("SELECT pg_advisory_xact_lock(hashtext($1))")).
		WithArgs("cadastro_import_autorizadores-formulario").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT EXISTS(SELECT 1 FROM autorizadores_formulario)")).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id FROM usuarios WHERE LOWER(email) = LOWER($1)")).
		WithArgs("fulano@exemplo.com").
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	handler := ImportarCadastroHandler(db)
	req := newCadastroRequest(http.MethodPost, "autorizadores-formulario", "", csv)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "linha 2") || !strings.Contains(rec.Body.String(), "não encontrado") {
		t.Fatalf("corpo não cita a linha/causa da rejeição: %s", rec.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (nenhum INSERT deveria ter sido tentado): %v", err)
	}
}

// TestAtualizarCadastroHandler_AutorizadoresFormulario_ColaboradorIDAusente
// cobre decodeJSONAutorizadoresFormulario rejeitando corpo sem
// 'colaborador_id' -> 400, nenhuma query tentada (decode acontece antes de
// db.Begin). Diferente de "regras-aprovacao" (onde colaborador_id é
// opcional), aqui é OBRIGATÓRIO — autorizadores_formulario é sempre pessoa
// (Boundaries "Always" da spec 3.3).
func TestAtualizarCadastroHandler_AutorizadoresFormulario_ColaboradorIDAusente(t *testing.T) {
	handler := AtualizarCadastroHandler(nil)
	const registroID = "d0000000-0000-0000-0000-000000000003"
	body := `{"centro_custo_codigo":"CC1","valor_minimo":1000,"valor_maximo":5000,"ativo":true}`
	req := newCadastroRequest(http.MethodPut, "autorizadores-formulario", registroID, body)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "colaborador_id") {
		t.Fatalf("corpo não cita o campo ausente: %s", rec.Body.String())
	}
}

// TestAtualizarCadastroHandler_AutorizadoresFormulario_ColaboradorIDInvalido
// cobre decodeJSONAutorizadoresFormulario rejeitando 'colaborador_id' que não
// bate com o formato UUID -> 400.
func TestAtualizarCadastroHandler_AutorizadoresFormulario_ColaboradorIDInvalido(t *testing.T) {
	handler := AtualizarCadastroHandler(nil)
	const registroID = "d0000000-0000-0000-0000-000000000003"
	body := `{"centro_custo_codigo":"CC1","valor_minimo":1000,"valor_maximo":5000,"colaborador_id":"nao-e-um-uuid","ativo":true}`
	req := newCadastroRequest(http.MethodPut, "autorizadores-formulario", registroID, body)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperado 400, obtido %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "colaborador_id") {
		t.Fatalf("corpo não cita o campo inválido: %s", rec.Body.String())
	}
}
