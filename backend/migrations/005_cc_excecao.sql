-- Story 2.3 — Exceção de centro de custo (FR-4, Epic 2).
--
-- `cc_excecao`: cadastro administrável que autoriza um colaborador a abrir e
-- acompanhar solicitação em CCs ALÉM do próprio (`usuarios.cc_proprio_id`,
-- Story 2.2) — nunca concede aprovação (Boundaries "Never" da spec; o motor
-- de aprovação do Epic 3 nunca consulta esta tabela). Governado pelo MESMO
-- regime de histórico/restauração dos 7 tipos da Story 2.1 (`cadastro_historico`,
-- tipo_cadastro='cc-excecao') — nenhuma tabela de histórico própria.
--
-- `UNIQUE (colaborador_id, centro_custo_id)`: impede exceção duplicada para o
-- mesmo par (I/O Matrix da spec: "Mesmo par duplicado" -> 400 por violação de
-- UNIQUE). Sem ON DELETE (default RESTRICT) nas duas FKs — mesma postura
-- conservadora já adotada em `centros_custo.divisao_id` (migration 003) e
-- `usuarios.cc_proprio_id` (migration 004).
CREATE TABLE IF NOT EXISTS cc_excecao (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    colaborador_id UUID NOT NULL REFERENCES usuarios(id),
    centro_custo_id UUID NOT NULL REFERENCES centros_custo(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (colaborador_id, centro_custo_id)
);

CREATE INDEX IF NOT EXISTS idx_cc_excecao_colaborador_id ON cc_excecao (colaborador_id);
CREATE INDEX IF NOT EXISTS idx_cc_excecao_centro_custo_id ON cc_excecao (centro_custo_id);
