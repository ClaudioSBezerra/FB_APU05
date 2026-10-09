package exportacao

// finalizar_test.go — cobre a I/O Matrix da Story 4.5 no nível de
// ExportadorVBA.Finalizar: sucesso sem linha CRIAR, sucesso com erro de
// exportação (resultado='erro'), resolução de linha(s) CRIAR,
// ordens_criadas incompleto/errado, numero_ordem já existente, ainda não
// exportada, dono errado, status/versão incompatível e id inexistente.
// Mesmo padrão sqlmock dos demais arquivos deste pacote (vba_test.go/
// obra_test.go).

import (
	"database/sql"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
)

const (
	testFinalizarSolicitacaoID   = "66666666-6666-6666-6666-666666666666"
	testFinalizarAdministradorID = "22222222-2222-2222-2222-222222222222"
	testFinalizarOutroAdminID    = "33333333-3333-3333-3333-333333333333"
	testFinalizarLinhaCriarID    = "77777777-7777-7777-7777-777777777777"
	testFinalizarLocalObraID     = "88888888-8888-8888-8888-888888888888"
	testFinalizarSubgrupoID      = "99999999-9999-9999-9999-999999999999"
	testFinalizarNumeroOrdem     = "SAP-0001"

	// testFinalizarLinhaCriarIDLower/Upper cobrem a normalização de caixa
	// entre o `id` canonicalizado em lowercase devolvido pelo Postgres
	// (coluna uuid) e um `linha_id` sintaticamente válido mas com letras
	// maiúsculas, aceito por handlers.uuidFormatRegexp sem normalização.
	testFinalizarLinhaCriarIDLower = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	testFinalizarLinhaCriarIDUpper = "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE"

	// testFinalizarLinhaCriarID2 é a 2ª linha CRIAR real usada pelo teste
	// de "ordensCriadas repete 1 id e omite o outro".
	testFinalizarLinhaCriarID2 = "aaaaaaaa-1111-2222-3333-444444444444"
)

// mockFinalizarUpdateSolicitacao programa o UPDATE condicional de
// `solicitacoes` (lock otimista) com sucesso, devolvendo a linha já com o
// novo status/versao.
func mockFinalizarUpdateSolicitacao(mock sqlmock.Sqlmock, id string, versaoLida int, administradorID, novoStatus string, novaVersao int) {
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs(novoStatus, id, versaoLida, administradorID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tipo_solicitacao", "status", "versao", "administrador_id"}).
			AddRow(id, "obras", novoStatus, novaVersao, administradorID))
}

// mockFinalizarMigrarExportacoes programa o UPDATE de `exportacoes_sap`
// (GERADO -> FINALIZADO), devolvendo `linhasAfetadas` linhas afetadas.
func mockFinalizarMigrarExportacoes(mock sqlmock.Sqlmock, id string, linhasAfetadas int64) {
	mock.ExpectExec(regexp.QuoteMeta("UPDATE exportacoes_sap SET status = 'FINALIZADO'")).
		WithArgs(id).
		WillReturnResult(sqlmock.NewResult(0, linhasAfetadas))
}

// --- Sucesso, sem linha CRIAR ---

func TestFinalizar_SucessoSemLinhaCriar(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mock.ExpectBegin()
	mockFinalizarUpdateSolicitacao(mock, testFinalizarSolicitacaoID, 3, testFinalizarAdministradorID, "finalizada_sucesso", 4)
	mockFinalizarMigrarExportacoes(mock, testFinalizarSolicitacaoID, 1)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, local_obra_id, subgrupo_despesa_id")).
		WithArgs(testFinalizarSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "local_obra_id", "subgrupo_despesa_id"}))
	mock.ExpectCommit()

	exportador := ExportadorVBA{}
	s, err := exportador.Finalizar(db, testFinalizarSolicitacaoID, 3, testFinalizarAdministradorID, "sucesso", nil)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if s.Status != "finalizada_sucesso" || s.Versao != 4 {
		t.Fatalf("resultado inesperado: %+v", s)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// --- Finalização com erro ---

func TestFinalizar_ComErro(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mock.ExpectBegin()
	mockFinalizarUpdateSolicitacao(mock, testFinalizarSolicitacaoID, 1, testFinalizarAdministradorID, "finalizada_erro", 2)
	mockFinalizarMigrarExportacoes(mock, testFinalizarSolicitacaoID, 1)
	mock.ExpectCommit()

	exportador := ExportadorVBA{}
	s, err := exportador.Finalizar(db, testFinalizarSolicitacaoID, 1, testFinalizarAdministradorID, "erro", nil)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if s.Status != "finalizada_erro" || s.Versao != 2 {
		t.Fatalf("resultado inesperado: %+v", s)
	}
	// Nenhuma query de "linhas CRIAR" deve acontecer quando resultado=erro
	// (Boundaries "Always" da spec: só resolvido quando resultado=sucesso).
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// --- Linha CRIAR resolvida ---

func TestFinalizar_ResolveLinhaCriar(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mock.ExpectBegin()
	mockFinalizarUpdateSolicitacao(mock, testFinalizarSolicitacaoID, 1, testFinalizarAdministradorID, "finalizada_sucesso", 2)
	mockFinalizarMigrarExportacoes(mock, testFinalizarSolicitacaoID, 1)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, local_obra_id, subgrupo_despesa_id")).
		WithArgs(testFinalizarSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "local_obra_id", "subgrupo_despesa_id"}).
			AddRow(testFinalizarLinhaCriarID, testFinalizarLocalObraID, testFinalizarSubgrupoID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO obra_ordens")).
		WithArgs(testFinalizarNumeroOrdem, testFinalizarLocalObraID, testFinalizarSubgrupoID).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE solicitacao_obras_linhas SET ordem_investimento")).
		WithArgs(testFinalizarNumeroOrdem, testFinalizarLinhaCriarID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	exportador := ExportadorVBA{}
	ordensCriadas := []OrdemCriada{{LinhaID: testFinalizarLinhaCriarID, NumeroOrdem: testFinalizarNumeroOrdem}}
	s, err := exportador.Finalizar(db, testFinalizarSolicitacaoID, 1, testFinalizarAdministradorID, "sucesso", ordensCriadas)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if s.Status != "finalizada_sucesso" {
		t.Fatalf("resultado inesperado: %+v", s)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// --- ordens_criadas incompleto/errado ---

func TestFinalizar_OrdensCriadasDivergentes(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mock.ExpectBegin()
	mockFinalizarUpdateSolicitacao(mock, testFinalizarSolicitacaoID, 1, testFinalizarAdministradorID, "finalizada_sucesso", 2)
	mockFinalizarMigrarExportacoes(mock, testFinalizarSolicitacaoID, 1)
	// 1 linha CRIAR existe no banco, mas o corpo não informou nenhuma —
	// divergência de contagem -> ErrOrdensCriadasInvalidas, rollback.
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, local_obra_id, subgrupo_despesa_id")).
		WithArgs(testFinalizarSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "local_obra_id", "subgrupo_despesa_id"}).
			AddRow(testFinalizarLinhaCriarID, testFinalizarLocalObraID, testFinalizarSubgrupoID))
	mock.ExpectRollback()

	exportador := ExportadorVBA{}
	_, err := exportador.Finalizar(db, testFinalizarSolicitacaoID, 1, testFinalizarAdministradorID, "sucesso", nil)
	if !errors.Is(err, ErrOrdensCriadasInvalidas) {
		t.Fatalf("esperava ErrOrdensCriadasInvalidas, recebeu: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestFinalizar_OrdensCriadasApontaLinhaErrada cobre "aponta linha que não
// é CRIAR"/"linha_id de outra solicitação": a contagem até bate (1=1), mas
// o linha_id informado não está no mapa de linhas CRIAR encontradas.
func TestFinalizar_OrdensCriadasApontaLinhaErrada(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mock.ExpectBegin()
	mockFinalizarUpdateSolicitacao(mock, testFinalizarSolicitacaoID, 1, testFinalizarAdministradorID, "finalizada_sucesso", 2)
	mockFinalizarMigrarExportacoes(mock, testFinalizarSolicitacaoID, 1)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, local_obra_id, subgrupo_despesa_id")).
		WithArgs(testFinalizarSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "local_obra_id", "subgrupo_despesa_id"}).
			AddRow(testFinalizarLinhaCriarID, testFinalizarLocalObraID, testFinalizarSubgrupoID))
	mock.ExpectRollback()

	exportador := ExportadorVBA{}
	ordensCriadas := []OrdemCriada{{LinhaID: testFinalizarOutroAdminID, NumeroOrdem: testFinalizarNumeroOrdem}}
	_, err := exportador.Finalizar(db, testFinalizarSolicitacaoID, 1, testFinalizarAdministradorID, "sucesso", ordensCriadas)
	if !errors.Is(err, ErrOrdensCriadasInvalidas) {
		t.Fatalf("esperava ErrOrdensCriadasInvalidas, recebeu: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// --- numero_ordem já existe ---

func TestFinalizar_NumeroOrdemJaExiste(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mock.ExpectBegin()
	mockFinalizarUpdateSolicitacao(mock, testFinalizarSolicitacaoID, 1, testFinalizarAdministradorID, "finalizada_sucesso", 2)
	mockFinalizarMigrarExportacoes(mock, testFinalizarSolicitacaoID, 1)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, local_obra_id, subgrupo_despesa_id")).
		WithArgs(testFinalizarSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "local_obra_id", "subgrupo_despesa_id"}).
			AddRow(testFinalizarLinhaCriarID, testFinalizarLocalObraID, testFinalizarSubgrupoID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO obra_ordens")).
		WithArgs(testFinalizarNumeroOrdem, testFinalizarLocalObraID, testFinalizarSubgrupoID).
		WillReturnError(&pq.Error{Code: "23505"})
	mock.ExpectRollback()

	exportador := ExportadorVBA{}
	ordensCriadas := []OrdemCriada{{LinhaID: testFinalizarLinhaCriarID, NumeroOrdem: testFinalizarNumeroOrdem}}
	_, err := exportador.Finalizar(db, testFinalizarSolicitacaoID, 1, testFinalizarAdministradorID, "sucesso", ordensCriadas)
	if !errors.Is(err, ErrOrdemJaExiste) {
		t.Fatalf("esperava ErrOrdemJaExiste, recebeu: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// --- Ainda não exportada ---

func TestFinalizar_AindaNaoExportada(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mock.ExpectBegin()
	mockFinalizarUpdateSolicitacao(mock, testFinalizarSolicitacaoID, 1, testFinalizarAdministradorID, "finalizada_sucesso", 2)
	mockFinalizarMigrarExportacoes(mock, testFinalizarSolicitacaoID, 0)
	mock.ExpectRollback()

	exportador := ExportadorVBA{}
	_, err := exportador.Finalizar(db, testFinalizarSolicitacaoID, 1, testFinalizarAdministradorID, "sucesso", nil)
	if !errors.Is(err, ErrNaoExportada) {
		t.Fatalf("esperava ErrNaoExportada, recebeu: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// --- Dono errado ---

func TestFinalizar_DonoErrado(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs("finalizada_sucesso", testFinalizarSolicitacaoID, 1, testFinalizarAdministradorID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT administrador_id, status, versao FROM solicitacoes WHERE id = $1")).
		WithArgs(testFinalizarSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"administrador_id", "status", "versao"}).
			AddRow(testFinalizarOutroAdminID, "em_atendimento", 1))

	exportador := ExportadorVBA{}
	_, err := exportador.Finalizar(db, testFinalizarSolicitacaoID, 1, testFinalizarAdministradorID, "sucesso", nil)
	if !errors.Is(err, ErrNaoAutorizado) {
		t.Fatalf("esperava ErrNaoAutorizado, recebeu: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// --- Status/versão incompatível ---

func TestFinalizar_ConflitoVersao(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs("finalizada_sucesso", testFinalizarSolicitacaoID, 1, testFinalizarAdministradorID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT administrador_id, status, versao FROM solicitacoes WHERE id = $1")).
		WithArgs(testFinalizarSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"administrador_id", "status", "versao"}).
			AddRow(testFinalizarAdministradorID, "pendente", 5))

	exportador := ExportadorVBA{}
	_, err := exportador.Finalizar(db, testFinalizarSolicitacaoID, 1, testFinalizarAdministradorID, "sucesso", nil)
	var conflito *ErrConflitoVersao
	if !errors.As(err, &conflito) || conflito.VersaoAtual != 5 {
		t.Fatalf("esperava ErrConflitoVersao{VersaoAtual:5}, recebeu: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// --- ID inexistente ---

func TestFinalizar_NaoEncontrada(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs("finalizada_sucesso", testFinalizarSolicitacaoID, 1, testFinalizarAdministradorID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT administrador_id, status, versao FROM solicitacoes WHERE id = $1")).
		WithArgs(testFinalizarSolicitacaoID).
		WillReturnError(sql.ErrNoRows)

	exportador := ExportadorVBA{}
	_, err := exportador.Finalizar(db, testFinalizarSolicitacaoID, 1, testFinalizarAdministradorID, "sucesso", nil)
	if !errors.Is(err, ErrNaoEncontrada) {
		t.Fatalf("esperava ErrNaoEncontrada, recebeu: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// --- Normalização de caixa do linha_id (achado de revisão) ---

// TestFinalizar_ResolveLinhaCriar_LinhaIDCaseInsensitive cobre um
// `linha_id` informado em MAIÚSCULAS que corresponde, só ignorando a
// caixa, ao `id` que o Postgres devolve em lowercase (coluna uuid é
// sempre canonicalizada em minúsculas) — antes da normalização, isso
// seria rejeitado por engano como ErrOrdensCriadasInvalidas.
func TestFinalizar_ResolveLinhaCriar_LinhaIDCaseInsensitive(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mock.ExpectBegin()
	mockFinalizarUpdateSolicitacao(mock, testFinalizarSolicitacaoID, 1, testFinalizarAdministradorID, "finalizada_sucesso", 2)
	mockFinalizarMigrarExportacoes(mock, testFinalizarSolicitacaoID, 1)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, local_obra_id, subgrupo_despesa_id")).
		WithArgs(testFinalizarSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "local_obra_id", "subgrupo_despesa_id"}).
			AddRow(testFinalizarLinhaCriarIDLower, testFinalizarLocalObraID, testFinalizarSubgrupoID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO obra_ordens")).
		WithArgs(testFinalizarNumeroOrdem, testFinalizarLocalObraID, testFinalizarSubgrupoID).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE solicitacao_obras_linhas SET ordem_investimento")).
		WithArgs(testFinalizarNumeroOrdem, testFinalizarLinhaCriarIDLower).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	exportador := ExportadorVBA{}
	ordensCriadas := []OrdemCriada{{LinhaID: testFinalizarLinhaCriarIDUpper, NumeroOrdem: testFinalizarNumeroOrdem}}
	s, err := exportador.Finalizar(db, testFinalizarSolicitacaoID, 1, testFinalizarAdministradorID, "sucesso", ordensCriadas)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if s.Status != "finalizada_sucesso" {
		t.Fatalf("resultado inesperado: %+v", s)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestFinalizar_OrdensCriadasLinhaIDDuplicadoOmiteOutra cobre 2 linhas
// CRIAR reais onde `ordensCriadas` repete o id de 1 delas e omite a outra
// — a contagem bate (2=2), a 1ª ocorrência do id repetido resolve
// normalmente (INSERT+UPDATE), mas a 2ª já foi consumida (delete do mapa)
// e não sobra entrada para ela -> ErrOrdensCriadasInvalidas, rollback da
// transação inteira (nenhuma escrita parcial sobrevive — defesa contra
// linha_id duplicado, anteriormente sem teste).
func TestFinalizar_OrdensCriadasLinhaIDDuplicadoOmiteOutra(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mock.ExpectBegin()
	mockFinalizarUpdateSolicitacao(mock, testFinalizarSolicitacaoID, 1, testFinalizarAdministradorID, "finalizada_sucesso", 2)
	mockFinalizarMigrarExportacoes(mock, testFinalizarSolicitacaoID, 1)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, local_obra_id, subgrupo_despesa_id")).
		WithArgs(testFinalizarSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "local_obra_id", "subgrupo_despesa_id"}).
			AddRow(testFinalizarLinhaCriarID, testFinalizarLocalObraID, testFinalizarSubgrupoID).
			AddRow(testFinalizarLinhaCriarID2, testFinalizarLocalObraID, testFinalizarSubgrupoID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO obra_ordens")).
		WithArgs("SAP-0001", testFinalizarLocalObraID, testFinalizarSubgrupoID).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE solicitacao_obras_linhas SET ordem_investimento")).
		WithArgs("SAP-0001", testFinalizarLinhaCriarID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectRollback()

	exportador := ExportadorVBA{}
	ordensCriadas := []OrdemCriada{
		{LinhaID: testFinalizarLinhaCriarID, NumeroOrdem: "SAP-0001"},
		{LinhaID: testFinalizarLinhaCriarID, NumeroOrdem: "SAP-0002"},
	}
	_, err := exportador.Finalizar(db, testFinalizarSolicitacaoID, 1, testFinalizarAdministradorID, "sucesso", ordensCriadas)
	if !errors.Is(err, ErrOrdensCriadasInvalidas) {
		t.Fatalf("esperava ErrOrdensCriadasInvalidas, recebeu: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}
