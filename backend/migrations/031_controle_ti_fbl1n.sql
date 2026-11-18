-- Controle TI: pagamentos pela FBL1N (18/11/2026). Ver docs/referencia/04-CONTROLE-TI.md.
--
-- A FBL1N (partidas de fornecedor) diz se cada nota lançada já foi paga (compensada), está em
-- aberto ou vencida. Cada carga é uma FOTO das partidas no dia da exportação e substitui a anterior.
--
-- ti_lancamentos ganha BP e CNPJ/CPF (colunas novas da planilha Despesas TI 2027, opcionais:
-- planilha sem elas continua importando, e o cruzamento tenta achar o BP pelo nome do fornecedor).

ALTER TABLE ti_cargas_sap DROP CONSTRAINT IF EXISTS ti_cargas_sap_tipo_check;
ALTER TABLE ti_cargas_sap ADD CONSTRAINT ti_cargas_sap_tipo_check CHECK (tipo IN ('ksb1', 'ksb2', 'fbl1n'));

ALTER TABLE ti_lancamentos ADD COLUMN IF NOT EXISTS bp   VARCHAR(20) NOT NULL DEFAULT '';
ALTER TABLE ti_lancamentos ADD COLUMN IF NOT EXISTS cnpj VARCHAR(30) NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS ti_fbl1n (
    id         BIGSERIAL      PRIMARY KEY,
    carga_id   BIGINT         NOT NULL REFERENCES ti_cargas_sap(id),
    empresa    VARCHAR(10)    NOT NULL DEFAULT '',
    bp         VARCHAR(20)    NOT NULL,
    fornecedor VARCHAR(255)   NOT NULL DEFAULT '',
    tipo_doc   VARCHAR(10)    NOT NULL DEFAULT '',
    referencia VARCHAR(60)    NOT NULL DEFAULT '',
    valor      NUMERIC(18, 2) NOT NULL,           -- já com o sinal do painel (nota a pagar = positivo)
    documento  VARCHAR(30)    NOT NULL DEFAULT '',
    dt_lanc    DATE,
    vencimento DATE,
    dt_comp    DATE,                              -- data de compensação: vazia = em aberto
    doc_comp   VARCHAR(30)    NOT NULL DEFAULT '',
    bloqueio   BOOLEAN        NOT NULL DEFAULT false,
    divisao    VARCHAR(10)    NOT NULL DEFAULT '',
    texto      VARCHAR(80)    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_ti_fbl1n_bp ON ti_fbl1n (bp);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'fb_apu05_app') THEN
        GRANT USAGE, SELECT ON SEQUENCE ti_fbl1n_id_seq TO fb_apu05_app;
    END IF;
END $$;
