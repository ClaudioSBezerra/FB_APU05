package services

// Envio de e-mail (SMTP) — hoje só pra recuperação de senha (ver
// handlers/auth.go, ForgotPasswordHandler). Versão enxuta do
// FB_APU02/backend/services/email.go: aquele arquivo também tem relatório
// executivo com IA e alertas de sincronização SAP, nenhum dos dois
// aplicável ao FB_APU05 — só o essencial (EmailConfig, sendMailSSL,
// SendPasswordResetEmail) foi replicado.

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/smtp"
	"os"
	"strconv"
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

	message := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: Modulo de Controladoria - Redefinicao de Senha\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n"+
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
