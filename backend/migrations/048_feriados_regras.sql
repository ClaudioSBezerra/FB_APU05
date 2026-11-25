-- Regras de feriado (retorno da homologação, item 27): feriados que se repetem todo ano não precisam ser recadastrados.
--   tipo 'fixo'   : dia/mês fixos (ex.: 25/12)
--   tipo 'pascoa' : deslocamento em dias a partir do domingo de Páscoa (ex.: Carnaval terça = -47, Corpus Christi = +60)
-- Valem junto com a tabela `feriados` (datas avulsas, como feriados municipais ou pontes).
CREATE TABLE IF NOT EXISTS feriados_regras (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    descricao VARCHAR(255) NOT NULL,
    tipo VARCHAR(10) NOT NULL CHECK (tipo IN ('fixo', 'pascoa')),
    dia INTEGER NOT NULL DEFAULT 0,
    mes INTEGER NOT NULL DEFAULT 0,
    deslocamento_dias INTEGER NOT NULL DEFAULT 0,
    ativo BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (tipo <> 'fixo' OR (mes BETWEEN 1 AND 12 AND dia BETWEEN 1 AND 31)),
    CHECK (tipo <> 'pascoa' OR deslocamento_dias BETWEEN -60 AND 70)
);

-- Carga inicial: feriados nacionais fixos e os móveis mais comuns. Podem ser desativados ou editados em Cadastros mestres.
INSERT INTO feriados_regras (descricao, tipo, dia, mes, deslocamento_dias)
SELECT v.descricao, v.tipo, v.dia, v.mes, v.desl FROM (VALUES
    ('Confraternização Universal', 'fixo', 1, 1, 0),
    ('Tiradentes', 'fixo', 21, 4, 0),
    ('Dia do Trabalho', 'fixo', 1, 5, 0),
    ('Independência do Brasil', 'fixo', 7, 9, 0),
    ('Nossa Senhora Aparecida', 'fixo', 12, 10, 0),
    ('Finados', 'fixo', 2, 11, 0),
    ('Proclamação da República', 'fixo', 15, 11, 0),
    ('Consciência Negra', 'fixo', 20, 11, 0),
    ('Natal', 'fixo', 25, 12, 0),
    ('Carnaval (segunda)', 'pascoa', 0, 0, -48),
    ('Carnaval (terça)', 'pascoa', 0, 0, -47),
    ('Sexta-feira Santa', 'pascoa', 0, 0, -2),
    ('Corpus Christi', 'pascoa', 0, 0, 60)
) AS v(descricao, tipo, dia, mes, desl)
WHERE NOT EXISTS (SELECT 1 FROM feriados_regras);
