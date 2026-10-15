-- ERP Bridge — credenciais para o daemon externo que sincroniza dados do
-- Oracle/SAP da Ferreira Costa (servidor FCCORP, ex.: VW_COLABORADORES).
-- Réplica, para o FB_APU05 (single-company, AD-7), do mesmo mecanismo já
-- validado em produção no FB_APU02 (migrations 074/079/080/085 de lá):
-- FB_APU05 nunca se conecta ao Oracle diretamente — só armazena as
-- credenciais (cifradas com AES-256-GCM, backend/crypto) e as serve ao
-- daemon via API key (GET /api/erp-bridge/credentials).
--
-- Singleton (nunca multi-tenant aqui, ao contrário do FB_APU02): PK fixa
-- `id boolean DEFAULT true CHECK (id)` garante no máximo 1 linha na tabela
-- inteira, sem precisar de company_id.
CREATE TABLE IF NOT EXISTS erp_bridge_config (
    id              BOOLEAN PRIMARY KEY DEFAULT true CHECK (id),
    erp_type        VARCHAR(20) NOT NULL DEFAULT 'sap_s4hana' CHECK (erp_type IN ('oracle_xml', 'sap_s4hana')),
    oracle_dsn      TEXT,
    fbtax_email     TEXT,
    fbtax_password  TEXT, -- AES-256-GCM cifrado (backend/crypto)
    oracle_usuario  TEXT, -- AES-256-GCM cifrado
    oracle_senha    TEXT, -- AES-256-GCM cifrado
    api_key         TEXT, -- AES-256-GCM cifrado (só para reexibir no painel)
    api_key_hash    TEXT, -- SHA-256 em hex do api_key em claro, para lookup rápido
    daemon_last_seen TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_erp_bridge_config_api_key_hash
    ON erp_bridge_config (api_key_hash)
    WHERE api_key_hash IS NOT NULL;
