package anexos

// anexos_test.go — cobre Salvar/Recuperar (Story 3.4, AD-13) no nível de
// pacote: hash SHA-256 correto gravado a partir do conteúdo decodificado
// (nunca recebido do cliente), erro genérico de banco traduzido com
// fmt.Errorf (nunca engolido), e ErrAnexoNaoEncontrado quando Recuperar não
// acha a linha — mesmo padrão sqlmock de internal/aprovacao
// (calculado_test.go/nominal_test.go).

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func newAnexosSQLMock(t *testing.T) (DBTX, sqlmock.Sqlmock, func()) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("erro ao criar sqlmock: %v", err)
	}
	return db, mock, func() { _ = db.Close() }
}

func TestSalvar_GravaHashCorreto(t *testing.T) {
	db, mock, closeFn := newAnexosSQLMock(t)
	defer closeFn()

	dados := []byte("conteudo de cotacao de teste")
	soma := sha256.Sum256(dados)
	hashEsperado := hex.EncodeToString(soma[:])

	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO solicitacao_anexos")).
		WithArgs("sol-1", "cotacao.pdf", "application/pdf", len(dados), hashEsperado, dados).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("anexo-1"))

	ref, err := Salvar(db, "sol-1", "cotacao.pdf", "application/pdf", dados)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if ref.ID != "anexo-1" {
		t.Fatalf("esperado id 'anexo-1', obtido %q", ref.ID)
	}
	if ref.HashSHA256 != hashEsperado {
		t.Fatalf("hash gravado (%q) não corresponde ao hash do conteúdo decodificado (%q)", ref.HashSHA256, hashEsperado)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

func TestSalvar_ErroDeBanco(t *testing.T) {
	db, mock, closeFn := newAnexosSQLMock(t)
	defer closeFn()

	dados := []byte("conteudo")
	soma := sha256.Sum256(dados)
	hashEsperado := hex.EncodeToString(soma[:])

	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO solicitacao_anexos")).
		WithArgs("sol-1", "cotacao.pdf", "application/pdf", len(dados), hashEsperado, dados).
		WillReturnError(errors.New("conexão perdida"))

	if _, err := Salvar(db, "sol-1", "cotacao.pdf", "application/pdf", dados); err == nil {
		t.Fatal("esperado erro, obtido nil")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

func TestRecuperar_Encontrado(t *testing.T) {
	db, mock, closeFn := newAnexosSQLMock(t)
	defer closeFn()

	dados := []byte("conteudo gravado")
	mock.ExpectQuery(regexp.QuoteMeta("SELECT nome_arquivo, content_type, dados FROM solicitacao_anexos WHERE id = $1")).
		WithArgs("anexo-1").
		WillReturnRows(sqlmock.NewRows([]string{"nome_arquivo", "content_type", "dados"}).
			AddRow("cotacao.pdf", "application/pdf", dados))

	reader, meta, err := Recuperar(db, "anexo-1")
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if meta.NomeArquivo != "cotacao.pdf" || meta.ContentType != "application/pdf" {
		t.Fatalf("metadado inesperado: %+v", meta)
	}
	lido, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("erro ao ler conteúdo: %v", err)
	}
	if string(lido) != string(dados) {
		t.Fatalf("conteúdo lido (%q) difere do gravado (%q)", lido, dados)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

func TestRecuperar_NaoEncontrado(t *testing.T) {
	db, mock, closeFn := newAnexosSQLMock(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT nome_arquivo, content_type, dados FROM solicitacao_anexos WHERE id = $1")).
		WithArgs("anexo-inexistente").
		WillReturnError(sql.ErrNoRows)

	_, _, err := Recuperar(db, "anexo-inexistente")
	if !errors.Is(err, ErrAnexoNaoEncontrado) {
		t.Fatalf("esperado ErrAnexoNaoEncontrado, obtido %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}

func TestRecuperar_ErroDeBanco(t *testing.T) {
	db, mock, closeFn := newAnexosSQLMock(t)
	defer closeFn()

	mock.ExpectQuery(regexp.QuoteMeta("SELECT nome_arquivo, content_type, dados FROM solicitacao_anexos WHERE id = $1")).
		WithArgs("anexo-1").
		WillReturnError(errors.New("conexão perdida"))

	_, _, err := Recuperar(db, "anexo-1")
	if err == nil {
		t.Fatal("esperado erro, obtido nil")
	}
	if errors.Is(err, ErrAnexoNaoEncontrado) {
		t.Fatal("erro genérico de banco não deveria ser confundido com ErrAnexoNaoEncontrado")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("expectativas do mock não satisfeitas: %v", err)
	}
}
