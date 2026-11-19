-- Controle TI: estrutura de 2027 (19/11/2026) — "Despesas TI AAAA.xlsx" (aba Lançamentos, com BP e
-- CNPJ/CPF) e "Orçamento TI AAAA.xlsx" (aba Orçamento), um arquivo por ANO. Reimportar um ano
-- substitui só esse ano: 2026 fica como histórico quando entra 2027.
ALTER TABLE ti_lancamentos ADD COLUMN IF NOT EXISTS ano SMALLINT;
UPDATE ti_lancamentos SET ano = COALESCE(EXTRACT(YEAR FROM competencia), 2026)::smallint WHERE ano IS NULL;
CREATE INDEX IF NOT EXISTS idx_ti_lancamentos_ano ON ti_lancamentos (fonte, ano);

ALTER TABLE ti_orcamento_linhas ADD COLUMN IF NOT EXISTS ano SMALLINT;

-- KSB1: data/hora de ENTRADA do documento no SAP (opcional: o layout antigo não traz). Com ela o
-- mês de um lançamento segue a regra do fechamento do dia 6 (ver sap_oficial.go).
ALTER TABLE ti_ksb1 ADD COLUMN IF NOT EXISTS data_entrada DATE;

-- Fechamento automático do dia 6 (um registro por mês fechado).
CREATE TABLE IF NOT EXISTS ti_fechamentos (
    mes             CHAR(7)      PRIMARY KEY,            -- AAAA-MM do mês que fecha
    status          VARCHAR(30)  NOT NULL,               -- aguardando_planilha | fechado
    foto_em         TIMESTAMPTZ  NOT NULL DEFAULT now(), -- quando o job rodou (00:30 do dia 6)
    fechado_em      TIMESTAMPTZ,
    planilha_em     TIMESTAMPTZ,                         -- planilha importada depois da foto
    resumo          JSONB        NOT NULL DEFAULT '{}'::jsonb,
    aviso_enviado   BOOLEAN      NOT NULL DEFAULT false,
    lembrete_enviado BOOLEAN     NOT NULL DEFAULT false
);

-- Gestores: só leem e só veem os próprios CCs (e/ou grupos de empresa). ADM continua sendo
-- usuarios.controle_ti.
CREATE TABLE IF NOT EXISTS ti_gestor_escopo (
    usuario_id UUID        NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    tipo       VARCHAR(10) NOT NULL CHECK (tipo IN ('cc', 'grupo')),
    valor      VARCHAR(20) NOT NULL,
    PRIMARY KEY (usuario_id, tipo, valor)
);

-- T_FornBP (aba Parâmetros): CNPJ/CPF -> BP(s). Um CNPJ pode ter mais de um BP; o cruzamento de
-- pagamentos com a FBL1N tenta todos.
CREATE TABLE IF NOT EXISTS ti_param_fornbp (
    cnpj VARCHAR(20) NOT NULL,   -- só dígitos
    bp   VARCHAR(20) NOT NULL,
    PRIMARY KEY (cnpj, bp)
);
