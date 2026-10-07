-- Story 3.1 — Abrir Transferência com aprovação calculada (FR-5/FR-10).
--
-- Base de dados do núcleo de aprovação calculada e da primeira solicitação
-- (`transferencia`): `centros_custo.filial` (coluna nova), `regras_aprovacao`
-- e `gerentes_aprovacao` (2 novos {tipo} do MESMO registry de cadastros
-- administráveis da Story 2.1 — CSV+PUT+histórico de graça, mesmo padrão da
-- Story 2.3 para `cc-excecao`), e `solicitacoes`+`solicitacao_lancamentos`
-- (raiz transacional do Epic 3).

-- `filial`: nenhum artefato de planejamento modela "filial" como cadastro
-- próprio — é sempre citada em par com CC (ver `alcadas.filial`, já
-- free-text desde a Story 2.1). Nullable: como todo cadastro mestre deste
-- produto, a carga real é tarefa do negócio (mesma natureza de
-- `rel_cc_conta`) — ausência de filial apenas garante que nenhuma linha de
-- `alcadas`/`regras_aprovacao` casa para aquele CC, caindo corretamente em
-- "sem alçada cadastrada" (Design Notes da spec).
ALTER TABLE centros_custo ADD COLUMN IF NOT EXISTS filial VARCHAR(50);

-- `regras_aprovacao`: regras especiais curadas (RH/DP/Manutenção/Marketing/
-- Utilidades etc., Epic 3 context) — precedência mais alta que a matriz de
-- alçadas (`alcadas`) quando uma linha casa. `centro_custo_codigo` é texto
-- livre SEM FK, mesmo padrão já estabelecido em `alcadas` (migration 003:
-- "pares filial×CC sem estratégia cadastrada no SAP" não podem ser
-- rejeitados por uma FK rígida). `colaborador_id`/`papel_aprovador` nuláveis
-- — exatamente um dos dois preenchido identifica, respectivamente,
-- Tipo="pessoa" ou Tipo="cargo" do `Aprovador` resolvido (AD-2); validado em
-- Go (cadastros.go), não aqui, pois XOR via CHECK ficaria ilegível para a
-- carga CSV genérica.
-- `precedencia`: menor número = prioridade mais alta entre regras curadas
-- concorrentes (critério de desempate do resolver, internal/aprovacao).
CREATE TABLE IF NOT EXISTS regras_aprovacao (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    precedencia INT NOT NULL,
    filial VARCHAR(50) NOT NULL,
    centro_custo_codigo VARCHAR(50) NOT NULL,
    valor_minimo NUMERIC(18, 2) NOT NULL,
    valor_maximo NUMERIC(18, 2),
    colaborador_id UUID REFERENCES usuarios(id),
    papel_aprovador VARCHAR(255),
    ativo BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_regras_aprovacao_colaborador_id ON regras_aprovacao (colaborador_id);

-- `gerentes_aprovacao`: unifica "janela do gerente do CC" e "fallback GR
-- mais próximo acima" numa MESMA tabela (Design Notes da spec) —
-- `centro_custo_id` preenchido = janela específica do CC; só `divisao_id`
-- preenchido (CC nulo) = fallback por divisão; os dois nulos = fallback
-- global. Subir a hierarquia é só tentar CC -> divisão -> global em ordem no
-- resolver. Mesma semântica pessoa/cargo de `regras_aprovacao` acima
-- (`colaborador_id` XOR `papel_aprovador`, validado em Go) cobre também o
-- caso colegiado (3 gerentes, qualquer um aprova = Tipo="cargo",
-- `colaborador_id` nulo). `teto` NOT NULL com default 20000 (FR-10: janela
-- do gerente vale até R$20.000; escalonamento acima disso é fora do fluxo
-- desta fase).
CREATE TABLE IF NOT EXISTS gerentes_aprovacao (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    centro_custo_id UUID REFERENCES centros_custo(id),
    divisao_id UUID REFERENCES divisoes(id),
    colaborador_id UUID REFERENCES usuarios(id),
    papel_aprovador VARCHAR(255),
    teto NUMERIC(18, 2) NOT NULL DEFAULT 20000,
    ativo BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_gerentes_aprovacao_centro_custo_id ON gerentes_aprovacao (centro_custo_id);
CREATE INDEX IF NOT EXISTS idx_gerentes_aprovacao_divisao_id ON gerentes_aprovacao (divisao_id);
CREATE INDEX IF NOT EXISTS idx_gerentes_aprovacao_colaborador_id ON gerentes_aprovacao (colaborador_id);

-- `solicitacoes`: raiz transacional do Epic 3 — esta story só INSERE
-- (nunca UPDATE) em `aprovador_snapshot`/`versao`; a proteção técnica
-- REVOKE/GRANT do AD-4 (especificamente sobre UPDATE) fica deliberadamente
-- fora desta migration (Boundaries "Never" / Design Notes da spec: não há
-- ainda nenhum caminho de escrita UPDATE para proteger — a primeira story
-- que fizer UPDATE nessas colunas é quem deve introduzir a separação de
-- roles).
-- `tipo_solicitacao`: exatamente os 5 valores do Glossário (Architecture
-- Spine, Consistency Conventions) — só "transferencia" tem handler nesta
-- story (histórias 3.2-3.5 habilitam os demais).
-- `aprovador_snapshot` NOT NULL: toda solicitação gravada já tem um
-- aprovador resolvido — quando o resolver devolve ErrSemAlcadaCadastrada,
-- nada é gravado (I/O Matrix da spec), então nunca existe uma linha "sem
-- aprovador ainda".
CREATE TABLE IF NOT EXISTS solicitacoes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tipo_solicitacao VARCHAR(20) NOT NULL
        CHECK (tipo_solicitacao IN ('transferencia', 'inclusao', 'inclusao_sfc', 'imobilizado', 'obras')),
    solicitante_id UUID NOT NULL REFERENCES usuarios(id),
    centro_custo_id UUID NOT NULL REFERENCES centros_custo(id),
    status VARCHAR(30) NOT NULL DEFAULT 'aberta',
    aprovador_snapshot JSONB NOT NULL,
    versao INT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_solicitacoes_solicitante_id ON solicitacoes (solicitante_id);
CREATE INDEX IF NOT EXISTS idx_solicitacoes_centro_custo_id ON solicitacoes (centro_custo_id);

-- `solicitacao_lancamentos`: linhas de uma solicitação (FR-5: divisão, CC,
-- conta, mês, valor por linha). `mes` é `DATE` por convenção de dia 1 — o
-- "exercício orçamentário" é derivado (`EXTRACT(YEAR FROM mes)`), nunca um
-- campo próprio (Design Notes da spec). `valor > 0` — nunca zero/negativo
-- (mesma regra herdada do Epic 3 context para os demais tipos).
CREATE TABLE IF NOT EXISTS solicitacao_lancamentos (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    solicitacao_id UUID NOT NULL REFERENCES solicitacoes(id),
    lado VARCHAR(10) NOT NULL CHECK (lado IN ('origem', 'destino')),
    divisao_id UUID NOT NULL REFERENCES divisoes(id),
    centro_custo_id UUID NOT NULL REFERENCES centros_custo(id),
    conta_id UUID NOT NULL REFERENCES contas(id),
    mes DATE NOT NULL,
    valor NUMERIC(18, 2) NOT NULL CHECK (valor > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_solicitacao_lancamentos_solicitacao_id ON solicitacao_lancamentos (solicitacao_id);
