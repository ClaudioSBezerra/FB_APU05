-- Módulo "Controle TI" (16/11/2026): controle das despesas de TI — orçado,
-- realizado, a lançar e projeção — que até 2026 roda em planilha
-- (FC FY27 Forecast.xlsx) + painel HTML. Ver docs/referencia/04-CONTROLE-TI.md.
--
-- Acesso: `usuarios.controle_ti`, interruptor no mesmo molde de `aprovador`
-- (migration 020), ligado em Gestão de Usuários. Sem o interruptor o usuário
-- não vê o menu e as rotas /api/ti/* respondem 403.
ALTER TABLE usuarios ADD COLUMN IF NOT EXISTS controle_ti BOOLEAN NOT NULL DEFAULT false;

-- Parâmetros próprios do módulo (espelham a aba Parâmetros da planilha).
-- Não reaproveitam divisoes/centros_custo/contas do cadastro mestre de
-- propósito: "divisão" aqui é o código da filial SAP (1002, 4016...) e o CC
-- tem 4 dígitos (6150), enquanto no cadastro mestre divisão é um conceito
-- interno e o CC vem do S/4HANA. Ligar os dois fica para quando a carga SAP
-- (Duda) estabilizar.
CREATE TABLE IF NOT EXISTS ti_param_cc (
    cc          VARCHAR(20)  PRIMARY KEY,
    descricao   VARCHAR(255) NOT NULL DEFAULT '',
    responsavel VARCHAR(255) NOT NULL DEFAULT '',
    pasta       VARCHAR(255) NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS ti_param_conta (
    conta     VARCHAR(20)  PRIMARY KEY,
    descricao VARCHAR(255) NOT NULL DEFAULT '',
    tipo      VARCHAR(100) NOT NULL DEFAULT ''
);

-- Status do lançamento -> classificação (Realizado | Comprometido | ...).
CREATE TABLE IF NOT EXISTS ti_param_status (
    status        VARCHAR(50) PRIMARY KEY,
    classificacao VARCHAR(50) NOT NULL
);

CREATE TABLE IF NOT EXISTS ti_param_divisao (
    divisao   VARCHAR(20)  PRIMARY KEY,
    descricao VARCHAR(255) NOT NULL DEFAULT '',
    empresa   VARCHAR(100) NOT NULL DEFAULT '',
    pasta     VARCHAR(255) NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS ti_param_fornecedor (
    fornecedor VARCHAR(255) PRIMARY KEY,
    pasta      VARCHAR(255) NOT NULL DEFAULT ''
);

-- Orçamento: uma linha por divisão/CC/conta/fornecedor e um valor por mês.
-- `fonte` identifica de onde veio (hoje 'forecast_xlsx'); reimportar a mesma
-- fonte substitui as linhas dela e nunca toca nas de outra fonte.
CREATE TABLE IF NOT EXISTS ti_orcamento_linhas (
    id         BIGSERIAL    PRIMARY KEY,
    fonte      VARCHAR(30)  NOT NULL,
    ref        VARCHAR(50)  NOT NULL DEFAULT '',
    origem     VARCHAR(50)  NOT NULL DEFAULT '',
    divisao    VARCHAR(20)  NOT NULL DEFAULT '',
    cc         VARCHAR(20)  NOT NULL DEFAULT '',
    conta      VARCHAR(20)  NOT NULL DEFAULT '',
    fornecedor VARCHAR(255) NOT NULL DEFAULT '',
    descricao  TEXT         NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_ti_orcamento_linhas_fonte ON ti_orcamento_linhas (fonte);

CREATE TABLE IF NOT EXISTS ti_orcamento_valores (
    linha_id BIGINT         NOT NULL REFERENCES ti_orcamento_linhas(id) ON DELETE CASCADE,
    mes      DATE           NOT NULL,
    valor    NUMERIC(18, 2) NOT NULL,
    PRIMARY KEY (linha_id, mes)
);

-- Lançamentos (uma linha por nota). `fonte`: 'planilha' (importada),
-- 'manual' (digitada no sistema) ou 'sap_api' (vinda do SAP). Reimportar a
-- planilha substitui só as linhas de fonte 'planilha'. Os campos derivados
-- (divisão, CC, descrições, tipo, classificação) NÃO são gravados: saem de
-- cc_sap, conta e status + tabelas ti_param_*, como na planilha.
CREATE TABLE IF NOT EXISTS ti_lancamentos (
    id          BIGSERIAL      PRIMARY KEY,
    fonte       VARCHAR(20)    NOT NULL DEFAULT 'planilha',
    ref         VARCHAR(50)    NOT NULL DEFAULT '',
    origem      VARCHAR(100)   NOT NULL DEFAULT '',
    status      VARCHAR(50)    NOT NULL DEFAULT '',
    pedido      VARCHAR(30)    NOT NULL DEFAULT '',
    folha       VARCHAR(30)    NOT NULL DEFAULT '',
    miro        VARCHAR(30)    NOT NULL DEFAULT '',
    nf          VARCHAR(50)    NOT NULL DEFAULT '',
    competencia DATE,
    vencimento  DATE,
    valor       NUMERIC(18, 2) NOT NULL,
    fornecedor  VARCHAR(255)   NOT NULL DEFAULT '',
    descricao   TEXT           NOT NULL DEFAULT '',
    cc_sap      VARCHAR(20)    NOT NULL DEFAULT '',
    conta       VARCHAR(20)    NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ    NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ    NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_ti_lancamentos_competencia ON ti_lancamentos (competencia);
CREATE INDEX IF NOT EXISTS idx_ti_lancamentos_fonte ON ti_lancamentos (fonte);
CREATE INDEX IF NOT EXISTS idx_ti_lancamentos_pedido ON ti_lancamentos (pedido) WHERE pedido <> '';

-- Configuração do módulo. `ultimo_mes_fechado` (AAAA-MM) é opcional: sem ele
-- o mês fecha sozinho — do dia 6 em diante o mês anterior está fechado.
CREATE TABLE IF NOT EXISTS ti_config (
    chave VARCHAR(50)  PRIMARY KEY,
    valor VARCHAR(255) NOT NULL
);

-- Histórico das importações de planilha (quem, quando, o que entrou).
CREATE TABLE IF NOT EXISTS ti_importacoes (
    id         BIGSERIAL    PRIMARY KEY,
    usuario_id UUID         REFERENCES usuarios(id),
    arquivo    VARCHAR(255) NOT NULL DEFAULT '',
    resumo     JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Permissão nas SEQUÊNCIAS das tabelas acima (BIGSERIAL). A migration 010
-- concede ao papel fb_apu05_app (o das requisições) só ALL nas tabelas — as
-- demais tabelas do projeto usam UUID e nunca precisaram de uma sequência.
-- Sem isto, INSERT em ti_* falha com "permission denied for sequence".
-- O ALTER DEFAULT PRIVILEGES cobre sequências de migrations futuras.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'fb_apu05_app') THEN
        GRANT USAGE, SELECT ON SEQUENCE ti_orcamento_linhas_id_seq, ti_lancamentos_id_seq, ti_importacoes_id_seq TO fb_apu05_app;
        ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO fb_apu05_app;
    END IF;
END $$;
