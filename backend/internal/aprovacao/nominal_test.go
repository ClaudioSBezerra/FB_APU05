package aprovacao

// nominal_test.go — cobre AutorizadorNominal.Resolve (Story 3.3, FR-6/FR-11)
// no nível de pacote: limites inclusivos e "sem teto" do predicado SQL,
// ErrAutorizadorInvalido sem match, e os branches de erro genérico de banco
// (consulta principal, versaoDoCadastro, nomeColaborador) que a I/O Matrix
// via HTTP (handlers/solicitacoes_test.go) não alcança — mesmo padrão
// sqlmock de calculado_test.go (Review Triage Log da spec 3.3).

import (
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func baseSolicitacaoNominal() Solicitacao {
	return Solicitacao{
		Filial:                 "F1",
		CentroCustoID:          "cc-1111-1111-1111-1111-111111111111",
		CentroCustoCodigo:      "CC1",
		DivisaoID:              "div-111-1111-1111-1111-111111111111",
		Valor:                  1000,
		AutorizadorEscolhidoID: "colab-1",
	}
}

// TestAutorizadorNominal_Match cobre o caminho feliz com valor estritamente
// dentro da faixa (valor_minimo < valor < valor_maximo).
func TestAutorizadorNominal_Match(t *testing.T) {
	db, mock, closeFn := newAprovacaoSQLMock(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("FROM autorizadores_formulario")).
		WithArgs("CC1", "colab-1", 1000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("regra-1"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 1) FROM cadastro_historico")).
		WithArgs("autorizadores-formulario", "regra-1").
		WillReturnRows(sqlmock.NewRows([]string{"versao"}).AddRow(2))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT nome FROM usuarios WHERE id = $1")).
		WithArgs("colab-1").
		WillReturnRows(sqlmock.NewRows([]string{"nome"}).AddRow("Fulano Autorizador"))

	nominal := NovoAutorizadorNominal(db)
	aprovador, err := nominal.Resolve(baseSolicitacaoNominal())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if aprovador.Tipo != TipoPessoa || aprovador.Nome != "Fulano Autorizador" {
		t.Fatalf("aprovador inesperado: %+v", aprovador)
	}
	if aprovador.ColaboradorID == nil || *aprovador.ColaboradorID != "colab-1" {
		t.Fatalf("ColaboradorID inesperado: %+v", aprovador)
	}
	if aprovador.RegraID != "regra-1" || aprovador.RegraVersao != 2 {
		t.Fatalf("regra_id/regra_versao inesperados: %+v", aprovador)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestAutorizadorNominal_LimitesInclusivos cobre valor == valor_minimo e
// valor == valor_maximo — o predicado SQL usa <=/>=, não </>.
func TestAutorizadorNominal_LimitesInclusivos(t *testing.T) {
	for _, valor := range []float64{100, 500} {
		db, mock, closeFn := newAprovacaoSQLMock(t)

		mock.ExpectQuery(regexp.QuoteMeta("FROM autorizadores_formulario")).
			WithArgs("CC1", "colab-1", valor).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("regra-1"))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 1) FROM cadastro_historico")).
			WithArgs("autorizadores-formulario", "regra-1").
			WillReturnRows(sqlmock.NewRows([]string{"versao"}).AddRow(1))
		mock.ExpectQuery(regexp.QuoteMeta("SELECT nome FROM usuarios WHERE id = $1")).
			WithArgs("colab-1").
			WillReturnRows(sqlmock.NewRows([]string{"nome"}).AddRow("Fulano Autorizador"))

		s := baseSolicitacaoNominal()
		s.Valor = valor
		nominal := NovoAutorizadorNominal(db)
		if _, err := nominal.Resolve(s); err != nil {
			t.Fatalf("valor=%v: erro inesperado: %v", valor, err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("valor=%v: expectativas do mock não satisfeitas: %v", valor, err)
		}
		closeFn()
	}
}

// TestAutorizadorNominal_SemTeto cobre valor_maximo IS NULL (sem teto) —
// qualquer valor >= valor_minimo casa.
func TestAutorizadorNominal_SemTeto(t *testing.T) {
	db, mock, closeFn := newAprovacaoSQLMock(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("FROM autorizadores_formulario")).
		WithArgs("CC1", "colab-1", 1000000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("regra-sem-teto"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 1) FROM cadastro_historico")).
		WithArgs("autorizadores-formulario", "regra-sem-teto").
		WillReturnRows(sqlmock.NewRows([]string{"versao"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT nome FROM usuarios WHERE id = $1")).
		WithArgs("colab-1").
		WillReturnRows(sqlmock.NewRows([]string{"nome"}).AddRow("Fulano Autorizador"))

	s := baseSolicitacaoNominal()
	s.Valor = 1000000
	nominal := NovoAutorizadorNominal(db)
	if _, err := nominal.Resolve(s); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestAutorizadorNominal_SemMatch cobre ErrAutorizadorInvalido — nenhuma
// linha ativa cobre o CC/colaborador/faixa de valor; nunca sintetiza um
// Aprovador.
func TestAutorizadorNominal_SemMatch(t *testing.T) {
	db, mock, closeFn := newAprovacaoSQLMock(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("FROM autorizadores_formulario")).
		WithArgs("CC1", "colab-1", 1000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	nominal := NovoAutorizadorNominal(db)
	_, err := nominal.Resolve(baseSolicitacaoNominal())
	if err == nil {
		t.Fatal("esperado ErrAutorizadorInvalido, obtido nil")
	}
	invalido, ok := err.(*ErrAutorizadorInvalido)
	if !ok {
		t.Fatalf("esperado *ErrAutorizadorInvalido, obtido %T: %v", err, err)
	}
	if invalido.CentroCusto != "CC1" {
		t.Fatalf("CentroCusto inesperado: %+v", invalido)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestAutorizadorNominal_ErroGenericoNaConsulta cobre o branch de erro de
// infraestrutura da consulta principal (não sql.ErrNoRows) — nunca
// confundido com ErrAutorizadorInvalido.
func TestAutorizadorNominal_ErroGenericoNaConsulta(t *testing.T) {
	db, mock, closeFn := newAprovacaoSQLMock(t)
	defer closeFn()

	falha := errors.New("conexão perdida")
	mock.ExpectQuery(regexp.QuoteMeta("FROM autorizadores_formulario")).
		WithArgs("CC1", "colab-1", 1000.0).
		WillReturnError(falha)

	nominal := NovoAutorizadorNominal(db)
	_, err := nominal.Resolve(baseSolicitacaoNominal())
	if err == nil {
		t.Fatal("esperado erro, obtido nil")
	}
	var invalido *ErrAutorizadorInvalido
	if errors.As(err, &invalido) {
		t.Fatalf("erro de infraestrutura não deveria virar ErrAutorizadorInvalido: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestAutorizadorNominal_ErroAoBuscarVersao cobre a falha de
// versaoDoCadastro após o match principal.
func TestAutorizadorNominal_ErroAoBuscarVersao(t *testing.T) {
	db, mock, closeFn := newAprovacaoSQLMock(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("FROM autorizadores_formulario")).
		WithArgs("CC1", "colab-1", 1000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("regra-1"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 1) FROM cadastro_historico")).
		WithArgs("autorizadores-formulario", "regra-1").
		WillReturnError(errors.New("conexão perdida"))

	nominal := NovoAutorizadorNominal(db)
	if _, err := nominal.Resolve(baseSolicitacaoNominal()); err == nil {
		t.Fatal("esperado erro, obtido nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestAutorizadorNominal_DesempateDeterministico guarda a cláusula
// `ORDER BY valor_minimo DESC, id` da consulta principal (Review Triage Log
// da spec 3.3: sem ela, 2 linhas ativas cobrindo o mesmo CC+colaborador+
// faixa de valor fariam RegraID/RegraVersao não-determinísticos no
// aprovador_snapshot de auditoria). sqlmock não executa SQL de fato — não
// ordena linhas devolvidas por WillReturnRows — então a única forma de
// travar esse desempate num teste de unidade é exigir que a query enviada
// ao banco contenha a cláusula: se um refactor futuro remover o ORDER BY, o
// regex abaixo deixa de casar e o mock falha a expectativa.
func TestAutorizadorNominal_DesempateDeterministico(t *testing.T) {
	db, mock, closeFn := newAprovacaoSQLMock(t)
	defer closeFn()

	mock.ExpectQuery(`FROM autorizadores_formulario[\s\S]*ORDER BY valor_minimo DESC, id[\s\S]*LIMIT 1`).
		WithArgs("CC1", "colab-1", 1000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("regra-1"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 1) FROM cadastro_historico")).
		WithArgs("autorizadores-formulario", "regra-1").
		WillReturnRows(sqlmock.NewRows([]string{"versao"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT nome FROM usuarios WHERE id = $1")).
		WithArgs("colab-1").
		WillReturnRows(sqlmock.NewRows([]string{"nome"}).AddRow("Fulano Autorizador"))

	nominal := NovoAutorizadorNominal(db)
	if _, err := nominal.Resolve(baseSolicitacaoNominal()); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (a query perdeu o ORDER BY?): %v", err)
	}
}

// --- Story 3.5: branch tipo-aware para "obras" (aprovadores_obra) ---

func baseSolicitacaoObras() Solicitacao {
	return Solicitacao{
		TipoSolicitacao:        "obras",
		Valor:                  1000,
		AutorizadorEscolhidoID: "colab-1",
	}
}

// TestAutorizadorNominal_Obras_Match cobre o caminho feliz: colaborador_id
// ativo com teto >= valor_total consulta `aprovadores_obra` (nunca
// `autorizadores_formulario`), sem nenhum predicado de CC/faixa.
func TestAutorizadorNominal_Obras_Match(t *testing.T) {
	db, mock, closeFn := newAprovacaoSQLMock(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("FROM aprovadores_obra")).
		WithArgs("colab-1", 1000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("regra-obra-1"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 1) FROM cadastro_historico")).
		WithArgs("aprovadores-obra", "regra-obra-1").
		WillReturnRows(sqlmock.NewRows([]string{"versao"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT nome FROM usuarios WHERE id = $1")).
		WithArgs("colab-1").
		WillReturnRows(sqlmock.NewRows([]string{"nome"}).AddRow("Aprovador de Obras"))

	nominal := NovoAutorizadorNominal(db)
	aprovador, err := nominal.Resolve(baseSolicitacaoObras())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if aprovador.Tipo != TipoPessoa || aprovador.Nome != "Aprovador de Obras" {
		t.Fatalf("aprovador inesperado: %+v", aprovador)
	}
	if aprovador.ColaboradorID == nil || *aprovador.ColaboradorID != "colab-1" {
		t.Fatalf("ColaboradorID inesperado: %+v", aprovador)
	}
	if aprovador.RegraID != "regra-obra-1" || aprovador.RegraVersao != 1 {
		t.Fatalf("regra_id/regra_versao inesperados: %+v", aprovador)
	}
	if aprovador.Motivo != "autorizador nominal validado contra o teto de alçada individual" {
		t.Fatalf("motivo inesperado: %+v", aprovador)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestAutorizadorNominal_Obras_TetoInsuficiente cobre "autorizador acima do
// teto individual" (I/O Matrix da spec 3.5): valor_total > teto da pessoa
// escolhida -> nenhuma linha casa -> ErrAutorizadorInvalido com
// CentroCusto="" (sinaliza a mensagem de teto individual, não CC/faixa).
func TestAutorizadorNominal_Obras_TetoInsuficiente(t *testing.T) {
	db, mock, closeFn := newAprovacaoSQLMock(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("FROM aprovadores_obra")).
		WithArgs("colab-1", 1000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	nominal := NovoAutorizadorNominal(db)
	_, err := nominal.Resolve(baseSolicitacaoObras())
	if err == nil {
		t.Fatal("esperado ErrAutorizadorInvalido, obtido nil")
	}
	invalido, ok := err.(*ErrAutorizadorInvalido)
	if !ok {
		t.Fatalf("esperado *ErrAutorizadorInvalido, obtido %T: %v", err, err)
	}
	if invalido.CentroCusto != "" {
		t.Fatalf("CentroCusto deveria ser vazio (sinaliza mensagem de teto individual): %+v", invalido)
	}
	if invalido.Error() != "autorizador selecionado não é válido para o teto de alçada desta pessoa" {
		t.Fatalf("mensagem inesperada: %v", invalido.Error())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestAutorizadorNominal_Obras_SemCadastro cobre "autorizador sem cadastro
// em aprovadores_obra" (I/O Matrix da spec 3.5) — mesma query/mesmo erro de
// TestAutorizadorNominal_Obras_TetoInsuficiente: a mensagem nunca distingue
// "não cadastrado" de "teto insuficiente" (mesmo princípio de
// ErrAutorizadorInvalido para os demais tipos).
func TestAutorizadorNominal_Obras_SemCadastro(t *testing.T) {
	db, mock, closeFn := newAprovacaoSQLMock(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("FROM aprovadores_obra")).
		WithArgs("colab-inexistente", 1000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	s := baseSolicitacaoObras()
	s.AutorizadorEscolhidoID = "colab-inexistente"
	nominal := NovoAutorizadorNominal(db)
	_, err := nominal.Resolve(s)
	var invalido *ErrAutorizadorInvalido
	if !errors.As(err, &invalido) {
		t.Fatalf("esperado *ErrAutorizadorInvalido, obtido %T: %v", err, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestAutorizadorNominal_Obras_NaoConsultaAutorizadoresFormulario trava que
// o branch "obras" nunca cai na query de autorizadores_formulario (CC/
// faixa) — se um refactor futuro remover o branch tipo-aware, este teste
// falha porque a expectativa abaixo (sem CC) não bateria com a query de
// autorizadores_formulario (que exige CentroCustoCodigo).
func TestAutorizadorNominal_Obras_NaoConsultaAutorizadoresFormulario(t *testing.T) {
	db, mock, closeFn := newAprovacaoSQLMock(t)
	defer closeFn()

	mock.ExpectQuery(`FROM aprovadores_obra\b`).
		WithArgs("colab-1", 1000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("regra-obra-1"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 1) FROM cadastro_historico")).
		WithArgs("aprovadores-obra", "regra-obra-1").
		WillReturnRows(sqlmock.NewRows([]string{"versao"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT nome FROM usuarios WHERE id = $1")).
		WithArgs("colab-1").
		WillReturnRows(sqlmock.NewRows([]string{"nome"}).AddRow("Aprovador de Obras"))

	nominal := NovoAutorizadorNominal(db)
	if _, err := nominal.Resolve(baseSolicitacaoObras()); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas (a query trocou de tabela?): %v", err)
	}
}

// TestAutorizadorNominal_ErroAoBuscarNome cobre a falha de nomeColaborador
// após o match principal e a versão resolvida.
func TestAutorizadorNominal_ErroAoBuscarNome(t *testing.T) {
	db, mock, closeFn := newAprovacaoSQLMock(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("FROM autorizadores_formulario")).
		WithArgs("CC1", "colab-1", 1000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("regra-1"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 1) FROM cadastro_historico")).
		WithArgs("autorizadores-formulario", "regra-1").
		WillReturnRows(sqlmock.NewRows([]string{"versao"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT nome FROM usuarios WHERE id = $1")).
		WithArgs("colab-1").
		WillReturnError(errors.New("conexão perdida"))

	nominal := NovoAutorizadorNominal(db)
	if _, err := nominal.Resolve(baseSolicitacaoNominal()); err == nil {
		t.Fatal("esperado erro, obtido nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}
