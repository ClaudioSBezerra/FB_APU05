package exportacao

// preview_test.go — cobre PreviewLoteDespesa/PreviewLoteObra: inclui
// elegíveis, ignora silenciosamente (sem erro) ids de outro administrador/
// fora de em_atendimento/tipo errado, e descarta linhas 'CRIAR' do lote
// Obra — mesma elegibilidade de GerarLoteDespesa/GerarLoteObra
// (vba_test.go/obra_test.go), mas nunca grava nada.

import (
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestPreviewLoteDespesa_IncluiElegivel(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mockSolicitacaoElegivel(mock, testSolicitacaoA, "transferencia", testAdministradorID)
	mockLancamentos(mock, testSolicitacaoA, 2026)

	linhas, err := PreviewLoteDespesa(db, []string{testSolicitacaoA}, testAdministradorID)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(linhas) != 1 {
		t.Fatalf("esperado 1 linha, obtido %d", len(linhas))
	}
	if linhas[0].TipoSolicitacao != "transferencia" || linhas[0].SolicitacaoID != testSolicitacaoA {
		t.Fatalf("linha inesperada: %+v", linhas[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestPreviewLoteDespesa_IgnoraNaoEncontrada cobre um id inexistente (0
// linhas devolvidas = sql.ErrNoRows -> ErrNaoEncontrada) misturado com um
// id elegível — diferente de GerarLoteDespesa (que recusaria o lote
// inteiro com 404), a prévia só pula o id ausente silenciosamente e
// segue para os demais.
func TestPreviewLoteDespesa_IgnoraNaoEncontrada(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, s.status, s.administrador_id, u.nome")).
		WithArgs(testSolicitacaoA).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "status", "administrador_id", "nome"}))
	mockSolicitacaoElegivel(mock, testSolicitacaoB, "inclusao", testAdministradorID)
	mockLancamentos(mock, testSolicitacaoB, 2026)

	linhas, err := PreviewLoteDespesa(db, []string{testSolicitacaoA, testSolicitacaoB}, testAdministradorID)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(linhas) != 1 || linhas[0].SolicitacaoID != testSolicitacaoB {
		t.Fatalf("esperado só a linha de %s, obtido: %+v", testSolicitacaoB, linhas)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestPreviewLoteDespesa_IgnoraOutroAdministrador cobre um id de outro
// administrador — ignorado, não vira erro.
func TestPreviewLoteDespesa_IgnoraOutroAdministrador(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mockSolicitacaoElegivel(mock, testSolicitacaoA, "transferencia", testOutroAdminID)

	linhas, err := PreviewLoteDespesa(db, []string{testSolicitacaoA}, testAdministradorID)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(linhas) != 0 {
		t.Fatalf("esperado 0 linhas, obtido %d", len(linhas))
	}
}

// TestPreviewLoteDespesa_IgnoraTipoObras cobre tipo='obras' — fora do
// lote Despesa, ignorado silenciosamente.
func TestPreviewLoteDespesa_IgnoraTipoObras(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mockSolicitacaoElegivel(mock, testSolicitacaoA, "obras", testAdministradorID)

	linhas, err := PreviewLoteDespesa(db, []string{testSolicitacaoA}, testAdministradorID)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(linhas) != 0 {
		t.Fatalf("esperado 0 linhas, obtido %d", len(linhas))
	}
}

// --- Obra --- (mockSolicitacaoObraElegivel vem de obra_test.go)

func TestPreviewLoteObra_DescartaLinhaCriar(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mockSolicitacaoObraElegivel(mock, testSolicitacaoA, testAdministradorID, "inclusao")
	mockLinhasObra(mock, testSolicitacaoA,
		[5]interface{}{nil, "LO1", "SD1", "CRIAR", 100.0},
		[5]interface{}{nil, "LO1", "SD1", "9999", 200.0},
	)

	linhas, err := PreviewLoteObra(db, []string{testSolicitacaoA}, testAdministradorID)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(linhas) != 1 {
		t.Fatalf("esperado 1 linha (CRIAR descartada), obtido %d: %+v", len(linhas), linhas)
	}
	if linhas[0].OrdemInvestimento != "9999" {
		t.Fatalf("linha inesperada: %+v", linhas[0])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestPreviewLoteObra_IgnoraTipoNaoObras(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, s.status, s.administrador_id, s.classificacao, u.nome")).
		WithArgs(testSolicitacaoA).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "status", "administrador_id", "classificacao", "nome"}).
			AddRow("transferencia", "em_atendimento", testAdministradorID, nil, "Fulano de Tal"))

	linhas, err := PreviewLoteObra(db, []string{testSolicitacaoA}, testAdministradorID)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(linhas) != 0 {
		t.Fatalf("esperado 0 linhas, obtido %d", len(linhas))
	}
}
