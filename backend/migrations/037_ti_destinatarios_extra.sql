-- Destinatários EXTRAS do resumo (PDF) por e-mail: endereços que podem receber o resumo mesmo sem serem
-- usuários do módulo (ex.: e-mail de teste ou de quem acompanha de fora). Só o ADM envia; só estes e os usuários
-- do módulo (ADM/gestor) aparecem na lista. Gerenciado por SQL (sem tela por enquanto).
CREATE TABLE IF NOT EXISTS ti_destinatarios_extra (
    email      VARCHAR(255) PRIMARY KEY,
    nome       VARCHAR(255) NOT NULL DEFAULT '',
    ativo      BOOLEAN      NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT ti_dest_extra_email_minusculo CHECK (email = lower(email))
);
