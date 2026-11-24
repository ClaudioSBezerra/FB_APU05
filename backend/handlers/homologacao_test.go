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
