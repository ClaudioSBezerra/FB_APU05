-- Fornecedores (20/10) — carga via ERP Bridge (mesmo conector Oracle já
-- usado pela sincronização de colaboradores, handlers/colaboradores_oracle_
-- sync.go), a partir da tabela FORN no servidor FCCORP:
--
--   SELECT f.cgc, f.razsoc, f.fantas, f.uf FROM forn f
--
-- `cgc` (CNPJ) é a chave de negócio — UNIQUE, usada como alvo do upsert em
-- SincronizarFornecedoresOracleHandler. Nunca editado manualmente aqui (ao
-- contrário de centros_custo/contas/etc. em Cadastros Mestres): é um
-- espelho read-only do FORN, resincronizado periodicamente pelo daemon
-- externo — mesmo princípio de usuarios.cc_proprio_id (migration 002/009).
CREATE TABLE IF NOT EXISTS fornecedores (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cgc VARCHAR(20) NOT NULL UNIQUE,
    razsoc VARCHAR(255) NOT NULL,
    fantas VARCHAR(255),
    uf VARCHAR(2),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_fornecedores_razsoc ON fornecedores (razsoc);
CREATE INDEX IF NOT EXISTS idx_fornecedores_fantas ON fornecedores (fantas);
