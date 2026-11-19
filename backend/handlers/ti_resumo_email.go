package handlers

// ti_resumo_email.go — envio do resumo do mês (PDF) por e-mail, depois de o Welton gerá-lo no painel
// (Visão geral → Exportar resumo (PDF) → Salvar como PDF). O PDF é gerado pelo navegador, então o ADM anexa
// o arquivo salvo e o servidor envia (o painel roda isolado e não fala com o servidor).
//
//	GET  /api/ti/destinatarios   ADM: usuários do módulo (ADM e gestores) — a lista de quem pode receber
//	POST /api/ti/enviar-resumo   ADM: multipart (destinatario, assunto, mensagem, arquivo=PDF)
//
// Só o ADM envia, e só para quem é do módulo (ADM ou gestor ativo): a rota não vira um relé de e-mail livre.

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"fb_apu05/services"
)

const (
	limitePDFResumo   = 8 << 20 // 8 MB (o resumo real tem ~80 KB)
	limiteCampoTexto  = 2000
	limiteAssuntoMail = 150
)

// trocável nos testes
var enviarPDFPorEmail = services.SendHTMLEmailWithAttachment

type destinatarioTI struct {
	Nome  string `json:"nome"`
	Email string `json:"email"`
	Tipo  string `json:"tipo"` // adm | gestor
	CCs   string `json:"ccs"`  // escopo do gestor, p/ a tela (ex.: "6720, 6150, 6810 · RetailX")
}

func listarDestinatariosTI(db *sql.DB) ([]destinatarioTI, error) {
	rows, err := db.Query(`
		SELECT u.nome, u.email, u.controle_ti,
		       COALESCE((SELECT string_agg(s.valor || CASE WHEN s.tipo = 'grupo' THEN ' (grupo)' ELSE '' END, ', ' ORDER BY s.tipo, s.valor)
		                   FROM ti_gestor_escopo s WHERE s.usuario_id = u.id), '')
		FROM usuarios u
		WHERE u.ativo AND (u.controle_ti OR EXISTS (SELECT 1 FROM ti_gestor_escopo s WHERE s.usuario_id = u.id))
		ORDER BY u.controle_ti DESC, u.nome`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []destinatarioTI{}
	for rows.Next() {
		var d destinatarioTI
		var adm bool
		if err := rows.Scan(&d.Nome, &d.Email, &adm, &d.CCs); err != nil {
			return nil, err
		}
		d.Tipo = "gestor"
		if adm {
			d.Tipo = "adm"
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// TIDestinatariosHandler — GET /api/ti/destinatarios.
func TIDestinatariosHandler(db *sql.DB) http.HandlerFunc {
	return comControleTI(db, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		lista, err := listarDestinatariosTI(db)
		if err != nil {
			log.Printf("[ControleTI] Erro ao listar destinatários: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(map[string]any{"destinatarios": lista})
	})
}

func registrarEnvioResumo(db *sql.DB, userID, dest, assunto, arquivo string, bytes int, errEnvio error) {
	msg := ""
	if errEnvio != nil {
		msg = errEnvio.Error()
		if utf8.RuneCountInString(msg) > 250 {
			msg = string([]rune(msg)[:250])
		}
	}
	if _, err := db.Exec(`INSERT INTO ti_envios_resumo (usuario_id, destinatario, assunto, arquivo, bytes, ok, erro) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		userID, dest, assunto, arquivo, bytes, errEnvio == nil, msg); err != nil {
		log.Printf("[ControleTI] Erro ao registrar o envio do resumo: %v", err)
	}
}

// TIEnviarResumoHandler — POST /api/ti/enviar-resumo.
func TIEnviarResumoHandler(db *sql.DB) http.HandlerFunc {
	return comControleTI(db, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, limitePDFResumo+(1<<20))
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			jsonErr(w, http.StatusBadRequest, "envio inválido ou arquivo maior que 8 MB")
			return
		}
		dest := strings.ToLower(strings.TrimSpace(r.FormValue("destinatario")))
		assunto := strings.TrimSpace(r.FormValue("assunto"))
		mensagem := strings.TrimSpace(r.FormValue("mensagem"))
		if assunto == "" {
			assunto = "Resumo de despesas de TI"
		}
		if utf8.RuneCountInString(assunto) > limiteAssuntoMail || strings.ContainsAny(assunto, "\r\n") {
			jsonErr(w, http.StatusBadRequest, "assunto inválido (até 150 caracteres, uma linha)")
			return
		}
		if utf8.RuneCountInString(mensagem) > limiteCampoTexto {
			jsonErr(w, http.StatusBadRequest, "mensagem muito longa (até 2.000 caracteres)")
			return
		}
		lista, err := listarDestinatariosTI(db)
		if err != nil {
			log.Printf("[ControleTI] Erro ao validar o destinatário: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		permitido := false
		for _, d := range lista {
			if strings.EqualFold(d.Email, dest) {
				permitido = true
			}
		}
		if !permitido {
			jsonErr(w, http.StatusBadRequest, "destinatário não faz parte do Controle de Despesas (use a lista)")
			return
		}
		f, cab, err := r.FormFile("arquivo")
		if err != nil {
			jsonErr(w, http.StatusBadRequest, "anexe o PDF do resumo (campo arquivo)")
			return
		}
		defer f.Close()
		dados, err := io.ReadAll(io.LimitReader(f, limitePDFResumo+1))
		if err != nil || len(dados) > limitePDFResumo {
			jsonErr(w, http.StatusBadRequest, "não foi possível ler o PDF (limite de 8 MB)")
			return
		}
		if len(dados) < 8 || string(dados[:5]) != "%PDF-" {
			jsonErr(w, http.StatusBadRequest, "o arquivo não é um PDF")
			return
		}
		nome := cab.Filename
		if !strings.HasSuffix(strings.ToLower(nome), ".pdf") {
			nome += ".pdf"
		}
		corpo := `<div style="font-family:Arial,sans-serif;line-height:1.6;color:#333;max-width:620px">`
		if mensagem != "" {
			corpo += "<p>" + strings.ReplaceAll(html.EscapeString(mensagem), "\n", "<br>") + "</p>"
		}
		corpo += fmt.Sprintf(`<p>Segue em anexo o resumo de despesas de TI (PDF).</p><p style="font-size:12px;color:#777">Enviado pelo Controle de Despesas em %s. Uso interno.</p></div>`, time.Now().In(fusoRecife).Format("02/01/2006 15:04"))

		uid := GetUserIDFromContext(r)
		errEnvio := enviarPDFPorEmail([]string{dest}, assunto, corpo, nome, "application/pdf", dados)
		registrarEnvioResumo(db, uid, dest, assunto, nome, len(dados), errEnvio)
		if errEnvio != nil {
			log.Printf("[ControleTI] Resumo não enviado a %s: %v", dest, errEnvio)
			jsonErr(w, http.StatusBadGateway, "não foi possível enviar o e-mail agora; tente de novo")
			return
		}
		log.Printf("[ControleTI] %s enviou o resumo %q (%d bytes) para %s", uid, nome, len(dados), dest)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "destinatario": dest, "arquivo": nome, "bytes": len(dados)})
	})
}
