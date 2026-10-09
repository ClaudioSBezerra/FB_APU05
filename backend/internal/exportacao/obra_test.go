package exportacao

// obra_test.go — cobre a I/O Matrix da Story 4.4 no nível de
// ExportadorVBA.GerarLoteObra: sucesso com descarte de linha CRIAR,
// bloqueio por solicitação (parcial e "todo o lote bloqueado"),
// elegibilidade (outro administrador/não em_atendimento/tipo != 'obras' —
// cobrindo a leitura sql.NullString de `classificacao` NULL), replay de
// chave idempotente e template ausente. Mesmo padrão sqlmock de
// vba_test.go — reaproveita os helpers/constantes já definidos ali
// (mesmo pacote): novoMockExportacao, templateValidoParaTeste,
// testAdministradorID, testOutroAdminID, testSolicitacaoA/B, testChave,
// testLoteGerado.

import (
	"database/sql"
	"errors"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

// mockSolicitacaoObraElegivel simula buscarSolicitacaoObraParaExportacao
// com uma solicitação em estado elegível (dono=administradorID,
// em_atendimento, tipo='obras', classificacao preenchida).
func mockSolicitacaoObraElegivel(mock sqlmock.Sqlmock, id, administradorID, classificacao string) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, s.status, s.administrador_id, s.classificacao, u.nome")).
		WithArgs(id).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "status", "administrador_id", "classificacao", "nome"}).
			AddRow("obras", "em_atendimento", administradorID, classificacao, "Fulano de Tal"))
}

// mockLinhasObra simula buscarLinhasObraParaExportacao com as linhas
// informadas como (lado, localObraCodigo, subgrupoCodigo,
// ordemInvestimento, valor).
func mockLinhasObra(mock sqlmock.Sqlmock, solicitacaoID string, linhas ...[5]interface{}) {
	rows := sqlmock.NewRows([]string{"lado", "local_obra_codigo", "subgrupo_codigo", "ordem_investimento", "valor"})
	for _, l := range linhas {
		rows.AddRow(l[0], l[1], l[2], l[3], l[4])
	}
	mock.ExpectQuery(regexp.QuoteMeta("FROM solicitacao_obras_linhas sol")).
		WithArgs(solicitacaoID).
		WillReturnRows(rows)
}

// mockGravarLoteObraSucesso simula a transação completa de gravarLoteObra
// para `ids` quando AINDA NÃO existe lote_id gravado para esta chave
// (primeira gravação) — mesmo padrão de mockGravarLoteSucesso
// (vba_test.go), sem comentário de aviso (não existe para o lote Obra).
// loteIDExistenteOuNovo faz a checagem prévia (sem linhas -> gera via
// gen_random_uuid()) antes de qualquer INSERT.
func mockGravarLoteObraSucesso(mock sqlmock.Sqlmock, ids []string, administradorID, chave, loteID string) {
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT lote_id FROM exportacoes_sap WHERE administrador_id = $1 AND chave_idempotencia = $2 LIMIT 1")).
		WithArgs(administradorID, chave).
		WillReturnError(sql.ErrNoRows)
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

// mockGravarLoteObraReusaLoteExistente simula a transação completa de
// gravarLoteObra para `ids` quando JÁ existe um lote_id gravado para esta
// chave (replay, possivelmente com um subconjunto diferente de
// solicitações incluíveis) — loteIDExistenteOuNovo encontra o lote_id
// existente e o reaproveita em todo INSERT desta chamada, sem gerar um
// novo via gen_random_uuid().
func mockGravarLoteObraReusaLoteExistente(mock sqlmock.Sqlmock, ids []string, administradorID, chave, loteIDExistente string) {
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT lote_id FROM exportacoes_sap WHERE administrador_id = $1 AND chave_idempotencia = $2 LIMIT 1")).
		WithArgs(administradorID, chave).
		WillReturnRows(sqlmock.NewRows([]string{"lote_id"}).AddRow(loteIDExistente))
	for _, id := range ids {
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO exportacoes_sap (lote_id, solicitacao_id, administrador_id, chave_idempotencia)")).
			WithArgs(loteIDExistente, id, administradorID, chave).
			WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT lote_id FROM exportacoes_sap WHERE administrador_id = $1 AND chave_idempotencia = $2 LIMIT 1")).
		WithArgs(administradorID, chave).
		WillReturnRows(sqlmock.NewRows([]string{"lote_id"}).AddRow(loteIDExistente))
	mock.ExpectCommit()
}

// --- Sucesso / descarte de linha CRIAR ---

// TestGerarLoteObra_Sucesso_DescartaLinhaCriar cobre "Linha CRIAR
// descartada, solicitação ainda válida" da I/O Matrix: a solicitação tem 2
// linhas, 1 com ordem_investimento='CRIAR' — ela entra no lote, mas só a
// linha válida chega ao arquivo/exportacoes_sap.
func TestGerarLoteObra_Sucesso_DescartaLinhaCriar(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mockSolicitacaoObraElegivel(mock, testSolicitacaoA, testAdministradorID, "inclusao")
	mockLinhasObra(mock, testSolicitacaoA,
		[5]interface{}{"retirada", "LO1", "SG1", "9999", 100.0},
		[5]interface{}{nil, "LO1", "SG1", "CRIAR", 50.0},
	)
	mockGravarLoteObraSucesso(mock, []string{testSolicitacaoA}, testAdministradorID, testChave, testLoteGerado)

	exportador := ExportadorVBA{TemplatePathObra: templateValidoParaTeste(t)}
	lote, bloqueados, err := exportador.GerarLoteObra(db, []string{testSolicitacaoA}, testAdministradorID, testChave)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(bloqueados) != 0 {
		t.Fatalf("bloqueados inesperado: %+v", bloqueados)
	}
	if lote.LoteID != testLoteGerado {
		t.Fatalf("LoteID = %q, esperado %q", lote.LoteID, testLoteGerado)
	}
	if len(lote.SolicitacoesIncluidas) != 1 || lote.SolicitacoesIncluidas[0] != testSolicitacaoA {
		t.Fatalf("SolicitacoesIncluidas inesperado: %+v", lote.SolicitacoesIncluidas)
	}
	if len(lote.ArquivoBytes) == 0 {
		t.Fatalf("ArquivoBytes vazio")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestGerarLoteObra_TodoLoteBloqueado cobre "Solicitação só com linha(s)
// CRIAR" + "Todo o lote bloqueado" da I/O Matrix: a única solicitação do
// lote só tem linha CRIAR -> Lote{} vazio (sem lote_id/arquivo), 1
// bloqueio com o motivo fixo, template NUNCA lido (TemplatePathObra fica
// vazio/inválido de propósito, provando que não foi usado).
func TestGerarLoteObra_TodoLoteBloqueado(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mockSolicitacaoObraElegivel(mock, testSolicitacaoA, testAdministradorID, "fl")
	mockLinhasObra(mock, testSolicitacaoA, [5]interface{}{nil, "LO1", "SG1", "CRIAR", 50.0})

	exportador := ExportadorVBA{TemplatePathObra: "/caminho/nao/deveria/ser/lido.xlsm"}
	lote, bloqueados, err := exportador.GerarLoteObra(db, []string{testSolicitacaoA}, testAdministradorID, testChave)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if lote.LoteID != "" || len(lote.ArquivoBytes) != 0 {
		t.Fatalf("lote deveria ficar vazio quando tudo é bloqueado: %+v", lote)
	}
	if len(bloqueados) != 1 || bloqueados[0].SolicitacaoID != testSolicitacaoA {
		t.Fatalf("bloqueados inesperado: %+v", bloqueados)
	}
	if bloqueados[0].Motivo != motivoBloqueioSemLinhaValida {
		t.Fatalf("Motivo = %q, esperado %q", bloqueados[0].Motivo, motivoBloqueioSemLinhaValida)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestGerarLoteObra_MisturaValidaEBloqueada cobre o segundo Acceptance
// Criterion da spec 4.4: solicitação A válida + solicitação B só-CRIAR no
// MESMO lote -> A entra no arquivo/exportacoes_sap, B aparece em
// bloqueados, sem abortar a geração de A.
func TestGerarLoteObra_MisturaValidaEBloqueada(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mockSolicitacaoObraElegivel(mock, testSolicitacaoA, testAdministradorID, "inclusao")
	mockLinhasObra(mock, testSolicitacaoA, [5]interface{}{nil, "LO1", "SG1", "9999", 100.0})
	mockSolicitacaoObraElegivel(mock, testSolicitacaoB, testAdministradorID, "fl")
	mockLinhasObra(mock, testSolicitacaoB, [5]interface{}{nil, "LO2", "SG2", "CRIAR", 20.0})
	mockGravarLoteObraSucesso(mock, []string{testSolicitacaoA}, testAdministradorID, testChave, testLoteGerado)

	exportador := ExportadorVBA{TemplatePathObra: templateValidoParaTeste(t)}
	lote, bloqueados, err := exportador.GerarLoteObra(db, []string{testSolicitacaoA, testSolicitacaoB}, testAdministradorID, testChave)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(lote.SolicitacoesIncluidas) != 1 || lote.SolicitacoesIncluidas[0] != testSolicitacaoA {
		t.Fatalf("SolicitacoesIncluidas inesperado: %+v", lote.SolicitacoesIncluidas)
	}
	if len(bloqueados) != 1 || bloqueados[0].SolicitacaoID != testSolicitacaoB {
		t.Fatalf("bloqueados inesperado: %+v", bloqueados)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// --- Idempotência ---

// TestGerarLoteObra_ReplayMesmaChave cobre "Repetir mesma chave" da I/O
// Matrix: 2ª chamada com os mesmos IDs devolve o mesmo lote_id (ON
// CONFLICT DO NOTHING + re-SELECT) — nenhuma checagem de conflito de
// idempotência é feita para o lote Obra (Design Notes da spec).
func TestGerarLoteObra_ReplayMesmaChave(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mockSolicitacaoObraElegivel(mock, testSolicitacaoA, testAdministradorID, "inclusao")
	mockLinhasObra(mock, testSolicitacaoA, [5]interface{}{nil, "LO1", "SG1", "9999", 100.0})
	mockGravarLoteObraSucesso(mock, []string{testSolicitacaoA}, testAdministradorID, testChave, testLoteGerado)

	exportador := ExportadorVBA{TemplatePathObra: templateValidoParaTeste(t)}
	lote, _, err := exportador.GerarLoteObra(db, []string{testSolicitacaoA}, testAdministradorID, testChave)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if lote.LoteID != testLoteGerado {
		t.Fatalf("LoteID = %q, esperado %q (mesmo lote do replay)", lote.LoteID, testLoteGerado)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestGerarLoteObra_ReplaySubconjuntoDiferenteReusaLoteID cobre o bug
// corrigido em gravarLoteObra: 1ª chamada só inclui a solicitação A sob a
// chave K (B está bloqueada, só linha CRIAR) -> grava lote_id=L. 2ª chamada
// reusa a MESMA chave K com os MESMOS dois IDs, mas agora B também tem uma
// linha válida (não-CRIAR) -> A e B são ambas incluíveis. Sem a correção,
// gravarLoteObra geraria um loteCandidato novo via gen_random_uuid() para
// a 2ª chamada, deixando a linha de A (já gravada com L) e a nova linha de
// B com lote_id divergentes. Com a correção, loteIDExistenteOuNovo
// encontra L já gravado para (administrador, K) e reaproveita L em TODO
// INSERT desta 2ª chamada — inclusive o de B, que é novo.
func TestGerarLoteObra_ReplaySubconjuntoDiferenteReusaLoteID(t *testing.T) {
	db, mock := novoMockExportacao(t)

	// 1ª chamada: A válida, B só-CRIAR (bloqueada) -> grava só A, lote_id=L.
	mockSolicitacaoObraElegivel(mock, testSolicitacaoA, testAdministradorID, "inclusao")
	mockLinhasObra(mock, testSolicitacaoA, [5]interface{}{nil, "LO1", "SG1", "9999", 100.0})
	mockSolicitacaoObraElegivel(mock, testSolicitacaoB, testAdministradorID, "fl")
	mockLinhasObra(mock, testSolicitacaoB, [5]interface{}{nil, "LO2", "SG2", "CRIAR", 20.0})
	mockGravarLoteObraSucesso(mock, []string{testSolicitacaoA}, testAdministradorID, testChave, testLoteGerado)

	exportador := ExportadorVBA{TemplatePathObra: templateValidoParaTeste(t)}
	lote1, bloqueados1, err := exportador.GerarLoteObra(db, []string{testSolicitacaoA, testSolicitacaoB}, testAdministradorID, testChave)
	if err != nil {
		t.Fatalf("1ª chamada: erro inesperado: %v", err)
	}
	if lote1.LoteID != testLoteGerado {
		t.Fatalf("1ª chamada: LoteID = %q, esperado %q", lote1.LoteID, testLoteGerado)
	}
	if len(bloqueados1) != 1 || bloqueados1[0].SolicitacaoID != testSolicitacaoB {
		t.Fatalf("1ª chamada: bloqueados inesperado: %+v", bloqueados1)
	}

	// 2ª chamada: mesma chave K, mesmos dois IDs, mas agora B também tem
	// linha válida -> A e B incluíveis. O lote_id já existente (L) deve
	// ser reaproveitado em todo INSERT, inclusive o de B (novo).
	mockSolicitacaoObraElegivel(mock, testSolicitacaoA, testAdministradorID, "inclusao")
	mockLinhasObra(mock, testSolicitacaoA, [5]interface{}{nil, "LO1", "SG1", "9999", 100.0})
	mockSolicitacaoObraElegivel(mock, testSolicitacaoB, testAdministradorID, "fl")
	mockLinhasObra(mock, testSolicitacaoB, [5]interface{}{nil, "LO2", "SG2", "8888", 20.0})
	mockGravarLoteObraReusaLoteExistente(mock, []string{testSolicitacaoA, testSolicitacaoB}, testAdministradorID, testChave, testLoteGerado)

	lote2, bloqueados2, err := exportador.GerarLoteObra(db, []string{testSolicitacaoA, testSolicitacaoB}, testAdministradorID, testChave)
	if err != nil {
		t.Fatalf("2ª chamada: erro inesperado: %v", err)
	}
	if len(bloqueados2) != 0 {
		t.Fatalf("2ª chamada: bloqueados inesperado: %+v", bloqueados2)
	}
	if lote2.LoteID != testLoteGerado {
		t.Fatalf("2ª chamada: LoteID = %q, esperado %q (mesmo lote_id da 1ª chamada, reaproveitado)", lote2.LoteID, testLoteGerado)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// --- Elegibilidade ---

func TestGerarLoteObra_NaoEncontrada(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, s.status, s.administrador_id, s.classificacao, u.nome")).
		WithArgs(testSolicitacaoA).
		WillReturnError(sql.ErrNoRows)

	exportador := ExportadorVBA{TemplatePathObra: "/caminho/nao/usado.xlsm"}
	_, _, err := exportador.GerarLoteObra(db, []string{testSolicitacaoA}, testAdministradorID, testChave)
	if !errors.Is(err, ErrNaoEncontrada) {
		t.Fatalf("esperava ErrNaoEncontrada, recebeu: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestGerarLoteObra_OutroAdministrador(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mockSolicitacaoObraElegivel(mock, testSolicitacaoA, testOutroAdminID, "inclusao")

	exportador := ExportadorVBA{TemplatePathObra: "/caminho/nao/usado.xlsm"}
	_, _, err := exportador.GerarLoteObra(db, []string{testSolicitacaoA}, testAdministradorID, testChave)
	if !errors.Is(err, ErrNaoAutorizado) {
		t.Fatalf("esperava ErrNaoAutorizado, recebeu: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

func TestGerarLoteObra_NaoEmAtendimento(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, s.status, s.administrador_id, s.classificacao, u.nome")).
		WithArgs(testSolicitacaoA).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "status", "administrador_id", "classificacao", "nome"}).
			AddRow("obras", "pendente", testAdministradorID, "inclusao", "Fulano"))

	exportador := ExportadorVBA{TemplatePathObra: "/caminho/nao/usado.xlsm"}
	_, _, err := exportador.GerarLoteObra(db, []string{testSolicitacaoA}, testAdministradorID, testChave)
	if !errors.Is(err, ErrElegibilidadeInvalida) {
		t.Fatalf("esperava ErrElegibilidadeInvalida, recebeu: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestGerarLoteObra_TipoDiferenteDeObras cobre "Tipo diferente de 'obras'
// incluído" da I/O Matrix e o cuidado do Code Map: uma solicitação com
// tipo_solicitacao != 'obras' tem `classificacao` NULL (migration 009) —
// o Scan como sql.NullString não pode falhar antes do 400 ser decidido.
func TestGerarLoteObra_TipoDiferenteDeObras(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT s.tipo_solicitacao, s.status, s.administrador_id, s.classificacao, u.nome")).
		WithArgs(testSolicitacaoA).
		WillReturnRows(sqlmock.NewRows([]string{"tipo_solicitacao", "status", "administrador_id", "classificacao", "nome"}).
			AddRow("transferencia", "em_atendimento", testAdministradorID, nil, "Fulano"))

	exportador := ExportadorVBA{TemplatePathObra: "/caminho/nao/usado.xlsm"}
	_, _, err := exportador.GerarLoteObra(db, []string{testSolicitacaoA}, testAdministradorID, testChave)
	if !errors.Is(err, ErrTipoNaoElegivel) {
		t.Fatalf("esperava ErrTipoNaoElegivel, recebeu: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// --- Template ausente ---

// TestGerarLoteObra_TemplateAusente cobre "Template ausente, com ao menos
// 1 incluível" da I/O Matrix: a solicitação passa por toda a validação e
// tem linha válida, mas TemplatePathObra não existe -> 503
// ErrTemplateAusente, nenhuma transação é iniciada.
func TestGerarLoteObra_TemplateAusente(t *testing.T) {
	db, mock := novoMockExportacao(t)

	mockSolicitacaoObraElegivel(mock, testSolicitacaoA, testAdministradorID, "inclusao")
	mockLinhasObra(mock, testSolicitacaoA, [5]interface{}{nil, "LO1", "SG1", "9999", 100.0})

	exportador := ExportadorVBA{TemplatePathObra: filepath.Join(t.TempDir(), "nao-existe.xlsm")}
	_, _, err := exportador.GerarLoteObra(db, []string{testSolicitacaoA}, testAdministradorID, testChave)
	if !errors.Is(err, ErrTemplateAusente) {
		t.Fatalf("esperava ErrTemplateAusente, recebeu: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}
