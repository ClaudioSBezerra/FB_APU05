-- "Plano" das contas deixa de ser BIFC/SFC e passa a ser o PLANO DE CONTAS do SAP (18/11/2026).
-- O plano de contas da Ferreira Costa no S/4HANA é o "0050" (sapodata.PlanoContas); a carga diária das
-- contas (gateway) grava com esse valor. Aqui só se retira a trava que aceitava apenas 'BIFC'/'SFC'.
-- Os dados já existentes são convertidos à parte, por ambiente (BIFC/SFC → 0050), porque os dois
-- planos antigos podiam ter o mesmo código e a chave é (plano, codigo).
ALTER TABLE contas DROP CONSTRAINT IF EXISTS contas_plano_check;
