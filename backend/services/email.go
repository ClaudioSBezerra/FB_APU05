package services

// Envio de e-mail (SMTP) — hoje só pra recuperação de senha (ver
// handlers/auth.go, ForgotPasswordHandler). Versão enxuta do
// FB_APU02/backend/services/email.go: aquele arquivo também tem relatório
// executivo com IA e alertas de sincronização SAP, nenhum dos dois
// aplicável ao FB_APU05 — só o essencial (EmailConfig, sendMailSSL,
// SendPasswordResetEmail) foi replicado.

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"html"
	"log"
	"mime"
	"net/mail"
	"net/smtp"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// EmailConfig contém a configuração SMTP, lida do ambiente a cada chamada
// (mesmo padrão de getJWTSecret em handlers/auth.go — garante que
// godotenv.Load() em main() já tenha efeito).
type EmailConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
}

// GetEmailConfig lê a configuração SMTP do ambiente. Sem defaults de
// produção hardcoded (diferente do FB_APU02, que assume smtp.hostinger.com)
// — cada ambiente declara o próprio SMTP_HOST explicitamente.
func GetEmailConfig() *EmailConfig {
	port := 465
	if portStr := os.Getenv("SMTP_PORT"); portStr != "" {
		if p, err := strconv.Atoi(portStr); err == nil {
			port = p
		}
	}
	from := os.Getenv("SMTP_FROM")
	if from == "" {
		// Sem acento aqui de propósito — "From" é cabeçalho RFC 5322, não
		// corpo (o Content-Type: text/html; charset=UTF-8 abaixo só cobre o
		// corpo da mensagem); mesma convenção já adotada no Subject logo
		// abaixo ("Redefinicao de Senha", sem acento).
		from = "Modulo de Controladoria <noreply@fortesbezerra.com.br>"
	}
	return &EmailConfig{
		Host:     os.Getenv("SMTP_HOST"),
		Port:     port,
		Username: os.Getenv("SMTP_USER"),
		Password: os.Getenv("SMTP_PASSWORD"),
		From:     from,
	}
}

// sendMailSSL envia por TLS implícito (porta 465, padrão da maioria dos
// provedores — inclusive Hostinger). Portas diferentes (587/25, STARTTLS)
// usam smtp.SendMail padrão direto em SendPasswordResetEmail.
func sendMailSSL(config *EmailConfig, to []string, msg []byte) error {
	addr := fmt.Sprintf("%s:%d", config.Host, config.Port)

	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: config.Host})
	if err != nil {
		return fmt.Errorf("TLS dial falhou: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, config.Host)
	if err != nil {
		return fmt.Errorf("criação do cliente SMTP falhou: %w", err)
	}
	defer client.Quit()

	auth := smtp.PlainAuth("", config.Username, config.Password, config.Host)
	if err = client.Auth(auth); err != nil {
		return fmt.Errorf("autenticação SMTP falhou: %w", err)
	}
	if err = client.Mail(config.Username); err != nil {
		return fmt.Errorf("MAIL FROM falhou: %w", err)
	}
	for _, recipient := range to {
		if err = client.Rcpt(recipient); err != nil {
			return fmt.Errorf("RCPT TO falhou para %s: %w", recipient, err)
		}
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA falhou: %w", err)
	}
	if _, err = writer.Write(msg); err != nil {
		return fmt.Errorf("escrita SMTP falhou: %w", err)
	}
	return writer.Close()
}

// SendPasswordResetEmail envia o link de redefinição de senha — o token em
// si (texto puro, antes do hash de qualquer coisa) só existe no corpo deste
// e-mail e na URL que ele monta; nunca é logado.
func SendPasswordResetEmail(email, resetToken string) error {
	config := GetEmailConfig()
	if config.Password == "" {
		log.Printf("[Email] SMTP não configurado (SMTP_PASSWORD vazio) — não enviado para %s", email)
		return fmt.Errorf("serviço de e-mail não configurado — configure SMTP_HOST/SMTP_USER/SMTP_PASSWORD")
	}

	appURL := os.Getenv("APP_URL")
	if appURL == "" {
		appURL = "http://localhost:3000"
	}
	resetLink := fmt.Sprintf("%s/reset-senha?token=%s", appURL, resetToken)

	message := fmt.Sprintf("From: %s\r\nTo: %s\r\n"+cabecalhosPadrao(config.From)+"Subject: Modulo de Controladoria - Redefinicao de Senha\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n"+
		`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"></head>
<body style="font-family: Arial, sans-serif; line-height: 1.6; color: #333333; max-width: 600px; margin: 0 auto;">
  <div style="background-color: #f4f4f8; padding: 40px; border-radius: 8px;">
    <div style="background: #2d3748; color: white; padding: 20px; border-radius: 8px; text-align: center;">
      <div style="font-size: 20px; font-weight: bold;">Módulo de Controladoria</div>
    </div>
    <div style="background: white; padding: 30px; border-radius: 8px; margin-top: 16px;">
      <p>Olá,</p>
      <p>Recebemos uma solicitação de redefinição de senha para sua conta no Módulo de Controladoria.</p>
      <p>Se você não solicitou esta alteração, ignore este e-mail.</p>
      <div style="text-align: center; margin: 30px 0;">
        <a href="%s" style="display: inline-block; padding: 12px 24px; background: #2d3748; color: white; text-decoration: none; border-radius: 4px; font-weight: bold;">Redefinir Minha Senha</a>
      </div>
      <p style="font-size: 13px; color: #666;">Ou copie e cole o link no navegador:<br><strong>%s</strong></p>
      <p style="font-size: 12px; color: #999;">Este link expira em 1 hora.</p>
    </div>
  </div>
</body>
</html>
`, config.From, email, resetLink, resetLink)

	log.Printf("[Email] Enviando redefinição de senha para %s via %s:%d", email, config.Host, config.Port)

	var err error
	if config.Port == 465 {
		err = sendMailSSL(config, []string{email}, []byte(message))
	} else {
		addr := fmt.Sprintf("%s:%d", config.Host, config.Port)
		auth := smtp.PlainAuth("", config.Username, config.Password, config.Host)
		err = smtp.SendMail(addr, auth, config.Username, []string{email}, []byte(message))
	}
	if err != nil {
		log.Printf("[Email] Falha ao enviar para %s: %v", email, err)
		return fmt.Errorf("falha ao enviar e-mail: %w", err)
	}
	log.Printf("[Email] Redefinição de senha enviada com sucesso para %s", email)
	return nil
}

// SendHTMLEmail envia um e-mail HTML simples (assunto com acento é codificado, RFC 2047).
// Sem SMTP configurado devolve erro (quem chama decide se repete depois).
func SendHTMLEmail(to []string, subject, bodyHTML string) error {
	config := GetEmailConfig()
	if config.Password == "" || len(to) == 0 {
		return fmt.Errorf("serviço de e-mail não configurado ou sem destinatários")
	}
	for _, t := range to {
		if strings.ContainsAny(t, "\r\n,;<>") {
			return fmt.Errorf("destinatário inválido")
		}
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\n%sSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		config.From, strings.Join(to, ", "), cabecalhosPadrao(config.From), mime.QEncoding.Encode("utf-8", subject), bodyHTML)
	var err error
	if config.Port == 465 {
		err = sendMailSSL(config, to, []byte(msg))
	} else {
		addr := fmt.Sprintf("%s:%d", config.Host, config.Port)
		err = smtp.SendMail(addr, smtp.PlainAuth("", config.Username, config.Password, config.Host), config.Username, to, []byte(msg))
	}
	if err != nil {
		log.Printf("[Email] Falha ao enviar %q: %v", subject, err)
		return fmt.Errorf("falha ao enviar e-mail: %w", err)
	}
	log.Printf("[Email] %q enviado para %d destinatário(s)", subject, len(to))
	return nil
}

// SendHTMLEmailWithAttachment envia um e-mail HTML com UM anexo (multipart/mixed, base64).
// O nome do arquivo é saneado (sem caminho, sem aspas/quebra de linha). Mesmas regras de SendHTMLEmail.
func SendHTMLEmailWithAttachment(to []string, subject, bodyHTML, filename, contentType string, data []byte) error {
	config := GetEmailConfig()
	if config.Password == "" || len(to) == 0 {
		return fmt.Errorf("serviço de e-mail não configurado ou sem destinatários")
	}
	for _, t := range to {
		if strings.ContainsAny(t, "\r\n,;<>") {
			return fmt.Errorf("destinatário inválido")
		}
	}
	msg := buildMessageWithAttachment(config.From, to, subject, bodyHTML, filename, contentType, data)
	var err error
	if config.Port == 465 {
		err = sendMailSSL(config, to, msg)
	} else {
		addr := fmt.Sprintf("%s:%d", config.Host, config.Port)
		err = smtp.SendMail(addr, smtp.PlainAuth("", config.Username, config.Password, config.Host), config.Username, to, msg)
	}
	if err != nil {
		log.Printf("[Email] Falha ao enviar %q com anexo: %v", subject, err)
		return fmt.Errorf("falha ao enviar e-mail: %w", err)
	}
	log.Printf("[Email] %q enviado com anexo (%d bytes) para %d destinatário(s)", subject, len(data), len(to))
	return nil
}

func nomeAnexoSeguro(nome string) string {
	nome = strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\' || r == '"' || r == '\r' || r == '\n' || r == '\x00' || r < 32:
			return '_'
		}
		return r
	}, nome)
	nome = strings.TrimSpace(nome)
	if nome == "" || nome == "." || nome == ".." {
		nome = "anexo.pdf"
	}
	if len(nome) > 120 {
		nome = nome[len(nome)-120:]
	}
	return nome
}

func buildMessageWithAttachment(from string, to []string, subject, bodyHTML, filename, contentType string, data []byte) []byte {
	var b strings.Builder
	sum := sha256.Sum256(append([]byte(subject), data[:min(len(data), 64)]...))
	boundary := fmt.Sprintf("fcx-%x", sum)[:36]
	alt := "alt-" + boundary[4:]
	nome := nomeAnexoSeguro(filename)
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\n%sSubject: %s\r\nMIME-Version: 1.0\r\n", from, strings.Join(to, ", "), cabecalhosPadrao(from), mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", boundary)
	// corpo: texto simples + HTML (multipart/alternative), como os clientes de e-mail enviam
	fmt.Fprintf(&b, "--%s\r\nContent-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary, alt)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n", alt, wrap76(base64.StdEncoding.EncodeToString([]byte(textoSimples(bodyHTML)))))
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n", alt, wrap76(base64.StdEncoding.EncodeToString([]byte(bodyHTML))))
	fmt.Fprintf(&b, "--%s--\r\n", alt)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: %s; name=%q\r\nContent-Transfer-Encoding: base64\r\nContent-Disposition: attachment; filename=%q\r\n\r\n%s\r\n", boundary, contentType, mime.QEncoding.Encode("utf-8", nome), mime.QEncoding.Encode("utf-8", nome), wrap76(base64.StdEncoding.EncodeToString(data)))
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return []byte(b.String())
}

func wrap76(s string) string {
	var out strings.Builder
	for len(s) > 76 {
		out.WriteString(s[:76])
		out.WriteString("\r\n")
		s = s[76:]
	}
	out.WriteString(s)
	return out.String()
}

// cabecalhosPadrao devolve Date e Message-ID (RFC 5322). Sem eles, filtros como o do Microsoft 365 pontuam a mensagem
// como suspeita; o domínio do Message-ID é o do remetente.
func cabecalhosPadrao(from string) string {
	dominio := "localhost"
	if a, err := mail.ParseAddress(from); err == nil {
		if i := strings.LastIndex(a.Address, "@"); i >= 0 {
			dominio = a.Address[i+1:]
		}
	}
	var r [16]byte
	_, _ = rand.Read(r[:])
	return fmt.Sprintf("Date: %s\r\nMessage-ID: <%x@%s>\r\n", time.Now().Format(time.RFC1123Z), r, dominio)
}

// textoSimples gera a versão texto de um corpo HTML (a parte text/plain do multipart/alternative).
var reTag = regexp.MustCompile(`(?s)<[^>]*>`)

func textoSimples(h string) string {
	h = strings.NewReplacer("</p>", "\n\n", "<br>", "\n", "<br/>", "\n", "</div>", "\n").Replace(h)
	t := html.UnescapeString(reTag.ReplaceAllString(h, ""))
	return strings.TrimSpace(regexp.MustCompile(`\n{3,}`).ReplaceAllString(t, "\n\n"))
}
