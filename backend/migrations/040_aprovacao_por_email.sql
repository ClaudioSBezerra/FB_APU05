-- Aprovação por link no e-mail (23/11/2026). Transferência acima do limite nasce 'aguardando_aprovacao'
-- (aprovada -> 'aberta', entra na fila; rejeitada -> 'rejeitada'). Até o limite vai direto ao atendente.
-- Um convite por solicitação/aprovador: só o hash do token fica no banco; uso único; expira.
CREATE TABLE IF NOT EXISTS solicitacao_aprovacoes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    solicitacao_id UUID NOT NULL REFERENCES solicitacoes(id),
    token_hash CHAR(64) UNIQUE,                 -- NULL na decisão manual (sem link)
    aprovador_nome TEXT NOT NULL DEFAULT '',
    aprovador_email TEXT NOT NULL DEFAULT '',
    canal VARCHAR(10) NOT NULL DEFAULT 'email' CHECK (canal IN ('email', 'manual')),
    expira_em TIMESTAMPTZ,
    decisao VARCHAR(10) CHECK (decisao IN ('aprovada', 'rejeitada')),
    motivo TEXT NOT NULL DEFAULT '',
    decidido_em TIMESTAMPTZ,
    decidido_por UUID REFERENCES usuarios(id),  -- só na decisão manual
    email_enviado_em TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_solic_aprovacoes_solicitacao ON solicitacao_aprovacoes (solicitacao_id);

INSERT INTO configuracoes (chave, valor) VALUES ('aprovacao_limite_transferencia', '50000')
ON CONFLICT (chave) DO NOTHING;
