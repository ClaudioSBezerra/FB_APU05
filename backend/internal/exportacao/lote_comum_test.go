package exportacao

import (
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestDeduplicarIDs_PreservaOrdemDaPrimeiraOcorrencia(t *testing.T) {
	got := deduplicarIDs([]string{"b", "a", "b", "c", "a"})
	want := []string{"b", "a", "c"}
	if len(got) != len(want) {
		t.Fatalf("deduplicarIDs = %v, esperado %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("deduplicarIDs = %v, esperado %v", got, want)
		}
	}
}

// DW-12: o mesmo id repetido no pedido é validado, lido e gravado 1 vez só
// (as expectativas do sqlmock falhariam numa 2ª leitura/INSERT).
func TestGerarLoteObra_IDRepetidoEntraUmaVez(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mockSolicitacaoObraElegivel(mock, testSolicitacaoA, testAdministradorID, "inclusao")
	mockLinhasObra(mock, testSolicitacaoA, [5]interface{}{"retirada", "LO1", "SG1", "9999", 100.0})
	mockGravarLoteObraSucesso(mock, []string{testSolicitacaoA}, testAdministradorID, testChave, testLoteGerado)

	exportador := ExportadorVBA{TemplatePathObra: templateValidoParaTeste(t)}
	lote, _, err := exportador.GerarLoteObra(db, []string{testSolicitacaoA, testSolicitacaoA}, testAdministradorID, testChave)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(lote.SolicitacoesIncluidas) != 1 {
		t.Fatalf("SolicitacoesIncluidas = %v, esperado só %s", lote.SolicitacoesIncluidas, testSolicitacaoA)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestGerarLoteDespesa_IDRepetidoEntraUmaVez(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mockSemUsoAnteriorDaChave(mock, testAdministradorID, testChave)
	mockSolicitacaoElegivel(mock, testSolicitacaoA, "transferencia", testAdministradorID)
	mockLancamentos(mock, testSolicitacaoA, 2026)
	mockSemRiscoVerbaDuplicada(mock, testSolicitacaoA, testChave)
	mockGravarLoteSucesso(mock, []string{testSolicitacaoA}, testAdministradorID, testChave, testLoteGerado)

	exportador := ExportadorVBA{TemplatePath: templateValidoParaTeste(t)}
	lote, _, err := exportador.GerarLoteDespesa(db, []string{testSolicitacaoA, testSolicitacaoA}, testAdministradorID, testChave, false)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if lote.LoteID != testLoteGerado {
		t.Fatalf("LoteID = %q, esperado %q", lote.LoteID, testLoteGerado)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// DW-13: a solicitação passou na validação, mas foi finalizada por outra
// requisição antes da gravação — a releitura sob lock devolve 409 e nada é
// inserido (sem ExpectExec de INSERT; o sqlmock acusaria qualquer escrita).
func TestGerarLoteObra_FinalizadaAntesDaGravacao_Aborta(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mockSolicitacaoObraElegivel(mock, testSolicitacaoA, testAdministradorID, "inclusao")
	mockLinhasObra(mock, testSolicitacaoA, [5]interface{}{"retirada", "LO1", "SG1", "9999", 100.0})
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT status, administrador_id FROM solicitacoes WHERE id = $1 FOR UPDATE")).
		WithArgs(testSolicitacaoA).
		WillReturnRows(sqlmock.NewRows([]string{"status", "administrador_id"}).AddRow("finalizada", testAdministradorID))
	mock.ExpectRollback()

	exportador := ExportadorVBA{TemplatePathObra: templateValidoParaTeste(t)}
	_, _, err := exportador.GerarLoteObra(db, []string{testSolicitacaoA}, testAdministradorID, testChave)
	if !errors.Is(err, ErrElegibilidadeInvalida) {
		t.Fatalf("erro = %v, esperado ErrElegibilidadeInvalida", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// DW-13: a solicitação mudou de administrador entre a validação e a
// gravação — 403, nada gravado.
func TestGerarLoteDespesa_OutroAdministradorAntesDaGravacao_Aborta(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mockSemUsoAnteriorDaChave(mock, testAdministradorID, testChave)
	mockSolicitacaoElegivel(mock, testSolicitacaoA, "transferencia", testAdministradorID)
	mockLancamentos(mock, testSolicitacaoA, 2026)
	mockSemRiscoVerbaDuplicada(mock, testSolicitacaoA, testChave)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT status, administrador_id FROM solicitacoes WHERE id = $1 FOR UPDATE")).
		WithArgs(testSolicitacaoA).
		WillReturnRows(sqlmock.NewRows([]string{"status", "administrador_id"}).AddRow("em_atendimento", "outro-admin"))
	mock.ExpectRollback()

	exportador := ExportadorVBA{TemplatePath: templateValidoParaTeste(t)}
	_, _, err := exportador.GerarLoteDespesa(db, []string{testSolicitacaoA}, testAdministradorID, testChave, false)
	if !errors.Is(err, ErrNaoAutorizado) {
		t.Fatalf("erro = %v, esperado ErrNaoAutorizado", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}
