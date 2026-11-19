package controleti

import (
	"testing"
	"time"
)

func TestMesQueFecha(t *testing.T) {
	rec := fusoFechamento
	casos := []struct {
		agora  time.Time
		mes    string
		devido bool
	}{
		{time.Date(2026, 11, 5, 23, 59, 59, 0, rec), "2026-10", false},   // antes do dia 6
		{time.Date(2026, 11, 6, 0, 29, 59, 0, rec), "2026-10", false},    // antes das 00:30
		{time.Date(2026, 11, 6, 0, 30, 0, 0, rec), "2026-10", true},      // o job roda às 00:30 do dia 6
		{time.Date(2026, 11, 20, 8, 0, 0, 0, rec), "2026-10", true},      // depois (sistema fora do ar no dia 6): ainda devido
		{time.Date(2027, 1, 6, 0, 30, 0, 0, rec), "2026-12", true},       // virada de ano
		{time.Date(2026, 11, 6, 3, 30, 0, 0, time.UTC), "2026-10", true}, // 00:30 em Recife = 03:30 UTC
	}
	for _, c := range casos {
		mes, devido := MesQueFecha(c.agora)
		if mes != c.mes || devido != c.devido {
			t.Errorf("%s: mês %s devido %v, esperava %s %v", c.agora, mes, devido, c.mes, c.devido)
		}
	}
}

func TestFimDoPrazo_DiaSeteAZero(t *testing.T) {
	fim := FimDoPrazo("2026-10", time.Date(2026, 11, 6, 10, 0, 0, 0, fusoFechamento))
	if !fim.Equal(time.Date(2026, 11, 7, 0, 0, 0, 0, fusoFechamento)) {
		t.Errorf("fim do prazo: %v", fim)
	}
}
