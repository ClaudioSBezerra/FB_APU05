-- Story 2.1 — Carga inicial e cadastros administráveis (FR-16, Epic 2).
--
-- Cria os 7 cadastros mestres do épico (divisões, centros de custo, contas,
-- classes de imobilizado, alçadas, papel×pessoa, feriados de SLA) mais a
-- tabela genérica `cadastro_historico` que dá a todos eles o MESMO regime de
-- histórico/restauração (ver Intent da spec — nenhum tipo pode divergir).
--
-- PK UUID (`gen_random_uuid()`) + `created_at`/`updated_at` em toda tabela,
-- mesmo padrão já estabelecido em `usuarios` (migration 001).

CREATE TABLE IF NOT EXISTS divisoes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    codigo VARCHAR(50) NOT NULL UNIQUE,
    nome VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- `divisao_id` é resolvido a partir de `divisao_codigo` no CSV (ver
-- cadastros_csv.go) — a carga de centros_custo só funciona depois que
-- divisoes já estiver populada (Design Notes da spec); não é imposto aqui
-- por constraint de ordem de migration, só pela FK em si (sem FK válida, a
-- resolução de código falha e a linha do CSV é rejeitada).
CREATE TABLE IF NOT EXISTS centros_custo (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    codigo VARCHAR(50) NOT NULL UNIQUE,
    nome VARCHAR(255) NOT NULL,
    divisao_id UUID NOT NULL REFERENCES divisoes(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_centros_custo_divisao_id ON centros_custo (divisao_id);

-- `UNIQUE (plano, codigo)`: interpretação adotada para "PK composta
-- (plano,codigo)" do Design Notes da spec — o identificador surrogate
-- continua sendo `id UUID` (convenção de toda tabela do sistema, ver
-- ARCHITECTURE-SPINE "PK é UUID"; também é o que `cadastro_historico.
-- registro_id` referencia de forma genérica para os 7 tipos), e a dupla
-- (plano,codigo) vira a chave de negócio garantida por constraint UNIQUE —
-- não uma troca da PK para colunas compostas, o que quebraria o endereçamento
-- genérico por {id} usado pelas 5 rotas (PUT/histórico/restaurar).
CREATE TABLE IF NOT EXISTS contas (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plano VARCHAR(10) NOT NULL CHECK (plano IN ('BIFC', 'SFC')),
    codigo VARCHAR(50) NOT NULL,
    nome VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (plano, codigo)
);

CREATE TABLE IF NOT EXISTS classes_imobilizado (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    codigo VARCHAR(50) NOT NULL UNIQUE,
    nome VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- `centro_custo_codigo`/`papel_aprovador` são texto livre, sem FK (Design
-- Notes da spec: o pacote de origem documenta pares filial×CC e papéis sem
-- estratégia cadastrada no SAP — uma FK aqui rejeitaria carga legítima).
-- `valor_maximo` nullable = "sem teto".
CREATE TABLE IF NOT EXISTS alcadas (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    filial VARCHAR(50) NOT NULL,
    centro_custo_codigo VARCHAR(50) NOT NULL,
    valor_minimo NUMERIC(18, 2) NOT NULL,
    valor_maximo NUMERIC(18, 2),
    papel_aprovador VARCHAR(255) NOT NULL,
    ativo BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Cadastro versionado apenas — o motor de resolução de aprovador (Epic 3)
-- ainda não consome esta tabela (Boundaries da spec: "Never").
CREATE TABLE IF NOT EXISTS papel_pessoa (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    papel VARCHAR(100) NOT NULL UNIQUE,
    pessoa_nome VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS feriados (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    data DATE NOT NULL UNIQUE,
    descricao VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- `cadastro_historico` (genérica, append-only) — ver Design Notes da spec.
-- Um único regime de histórico/restauração para os 7 tipos acima: nenhum
-- deles grava sua própria tabela de histórico.
CREATE TABLE IF NOT EXISTS cadastro_historico (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tipo_cadastro VARCHAR(30) NOT NULL,
    registro_id UUID NOT NULL,
    versao INT NOT NULL,
    acao VARCHAR(20) NOT NULL CHECK (acao IN ('criado', 'atualizado', 'restaurado')),
    dados JSONB NOT NULL,
    ator_id UUID REFERENCES usuarios(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cadastro_historico_tipo_registro_versao
    ON cadastro_historico (tipo_cadastro, registro_id, versao);
