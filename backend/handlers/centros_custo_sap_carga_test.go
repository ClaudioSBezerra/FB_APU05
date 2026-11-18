package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"fb_apu05/internal/sapodata"
)

func TestCarregarCentrosCustoSAP(t *testing.T) {
	orig := lerCentrosCustoSAP
	t.Cleanup(func() { lerCentrosCustoSAP = orig })
	lerCentrosCustoSAP = func(context.Context, sapodata.Config, time.Time) ([]sapodata.CentroCusto, error) {
		return []sapodata.CentroCusto{
			{Codigo: "816000", Nome: "ADMINISTRATIVO", AreaNegocio: "0081"}, // divisão por zeros à esquerda → cria
			{Codigo: "816001", Nome: "TI", AreaNegocio: "81"},               // divisão exata → atualiza
			{Codigo: "700000", Nome: "EXISTENTE", AreaNegocio: "9999"},      // sem divisão, já existe → só nome
			{Codigo: "700001", Nome: "NOVO SEM DIV", AreaNegocio: "9999"},   // sem divisão, novo → não cria
			{Codigo: "700002", Nome: "", AreaNegocio: "81"},                 // sem nome → ignorado
		}, nil
	}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT codigo, id FROM divisoes`).WillReturnRows(sqlmock.NewRows([]string{"codigo", "id"}).AddRow("81", "div-81"))
	mock.ExpectQuery(`INSERT INTO centros_custo`).WithArgs("816000", "ADMINISTRATIVO", "div-81").WillReturnRows(sqlmock.NewRows([]string{"x"}).AddRow(true))
	mock.ExpectQuery(`INSERT INTO centros_custo`).WithArgs("816001", "TI", "div-81").WillReturnRows(sqlmock.NewRows([]string{"x"}).AddRow(false))
	mock.ExpectExec(`UPDATE centros_custo SET nome`).WithArgs("700000", "EXISTENTE").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE centros_custo SET nome`).WithArgs("700001", "NOVO SEM DIV").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	r, err := carregarCentrosCustoSAP(context.Background(), db, sapodata.Config{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if r.Lidos != 5 || r.Criados != 1 || r.Atualizados != 2 || r.SemDivisao != 1 || r.Ignorados != 1 {
		t.Fatalf("contagem errada: %+v", r)
	}
	if !strings.Contains(r.mensagem(), "1 novos sem divisão") {
		t.Errorf("mensagem: %s", r.mensagem())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestCarregarCentrosCustoSAP_ZeroResultadosENaoSucesso(t *testing.T) {
	orig := lerCentrosCustoSAP
	t.Cleanup(func() { lerCentrosCustoSAP = orig })
	lerCentrosCustoSAP = func(context.Context, sapodata.Config, time.Time) ([]sapodata.CentroCusto, error) { return nil, nil }
	db, _, _ := sqlmock.New() // nenhuma transação esperada: nada pode ser gravado
	defer db.Close()
	if _, err := carregarCentrosCustoSAP(context.Background(), db, sapodata.Config{}, time.Now()); err == nil {
		t.Fatal("zero centros deveria ser erro")
	}
	lerCentrosCustoSAP = func(context.Context, sapodata.Config, time.Time) ([]sapodata.CentroCusto, error) {
		return nil, errors.New("SAP respondeu HTTP 500")
	}
	if _, err := carregarCentrosCustoSAP(context.Background(), db, sapodata.Config{}, time.Now()); err == nil {
		t.Fatal("erro do SAP deveria propagar")
	}
}
