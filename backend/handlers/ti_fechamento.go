package handlers

// ti_fechamento.go — job do fechamento do dia 6 do Controle TI (00:30, Recife). Chamado a cada 30 s
// pelo laço de main.go (junto da rotina do SAP); a lógica e a idempotência estão em
// internal/controleti/fechamento.go. Aviso SÓ por e-mail, para os ADM do Controle TI (Welton e Wanessa).

import (
	"database/sql"
	"fmt"
	"html"
	"log"
	"os"
	"strings"
	"time"

	"fb_apu05/internal/controleti"
	"fb_apu05/services"
)

// trocáveis nos testes
var (
	enviarEmailTI   = services.SendHTMLEmail
	rodarFechamento = controleti.RodarFechamento
)

func destinatariosFechamento(db *sql.DB) []string {
	rows, err := db.Query(`SELECT email FROM usuarios WHERE controle_ti AND ativo ORDER BY email`)
	if err != nil {
		log.Printf("[TI-Fechamento] Erro ao listar destinatários: %v", err)
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var e string
		if rows.Scan(&e) == nil && strings.Contains(e, "@") {
			out = append(out, e)
		}
	}
	return out
}

func rotuloMesBR(ym string) string {
	nomes := []string{"janeiro", "fevereiro", "março", "abril", "maio", "junho", "julho", "agosto", "setembro", "outubro", "novembro", "dezembro"}
	if len(ym) == 7 {
		var m int
		if _, err := fmt.Sscanf(ym[5:], "%d", &m); err == nil && m >= 1 && m <= 12 {
			return nomes[m-1] + "/" + ym[:4]
		}
	}
	return ym
}

func corpoEmailFechamento(mes string, lembrete bool) (assunto, corpo string) {
	app := strings.TrimRight(os.Getenv("APP_URL"), "/")
	link := ""
	if app != "" {
		link = fmt.Sprintf(`<p><a href="%s/controle-ti">Abrir o Controle TI</a></p>`, html.EscapeString(app))
	}
	pre := ""
	if lembrete {
		pre = "Lembrete: "
	}
	assunto = fmt.Sprintf("%sControle TI - fechamento de %s: sincronize a planilha com o SAP", pre, rotuloMesBR(mes))
	corpo = fmt.Sprintf(`<div style="font-family:Arial,sans-serif;line-height:1.6;color:#333;max-width:620px">
<p>O mês de <b>%s</b> acabou de ser fechado automaticamente (foto do SAP até 23:59 do dia 5).</p>
<p>Falta a planilha atualizada: importe a <b>Despesas TI</b> no Controle TI (Importar planilha), junto com a KSB1 e a FBL1N,
para concluir o fechamento. Se nenhuma planilha for importada até o fim do dia 6, o mês fecha com a última planilha importada.</p>
<p style="font-size:13px;color:#666">Lançamento com data do mês atual feito de 1 a 5 conta no mês atual; só o retroativo fica no mês que fecha.</p>
%s</div>`, html.EscapeString(rotuloMesBR(mes)), link)
	return
}

// RotinaFechamentoTI faz o que o fechamento deve neste instante (foto, e-mail, conclusão).
func RotinaFechamentoTI(db *sql.DB, agora time.Time) {
	reg, criou, lembra, err := rodarFechamento(db, agora)
	if err != nil {
		log.Printf("[TI-Fechamento] Erro: %v", err)
		return
	}
	if reg == nil || (!criou && !lembra) {
		return
	}
	assunto, corpo := corpoEmailFechamento(reg.Mes, lembra && !criou)
	para := destinatariosFechamento(db)
	if err := enviarEmailTI(para, assunto, corpo); err != nil {
		// não marca como enviado: tenta de novo na próxima rodada (só o lembrete; o 1º aviso também)
		log.Printf("[TI-Fechamento] E-mail do fechamento de %s não enviado: %v", reg.Mes, err)
		return
	}
	if err := controleti.MarcarAviso(db, reg.Mes, !criou); err != nil {
		log.Printf("[TI-Fechamento] Erro ao marcar o aviso: %v", err)
	}
	if criou {
		log.Printf("[TI-Fechamento] Foto de %s gravada; aviso enviado para %d ADM", reg.Mes, len(para))
	}
}
