-- Correção da 033: o ano dos lançamentos que já existiam (planilha FC FY27 Forecast de 2026) foi derivado da
-- competência, e as notas de dez/2025 viraram "2025". O ano de um lançamento é o da PLANILHA (Despesas TI AAAA),
-- não o da competência: reimportar a Despesas TI 2026 apaga o ano 2026 inteiro.
UPDATE ti_lancamentos SET ano = 2026 WHERE fonte = 'planilha' AND ano < 2026;
