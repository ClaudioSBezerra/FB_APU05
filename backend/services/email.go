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
	"html"
	"log"
	"mime"
	"net/smtp"
	"os"
	"strconv"
	"strings"
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

// PedidoPromocao reúne o que vai no e-mail de "Solicitar promoção".
type PedidoPromocao struct {
	Nome, Email, PerfilAtual, PerfilSolicitado, Mensagem string
}

// SendPromotionRequestEmail avisa os administradores de que um usuário pediu promoção. Envia UM
// e-mail por administrador (um destinatário em cada mensagem: um endereço inválido não derruba os
// outros, e os administradores não veem os e-mails uns dos outros). Todo texto vindo do usuário é
// escapado no HTML e saneado no assunto (sem quebra de linha: não há injeção de cabeçalho).
// Devolve quantos e-mails foram enviados; erro só se NENHUM saiu.
func SendPromotionRequestEmail(admins []string, p PedidoPromocao) (int, error) {
	config := GetEmailConfig()
	if config.Password == "" {
		log.Printf("[Email] SMTP não configurado (SMTP_PASSWORD vazio) — pedido de promoção de %s não enviado", p.Email)
		return 0, fmt.Errorf("serviço de e-mail não configurado — configure SMTP_HOST/SMTP_USER/SMTP_PASSWORD")
	}
	appURL := strings.TrimRight(os.Getenv("APP_URL"), "/")
	if appURL == "" {
		appURL = "http://localhost:3000"
	}
	assunto, corpo := montarEmailPromocao(appURL, p)

	enviados := 0
	var ultimoErro error
	for _, destino := range admins {
		if strings.ContainsAny(destino, "\r\n<>,;") { // defesa extra contra cabeçalho forjado
			continue
		}
		msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
			config.From, destino, assunto, corpo)
		var err error
		if config.Port == 465 {
			err = sendMailSSL(config, []string{destino}, []byte(msg))
		} else {
			addr := fmt.Sprintf("%s:%d", config.Host, config.Port)
			err = smtp.SendMail(addr, smtp.PlainAuth("", config.Username, config.Password, config.Host), config.Username, []string{destino}, []byte(msg))
		}
		if err != nil {
			log.Printf("[Email] Falha ao avisar %s do pedido de promoção: %v", destino, err)
			ultimoErro = err
			continue
		}
		enviados++
	}
	if enviados == 0 {
		if ultimoErro == nil {
			ultimoErro = fmt.Errorf("nenhum destinatário válido")
		}
		return 0, fmt.Errorf("falha ao enviar e-mail: %w", ultimoErro)
	}
	log.Printf("[Email] Pedido de promoção de %s enviado a %d administrador(es)", p.Email, enviados)
	return enviados, nil
}

// montarEmailPromocao monta o assunto (já codificado, RFC 2047) e o corpo HTML do pedido de promoção.
// Função pura, para teste: todo texto do usuário é escapado no HTML e o assunto não tem quebra de linha.
func montarEmailPromocao(appURL string, p PedidoPromocao) (assunto, corpo string) {
	link := appURL + "/admin/usuarios"

	esc := html.EscapeString
	mensagem := ""
	if strings.TrimSpace(p.Mensagem) != "" {
		mensagem = `<p style="margin:16px 0 4px;"><strong>Mensagem do usuário:</strong></p>` +
			`<p style="background:#f4f4f8;padding:12px;border-radius:6px;margin:0;">` + esc(p.Mensagem) + `</p>`
	}
	corpo = fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0"></head>
<body style="font-family: Arial, sans-serif; line-height: 1.6; color: #333333; max-width: 600px; margin: 0 auto;">
  <div style="background-color: #f4f4f8; padding: 40px; border-radius: 8px;">
    <div style="background: #2d3748; color: white; padding: 20px; border-radius: 8px; text-align: center;">
      <div style="font-size: 20px; font-weight: bold;">Módulo de Controladoria</div>
    </div>
    <div style="background: white; padding: 30px; border-radius: 8px; margin-top: 16px;">
      <p>Olá, administrador(a).</p>
      <p><strong>%s</strong> (%s) pediu promoção do perfil <strong>%s</strong> para <strong>%s</strong>.</p>
      %s
      <p style="margin-top:20px;">Para atender, abra <em>Gestão de usuários</em>, localize o usuário e altere o perfil. Se não for o caso, basta ignorar este e-mail.</p>
      <div style="text-align: center; margin: 30px 0;">
        <a href="%s" style="display: inline-block; padding: 12px 24px; background: #2d3748; color: white; text-decoration: none; border-radius: 4px; font-weight: bold;">Abrir Gestão de usuários</a>
      </div>
    </div>
  </div>
</body>
</html>
`, esc(p.Nome), esc(p.Email), esc(p.PerfilAtual), esc(p.PerfilSolicitado), mensagem, esc(link))

	// Assunto: o nome pode ter acento, então vai codificado (RFC 2047); controles já foram removidos.
	assunto = mime.QEncoding.Encode("utf-8", "Módulo de Controladoria - Pedido de promoção: "+strings.Join(strings.Fields(p.Nome), " "))

	return assunto, corpo
}
