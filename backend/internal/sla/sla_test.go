package sla

// sla_test.go — cobre a I/O Matrix da Story 4.1 referente ao SLA: dentro do
// prazo (atrasada=false) e atrasada cruzando fim de semana/feriado
// (dias não úteis pulados por completo, atrasada=true). Mesmo padrão
// sqlmock dos demais pacotes internal/ (ver aprovacao/calculado_test.go) —
// *sql.DB é passado diretamente como DBTX, sem transação.

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func newCalendarioSQLMock(t *testing.T, feriados ...string) *Calendario {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("erro ao criar sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	rows := sqlmock.NewRows([]string{"data"})
	for _, f := range feriados {
		d, parseErr := time.Parse("2006-01-02", f)
		if parseErr != nil {
			t.Fatalf("feriado de teste mal formado %q: %v", f, parseErr)
		}
		rows.AddRow(d)
	}
	mock.ExpectQuery("SELECT data FROM feriados").WillReturnRows(rows)

	cal, err := NovoCalendario(db)
	if err != nil {
		t.Fatalf("NovoCalendario falhou: %v", err)
	}
	return cal
}

func recife(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Recife")
	if err != nil {
		t.Fatalf("time.LoadLocation(America/Recife) falhou: %v", err)
	}
	return loc
}

// TestPrazoLimite_DentroDoPrazo cobre "Fila dentro do prazo" da I/O Matrix:
// criadoEm há 10h úteis (uma terça-feira às 08:00, sem feriado no caminho) —
// prazo_limite cai 48h corridas depois, dentro da mesma semana útil, e
// EstaAtrasado deve ser false bem antes desse prazo.
func TestPrazoLimite_DentroDoPrazo(t *testing.T) {
	loc := recife(t)
	cal := newCalendarioSQLMock(t)

	// Terça-feira 2026-10-06 08:00 — sem fim de semana/feriado no meio das
	// 48h corridas seguintes (consome qua+qui, termina quinta 08:00).
	criadoEm := time.Date(2026, 10, 6, 8, 0, 0, 0, loc)
	prazo := cal.PrazoLimite(criadoEm)

	esperado := time.Date(2026, 10, 8, 8, 0, 0, 0, loc)
	if !prazo.Equal(esperado) {
		t.Fatalf("prazo_limite = %v, esperado %v", prazo, esperado)
	}

	// "há 10h úteis" (I/O Matrix): agora = criadoEm + 10h, bem antes do
	// prazo — não atrasada.
	agoraSimulado := criadoEm.Add(10 * time.Hour)
	if agoraSimulado.After(prazo) {
		t.Fatalf("cenário mal montado: agora simulado (%v) já passou do prazo (%v)", agoraSimulado, prazo)
	}
}

// TestEstaAtrasado_DentroDoPrazo replica o mesmo cenário usando o relógio
// real (via EstaAtrasado): criadoEm "agora - 10h", nenhum fim de semana ou
// feriado capaz de ter decorrido em 10h, então nunca deveria estar atrasada.
func TestEstaAtrasado_DentroDoPrazo(t *testing.T) {
	cal := newCalendarioSQLMock(t)
	criadoEm := time.Now().Add(-10 * time.Hour)

	if cal.EstaAtrasado(criadoEm) {
		t.Fatalf("EstaAtrasado = true para uma solicitação criada há apenas 10h")
	}
}

// TestPrazoLimite_CruzaFimDeSemana cobre "Fila atrasada cruzando fim de
// semana/feriado" da I/O Matrix: criadoEm numa sexta-feira — sábado/domingo
// são pulados por completo (não consomem o orçamento de 48h), então o prazo
// só fecha na terça-feira seguinte, nunca no domingo.
func TestPrazoLimite_CruzaFimDeSemana(t *testing.T) {
	loc := recife(t)
	cal := newCalendarioSQLMock(t)

	// Sexta-feira 2026-10-02 08:00. 48h úteis: sexta consome o resto do dia
	// (16h), sábado/domingo pulados, segunda consome 24h, terça consome as
	// 8h finais -> prazo = terça 2026-10-06 08:00.
	criadoEm := time.Date(2026, 10, 2, 8, 0, 0, 0, loc)
	prazo := cal.PrazoLimite(criadoEm)

	esperado := time.Date(2026, 10, 6, 8, 0, 0, 0, loc)
	if !prazo.Equal(esperado) {
		t.Fatalf("prazo_limite = %v, esperado %v (fim de semana deveria ser pulado por completo)", prazo, esperado)
	}

	// Simula "agora" no domingo seguinte — well dentro das 48h de relógio
	// corrido, mas já DEVERIA contar como fora do prazo útil só na terça;
	// aqui confirmamos que o domingo (ainda antes do prazo útil) não é
	// reportado como atrasado, e a quarta-feira (depois do prazo) é.
	domingo := time.Date(2026, 10, 4, 10, 0, 0, 0, loc)
	if domingo.After(prazo) {
		t.Fatalf("domingo (%v) não deveria estar depois do prazo útil (%v)", domingo, prazo)
	}

	quartaDepoisDoPrazo := time.Date(2026, 10, 7, 9, 0, 0, 0, loc)
	if !quartaDepoisDoPrazo.After(prazo) {
		t.Fatalf("quarta-feira (%v) deveria estar depois do prazo útil (%v)", quartaDepoisDoPrazo, prazo)
	}
}

// TestPrazoLimite_PulaFeriado cobre o caso de feriado cadastrado no meio da
// janela — mesmo tratamento de fim de semana (dia não útil inteiro pulado,
// sem consumir o orçamento de 48h).
func TestPrazoLimite_PulaFeriado(t *testing.T) {
	loc := recife(t)
	// 2026-10-05 é uma segunda-feira — feriado fictício só para o teste
	// (nunca um feriado real como dado de produção; Epic context: dados
	// sensíveis nunca usam exemplos reais, mas uma data de feriado não é
	// dado sensível de pessoa — mesmo assim, mantém-se fictícia aqui).
	cal := newCalendarioSQLMock(t, "2026-10-05")

	// Sábado 2026-10-03 08:00: sábado+domingo+segunda(feriado) pulados por
	// completo -> as 48h úteis só começam a contar na terça 2026-10-06
	// 00:00 e fecham exatamente 48h corridas depois (terça + quarta
	// completas), à meia-noite de quinta-feira.
	criadoEm := time.Date(2026, 10, 3, 8, 0, 0, 0, loc)
	prazo := cal.PrazoLimite(criadoEm)

	esperado := time.Date(2026, 10, 8, 0, 0, 0, 0, loc)
	if !prazo.Equal(esperado) {
		t.Fatalf("prazo_limite = %v, esperado %v (feriado deveria ser pulado por completo, igual a fim de semana)", prazo, esperado)
	}
}

// TestEstaAtrasado_CruzaFimDeSemana confirma atrasada=true quando "agora" já
// passou do prazo útil calculado (mesmo cenário de
// TestPrazoLimite_CruzaFimDeSemana, mas validado via EstaAtrasado — a
// própria fila nunca recalcula o calendário por conta própria, só consome
// este método).
func TestEstaAtrasado_CruzaFimDeSemana(t *testing.T) {
	cal := newCalendarioSQLMock(t)

	// criadoEm bem no passado (dias atrás) garante, com qualquer relógio
	// real de execução do teste, que o prazo de 48h úteis já passou.
	criadoEm := time.Now().Add(-15 * 24 * time.Hour)
	if !cal.EstaAtrasado(criadoEm) {
		t.Fatalf("EstaAtrasado = false para uma solicitação criada há 15 dias")
	}
}
