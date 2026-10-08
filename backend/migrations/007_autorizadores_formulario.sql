-- Story 3.3 — Abrir Inclusão com autorizador nominal (FR-6/FR-11).
--
-- `autorizadores_formulario`: base de dados da 2ª implementação de Resolver
-- (AD-2), AutorizadorNominal — lista fixa de autorizadores por CC/faixa de
-- valor. SEMPRE pessoa (`colaborador_id UUID NOT NULL`), nunca
-- `papel_aprovador`/cargo — diferente de `alcadas`/`regras_aprovacao`
-- (migrations 003/006): FR-6/FR-11 só descrevem "pessoa escolhida pelo
-- solicitante... validada contra lista fixa de autorizadores", sem noção de
-- cargo colegiado sem titular para este motor (Design Notes da spec).
-- `colaborador_id NOT NULL` torna essa regra um invariante de SCHEMA, não só
-- de validação em Go.
--
-- Novo {tipo} ("autorizadores-formulario") no MESMO registry de cadastros
-- administráveis da Story 2.1/3.1 (CSV+PUT+histórico de graça, mesmo padrão
-- de `regras-aprovacao`/`gerentes-aprovacao`) — nenhuma rota nova.
--
-- `centro_custo_codigo` é texto livre SEM FK, mesmo padrão já estabelecido
-- em `alcadas`/`regras_aprovacao` (pares filial×CC sem estratégia cadastrada
-- no SAP não podem ser rejeitados por uma FK rígida). `valor_maximo` nulo =
-- sem teto, mesmo padrão de `alcadas`/`regras_aprovacao`.
--
-- Sem `filial` (Design Notes da spec): diferente de `alcadas`/
-- `regras_aprovacao` (filial+CC+valor), nenhum artefato de planejamento
-- (FR-6/FR-11, AC da Story 3.3) menciona filial para autorizador nominal.
CREATE TABLE IF NOT EXISTS autorizadores_formulario (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    centro_custo_codigo VARCHAR(50) NOT NULL,
    valor_minimo NUMERIC(18, 2) NOT NULL,
    valor_maximo NUMERIC(18, 2),
    colaborador_id UUID NOT NULL REFERENCES usuarios(id),
    ativo BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Índice composto cobrindo a consulta real de AutorizadorNominal.Resolve
-- (ativo + centro_custo_codigo + colaborador_id, mais a faixa de valor) —
-- mesmo fix já aplicado a `regras_aprovacao` na Story 3.1 para a lacuna
-- idêntica (2 índices de coluna única não cobrem a busca combinada).
CREATE INDEX IF NOT EXISTS idx_autorizadores_formulario_busca ON autorizadores_formulario (centro_custo_codigo, colaborador_id);
