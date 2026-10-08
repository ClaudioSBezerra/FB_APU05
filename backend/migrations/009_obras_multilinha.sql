-- Story 3.5 — Abrir Obras multi-linha (FR-9/FR-11).
--
-- (a) `solicitacoes`: `centro_custo_id` torna-se nulável (Obras não tem CC,
-- Epic 3 context) e ganha `classificacao` (nível da solicitação). O CHECK
-- abaixo torna o par "obras <-> sem CC, com classificacao" / "demais tipos
-- <-> com CC, sem classificacao" um invariante de SCHEMA, não só de
-- validação em Go — mesmo padrão já estabelecido por
-- `chk_lancamento_conta_xor_classe` (migration 008) para o par
-- conta_id/classe_imobilizado_id.
ALTER TABLE solicitacoes
    ALTER COLUMN centro_custo_id DROP NOT NULL,
    ADD COLUMN classificacao VARCHAR(30)
        CHECK (classificacao IN ('inclusao', 'fl', 'retirada_saldo', 'transferencia_saldo'));

ALTER TABLE solicitacoes
    ADD CONSTRAINT chk_solicitacao_obras_cc_xor_classificacao CHECK (
        (tipo_solicitacao = 'obras' AND centro_custo_id IS NULL AND classificacao IS NOT NULL)
        OR
        (tipo_solicitacao != 'obras' AND centro_custo_id IS NOT NULL AND classificacao IS NULL)
    );

-- (b)/(c) `locais_obra`/`subgrupos_despesa`: cadastros mestres mínimos
-- (código + nome) — "PK não é nome" (02-REQUISITOS-NEGOCIO-ENVIO-TI.md),
-- mesmo padrão de `divisoes`/`classes_imobilizado`. Entram no MESMO registry
-- de cadastros administráveis (handlers/cadastros.go) — nenhuma rota nova.
CREATE TABLE IF NOT EXISTS locais_obra (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    codigo VARCHAR(50) UNIQUE NOT NULL,
    nome VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS subgrupos_despesa (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    codigo VARCHAR(50) UNIQUE NOT NULL,
    nome VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- (d) `obra_ordens`: ordens de investimento já existentes no SAP — cada
-- ordem pertence a exatamente um par (local de obra, subgrupo de despesa).
-- Uma linha de Obras com `ordem_investimento` != "CRIAR" precisa casar com
-- esse par (handlers/solicitacoes.go, Story 3.5); `ordem_investimento` =
-- "CRIAR" nunca gera linha aqui nesta etapa (isso só acontece na
-- finalização, Epic 4 — Boundaries "Never" da spec).
CREATE TABLE IF NOT EXISTS obra_ordens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    numero_ordem VARCHAR(50) UNIQUE NOT NULL,
    local_obra_id UUID NOT NULL REFERENCES locais_obra(id),
    subgrupo_despesa_id UUID NOT NULL REFERENCES subgrupos_despesa(id),
    tipo_despesa VARCHAR(100),
    tipo_faturamento VARCHAR(100),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_obra_ordens_local_subgrupo ON obra_ordens (local_obra_id, subgrupo_despesa_id);

-- (e) `aprovadores_obra`: base de dados do branch tipo-aware de
-- AutorizadorNominal para `obras` (Design Notes da spec 3.5) — teto de
-- alçada INDIVIDUAL por pessoa, sem CC/faixa (Obras não tem CC).
-- `colaborador_id NOT NULL` — sempre pessoa, nunca cargo, mesmo invariante
-- de schema de `autorizadores_formulario` (migration 007). Mesmo registry de
-- cadastros administráveis — nenhuma rota nova.
CREATE TABLE IF NOT EXISTS aprovadores_obra (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    colaborador_id UUID NOT NULL REFERENCES usuarios(id),
    teto NUMERIC(18, 2) NOT NULL,
    ativo BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_aprovadores_obra_colaborador_id ON aprovadores_obra (colaborador_id);

-- (f) `solicitacao_obras_linhas`: tabela de linha PRÓPRIA de Obras — nunca
-- `solicitacao_lancamentos` (Boundaries "Never" da spec: Obras não tem
-- nenhum dos campos que dão nome a "lançamento", divisão/CC/conta/mês).
-- Local de obra + subgrupo de despesa + ordem de investimento (literal
-- "CRIAR" ou `numero_ordem` já existente em `obra_ordens`, checado em Go) +
-- valor. `lado` só é preenchido ("retirada"/"inclusao") quando a
-- solicitação é classificacao="transferencia_saldo" — NULL para as outras 3
-- classificações (Design Notes da spec: rótulos de lado próprios do domínio
-- de Obras, tabela nova, sem CHECK compartilhado com
-- `solicitacao_lancamentos.lado`).
CREATE TABLE IF NOT EXISTS solicitacao_obras_linhas (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    solicitacao_id UUID NOT NULL REFERENCES solicitacoes(id),
    lado VARCHAR(10) CHECK (lado IS NULL OR lado IN ('retirada', 'inclusao')),
    local_obra_id UUID NOT NULL REFERENCES locais_obra(id),
    subgrupo_despesa_id UUID NOT NULL REFERENCES subgrupos_despesa(id),
    ordem_investimento VARCHAR(50) NOT NULL,
    valor NUMERIC(18, 2) NOT NULL CHECK (valor > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_solicitacao_obras_linhas_solicitacao_id ON solicitacao_obras_linhas (solicitacao_id);
