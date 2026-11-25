-- Código sequencial legível do chamado (retorno da homologação, item 26): GO-1, GO-2, ...
-- Gerado pelo servidor (sequência do banco); o uuid continua sendo a chave técnica.
CREATE SEQUENCE IF NOT EXISTS solicitacoes_numero_seq;

ALTER TABLE solicitacoes ADD COLUMN IF NOT EXISTS numero BIGINT;

-- chamados já existentes recebem número na ordem de criação (só os que ainda não têm)
WITH ordem AS (
    SELECT id, row_number() OVER (ORDER BY created_at, id) AS n FROM solicitacoes WHERE numero IS NULL
)
UPDATE solicitacoes s SET numero = nextval('solicitacoes_numero_seq') FROM (SELECT id FROM ordem ORDER BY n) o WHERE s.id = o.id;

ALTER TABLE solicitacoes ALTER COLUMN numero SET DEFAULT nextval('solicitacoes_numero_seq');
ALTER TABLE solicitacoes ALTER COLUMN numero SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_solicitacoes_numero ON solicitacoes (numero);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'fb_apu05_app') THEN
        GRANT USAGE, SELECT ON SEQUENCE solicitacoes_numero_seq TO fb_apu05_app;
    END IF;
END $$;
