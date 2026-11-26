-- Terceira leva da Controladoria (26/11/2026):
--   F8b ordem bloqueada para lançamento no SAP (planilha de 06/08: "Ordem bloqueada")
--   F12 prioridade (reabertura por comentário = alta), "respondido pelo solicitante", anexos em comentários e notas
--   F16 tipo da linha de Obras (FL / Externa) na retirada e na transferência de saldo
--   F15 edição completa de Obras pela Controladoria (troca de linhas)
ALTER TABLE obra_ordens ADD COLUMN IF NOT EXISTS bloqueada BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE obra_ordens ADD COLUMN IF NOT EXISTS observacao TEXT;

ALTER TABLE solicitacoes ADD COLUMN IF NOT EXISTS prioridade VARCHAR(10) NOT NULL DEFAULT 'normal';
ALTER TABLE solicitacoes ADD COLUMN IF NOT EXISTS respondido BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE solicitacao_obras_linhas ADD COLUMN IF NOT EXISTS tipo VARCHAR(10);

ALTER TABLE solicitacao_anexos ADD COLUMN IF NOT EXISTS interno BOOLEAN NOT NULL DEFAULT false;
CREATE TABLE IF NOT EXISTS comentario_anexos (
    comentario_id UUID NOT NULL REFERENCES solicitacao_comentarios(id) ON DELETE CASCADE,
    anexo_id UUID NOT NULL REFERENCES solicitacao_anexos(id) ON DELETE CASCADE,
    PRIMARY KEY (comentario_id, anexo_id)
);

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'fb_apu05_privilegiado') THEN
        GRANT UPDATE (prioridade, respondido) ON solicitacoes TO fb_apu05_privilegiado;
        GRANT SELECT, INSERT ON solicitacao_anexos, comentario_anexos TO fb_apu05_privilegiado;
        GRANT SELECT ON solicitacao_comentarios TO fb_apu05_privilegiado;
        GRANT INSERT, DELETE ON solicitacao_obras_linhas TO fb_apu05_privilegiado;
        GRANT UPDATE (local_obra_id, subgrupo_despesa_id, lado, tipo) ON solicitacao_obras_linhas TO fb_apu05_privilegiado;
        GRANT UPDATE (classificacao, ano_orcamento) ON solicitacoes TO fb_apu05_privilegiado;
        GRANT SELECT ON fornecedores TO fb_apu05_privilegiado;
    END IF;
END $$;

-- ordens marcadas na planilha da Controladoria (cadastro de 06/08)
UPDATE obra_ordens SET bloqueada = false, observacao = 'Ordem criada para os combustíveis de Sr. André' WHERE numero_ordem = '601983';
UPDATE obra_ordens SET bloqueada = true, observacao = 'Ordem bloqueada' WHERE numero_ordem = '601250';
UPDATE obra_ordens SET bloqueada = true, observacao = 'Ordem bloqueada' WHERE numero_ordem = '601251';
UPDATE obra_ordens SET bloqueada = true, observacao = 'Ordem bloqueada' WHERE numero_ordem = '601252';
UPDATE obra_ordens SET bloqueada = true, observacao = 'Ordem bloqueada' WHERE numero_ordem = '601253';
UPDATE obra_ordens SET bloqueada = true, observacao = 'Ordem bloqueada' WHERE numero_ordem = '601254';
UPDATE obra_ordens SET bloqueada = true, observacao = 'Ordem bloqueada' WHERE numero_ordem = '601255';
UPDATE obra_ordens SET bloqueada = true, observacao = 'Ordem bloqueada' WHERE numero_ordem = '601256';
UPDATE obra_ordens SET bloqueada = true, observacao = 'Ordem bloqueada' WHERE numero_ordem = '601257';
UPDATE obra_ordens SET bloqueada = true, observacao = 'Ordem bloqueada' WHERE numero_ordem = '601258';
UPDATE obra_ordens SET bloqueada = true, observacao = 'Ordem bloqueada' WHERE numero_ordem = '601259';
UPDATE obra_ordens SET bloqueada = true, observacao = 'Ordem bloqueada' WHERE numero_ordem = '601260';
UPDATE obra_ordens SET bloqueada = true, observacao = 'Ordem bloqueada' WHERE numero_ordem = '601261';
UPDATE obra_ordens SET bloqueada = true, observacao = 'Ordem bloqueada' WHERE numero_ordem = '601262';
UPDATE obra_ordens SET bloqueada = true, observacao = 'Ordem bloqueada' WHERE numero_ordem = '601263';
UPDATE obra_ordens SET bloqueada = true, observacao = 'Ordem bloqueada' WHERE numero_ordem = '601264';
UPDATE obra_ordens SET bloqueada = true, observacao = 'Ordem bloqueada' WHERE numero_ordem = '601265';
