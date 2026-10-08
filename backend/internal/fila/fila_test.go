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
