package fila

// fila_test.go — cobre a I/O Matrix da Story 4.1 referente a Assumir:
// sucesso (200, versao incrementada), conflito de versão (409), já em
// atendimento (409, mesma condição `status='aberta'` falhando) e
// solicitação inexistente (404). Mesmo padrão sqlmock dos demais pacotes
// internal/ (ver aprovacao/calculado_test.go).

import (
	"database/sql"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

const (
	testFilaSolicitacaoID   = "11111111-1111-1111-1111-111111111111"
	testFilaAdministradorID = "22222222-2222-2222-2222-222222222222"
	testFilaSolicitanteID   = "55555555-5555-5555-5555-555555555555"
	testFilaOutroID         = "66666666-6666-6666-6666-666666666666"
)

// TestAssumir_Sucesso cobre "Assumir solicitação livre" da I/O Matrix: 0
// linhas no WHERE casam -> status=aberta e versao=1, UPDATE afeta 1 linha,
// devolve status=em_atendimento/administrador_id/versao=2.
func TestAssumir_Sucesso(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("erro ao criar sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE solicitacoes`)).
		WithArgs(testFilaAdministradorID, testFilaSolicitacaoID, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tipo_solicitacao", "status", "versao", "administrador_id"}).
			AddRow(testFilaSolicitacaoID, "transferencia", "em_atendimento", 2, testFilaAdministradorID))

	resultado, err := Assumir(db, testFilaSolicitacaoID, 1, testFilaAdministradorID)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if resultado.Status != "em_atendimento" || resultado.Versao != 2 || resultado.AdministradorID != testFilaAdministradorID {
		t.Fatalf("resultado inesperado: %+v", resultado)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestAssumir_ConflitoVersao cobre "Conflito de versão" da I/O Matrix: o
// UPDATE não afeta nenhuma linha (versão já consumida por outro
// administrador) e o SELECT de fallback confirma que a solicitação EXISTE
// com uma versão diferente da enviada -> 409 com versao_atual.
func TestAssumir_ConflitoVersao(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("erro ao criar sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE solicitacoes`)).
		WithArgs(testFilaAdministradorID, testFilaSolicitacaoID, 1).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT versao FROM solicitacoes WHERE id = $1`)).
		WithArgs(testFilaSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"versao"}).AddRow(2))

	_, err = Assumir(db, testFilaSolicitacaoID, 1, testFilaAdministradorID)
	var conflito *ErrConflitoVersao
	if !errors.As(err, &conflito) {
		t.Fatalf("esperava *ErrConflitoVersao, recebeu: %v", err)
	}
	if conflito.VersaoAtual != 2 {
		t.Fatalf("VersaoAtual = %d, esperado 2", conflito.VersaoAtual)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestAssumir_JaEmAtendimento cobre "Já em atendimento" da I/O Matrix: a
// mesma condição `status='aberta'` do UPDATE falha (status já é
// em_atendimento), mesmo com a versão certa enviada — mesmo 409
// conflito_versao do caso anterior, sem distinguir o motivo.
func TestAssumir_JaEmAtendimento(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("erro ao criar sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE solicitacoes`)).
		WithArgs(testFilaAdministradorID, testFilaSolicitacaoID, 2).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT versao FROM solicitacoes WHERE id = $1`)).
		WithArgs(testFilaSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"versao"}).AddRow(2))

	_, err = Assumir(db, testFilaSolicitacaoID, 2, testFilaAdministradorID)
	var conflito *ErrConflitoVersao
	if !errors.As(err, &conflito) {
		t.Fatalf("esperava *ErrConflitoVersao, recebeu: %v", err)
	}
	if conflito.VersaoAtual != 2 {
		t.Fatalf("VersaoAtual = %d, esperado 2", conflito.VersaoAtual)
	}
}

// TestAssumir_NaoEncontrada cobre "Solicitação inexistente" da I/O Matrix:
// UPDATE não afeta nada e o SELECT de fallback também não encontra a
// solicitação -> ErrNaoEncontrada (404).
func TestAssumir_NaoEncontrada(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("erro ao criar sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`UPDATE solicitacoes`)).
		WithArgs(testFilaAdministradorID, testFilaSolicitacaoID, 1).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT versao FROM solicitacoes WHERE id = $1`)).
		WithArgs(testFilaSolicitacaoID).
		WillReturnError(sql.ErrNoRows)

	_, err = Assumir(db, testFilaSolicitacaoID, 1, testFilaAdministradorID)
	if !errors.Is(err, ErrNaoEncontrada) {
		t.Fatalf("esperava ErrNaoEncontrada, recebeu: %v", err)
	}
}

// --- MarcarPendencia (Story 4.2) ---

// TestMarcarPendencia_Sucesso cobre "Pendência com sucesso" da I/O Matrix:
// UPDATE afeta 1 linha (status pendente, versao+1) e o INSERT do comentário
// de pendência roda na MESMA transação antes do COMMIT.
func TestMarcarPendencia_Sucesso(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("erro ao criar sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs(testFilaSolicitacaoID, 1, testFilaAdministradorID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tipo_solicitacao", "status", "versao", "administrador_id"}).
			AddRow(testFilaSolicitacaoID, "transferencia", "pendente", 2, testFilaAdministradorID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO solicitacao_comentarios")).
		WithArgs(testFilaSolicitacaoID, testFilaAdministradorID, "falta nota fiscal").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	resultado, err := MarcarPendencia(db, testFilaSolicitacaoID, 1, testFilaAdministradorID, "falta nota fiscal")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if resultado.Status != "pendente" || resultado.Versao != 2 || resultado.AdministradorID != testFilaAdministradorID {
		t.Fatalf("resultado inesperado: %+v", resultado)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestMarcarPendencia_AdminErrado cobre "Pendência por admin errado" da I/O
// Matrix: o UPDATE não afeta nada (administrador_id da linha != chamador) e
// o fallback confirma que a linha pertence a outro administrador -> 403
// ErrNaoAutorizado, nunca 409.
func TestMarcarPendencia_AdminErrado(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("erro ao criar sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs(testFilaSolicitacaoID, 1, testFilaAdministradorID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT administrador_id, status, versao FROM solicitacoes WHERE id = $1")).
		WithArgs(testFilaSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"administrador_id", "status", "versao"}).
			AddRow(testFilaOutroID, "em_atendimento", 1))

	_, err = MarcarPendencia(db, testFilaSolicitacaoID, 1, testFilaAdministradorID, "texto")
	if !errors.Is(err, ErrNaoAutorizado) {
		t.Fatalf("esperava ErrNaoAutorizado, recebeu: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestMarcarPendencia_StatusIncompativel cobre "Pendência com status/versão
// incompatível" da I/O Matrix: a linha pertence ao chamador (administrador
// correto), mas o status já não é 'em_atendimento' (ex.: 'aberta', nunca
// assumida) -> 409 ErrConflitoVersao, não 403 (a ordem de checagem no
// fallback é 404 -> 403 -> 409, e aqui o dono bate).
func TestMarcarPendencia_StatusIncompativel(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("erro ao criar sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs(testFilaSolicitacaoID, 1, testFilaAdministradorID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT administrador_id, status, versao FROM solicitacoes WHERE id = $1")).
		WithArgs(testFilaSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"administrador_id", "status", "versao"}).
			AddRow(testFilaAdministradorID, "aberta", 3))

	_, err = MarcarPendencia(db, testFilaSolicitacaoID, 1, testFilaAdministradorID, "texto")
	var conflito *ErrConflitoVersao
	if !errors.As(err, &conflito) {
		t.Fatalf("esperava *ErrConflitoVersao, recebeu: %v", err)
	}
	if conflito.VersaoAtual != 3 {
		t.Fatalf("VersaoAtual = %d, esperado 3", conflito.VersaoAtual)
	}
}

// TestMarcarPendencia_StatusIncompativelAdministradorNulo cobre a mesma
// linha "status/versão incompatível" da I/O Matrix, mas com
// administrador_id NULL (solicitação nunca assumida, status='aberta') —
// achado de revisão: NULL nunca é "outro administrador", então deve cair
// no mesmo 409 ErrConflitoVersao, nunca 403 ErrNaoAutorizado.
func TestMarcarPendencia_StatusIncompativelAdministradorNulo(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("erro ao criar sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs(testFilaSolicitacaoID, 1, testFilaAdministradorID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT administrador_id, status, versao FROM solicitacoes WHERE id = $1")).
		WithArgs(testFilaSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"administrador_id", "status", "versao"}).
			AddRow(nil, "aberta", 1))

	_, err = MarcarPendencia(db, testFilaSolicitacaoID, 1, testFilaAdministradorID, "texto")
	var conflito *ErrConflitoVersao
	if !errors.As(err, &conflito) {
		t.Fatalf("esperava *ErrConflitoVersao, recebeu: %v", err)
	}
	if conflito.VersaoAtual != 1 {
		t.Fatalf("VersaoAtual = %d, esperado 1", conflito.VersaoAtual)
	}
}

// TestMarcarPendencia_NaoEncontrada cobre o id inexistente -> 404
// ErrNaoEncontrada, mesmo padrão de TestAssumir_NaoEncontrada.
func TestMarcarPendencia_NaoEncontrada(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("erro ao criar sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs(testFilaSolicitacaoID, 1, testFilaAdministradorID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT administrador_id, status, versao FROM solicitacoes WHERE id = $1")).
		WithArgs(testFilaSolicitacaoID).
		WillReturnError(sql.ErrNoRows)

	_, err = MarcarPendencia(db, testFilaSolicitacaoID, 1, testFilaAdministradorID, "texto")
	if !errors.Is(err, ErrNaoEncontrada) {
		t.Fatalf("esperava ErrNaoEncontrada, recebeu: %v", err)
	}
}

// --- Comentar (Story 4.2) ---

// TestComentar_ReabreDePendente cobre "Comentário reabre de pendente" da
// I/O Matrix: UPDATE afeta 1 linha (o CASE do SQL resolve status='pendente'
// -> 'em_atendimento' no banco; aqui o mock só devolve o resultado já
// resolvido), INSERT do comentário tipo='comentario' na mesma transação.
func TestComentar_ReabreDePendente(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("erro ao criar sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs(testFilaSolicitacaoID, 1, testFilaSolicitanteID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tipo_solicitacao", "status", "versao", "administrador_id"}).
			AddRow(testFilaSolicitacaoID, "transferencia", "em_atendimento", 2, testFilaAdministradorID))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO solicitacao_comentarios")).
		WithArgs(testFilaSolicitacaoID, testFilaSolicitanteID, "ja resolvi").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	resultado, err := Comentar(db, testFilaSolicitacaoID, 1, testFilaSolicitanteID, "ja resolvi")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if resultado.Status != "em_atendimento" || resultado.Versao != 2 || resultado.AdministradorID != testFilaAdministradorID {
		t.Fatalf("resultado inesperado: %+v", resultado)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestComentar_StatusJaAtivoPermanece cobre "Comentário em status já ativo"
// da I/O Matrix: status permanece inalterado (ex.: 'aberta'), só a versão
// incrementa e o comentário é registrado.
func TestComentar_StatusJaAtivoPermanece(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("erro ao criar sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	// administrador_id = NULL (nunca assumida, status 'aberta') — exercita
	// o sql.NullString de Comentar (Boundaries "Always" da spec: o WHERE
	// não exige nenhum administrador específico).
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs(testFilaSolicitacaoID, 1, testFilaSolicitanteID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "tipo_solicitacao", "status", "versao", "administrador_id"}).
			AddRow(testFilaSolicitacaoID, "transferencia", "aberta", 2, nil))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO solicitacao_comentarios")).
		WithArgs(testFilaSolicitacaoID, testFilaSolicitanteID, "so um aviso").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	resultado, err := Comentar(db, testFilaSolicitacaoID, 1, testFilaSolicitanteID, "so um aviso")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if resultado.Status != "aberta" || resultado.Versao != 2 || resultado.AdministradorID != "" {
		t.Fatalf("resultado inesperado: %+v", resultado)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestComentar_SolicitanteErrado cobre "Comentário por quem não é o
// solicitante dono" da I/O Matrix: UPDATE não afeta nada e o fallback
// confirma que a linha pertence a outro solicitante -> 403 ErrNaoAutorizado.
func TestComentar_SolicitanteErrado(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("erro ao criar sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs(testFilaSolicitacaoID, 1, testFilaSolicitanteID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT solicitante_id, versao FROM solicitacoes WHERE id = $1")).
		WithArgs(testFilaSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"solicitante_id", "versao"}).AddRow(testFilaOutroID, 1))

	_, err = Comentar(db, testFilaSolicitacaoID, 1, testFilaSolicitanteID, "texto")
	if !errors.Is(err, ErrNaoAutorizado) {
		t.Fatalf("esperava ErrNaoAutorizado, recebeu: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas não cumpridas: %v", err)
	}
}

// TestComentar_VersaoObsoleta cobre o conflito de versão (dono bate, versão
// não) -> 409 ErrConflitoVersao.
func TestComentar_VersaoObsoleta(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("erro ao criar sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs(testFilaSolicitacaoID, 1, testFilaSolicitanteID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT solicitante_id, versao FROM solicitacoes WHERE id = $1")).
		WithArgs(testFilaSolicitacaoID).
		WillReturnRows(sqlmock.NewRows([]string{"solicitante_id", "versao"}).AddRow(testFilaSolicitanteID, 4))

	_, err = Comentar(db, testFilaSolicitacaoID, 1, testFilaSolicitanteID, "texto")
	var conflito *ErrConflitoVersao
	if !errors.As(err, &conflito) {
		t.Fatalf("esperava *ErrConflitoVersao, recebeu: %v", err)
	}
	if conflito.VersaoAtual != 4 {
		t.Fatalf("VersaoAtual = %d, esperado 4", conflito.VersaoAtual)
	}
}

// TestComentar_NaoEncontrada cobre o id inexistente -> 404 ErrNaoEncontrada.
func TestComentar_NaoEncontrada(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("erro ao criar sqlmock: %v", err)
	}
	defer db.Close()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("UPDATE solicitacoes")).
		WithArgs(testFilaSolicitacaoID, 1, testFilaSolicitanteID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT solicitante_id, versao FROM solicitacoes WHERE id = $1")).
		WithArgs(testFilaSolicitacaoID).
		WillReturnError(sql.ErrNoRows)

	_, err = Comentar(db, testFilaSolicitacaoID, 1, testFilaSolicitanteID, "texto")
	if !errors.Is(err, ErrNaoEncontrada) {
		t.Fatalf("esperava ErrNaoEncontrada, recebeu: %v", err)
	}
}
