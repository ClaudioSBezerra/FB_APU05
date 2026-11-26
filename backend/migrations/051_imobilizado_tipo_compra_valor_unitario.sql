-- Sinalizações da equipe (26/11/2026):
--   A1: Imobilizado ganha "Tipo de compra" (FL ou Externa), obrigatório na abertura.
--   A4: Imobilizado guarda o valor UNITÁRIO; o total (quantidade × unitário) continua em solicitacao_lancamentos.valor.
ALTER TABLE solicitacoes ADD COLUMN IF NOT EXISTS tipo_compra VARCHAR(10);
ALTER TABLE solicitacoes ADD COLUMN IF NOT EXISTS valor_unitario NUMERIC(18, 2);
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'solicitacoes_tipo_compra_check') THEN
        ALTER TABLE solicitacoes ADD CONSTRAINT solicitacoes_tipo_compra_check CHECK (tipo_compra IS NULL OR tipo_compra IN ('FL', 'Externa'));
    END IF;
END $$;
