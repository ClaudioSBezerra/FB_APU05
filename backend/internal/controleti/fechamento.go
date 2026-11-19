package controleti

// fechamento.go — fechamento automático do dia 6 (Welton, 19/11/2026):
//   - o job roda às 00:30 do dia 6 (horário de Recife): grava a "foto" do mês anterior
//     (status aguardando_planilha) e pede, por e-mail, a planilha atualizada ao Welton e à Wanessa;
//   - o corte é a ENTRADA no SAP até 23:59:59 do dia 5 (ver sap_oficial.go);
//   - a planilha (Despesas TI) importada depois da foto conclui o fechamento; sem ela, às 23:59 do
//     dia 6 o mês fecha com a última planilha. Aviso só por e-mail (sem Teams).

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

var fusoFechamento = func() *time.Location {
	if loc, err := time.LoadLocation("America/Recife"); err == nil {
		return loc
	}
	return time.FixedZone("BRT", -3*3600)
}()

// FechamentoReg é um registro de ti_fechamentos.
type FechamentoReg struct {
	Mes             string
	Status          string
	FotoEm          time.Time
	FechadoEm       *time.Time
	PlanilhaEm      *time.Time
	AvisoEnviado    bool
	LembreteEnviado bool
	ResumoJSON      string
}

// MesQueFecha devolve o mês (AAAA-MM) que o fechamento do dia 6 de `agora` fecha, e se o momento já
// passou das 00:30 do dia 6 (ou seja, se o job está devido neste mês).
func MesQueFecha(agora time.Time) (mes string, devido bool) {
	l := agora.In(fusoFechamento)
	marco := time.Date(l.Year(), l.Month(), 6, 0, 30, 0, 0, fusoFechamento)
	ant := time.Date(l.Year(), l.Month(), 1, 0, 0, 0, 0, fusoFechamento).AddDate(0, -1, 0)
	return ant.Format("2006-01"), !l.Before(marco)
}

// FimDoPrazo é o fim do dia 6 (23:59:59) do mês seguinte ao que fecha: depois disso o mês fecha
// com a última planilha confirmada.
func FimDoPrazo(mes string, agora time.Time) time.Time {
	l := agora.In(fusoFechamento)
	return time.Date(l.Year(), l.Month(), 7, 0, 0, 0, 0, fusoFechamento)
}

// Foto resume o mês no momento do corte (guardada no registro).
type Foto struct {
	Mes         string  `json:"mes"`
	Realizado   float64 `json:"realizado"`
	Notas       int     `json:"notas"`
	Tardios     int     `json:"tardios"`
	ValorTardio float64 `json:"valorTardio"`
	K1Modo      string  `json:"k1modo"`
}

func fotoDoMes(m *Modelo, mes string) Foto {
	f := Foto{Mes: mes, Tardios: m.Meta.Tardios.N, ValorTardio: m.Meta.Tardios.Valor, K1Modo: m.Meta.K1Modo}
	for _, l := range m.Lanc {
		if l.Comp != nil && *l.Comp == mes && l.Classif == "Realizado" && l.Kind != "X" {
			f.Realizado += l.Valor
			f.Notas++
		}
	}
	f.Realizado = r2(f.Realizado)
	return f
}

// UltimoFechamento devolve o registro mais recente (nil se nunca houve).
func UltimoFechamento(db *sql.DB) (*FechamentoReg, error) {
	var f FechamentoReg
	var fe, pe sql.NullTime
	err := db.QueryRow(`SELECT mes, status, foto_em, fechado_em, planilha_em, aviso_enviado, lembrete_enviado, resumo::text
		FROM ti_fechamentos ORDER BY mes DESC LIMIT 1`).Scan(&f.Mes, &f.Status, &f.FotoEm, &fe, &pe, &f.AvisoEnviado, &f.LembreteEnviado, &f.ResumoJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ler fechamento: %w", err)
	}
	if fe.Valid {
		f.FechadoEm = &fe.Time
	}
	if pe.Valid {
		f.PlanilhaEm = &pe.Time
	}
	return &f, nil
}

// ParaMeta converte o registro no formato que o painel lê (meta.fechamento).
func (f *FechamentoReg) ParaMeta() *Fechamento {
	if f == nil {
		return nil
	}
	out := &Fechamento{Mes: f.Mes, Em: f.FotoEm.Format(time.RFC3339), Status: f.Status}
	if f.FechadoEm != nil && f.Status == "fechado" {
		out.Em = f.FotoEm.Format(time.RFC3339)
	}
	if f.PlanilhaEm != nil {
		out.PlanilhaEnviadaEm = f.PlanilhaEm.Format(time.RFC3339)
	}
	return out
}

// RodarFechamento faz o que está devido agora e devolve o que precisa de e-mail:
//
//	criou  = precisa enviar o 1º aviso (acabou de gravar a foto, ou o envio anterior falhou)
//	lembra = chegou 09:00 do dia 6 e a planilha ainda não veio (envia o lembrete)
//
// Idempotente: pode ser chamado a cada 30 s.
func RodarFechamento(db *sql.DB, agora time.Time) (reg *FechamentoReg, criou, lembra bool, err error) {
	mes, devido := MesQueFecha(agora)
	if !devido {
		return nil, false, false, nil
	}
	reg, err = UltimoFechamento(db)
	if err != nil {
		return nil, false, false, err
	}
	if reg == nil || reg.Mes < mes {
		m, errM := Carregar(db, agora)
		if errM != nil {
			return nil, false, false, errM
		}
		resumo, _ := json.Marshal(fotoDoMes(m, mes))
		if _, err = db.Exec(`INSERT INTO ti_fechamentos (mes, status, foto_em, resumo) VALUES ($1, 'aguardando_planilha', $2, $3)
			ON CONFLICT (mes) DO NOTHING`, mes, agora, string(resumo)); err != nil {
			return nil, false, false, fmt.Errorf("gravar fechamento: %w", err)
		}
		reg, err = UltimoFechamento(db)
		return reg, true, false, err
	}
	// 1º aviso que não saiu (SMTP fora do ar, ou reativado à mão): tenta de novo, mesmo com o mês já fechado
	if reg.Mes == mes && !reg.AvisoEnviado {
		return reg, true, false, nil
	}
	if reg.Mes != mes || reg.Status == "fechado" {
		return reg, false, false, nil
	}
	// aguardando: a planilha chegou depois da foto -> fecha com ela; senão fecha ao fim do dia 6
	if reg.PlanilhaEm != nil || !agora.Before(FimDoPrazo(mes, agora)) {
		if _, err = db.Exec(`UPDATE ti_fechamentos SET status = 'fechado', fechado_em = $2 WHERE mes = $1 AND status = 'aguardando_planilha'`, mes, agora); err != nil {
			return nil, false, false, fmt.Errorf("fechar mês: %w", err)
		}
		reg, err = UltimoFechamento(db)
		return reg, false, false, err
	}
	l := agora.In(fusoFechamento)
	lembra = reg.AvisoEnviado && !reg.LembreteEnviado && l.Hour() >= 9
	// 1º aviso que falhou (SMTP fora do ar): tenta de novo
	return reg, !reg.AvisoEnviado, lembra, nil
}

// MarcarAviso registra que o e-mail (aviso ou lembrete) foi enviado.
func MarcarAviso(db *sql.DB, mes string, lembrete bool) error {
	col := "aviso_enviado"
	if lembrete {
		col = "lembrete_enviado"
	}
	_, err := db.Exec(`UPDATE ti_fechamentos SET `+col+` = true WHERE mes = $1`, mes)
	return err
}

// RegistrarPlanilha anota que uma planilha Despesas TI chegou enquanto o fechamento aguarda.
func RegistrarPlanilha(tx *sql.Tx) error {
	_, err := tx.Exec(`UPDATE ti_fechamentos SET planilha_em = now() WHERE status = 'aguardando_planilha' AND planilha_em IS NULL`)
	return err
}
