-- Controle TI, fase 3 (16/11/2026): exportações do SAP — KSB1 (realizado) e KSB2
-- (compromissado) — e a Conferência SAP. Ver docs/referencia/04-CONTROLE-TI.md.
--
-- Histórico de cargas: quem subiu o quê e quando (as linhas das KSB não guardam
-- usuário do SAP; minimização de dados).
CREATE TABLE IF NOT EXISTS ti_cargas_sap (
    id           BIGSERIAL    PRIMARY KEY,
    tipo         VARCHAR(10)  NOT NULL CHECK (tipo IN ('ksb1', 'ksb2')),
    arquivo      VARCHAR(255) NOT NULL DEFAULT '',
    linhas       INTEGER      NOT NULL DEFAULT 0, -- linhas de dados no arquivo
    aproveitadas INTEGER      NOT NULL DEFAULT 0, -- as que foram gravadas
    periodo_ini  DATE,                            -- KSB1: primeiro e último dia dos meses cobertos
    periodo_fim  DATE,
    usuario_id   UUID         REFERENCES usuarios(id),
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- KSB1: só as linhas COM pedido (as sem pedido — folha, provisões — não entram na
-- conferência). Subir uma KSB1 substitui as linhas cujos meses de lançamento ela
-- cobre; os outros meses ficam como estão (o SAP não muda o passado fechado).
CREATE TABLE IF NOT EXISTS ti_ksb1 (
    id              BIGSERIAL      PRIMARY KEY,
    carga_id        BIGINT         NOT NULL REFERENCES ti_cargas_sap(id),
    pedido          VARCHAR(30)    NOT NULL,
    cc_sap          VARCHAR(20)    NOT NULL DEFAULT '',
    conta           VARCHAR(20)    NOT NULL DEFAULT '',
    fornecedor      VARCHAR(255)   NOT NULL DEFAULT '',
    cnpj            VARCHAR(20)    NOT NULL DEFAULT '',
    texto           TEXT           NOT NULL DEFAULT '',
    valor           NUMERIC(18, 2) NOT NULL,
    data_lancamento DATE           NOT NULL,
    doc_ref         VARCHAR(30)    NOT NULL DEFAULT '',
    estornado       BOOLEAN        NOT NULL DEFAULT false,
    doc_estorno     VARCHAR(30)    NOT NULL DEFAULT '',
    ref_estorno     VARCHAR(30)    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_ti_ksb1_pedido ON ti_ksb1 (pedido);
CREATE INDEX IF NOT EXISTS idx_ti_ksb1_data ON ti_ksb1 (data_lancamento);

-- KSB2: foto do saldo em aberto no dia da exportação; cada carga substitui tudo.
CREATE TABLE IF NOT EXISTS ti_ksb2 (
    id         BIGSERIAL      PRIMARY KEY,
    carga_id   BIGINT         NOT NULL REFERENCES ti_cargas_sap(id),
    pedido     VARCHAR(30)    NOT NULL,
    item       VARCHAR(10)    NOT NULL DEFAULT '',
    cc_sap     VARCHAR(20)    NOT NULL DEFAULT '',
    conta      VARCHAR(20)    NOT NULL DEFAULT '',
    fornecedor VARCHAR(255)   NOT NULL DEFAULT '',
    valor      NUMERIC(18, 2) NOT NULL,
    data_doc   DATE,
    texto      TEXT           NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_ti_ksb2_pedido ON ti_ksb2 (pedido);

-- Permissão nas sequências (ver 025): o papel das requisições é fb_apu05_app.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'fb_apu05_app') THEN
        GRANT USAGE, SELECT ON SEQUENCE ti_cargas_sap_id_seq, ti_ksb1_id_seq, ti_ksb2_id_seq TO fb_apu05_app;
    END IF;
END $$;
