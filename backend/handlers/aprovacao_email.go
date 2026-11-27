package handlers

// aprovacao_email.go — aprovação por link no e-mail (23/11/2026).
// Regra (Wanessa): transferência até o limite (padrão R$ 50.000) vai direto ao atendente; acima dele nasce
// 'aguardando_aprovacao' e o aprovador calculado (pessoa) recebe um e-mail com link para aprovar/rejeitar.
// Aprovador que é cargo/grupo (ou sem e-mail): o atendente registra a decisão manualmente.
// O link abre uma página de confirmação (GET só lê; quem decide é o POST), para que leitores de
// e-mail que "visitam" links não aprovem sozinhos. Token: 256 bits, só o hash vai ao banco, uso único.

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"fb_apu05/internal/aprovacao"
	"fb_apu05/internal/fila"
	"fb_apu05/services"
)

const (
	limiteTransferenciaPadrao = 50000.0
	validadeConviteAprovacao  = 7 * 24 * time.Hour
	papelSuperintendencia     = "Superintendência" // papel em "Papel × Pessoa" que aprova todo imobilizado
)

// rotuloAprovacao é o nome da solicitação nos e-mails e na página de aprovação.
func rotuloAprovacao(tipo string) string {
	if tipo == "imobilizado" {
		return "solicitação de imobilizado"
	}
	return "transferência de orçamento"
}

var aprovacaoTokenRL = newRateLimiter(60, time.Minute)

type consultaRow interface {
	QueryRow(query string, args ...any) *sql.Row
}

// aprovacaoObrigatoria: configuracoes.aprovacao_obrigatoria. Desligada (padrão), a alçada não é obrigatória e
// toda solicitação vai direto para a fila do atendente, que documenta a autorização recebida do gerente.
func aprovacaoObrigatoria(q consultaRow) bool {
	var v string
	if err := q.QueryRow(`SELECT valor FROM configuracoes WHERE chave = 'aprovacao_obrigatoria'`).Scan(&v); err != nil {
		return false
	}
	return v == "true"
}

// limiteAprovacaoTransferencia lê o teto sem aprovação (configuracoes.aprovacao_limite_transferencia).
func limiteAprovacaoTransferencia(q consultaRow) float64 {
	var v string
	if err := q.QueryRow(`SELECT valor FROM configuracoes WHERE chave = 'aprovacao_limite_transferencia'`).Scan(&v); err != nil {
		return limiteTransferenciaPadrao
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || f < 0 {
		return limiteTransferenciaPadrao
	}
	return f
}

func novoTokenAprovacao() (token, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, hashToken(token), nil
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

type linhaResumo struct {
	Lado  string  `json:"lado"`
	CC    string  `json:"cc"`
	Conta string  `json:"conta"`
	Mes   string  `json:"mes"`
	Valor float64 `json:"valor"`
}

type resumoSolicitacao struct {
	ID          string        `json:"id"`
	Tipo        string        `json:"tipo_solicitacao"`
	Solicitante string        `json:"solicitante"`
	Status      string        `json:"status"`
	Total       float64       `json:"total"`
	Linhas      []linhaResumo `json:"linhas"`
}

func carregarResumo(q interface {
	consultaRow
	Query(query string, args ...any) (*sql.Rows, error)
}, id string) (*resumoSolicitacao, error) {
	r := &resumoSolicitacao{ID: id}
	err := q.QueryRow(`
		SELECT s.tipo_solicitacao, s.status, COALESCE(u.nome, '')
		FROM solicitacoes s LEFT JOIN usuarios u ON u.id = s.solicitante_id WHERE s.id = $1`, id).
		Scan(&r.Tipo, &r.Status, &r.Solicitante)
	if err != nil {
		return nil, err
	}
	rows, err := q.Query(`
		SELECT l.lado, cc.codigo || ' — ' || cc.nome, COALESCE(c.codigo || ' — ' || c.nome, 'Classe: ' || ci.nome, ''), COALESCE(to_char(l.mes, 'YYYY-MM'), ''), l.valor::float8
		FROM solicitacao_lancamentos l
		JOIN centros_custo cc ON cc.id = l.centro_custo_id
		LEFT JOIN contas c ON c.id = l.conta_id
		LEFT JOIN classes_imobilizado ci ON ci.id = l.classe_imobilizado_id
		WHERE l.solicitacao_id = $1 ORDER BY l.lado DESC, l.mes, l.created_at`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var orig, dest float64
	for rows.Next() {
		var l linhaResumo
		if err := rows.Scan(&l.Lado, &l.CC, &l.Conta, &l.Mes, &l.Valor); err != nil {
			return nil, err
		}
		if l.Lado == "origem" {
			orig += l.Valor
		} else {
			dest += l.Valor
		}
		r.Linhas = append(r.Linhas, l)
	}
	r.Total = orig
	if dest > orig {
		r.Total = dest
	}
	return r, rows.Err()
}

func moedaBR(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	partes := strings.SplitN(s, ".", 2)
	in := partes[0]
	var out []byte
	for i, c := range []byte(in) {
		if i > 0 && (len(in)-i)%3 == 0 {
			out = append(out, '.')
		}
		out = append(out, c)
	}
	return "R$ " + string(out) + "," + partes[1]
}

func appURL() string {
	u := strings.TrimRight(os.Getenv("APP_URL"), "/")
	if u == "" {
		u = "http://localhost:3000"
	}
	return u
}

// convidarAprovador cria o convite e envia o e-mail. Roda depois do commit (em goroutine); qualquer
// falha só é logada: a solicitação continua 'aguardando_aprovacao' e o atendente pode decidir à mão.
func convidarAprovador(db *sql.DB, solicitacaoID string, ap aprovacao.Aprovador, tipo string) {
	type dest struct{ email, nome string }
	var destinos []dest
	switch {
	case ap.Tipo == aprovacao.TipoPessoa && ap.ColaboradorID != nil:
		var d dest
		if err := db.QueryRow(`SELECT email, nome FROM usuarios WHERE id = $1`, *ap.ColaboradorID).Scan(&d.email, &d.nome); err == nil {
			destinos = append(destinos, d)
		}
	case strings.TrimSpace(ap.Nome) != "":
		// cargo: todos os usuários ligados ao papel em "Papel × Pessoa" recebem; o primeiro que decidir vale
		rows, err := db.Query(`
			SELECT DISTINCT u.email, u.nome FROM papel_pessoa pp JOIN usuarios u ON u.id = pp.colaborador_id
			WHERE LOWER(TRIM(pp.papel)) = LOWER(TRIM($1)) AND u.ativo = true AND u.email <> ''`, ap.Nome)
		if err != nil {
			log.Printf("[Aprovacao] %s: erro ao buscar as pessoas do papel %q: %v", solicitacaoID, ap.Nome, err)
		} else {
			for rows.Next() {
				var d dest
				if rows.Scan(&d.email, &d.nome) == nil {
					destinos = append(destinos, d)
				}
			}
			rows.Close()
		}
	}
	if len(destinos) == 0 {
		log.Printf("[Aprovacao] %s: nenhum aprovador com e-mail para %q — decisão manual pelo atendente", solicitacaoID, ap.Nome)
		return
	}
	resumo, err := carregarResumo(db, solicitacaoID)
	if err != nil {
		log.Printf("[Aprovacao] %s: erro ao montar o resumo: %v", solicitacaoID, err)
		return
	}
	for _, d := range destinos {
		token, hash, err := novoTokenAprovacao()
		if err != nil {
			log.Printf("[Aprovacao] %s: não foi possível gerar o token: %v", solicitacaoID, err)
			continue
		}
		var conviteID string
		if err := db.QueryRow(`
			INSERT INTO solicitacao_aprovacoes (solicitacao_id, token_hash, aprovador_nome, aprovador_email, canal, expira_em)
			VALUES ($1, $2, $3, $4, 'email', now() + $5::interval) RETURNING id`,
			solicitacaoID, hash, d.nome, d.email, fmt.Sprintf("%d hours", int(validadeConviteAprovacao.Hours()))).Scan(&conviteID); err != nil {
			log.Printf("[Aprovacao] %s: erro ao gravar o convite: %v", solicitacaoID, err)
			continue
		}
		link := appURL() + "/aprovacao/" + token
		if err := services.SendHTMLEmail([]string{d.email}, "Aprovação de "+rotuloAprovacao(tipo)+" — "+moedaBR(resumo.Total), corpoConvite(d.nome, resumo, link)); err != nil {
			log.Printf("[Aprovacao] %s: falha ao enviar o e-mail para %s: %v", solicitacaoID, d.nome, err)
			continue
		}
		_, _ = db.Exec(`UPDATE solicitacao_aprovacoes SET email_enviado_em = now() WHERE id = $1`, conviteID)
		log.Printf("[Aprovacao] %s: convite enviado a %s", solicitacaoID, d.nome)
	}
}

func tabelaLinhas(r *resumoSolicitacao) string {
	var b strings.Builder
	b.WriteString(`<table style="border-collapse:collapse;width:100%;font-size:13px"><tr style="background:#f4f4f8"><th align="left" style="padding:6px">Lado</th><th align="left" style="padding:6px">Centro de custo</th><th align="left" style="padding:6px">Conta</th><th style="padding:6px">Mês</th><th align="right" style="padding:6px">Valor</th></tr>`)
	for _, l := range r.Linhas {
		fmt.Fprintf(&b, `<tr><td style="padding:6px;border-top:1px solid #eee">%s</td><td style="padding:6px;border-top:1px solid #eee">%s</td><td style="padding:6px;border-top:1px solid #eee">%s</td><td align="center" style="padding:6px;border-top:1px solid #eee">%s</td><td align="right" style="padding:6px;border-top:1px solid #eee">%s</td></tr>`,
			html.EscapeString(l.Lado), html.EscapeString(l.CC), html.EscapeString(l.Conta), html.EscapeString(l.Mes), moedaBR(l.Valor))
	}
	b.WriteString(`</table>`)
	return b.String()
}

func corpoConvite(nome string, r *resumoSolicitacao, link string) string {
	return fmt.Sprintf(`<div style="font-family:Arial,sans-serif;color:#333;max-width:640px;margin:0 auto">
<p>Olá, %s.</p>
<p><strong>%s</strong> abriu uma %s de <strong>%s</strong> que precisa da sua aprovação.</p>
%s
<p style="text-align:center;margin:28px 0"><a href="%s" style="background:#2d3748;color:#fff;text-decoration:none;padding:12px 24px;border-radius:4px;font-weight:bold">Ver e decidir</a></p>
<p style="font-size:12px;color:#777">Ou copie o link: %s<br>O link vale por 7 dias e só pode ser usado uma vez. A decisão só é registrada depois que você confirmar na página.</p></div>`,
		html.EscapeString(nome), html.EscapeString(r.Solicitante), rotuloAprovacao(r.Tipo), moedaBR(r.Total), tabelaLinhas(r), link, html.EscapeString(link))
}

// avisarSolicitante manda ao solicitante o resultado da aprovação.
func avisarSolicitante(db *sql.DB, solicitacaoID string, aprovada bool, motivo, quem string) {
	var email, nome string
	if err := db.QueryRow(`SELECT u.email, u.nome FROM solicitacoes s JOIN usuarios u ON u.id = s.solicitante_id WHERE s.id = $1`, solicitacaoID).Scan(&email, &nome); err != nil || email == "" {
		return
	}
	resumo, err := carregarResumo(db, solicitacaoID)
	if err != nil {
		return
	}
	res, cor := "rejeitada", "#b91c1c"
	if aprovada {
		res, cor = "aprovada", "#15803d"
	}
	extra := ""
	if !aprovada && strings.TrimSpace(motivo) != "" {
		extra = `<p><strong>Motivo:</strong> ` + html.EscapeString(motivo) + `</p>`
	}
	if aprovada {
		extra = `<p>Ela segue para o atendimento.</p>`
	}
	corpo := fmt.Sprintf(`<div style="font-family:Arial,sans-serif;color:#333;max-width:640px;margin:0 auto"><p>Olá, %s.</p>
<p>Sua %s de <strong>%s</strong> foi <strong style="color:%s">%s</strong> por %s.</p>%s%s</div>`,
		html.EscapeString(nome), rotuloAprovacao(resumo.Tipo), moedaBR(resumo.Total), cor, res, html.EscapeString(quem), extra, tabelaLinhas(resumo))
	if err := services.SendHTMLEmail([]string{email}, "Sua "+rotuloAprovacao(resumo.Tipo)+" foi "+res, corpo); err != nil {
		log.Printf("[Aprovacao] %s: falha ao avisar o solicitante: %v", solicitacaoID, err)
	}
}

// ---------- rotas públicas (sem login): o token é a credencial ----------

type consultaAprovacaoResp struct {
	Estado    string             `json:"estado"` // pendente | decidida | expirada | encerrada
	Decisao   string             `json:"decisao,omitempty"`
	Aprovador string             `json:"aprovador"`
	Resumo    *resumoSolicitacao `json:"resumo,omitempty"`
}

// ConsultarAprovacaoHandler — GET /api/aprovacao/{token}.
func ConsultarAprovacaoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !aprovacaoTokenRL.Allow(GetClientIP(r)) {
			jsonErr(w, http.StatusTooManyRequests, "muitas tentativas; aguarde um minuto")
			return
		}
		var solID, nome string
		var decisao sql.NullString
		var expira sql.NullTime
		err := db.QueryRow(`SELECT solicitacao_id, aprovador_nome, decisao, expira_em FROM solicitacao_aprovacoes WHERE token_hash = $1`,
			hashToken(r.PathValue("token"))).Scan(&solID, &nome, &decisao, &expira)
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusNotFound, "link inválido")
			return
		} else if err != nil {
			log.Printf("[Aprovacao] erro ao consultar convite: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		resumo, err := carregarResumo(db, solID)
		if err != nil {
			log.Printf("[Aprovacao] erro ao carregar o resumo: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		out := consultaAprovacaoResp{Aprovador: nome, Resumo: resumo, Estado: "pendente"}
		switch {
		case decisao.Valid:
			out.Estado, out.Decisao = "decidida", decisao.String
		case resumo.Status != "aguardando_aprovacao":
			out.Estado = "encerrada" // já decidida por outra via (ex.: manualmente pelo atendente)
		case expira.Valid && time.Now().After(expira.Time):
			out.Estado = "expirada"
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(out)
	}
}

type decisaoEntrada struct {
	Decisao string `json:"decisao"`
	Motivo  string `json:"motivo"`
	Versao  int    `json:"versao"` // só na decisão manual
}

func lerDecisao(w http.ResponseWriter, r *http.Request) (decisaoEntrada, bool) {
	var in decisaoEntrada
	corpo, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<16))
	if err != nil || json.Unmarshal(corpo, &in) != nil {
		jsonErr(w, http.StatusBadRequest, "corpo da requisição inválido")
		return in, false
	}
	in.Motivo = strings.TrimSpace(in.Motivo)
	if in.Decisao != "aprovada" && in.Decisao != "rejeitada" {
		jsonErr(w, http.StatusBadRequest, "decisão deve ser 'aprovada' ou 'rejeitada'")
		return in, false
	}
	if in.Decisao == "rejeitada" && len(in.Motivo) < 3 {
		jsonErr(w, http.StatusBadRequest, "informe o motivo da rejeição")
		return in, false
	}
	if len(in.Motivo) > 1000 {
		jsonErr(w, http.StatusBadRequest, "motivo longo demais (máx. 1000 caracteres)")
		return in, false
	}
	return in, true
}

// DecidirAprovacaoPorLinkHandler — POST /api/aprovacao/{token}. db = conexão geral; dbPriv = privilegiada
// (só ela grava o status — AD-4).
func DecidirAprovacaoPorLinkHandler(db, dbPriv *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !aprovacaoTokenRL.Allow(GetClientIP(r)) {
			jsonErr(w, http.StatusTooManyRequests, "muitas tentativas; aguarde um minuto")
			return
		}
		in, ok := lerDecisao(w, r)
		if !ok {
			return
		}
		hash := hashToken(r.PathValue("token"))
		// 1) reserva o convite (uso único, dentro da validade)
		var solID, nome string
		err := db.QueryRow(`
			UPDATE solicitacao_aprovacoes SET decisao = $2, motivo = $3, decidido_em = now()
			WHERE token_hash = $1 AND decisao IS NULL AND expira_em > now()
			RETURNING solicitacao_id, aprovador_nome`, hash, in.Decisao, in.Motivo).Scan(&solID, &nome)
		if err == sql.ErrNoRows {
			jsonErr(w, http.StatusGone, "este link já foi usado, expirou ou não é válido")
			return
		} else if err != nil {
			log.Printf("[Aprovacao] erro ao reservar o convite: %v", err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		// 2) grava o status pela conexão privilegiada
		if _, err := fila.Decidir(dbPriv, solID, 0, in.Decisao == "aprovada"); err != nil {
			_, _ = db.Exec(`UPDATE solicitacao_aprovacoes SET decisao = NULL, motivo = '', decidido_em = NULL WHERE token_hash = $1`, hash)
			var conflito *fila.ErrConflitoVersao
			if errors.As(err, &conflito) {
				jsonErr(w, http.StatusConflict, "esta solicitação já foi decidida por outra via")
				return
			}
			log.Printf("[Aprovacao] erro ao decidir %s: %v", solID, err)
			jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			return
		}
		log.Printf("[Aprovacao] %s %s por link (%s)", solID, in.Decisao, nome)
		go avisarSolicitante(db, solID, in.Decisao == "aprovada", in.Motivo, nome)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"decisao": in.Decisao})
	}
}

// DecidirAprovacaoManualHandler — POST /api/solicitacoes/{id}/aprovacao-manual (administrador/atendente):
// para aprovador que é cargo/grupo, sem e-mail, ou e-mail que não chegou.
func DecidirAprovacaoManualHandler(db, dbPriv *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		in, ok := lerDecisao(w, r)
		if !ok {
			return
		}
		if in.Versao < 1 {
			jsonErr(w, http.StatusBadRequest, "campo 'versao' é obrigatório e deve ser >= 1")
			return
		}
		id, quem := r.PathValue("id"), GetUserIDFromContext(r)
		// 27/11 (pré-pentest): segregação de funções — quem abriu o chamado não registra a aprovação dele.
		var solicitante string
		if err := db.QueryRow(`SELECT solicitante_id FROM solicitacoes WHERE id = $1`, id).Scan(&solicitante); err == nil && solicitante == quem {
			jsonErr(w, http.StatusForbidden, "Você abriu este chamado: a aprovação manual tem de ser registrada por outra pessoa do atendimento.")
			return
		}
		s, err := fila.Decidir(dbPriv, id, in.Versao, in.Decisao == "aprovada")
		if err != nil {
			var conflito *fila.ErrConflitoVersao
			switch {
			case errors.Is(err, fila.ErrNaoEncontrada):
				jsonErr(w, http.StatusNotFound, "solicitação não encontrada")
			case errors.As(err, &conflito):
				jsonErrConflitoVersao(w, conflito.VersaoAtual)
			default:
				log.Printf("[Aprovacao] erro na decisão manual de %s: %v", id, err)
				jsonErr(w, http.StatusInternalServerError, "Erro no servidor")
			}
			return
		}
		var nomeQuem string
		_ = db.QueryRow(`SELECT nome FROM usuarios WHERE id = $1`, quem).Scan(&nomeQuem)
		if _, err := db.Exec(`
			INSERT INTO solicitacao_aprovacoes (solicitacao_id, aprovador_nome, canal, decisao, motivo, decidido_em, decidido_por)
			VALUES ($1, $2, 'manual', $3, $4, now(), $5)`, id, nomeQuem, in.Decisao, in.Motivo, quem); err != nil {
			log.Printf("[Aprovacao] decisão de %s gravada, mas o registro de auditoria falhou: %v", id, err)
		}
		log.Printf("[Aprovacao] %s %s manualmente por %s", id, in.Decisao, quem)
		go avisarSolicitante(db, id, in.Decisao == "aprovada", in.Motivo, nomeQuem+" (registrado pelo atendimento)")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": s.ID, "status": s.Status, "versao": s.Versao})
	}
}
