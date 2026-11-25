package handlers

import (
	"strings"
	"testing"
	"time"
)

func TestTransferenciaPropriaRecusada(t *testing.T) {
	m := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	mesma := []lancamentoValidado{
		{Lado: "origem", CentroCustoID: "a", ContaID: "c", Mes: m, Valor: 10},
		{Lado: "destino", CentroCustoID: "a", ContaID: "c", Mes: m, Valor: 10},
	}
	if msg := validarTransferenciaPropria(mesma); msg == "" {
		t.Error("mesma conta/CC/mês deveria ser recusada")
	}
	outroMes := []lancamentoValidado{mesma[0], {Lado: "destino", CentroCustoID: "a", ContaID: "c", Mes: m.AddDate(0, 1, 0), Valor: 10}}
	if msg := validarTransferenciaPropria(outroMes); msg != "" {
		t.Errorf("mês diferente é válido: %s", msg)
	}
}

func TestAnexoFaltando_MensagemPorTipo(t *testing.T) {
	_, m := validarAnexosTipo(nil, true, "transferencia")
	if strings.Contains(m, "Imobilizado") || m == "" {
		t.Errorf("transferência não pode citar Imobilizado: %q", m)
	}
	_, m = validarAnexosTipo(nil, true, "imobilizado")
	if !strings.Contains(m, "Imobilizado") {
		t.Errorf("imobilizado mantém a mensagem: %q", m)
	}
}

func TestValidarRegraFeriado(t *testing.T) {
	if _, err := validarRegraFeriado("Natal", "fixo", 25, 12, 0); err != nil {
		t.Errorf("25/12 deveria valer: %v", err)
	}
	for _, c := range []struct {
		tipo           string
		dia, mes, desl float64
	}{
		{"fixo", 31, 4, 0}, {"fixo", 0, 5, 0}, {"fixo", 10, 13, 0}, {"pascoa", 0, 0, 100}, {"pascoa", 0, 0, -61}, {"outro", 1, 1, 0}, {"fixo", 1.5, 1, 0},
	} {
		if _, err := validarRegraFeriado("x", c.tipo, c.dia, c.mes, c.desl); err == nil {
			t.Errorf("%+v deveria ser recusada", c)
		}
	}
	v, err := validarRegraFeriado("Carnaval", "pascoa", 7, 7, -47)
	if err != nil || v[2] != float64(0) || v[3] != float64(0) || v[4] != float64(-47) {
		t.Errorf("em 'pascoa' dia/mês são zerados: %v %v", v, err)
	}
}
