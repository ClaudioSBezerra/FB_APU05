-- Pedidos de promoção de perfil (17/11/2026). Todo usuário novo entra como "solicitante"; para
-- virar "atendente" ele pede em Conta > Solicitar promoção, e todos os administradores ativos
-- recebem um e-mail. Esta tabela é o registro do pedido e a trava anti-spam (1 pedido a cada 24 h
-- por usuário); a promoção em si continua sendo feita por um administrador em Gestão de Usuários.
CREATE TABLE IF NOT EXISTS usuarios_promocao_pedidos (
    id                BIGSERIAL    PRIMARY KEY,
    usuario_id        UUID         NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    perfil_atual      VARCHAR(20)  NOT NULL,
    perfil_solicitado VARCHAR(20)  NOT NULL DEFAULT 'atendente',
    mensagem          TEXT         NOT NULL DEFAULT '',
    destinatarios     INTEGER      NOT NULL DEFAULT 0, -- administradores que receberam o e-mail
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_promocao_pedidos_usuario ON usuarios_promocao_pedidos (usuario_id, created_at DESC);

-- Permissão na sequência (ver 025/026): o papel das requisições é fb_apu05_app.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'fb_apu05_app') THEN
        GRANT USAGE, SELECT ON SEQUENCE usuarios_promocao_pedidos_id_seq TO fb_apu05_app;
    END IF;
END $$;
