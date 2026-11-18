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

func TestFilialDoCodigo(t *testing.T) {
	casos := map[string]string{"10016000": "1001", "816000": "81", "9166000": "916", "40166010": "4016"}
	for cod, esperado := range casos {
		if f, ok := filialDoCodigo(cod); !ok || f != esperado {
			t.Errorf("%s: esperava %s, veio %q ok=%v", cod, esperado, f, ok)
		}
	}
	for _, ruim := range []string{"6000", "", "CC-TESTE-01", "1001600A", "1001 6000"} {
		if _, ok := filialDoCodigo(ruim); ok {
			t.Errorf("%q deveria ser recusado", ruim)
		}
	}
}

func TestCarregarCentrosCustoSAP(t *testing.T) {
	orig := lerCentrosCustoSAP
	t.Cleanup(func() { lerCentrosCustoSAP = orig })
	lerCentrosCustoSAP = func(context.Context, sapodata.Config, time.Time) ([]sapodata.CentroCusto, error) {
		return []sapodata.CentroCusto{
			{Codigo: "10016000", Nome: "ADMINISTRATIVO"}, // filial 1001 já existe → cria CC
			{Codigo: "10026000", Nome: "ADMINISTRATIVO"}, // filial 1002 nova, com nome no Controle TI
			{Codigo: "816000", Nome: "ADM"},              // filial 81 nova, sem nome → usa o código
			{Codigo: "10016010", Nome: "IMOVEIS"},        // já existe → atualiza
			{Codigo: "6000", Nome: "CURTO"},              // sem filial → ignorado
			{Codigo: "10016020", Nome: ""},               // sem nome → ignorado
		}, nil
	}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery(`SELECT divisao, descricao FROM ti_param_divisao`).
		WillReturnRows(sqlmock.NewRows([]string{"divisao", "descricao"}).AddRow("1002", "IMBIRIBEIRA"))
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT codigo, id FROM divisoes`).WillReturnRows(sqlmock.NewRows([]string{"codigo", "id"}).AddRow("1001", "div-1001"))
	cc := func(cod, nome, div, fil string, inserida bool) {
		mock.ExpectQuery(`INSERT INTO centros_custo`).WithArgs(cod, nome, div, fil).WillReturnRows(sqlmock.NewRows([]string{"x"}).AddRow(inserida))
	}
	cc("10016000", "ADMINISTRATIVO", "div-1001", "1001", true)
	mock.ExpectQuery(`INSERT INTO divisoes`).WithArgs("1002", "IMBIRIBEIRA").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("div-1002"))
	cc("10026000", "ADMINISTRATIVO", "div-1002", "1002", true)
	mock.ExpectQuery(`INSERT INTO divisoes`).WithArgs("81", "81").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("div-81"))
	cc("816000", "ADM", "div-81", "81", true)
	cc("10016010", "IMOVEIS", "div-1001", "1001", false)
	mock.ExpectCommit()

	r, err := carregarCentrosCustoSAP(context.Background(), db, sapodata.Config{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if r.Lidos != 6 || r.Criados != 3 || r.Atualizados != 1 || r.DivisoesCriadas != 2 || r.Ignorados != 2 {
		t.Fatalf("contagem errada: %+v", r)
	}
	if !strings.Contains(r.mensagem(), "2 divisões (filiais) novas") {
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
	db, _, _ := sqlmock.New() // nenhuma consulta esperada: nada pode ser gravado
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
