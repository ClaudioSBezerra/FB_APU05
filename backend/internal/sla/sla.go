// Package sla é o único dono do cálculo de prazo (Story 4.1, Epic 4
// Technical Decisions: "Módulo único de SLA ... Fila do Administrador e
// Painéis consomem esse módulo — nenhum recalcula o calendário por conta
// própria"). 48h úteis, feriados de `feriados`, fuso America/Recife, sem
// janela de horário comercial (não existe cadastro de horário no produto —
// Design Notes da spec).
//
// Calendario carrega os feriados UMA VEZ (NovoCalendario) para que
// PrazoLimite/EstaAtrasado tenham exatamente a assinatura pedida pela spec
// (sem *sql.DB, sem error) — mesmo precedente de internal/aprovacao
// (NovoResolverCalculado(db) constrói sobre a conexão/transação informada;
// os métodos de cálculo em si não tocam banco de novo).
package sla

import (
	"database/sql"
	"fmt"
	"time"

	// Garante que time.LoadLocation("America/Recife") funcione mesmo sem
	// tzdata instalado no sistema/imagem onde o binário roda (ex. um
	// ambiente de CI minimalista) — a imagem de produção (Dockerfile) já
	// instala tzdata via apk, mas isso não deveria ser um pré-requisito
	// silencioso para `go test` em qualquer outro ambiente.
	_ "time/tzdata"
)

// janelaSLA é o prazo de SLA (FR do Epic 4 context): 48h úteis.
const janelaSLA = 48 * time.Hour

// fusoSLA é o fuso único de referência para todo o cálculo (Design Notes da
// spec: "fuso America/Recife").
const fusoSLA = "America/Recife"

// DBTX é o subconjunto de *sql.DB/*sql.Tx que NovoCalendario precisa para
// consultar `feriados` — mesmo princípio de aprovacao.DBTX (AD-1: este
// pacote não depende de um driver de banco específico, só database/sql).
type DBTX interface {
	Query(query string, args ...interface{}) (*sql.Rows, error)
}

// Calendario é o calendário de dias úteis já carregado (feriados + fuso) —
// construído uma vez por NovoCalendario e reaproveitado por PrazoLimite/
// EstaAtrasado para cada solicitação da fila, sem reconsultar `feriados` a
// cada chamada.
type Calendario struct {
	feriados map[string]bool // chave "YYYY-MM-DD" no fuso America/Recife
	fuso     *time.Location
}

// NovoCalendario carrega todos os feriados cadastrados e resolve o fuso
// America/Recife. Erro de infraestrutura (fuso ausente/consulta falhou) —
// o chamador (handler) deve traduzir como 500, mesmo padrão dos demais
// construtores deste projeto.
func NovoCalendario(db DBTX) (*Calendario, error) {
	fuso, err := time.LoadLocation(fusoSLA)
	if err != nil {
		return nil, fmt.Errorf("carregar fuso %s: %w", fusoSLA, err)
	}

	rows, err := db.Query(`SELECT data FROM feriados`)
	if err != nil {
		return nil, fmt.Errorf("consultar feriados: %w", err)
	}
	defer rows.Close()

	feriados := make(map[string]bool)
	for rows.Next() {
		var data time.Time
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("ler feriado: %w", err)
		}
		feriados[data.Format("2006-01-02")] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterar feriados: %w", err)
	}

	return &Calendario{feriados: feriados, fuso: fuso}, nil
}

// PrazoLimite calcula o fim do prazo de SLA a partir de `criadoEm`: 48h
// corridas contadas só dentro de dias úteis (não sábado/domingo/feriado) —
// um dia não útil é pulado por completo, sem consumir o orçamento de 48h
// (Design Notes da spec, pseudocódigo replicado aqui literalmente).
func (c *Calendario) PrazoLimite(criadoEm time.Time) time.Time {
	t := criadoEm.In(c.fuso)
	restante := janelaSLA

	for restante > 0 {
		ano, mes, dia := t.Date()
		fimDoDia := time.Date(ano, mes, dia, 0, 0, 0, 0, c.fuso).AddDate(0, 0, 1)
		disponivel := fimDoDia.Sub(t)

		if c.diaUtil(t) {
			if disponivel >= restante {
				t = t.Add(restante)
				restante = 0
			} else {
				t = fimDoDia
				restante -= disponivel
			}
		} else {
			// Dia não útil: avança até a meia-noite seguinte sem consumir
			// `restante` — o orçamento de 48h só é gasto em dia útil.
			t = fimDoDia
		}
	}

	return t
}

// EstaAtrasado compara "agora" (no mesmo fuso) com PrazoLimite(criadoEm) —
// nunca recalcula o calendário por conta própria, delega inteiramente a
// PrazoLimite.
func (c *Calendario) EstaAtrasado(criadoEm time.Time) bool {
	agora := time.Now().In(c.fuso)
	return agora.After(c.PrazoLimite(criadoEm))
}

// diaUtil — não sábado, não domingo, não feriado cadastrado (Design Notes
// da spec).
func (c *Calendario) diaUtil(t time.Time) bool {
	switch t.Weekday() {
	case time.Saturday, time.Sunday:
		return false
	}
	return !c.feriados[t.Format("2006-01-02")]
}
