-- Story 5.1 — Visualizar painel consolidado de solicitações (Epic 5, FR-17,
-- Architecture Spine AD-1/AD-14).
--
-- `painel_snapshots`: 1 tabela genérica para os painéis agregados do
-- Epic 5, em vez de 1 tabela por painel/dimensão — `dimensao` distingue os
-- 3 agrupamentos do escopo mínimo desta story ('tipo'/'status'/'cc');
-- `painel` já deixa a tabela extensível a um futuro segundo painel
-- (`GET /api/paineis/{painel}`) sem nova migration de schema (Design Notes
-- da spec). `rotulo` é denormalizado (nunca um JOIN no caminho de leitura,
-- Architecture Spine AD-1: "Handlers de Painéis leem exclusivamente de
-- painel_snapshots") — quem grava o snapshot resolve o nome legível uma
-- única vez na gravação.
--
-- O job/rotina que calcula e grava estas linhas a partir das tabelas
-- transacionais está fora do escopo desta story (PRD §7.3, Questão Aberta
-- 14) — ver Design Notes da spec. Até essa decisão ser tomada, esta tabela
-- nasce e permanece vazia em produção, e o handler de leitura (painel.go)
-- trata isso como estado válido (200, arrays vazios, gerado_em: null),
-- nunca como erro.
CREATE TABLE IF NOT EXISTS painel_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    painel VARCHAR(50) NOT NULL,
    dimensao VARCHAR(20) NOT NULL CHECK (dimensao IN ('tipo', 'status', 'cc')),
    chave VARCHAR(255) NOT NULL,
    rotulo VARCHAR(255) NOT NULL,
    quantidade INTEGER NOT NULL CHECK (quantidade >= 0),
    gerado_em TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (painel, dimensao, chave)
);

CREATE INDEX IF NOT EXISTS idx_painel_snapshots_painel ON painel_snapshots (painel);

-- Sem GRANT explícito aqui: a migration 010 já cobre toda tabela nova para
-- `fb_apu05_app` via `ALTER DEFAULT PRIVILEGES ... GRANT ALL ON TABLES` —
-- ObterPainelHandler roda na conexão GERAL (withDB), só leitura, sem
-- escrita nenhuma nesta tabela nesta story (Code Map da spec).
