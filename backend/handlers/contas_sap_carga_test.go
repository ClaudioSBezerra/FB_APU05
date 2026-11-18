package handlers

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"fb_apu05/internal/sapodata"
)

func TestCarregarContasSAP(t *testing.T) {
	orig := lerContasSAP
	t.Cleanup(func() { lerContasSAP = orig })
	var planoPedido string
	lerContasSAP = func(_ context.Context, _ sapodata.Config, plano string) ([]sapodata.ContaContabil, error) {
		planoPedido = plano
		return []sapodata.ContaContabil{
			{Codigo: "4110100005", Nome: "Custo Frete Revenda"},
			{Codigo: "4210100010", Nome: "Mão-de-Obra - PJ"},
			{Codigo: "4210200001", Nome: ""}, // sem nome → ignorada
		}, nil
	}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO contas`).WithArgs("BIFC", "4110100005", "Custo Frete Revenda").WillReturnRows(sqlmock.NewRows([]string{"x"}).AddRow(true))
	mock.ExpectQuery(`INSERT INTO contas`).WithArgs("BIFC", "4210100010", "Mão-de-Obra - PJ").WillReturnRows(sqlmock.NewRows([]string{"x"}).AddRow(false))
	mock.ExpectCommit()

	r, plano, err := carregarContasSAP(context.Background(), db, sapodata.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if planoPedido != "0050" || plano != "0050" {
		t.Errorf("plano pedido ao SAP: %q", planoPedido)
	}
	if r.Lidas != 3 || r.Criadas != 1 || r.Atualizadas != 1 || r.Ignoradas != 1 {
		t.Fatalf("contagem errada: %+v", r)
	}
	if !strings.Contains(r.mensagem(plano), "plano 0050") {
		t.Errorf("mensagem: %s", r.mensagem(plano))
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}
}

func TestCarregarContasSAP_ZeroNaoESucesso(t *testing.T) {
	orig := lerContasSAP
	t.Cleanup(func() { lerContasSAP = orig })
	lerContasSAP = func(context.Context, sapodata.Config, string) ([]sapodata.ContaContabil, error) { return nil, nil }
	db, _, _ := sqlmock.New() // nenhuma consulta esperada
	defer db.Close()
	if _, _, err := carregarContasSAP(context.Background(), db, sapodata.Config{}); err == nil {
		t.Fatal("zero contas deveria ser erro")
	}
}
