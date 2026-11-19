package handlers

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"fb_apu05/internal/controleti"
)

func TestRotinaFechamentoTI_EnviaAvisoEMarca(t *testing.T) {
	db, mock := newSQLMock(t)
	origE, origR := enviarEmailTI, rodarFechamento
	t.Cleanup(func() { enviarEmailTI, rodarFechamento = origE, origR })
	var assunto string
	var para []string
	enviarEmailTI = func(to []string, a, b string) error { para, assunto = to, a; return nil }
	rodarFechamento = func(*sql.DB, time.Time) (*controleti.FechamentoReg, bool, bool, error) {
		return &controleti.FechamentoReg{Mes: "2026-10"}, true, false, nil
	}
	mock.ExpectQuery("FROM usuarios WHERE controle_ti").WillReturnRows(sqlmock.NewRows([]string{"email"}).AddRow("a@x.com").AddRow("b@x.com"))
	mock.ExpectExec("UPDATE ti_fechamentos SET aviso_enviado").WithArgs("2026-10").WillReturnResult(sqlmock.NewResult(0, 1))
	RotinaFechamentoTI(db, time.Now())
	if len(para) != 2 || !strings.Contains(assunto, "outubro/2026") || strings.Contains(assunto, "Lembrete") {
		t.Errorf("para=%v assunto=%q", para, assunto)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestRotinaFechamentoTI_EmailFalhouNaoMarca(t *testing.T) {
	db, mock := newSQLMock(t)
	origE, origR := enviarEmailTI, rodarFechamento
	t.Cleanup(func() { enviarEmailTI, rodarFechamento = origE, origR })
	enviarEmailTI = func([]string, string, string) error { return errors.New("smtp fora") }
	rodarFechamento = func(*sql.DB, time.Time) (*controleti.FechamentoReg, bool, bool, error) {
		return &controleti.FechamentoReg{Mes: "2026-10"}, true, false, nil
	}
	mock.ExpectQuery("FROM usuarios WHERE controle_ti").WillReturnRows(sqlmock.NewRows([]string{"email"}).AddRow("a@x.com"))
	RotinaFechamentoTI(db, time.Now()) // não pode fazer UPDATE: o sqlmock falharia
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}
