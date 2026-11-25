-- Compartilhar chamado (pedido da Controladoria, 25/11/2026): o dono do chamado dá acesso SOMENTE DE LEITURA
-- a outros usuários já cadastrados.
CREATE TABLE IF NOT EXISTS solicitacao_compartilhamentos (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    solicitacao_id UUID NOT NULL REFERENCES solicitacoes(id),
    usuario_id UUID NOT NULL REFERENCES usuarios(id),
    compartilhado_por UUID NOT NULL REFERENCES usuarios(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (solicitacao_id, usuario_id)
);
CREATE INDEX IF NOT EXISTS idx_compartilhamentos_usuario ON solicitacao_compartilhamentos (usuario_id);
