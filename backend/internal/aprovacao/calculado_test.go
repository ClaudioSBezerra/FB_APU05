package aprovacao

// calculado_test.go — cobre as 5 etapas do encadeamento de ResolverCalculado
// (regra curada -> alçada -> janela do gerente do CC -> fallback divisão ->
// fallback global) + ErrSemAlcadaCadastrada + cargo colegiado sem titular
// (I/O Matrix da spec da Story 3.1). Mesmo padrão sqlmock dos demais pacotes
// (handlers/cadastros_test.go) — `*sql.DB` é passado diretamente como DBTX
// (sem transação), já que ResolverCalculado só executa SELECTs.

import (
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func newAprovacaoSQLMock(t *testing.T) (DBTX, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("erro ao criar sqlmock: %v", err)
	}
	return db, mock, func() { _ = db.Close() }
}

// baseSolicitacao é reaproveitada pelos testes abaixo — cada um ajusta só o
// que precisa.
func baseSolicitacao() Solicitacao {
	return Solicitacao{
		Filial:            "F1",
		CentroCustoID:     "cc-1111-1111-1111-1111-111111111111",
		CentroCustoCodigo: "CC1",
		DivisaoID:         "div-111-1111-1111-1111-111111111111",
		Valor:             5000,
	}
}

// TestResolve_RegraCurada cobre "Regra especial curada casa" da I/O Matrix:
// precedência mais alta que a matriz de alçadas — a query de
// regras_aprovacao roda primeiro e, ao casar, alcadas NUNCA é consultada.
func TestResolve_RegraCurada(t *testing.T) {
	db, mock, closeFn := newAprovacaoSQLMock(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("FROM regras_aprovacao")).
		WithArgs("F1", "CC1", 5000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}).
			AddRow("regra-1", nil, "Gerente de RH"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 1) FROM cadastro_historico")).
		WithArgs("regras-aprovacao", "regra-1").
		WillReturnRows(sqlmock.NewRows([]string{"versao"}).AddRow(2))

	resolver := NovoResolverCalculado(db)
	aprovador, err := resolver.Resolve(baseSolicitacao())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if aprovador.Tipo != TipoCargo || aprovador.Nome != "Gerente de RH" {
		t.Fatalf("aprovador inesperado: %+v", aprovador)
	}
	if aprovador.RegraID != "regra-1" || aprovador.RegraVersao != 2 {
		t.Fatalf("regra_id/regra_versao inesperados: %+v", aprovador)
	}
	if aprovador.ColaboradorID != nil {
		t.Fatalf("esperado ColaboradorID nulo, obtido %v", *aprovador.ColaboradorID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestResolve_Alcada cobre "Envio feliz, alçada normal": nenhuma regra
// curada casa, a matriz de alçadas casa — sempre Tipo="cargo" (alcadas só
// tem papel_aprovador).
func TestResolve_Alcada(t *testing.T) {
	db, mock, closeFn := newAprovacaoSQLMock(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("FROM regras_aprovacao")).
		WithArgs("F1", "CC1", 5000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("FROM alcadas")).
		WithArgs("F1", "CC1", 5000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "papel_aprovador"}).AddRow("alcada-1", "Gerente Regional"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 1) FROM cadastro_historico")).
		WithArgs("alcadas", "alcada-1").
		WillReturnRows(sqlmock.NewRows([]string{"versao"}).AddRow(1))

	resolver := NovoResolverCalculado(db)
	aprovador, err := resolver.Resolve(baseSolicitacao())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if aprovador.Tipo != TipoCargo || aprovador.Nome != "Gerente Regional" || aprovador.Motivo != "matriz de alçadas" {
		t.Fatalf("aprovador inesperado: %+v", aprovador)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestResolve_JanelaGerenteCC cobre "Janela do gerente": nem alçadas nem
// regras_aprovacao casam, valor <= R$20.000, gerentes_aprovacao tem linha
// específica do CC com colaborador_id preenchido -> Tipo="pessoa".
func TestResolve_JanelaGerenteCC(t *testing.T) {
	db, mock, closeFn := newAprovacaoSQLMock(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("FROM regras_aprovacao")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("FROM alcadas")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("centro_custo_id = $2")).
		WithArgs(5000.0, "cc-1111-1111-1111-1111-111111111111").
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}).
			AddRow("gerente-1", "colab-1", nil))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 1) FROM cadastro_historico")).
		WithArgs("gerentes-aprovacao", "gerente-1").
		WillReturnRows(sqlmock.NewRows([]string{"versao"}).AddRow(1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT nome FROM usuarios WHERE id = $1")).
		WithArgs("colab-1").
		WillReturnRows(sqlmock.NewRows([]string{"nome"}).AddRow("Gerente do CC"))

	resolver := NovoResolverCalculado(db)
	aprovador, err := resolver.Resolve(baseSolicitacao())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if aprovador.Tipo != TipoPessoa || aprovador.Nome != "Gerente do CC" {
		t.Fatalf("aprovador inesperado: %+v", aprovador)
	}
	if aprovador.ColaboradorID == nil || *aprovador.ColaboradorID != "colab-1" {
		t.Fatalf("ColaboradorID inesperado: %+v", aprovador)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestResolve_FallbackDivisao cobre "Fallback GR": só existe linha de
// gerentes_aprovacao por divisao_id (nenhuma específica do CC) — e também
// o caso colegiado (3 gerentes de Logística, qualquer um aprova):
// Tipo="cargo", ColaboradorID nulo — aceito, não bloqueia.
func TestResolve_FallbackDivisao(t *testing.T) {
	db, mock, closeFn := newAprovacaoSQLMock(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("FROM regras_aprovacao")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("FROM alcadas")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("centro_custo_id = $2")).
		WithArgs(5000.0, "cc-1111-1111-1111-1111-111111111111").
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("centro_custo_id IS NULL AND divisao_id = $2")).
		WithArgs(5000.0, "div-111-1111-1111-1111-111111111111").
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}).
			AddRow("divisao-gerente-1", nil, "Gerentes Regionais de Logística"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 1) FROM cadastro_historico")).
		WithArgs("gerentes-aprovacao", "divisao-gerente-1").
		WillReturnRows(sqlmock.NewRows([]string{"versao"}).AddRow(1))

	resolver := NovoResolverCalculado(db)
	aprovador, err := resolver.Resolve(baseSolicitacao())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if aprovador.Tipo != TipoCargo || aprovador.ColaboradorID != nil {
		t.Fatalf("aprovador colegiado inesperado: %+v", aprovador)
	}
	if aprovador.Nome != "Gerentes Regionais de Logística" {
		t.Fatalf("nome inesperado: %+v", aprovador)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestResolve_FallbackGlobal cobre o último degrau antes de
// ErrSemAlcadaCadastrada: só existe linha global (os dois nulos) — e o caso
// de cargo sem titular (ex. Superintendência).
func TestResolve_FallbackGlobal(t *testing.T) {
	db, mock, closeFn := newAprovacaoSQLMock(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("FROM regras_aprovacao")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("FROM alcadas")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("centro_custo_id = $2")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("centro_custo_id IS NULL AND divisao_id = $2")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("centro_custo_id IS NULL AND divisao_id IS NULL")).
		WithArgs(5000.0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}).
			AddRow("global-1", nil, "Superintendência"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COALESCE(MAX(versao), 1) FROM cadastro_historico")).
		WithArgs("gerentes-aprovacao", "global-1").
		WillReturnRows(sqlmock.NewRows([]string{"versao"}).AddRow(1))

	resolver := NovoResolverCalculado(db)
	aprovador, err := resolver.Resolve(baseSolicitacao())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if aprovador.Tipo != TipoCargo || aprovador.ColaboradorID != nil || aprovador.Nome != "Superintendência" {
		t.Fatalf("aprovador inesperado: %+v", aprovador)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestResolve_SemAlcadaCadastrada cobre "Sem alçada cadastrada": nada casa
// em nenhuma etapa -> ErrSemAlcadaCadastrada citando CC e filial, nunca um
// Aprovador sintetizado.
func TestResolve_SemAlcadaCadastrada(t *testing.T) {
	db, mock, closeFn := newAprovacaoSQLMock(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("FROM regras_aprovacao")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("FROM alcadas")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("centro_custo_id = $2")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("centro_custo_id IS NULL AND divisao_id = $2")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}))
	mock.ExpectQuery(regexp.QuoteMeta("centro_custo_id IS NULL AND divisao_id IS NULL")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "colaborador_id", "papel_aprovador"}))

	resolver := NovoResolverCalculado(db)
	_, err := resolver.Resolve(baseSolicitacao())
	if err == nil {
		t.Fatal("esperado ErrSemAlcadaCadastrada, obtido nil")
	}
	semAlcada, ok := err.(*ErrSemAlcadaCadastrada)
	if !ok {
		t.Fatalf("esperado *ErrSemAlcadaCadastrada, obtido %T: %v", err, err)
	}
	if semAlcada.CentroCusto != "CC1" || semAlcada.Filial != "F1" {
		t.Fatalf("CC/Filial inesperados: %+v", semAlcada)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

// TestResolverParaTipo cobre a fábrica de dispatch — só "transferencia" é
// mapeado nesta story.
func TestResolverParaTipo(t *testing.T) {
	db, _, closeFn := newAprovacaoSQLMock(t)
	defer closeFn()

	if _, err := ResolverParaTipo("inclusao", db); err != ErrTipoNaoSuportado {
		t.Fatalf("esperado ErrTipoNaoSuportado, obtido %v", err)
	}
	resolver, err := ResolverParaTipo("transferencia", db)
	if err != nil || resolver == nil {
		t.Fatalf("esperado Resolver não-nil sem erro para 'transferencia', obtido resolver=%v err=%v", resolver, err)
	}

	// Story 3.2: "inclusao_sfc" reaproveita o MESMO ResolverCalculado.
	resolverInclusaoSFC, err := ResolverParaTipo("inclusao_sfc", db)
	if err != nil || resolverInclusaoSFC == nil {
		t.Fatalf("esperado Resolver não-nil sem erro para 'inclusao_sfc', obtido resolver=%v err=%v", resolverInclusaoSFC, err)
	}
}
