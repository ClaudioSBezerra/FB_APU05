package exportacao

// vba_test.go — cobre a I/O Matrix da Story 4.3 no nível de
// ExportadorVBA.GerarLoteDespesa: sucesso, idempotência (replay com os
// mesmos IDs vs. reuso da chave com IDs diferentes), elegibilidade (outro
// administrador/não em_atendimento/tipo 'obras'/não encontrada), exercício
// misto, verba duplicada (aviso vs. ignorado) e template ausente. Mesmo
// padrão sqlmock dos demais pacotes internal/ (ver internal/fila/fila_test.go).

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

const (
	testAdministradorID = "22222222-2222-2222-2222-222222222222"
	testOutroAdminID    = "33333333-3333-3333-3333-333333333333"
	testSolicitacaoA    = "11111111-1111-1111-1111-111111111111"
	testSolicitacaoB    = "44444444-4444-4444-4444-444444444444"
	testChave           = "chave-idempotencia-1"
	testLoteGerado      = "55555555-5555-5555-5555-555555555555"
)

func novoMockExportacao(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("erro ao criar sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, mock
}

// templateValidoParaTeste grava um .xlsm sintético (xlsm_test.go) num
// diretório temporário e devolve o caminho — usado pelos testes que
// precisam passar da checagem de template (o caminho de SUCESSO).
func templateValidoParaTeste(t *testing.T) string {
	t.Helper()
	caminho := filepath.Join(t.TempDir(), "template.xlsm")
	if err := os.WriteFile(caminho, construirXLSMSintetico(t, []byte("vba")), 0o600); err != nil {
		t.Fatalf("erro ao escrever template sintético: %v", err)
	}
	return caminho
}

// mockSemUsoAnteriorDaChave simula verificarConflitoIdempotencia sem
// nenhum uso anterior registrado para (administrador, chave) — caminho
// comum à maioria dos testes (primeira chamada com essa chave).
func mockSemUsoAnteriorDaChave(mock sqlmock.Sqlmock, administradorID, chave string) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT solicitacao_id FROM exportacoes_sap WHERE administrador_id = $1 AND chave_idempotencia = $2")).
		WithArgs(administradorID, chave).
		WillReturnRows(sqlmock.NewRows([]string{"solicitacao_id"}))
}

// mockSolicitacaoElegivel simula buscarSolicitacaoParaExportacao com uma
// solicitação em estado elegível (dono=administradorID, em_atendimento,
// tipo informado).
func mockSolicitacaoElegivel(mock sqlmock.Sqlmock, id, tipo, administradorID string) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, s.status, s.administrador_id, u.nome")).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "status", "administrador_id", "nome"}).
			AddRow(tipo, "em_atendimento", administradorID, "Fulano de Tal"))
}

// mockLancamentos simula buscarLancamentosParaExportacao com 1 linha de
// transferência/inclusão no ano informado.
func mockLancamentos(mock sqlmock.Sqlmock, solicitacaoID string, ano int) {
	mes := time.Date(ano, 1, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(regexp.QuoteMeta("FROM solicitacao_lancamentos sl")).
		WithArgs(solicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"lado", "divisao_codigo", "cc_codigo", "conta_plano", "conta_codigo", "classe_codigo", "mes", "valor"}).
			AddRow("origem", "D1", "CC1", "BIFC", "1000", nil, mes, 100.0))
}

// mockSemRiscoVerbaDuplicada simula detectarVerbaDuplicada sem nenhum
// risco encontrado para `id`.
func mockSemRiscoVerbaDuplicada(mock sqlmock.Sqlmock, id, chave string) {
	mock.ExpectQuery(regexp.QuoteMeta("WHERE solicitacao_id = $1 AND status = 'GERADO' AND chave_idempotencia != $2")).
		WithArgs(id, chave).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
}

// mockGravarLoteSucesso simula a transação completa de gravarLote para
// `ids`, sem comentário de aviso.
func mockGravarLoteSucesso(mock sqlmock.Sqlmock, ids []string, administradorID, chave, loteID string) {
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT gen_random_uuid()")).
		WillReturnRows(sqlmock.NewRows([]string{"gen_random_uuid"}).AddRow(loteID))
	for _, id := range ids {
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO exportacoes_sap (lote_id, solicitacao_id, administrador_id, chave_idempotencia)")).
			WithArgs(loteID, id, administradorID, chave).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT lote_id FROM exportacoes_sap WHERE administrador_id = $1 AND chave_idempotencia = $2 LIMIT 1")).
		WithArgs(administradorID, chave).
		WillReturnRows(sqlmock.NewRows([]string{"lote_id"}).AddRow(loteID))
	mock.ExpectCommit()
}

// --- Sucesso ---

func TestGerarLoteDespesa_Sucesso(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mockSemUsoAnteriorDaChave(mock, testAdministradorID, testChave)
	mockSolicitacaoElegivel(mock, testSolicitacaoA, "transferencia", testAdministradorID)
	mockLancamentos(mock, testSolicitacaoA, 2026)
	mockSemRiscoVerbaDuplicada(mock, testSolicitacaoA, testChave)
	mockGravarLoteSucesso(mock, []string{testSolicitacaoA}, testAdministradorID, testChave, testLoteGerado)

	exportador := ExportadorVBA{TemplatePath: templateValidoParaTeste(t)}
	lote, aviso, err := exportador.GerarLoteDespesa(db, []string{testSolicitacaoA}, testAdministradorID, testChave, false)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if aviso != nil {
		t.Fatalf("aviso inesperado: %+v", aviso)
	}
	if lote.LoteID != testLoteGerado {
		t.Fatalf("LoteID = %q, esperado %q", lote.LoteID, testLoteGerado)
	}
	if len(lote.ArquivoBytes) == 0 {
		t.Fatalf("ArquivoBytes vazio")
	}
	if len(lote.SolicitacoesIncluidas) != 1 || lote.SolicitacoesIncluidas[0] != testSolicitacaoA {
		t.Fatalf("SolicitacoesIncluidas inesperado: %+v", lote.SolicitacoesIncluidas)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestGerarLoteDespesa_ReplayIdempotente cobre "Repetir mesma chave" da
// I/O Matrix: o conjunto existente para (administrador, chave) já é
// EXATAMENTE {testSolicitacaoA} -> sem conflito, segue normalmente; o
// INSERT (ON CONFLICT DO NOTHING) não insere nada nova mas a chamada
// ainda devolve o mesmo lote_id via re-SELECT.
func TestGerarLoteDespesa_ReplayIdempotente(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT solicitacao_id FROM exportacoes_sap WHERE administrador_id = $1 AND chave_idempotencia = $2")).
		WithArgs(testAdministradorID, testChave).
		WillReturnRows(sqlmock.NewRows([]string{"solicitacao_id"}).AddRow(testSolicitacaoA))
	mockSolicitacaoElegivel(mock, testSolicitacaoA, "transferencia", testAdministradorID)
	mockLancamentos(mock, testSolicitacaoA, 2026)
	mockSemRiscoVerbaDuplicada(mock, testSolicitacaoA, testChave)
	mockGravarLoteSucesso(mock, []string{testSolicitacaoA}, testAdministradorID, testChave, testLoteGerado)

	exportador := ExportadorVBA{TemplatePath: templateValidoParaTeste(t)}
	lote, aviso, err := exportador.GerarLoteDespesa(db, []string{testSolicitacaoA}, testAdministradorID, testChave, false)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if aviso != nil {
		t.Fatalf("aviso inesperado: %+v", aviso)
	}
	if lote.LoteID != testLoteGerado {
		t.Fatalf("LoteID = %q, esperado %q (mesmo lote do replay)", lote.LoteID, testLoteGerado)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestGerarLoteDespesa_ChaveReusadaComIDsDiferentes cobre "Chave reusada
// com IDs diferentes" da I/O Matrix -> 409 ErrChaveIdempotenciaConflitante,
// sem nenhuma outra consulta (short-circuit antes de validar
// elegibilidade).
func TestGerarLoteDespesa_ChaveReusadaComIDsDiferentes(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT solicitacao_id FROM exportacoes_sap WHERE administrador_id = $1 AND chave_idempotencia = $2")).
		WithArgs(testAdministradorID, testChave).
		WillReturnRows(sqlmock.NewRows([]string{"solicitacao_id"}).AddRow(testSolicitacaoB))

	exportador := ExportadorVBA{TemplatePath: "/caminho/nao/usado.xlsm"}
	_, _, err := exportador.GerarLoteDespesa(db, []string{testSolicitacaoA}, testAdministradorID, testChave, false)
	if !errors.Is(err, ErrChaveIdempotenciaConflitante) {
		t.Fatalf("esperava ErrChaveIdempotenciaConflitante, recebeu: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// --- Elegibilidade ---

func TestGerarLoteDespesa_NaoEncontrada(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mockSemUsoAnteriorDaChave(mock, testAdministradorID, testChave)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, s.status, s.administrador_id, u.nome")).
		WithArgs(testSolicitacaoA).
		WillReturnError(sql.ErrNoRows)

	exportador := ExportadorVBA{TemplatePath: "/caminho/nao/usado.xlsm"}
	_, _, err := exportador.GerarLoteDespesa(db, []string{testSolicitacaoA}, testAdministradorID, testChave, false)
	if !errors.Is(err, ErrNaoEncontrada) {
		t.Fatalf("esperava ErrNaoEncontrada, recebeu: %v", err)
	}
}

func TestGerarLoteDespesa_OutroAdministrador(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mockSemUsoAnteriorDaChave(mock, testAdministradorID, testChave)
	mockSolicitacaoElegivel(mock, testSolicitacaoA, "transferencia", testOutroAdminID)

	exportador := ExportadorVBA{TemplatePath: "/caminho/nao/usado.xlsm"}
	_, _, err := exportador.GerarLoteDespesa(db, []string{testSolicitacaoA}, testAdministradorID, testChave, false)
	if !errors.Is(err, ErrNaoAutorizado) {
		t.Fatalf("esperava ErrNaoAutorizado, recebeu: %v", err)
	}
}

func TestGerarLoteDespesa_NaoEmAtendimento(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mockSemUsoAnteriorDaChave(mock, testAdministradorID, testChave)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, s.status, s.administrador_id, u.nome")).
		WithArgs(testSolicitacaoA).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "status", "administrador_id", "nome"}).
			AddRow("transferencia", "pendente", testAdministradorID, "Fulano"))

	exportador := ExportadorVBA{TemplatePath: "/caminho/nao/usado.xlsm"}
	_, _, err := exportador.GerarLoteDespesa(db, []string{testSolicitacaoA}, testAdministradorID, testChave, false)
	if !errors.Is(err, ErrElegibilidadeInvalida) {
		t.Fatalf("esperava ErrElegibilidadeInvalida, recebeu: %v", err)
	}
}

func TestGerarLoteDespesa_TipoObras(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mockSemUsoAnteriorDaChave(mock, testAdministradorID, testChave)
	mockSolicitacaoElegivel(mock, testSolicitacaoA, "obras", testAdministradorID)

	exportador := ExportadorVBA{TemplatePath: "/caminho/nao/usado.xlsm"}
	_, _, err := exportador.GerarLoteDespesa(db, []string{testSolicitacaoA}, testAdministradorID, testChave, false)
	if !errors.Is(err, ErrTipoNaoElegivel) {
		t.Fatalf("esperava ErrTipoNaoElegivel, recebeu: %v", err)
	}
}

// TestGerarLoteDespesa_ExercicioMisto cobre "Exercícios mistos" da I/O
// Matrix: testSolicitacaoA tem linhas em 2026, testSolicitacaoB em 2027.
func TestGerarLoteDespesa_ExercicioMisto(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT solicitacao_id FROM exportacoes_sap WHERE administrador_id = $1 AND chave_idempotencia = $2")).
		WithArgs(testAdministradorID, testChave).
		WillReturnRows(sqlmock.NewRows([]string{"solicitacao_id"}))
	mockSolicitacaoElegivel(mock, testSolicitacaoA, "transferencia", testAdministradorID)
	mockLancamentos(mock, testSolicitacaoA, 2026)
	mockSolicitacaoElegivel(mock, testSolicitacaoB, "transferencia", testAdministradorID)
	mockLancamentos(mock, testSolicitacaoB, 2027)

	exportador := ExportadorVBA{TemplatePath: "/caminho/nao/usado.xlsm"}
	_, _, err := exportador.GerarLoteDespesa(db, []string{testSolicitacaoA, testSolicitacaoB}, testAdministradorID, testChave, false)
	if !errors.Is(err, ErrExercicioMisto) {
		t.Fatalf("esperava ErrExercicioMisto, recebeu: %v", err)
	}
}

// --- Verba duplicada ---

// TestGerarLoteDespesa_VerbaDuplicadaSemIgnorar cobre "Risco de verba
// duplicada, sem ignorar" da I/O Matrix: 200 com Aviso, nada gravado (sem
// Begin/INSERT).
func TestGerarLoteDespesa_VerbaDuplicadaSemIgnorar(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mockSemUsoAnteriorDaChave(mock, testAdministradorID, testChave)
	mockSolicitacaoElegivel(mock, testSolicitacaoA, "transferencia", testAdministradorID)
	mockLancamentos(mock, testSolicitacaoA, 2026)
	mock.ExpectQuery(regexp.QuoteMeta("WHERE solicitacao_id = $1 AND status = 'GERADO' AND chave_idempotencia != $2")).
		WithArgs(testSolicitacaoA, testChave).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	exportador := ExportadorVBA{TemplatePath: "/caminho/nao/usado.xlsm"}
	lote, aviso, err := exportador.GerarLoteDespesa(db, []string{testSolicitacaoA}, testAdministradorID, testChave, false)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if aviso == nil {
		t.Fatalf("esperava um Aviso, recebeu nil")
	}
	if aviso.Codigo != "verba_duplicada" {
		t.Fatalf("Codigo = %q, esperado verba_duplicada", aviso.Codigo)
	}
	if len(aviso.SolicitacoesEmRisco) != 1 || aviso.SolicitacoesEmRisco[0] != testSolicitacaoA {
		t.Fatalf("SolicitacoesEmRisco inesperado: %+v", aviso.SolicitacoesEmRisco)
	}
	if lote.LoteID != "" {
		t.Fatalf("Lote não deveria ter sido gravado: %+v", lote)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestGerarLoteDespesa_VerbaDuplicadaIgnorada cobre "Risco de verba
// duplicada, ignorado" da I/O Matrix: gera normalmente + 1
// solicitacao_comentarios (tipo='aviso_verba_duplicada') por solicitação
// em risco.
func TestGerarLoteDespesa_VerbaDuplicadaIgnorada(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mockSemUsoAnteriorDaChave(mock, testAdministradorID, testChave)
	mockSolicitacaoElegivel(mock, testSolicitacaoA, "transferencia", testAdministradorID)
	mockLancamentos(mock, testSolicitacaoA, 2026)
	mock.ExpectQuery(regexp.QuoteMeta("WHERE solicitacao_id = $1 AND status = 'GERADO' AND chave_idempotencia != $2")).
		WithArgs(testSolicitacaoA, testChave).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT gen_random_uuid()")).
		WillReturnRows(sqlmock.NewRows([]string{"gen_random_uuid"}).AddRow(testLoteGerado))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO exportacoes_sap (lote_id, solicitacao_id, administrador_id, chave_idempotencia)")).
		WithArgs(testLoteGerado, testSolicitacaoA, testAdministradorID, testChave).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO solicitacao_comentarios (solicitacao_id, autor_id, tipo, texto)")).
		WithArgs(testSolicitacaoA, testAdministradorID, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT lote_id FROM exportacoes_sap WHERE administrador_id = $1 AND chave_idempotencia = $2 LIMIT 1")).
		WithArgs(testAdministradorID, testChave).
		WillReturnRows(sqlmock.NewRows([]string{"lote_id"}).AddRow(testLoteGerado))
	mock.ExpectCommit()

	exportador := ExportadorVBA{TemplatePath: templateValidoParaTeste(t)}
	lote, aviso, err := exportador.GerarLoteDespesa(db, []string{testSolicitacaoA}, testAdministradorID, testChave, true)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if aviso != nil {
		t.Fatalf("aviso inesperado quando ignorarAvisoVerbaDuplicada=true: %+v", aviso)
	}
	if lote.LoteID != testLoteGerado {
		t.Fatalf("LoteID = %q, esperado %q", lote.LoteID, testLoteGerado)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// --- Template ausente ---

// TestGerarLoteDespesa_TemplateAusente cobre "Template ausente" da I/O
// Matrix: todas as validações de negócio passam, mas o caminho do
// template não existe -> 503 ErrTemplateAusente, nenhuma transação é
// iniciada (nada gravado).
func TestGerarLoteDespesa_TemplateAusente(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mockSemUsoAnteriorDaChave(mock, testAdministradorID, testChave)
	mockSolicitacaoElegivel(mock, testSolicitacaoA, "transferencia", testAdministradorID)
	mockLancamentos(mock, testSolicitacaoA, 2026)
	mockSemRiscoVerbaDuplicada(mock, testSolicitacaoA, testChave)

	exportador := ExportadorVBA{TemplatePath: filepath.Join(t.TempDir(), "nao-existe.xlsm")}
	_, _, err := exportador.GerarLoteDespesa(db, []string{testSolicitacaoA}, testAdministradorID, testChave, false)
	if !errors.Is(err, ErrTemplateAusente) {
		t.Fatalf("esperava ErrTemplateAusente, recebeu: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}
