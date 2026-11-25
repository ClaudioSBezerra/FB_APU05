-- Retorno da homologação, itens 7 e 8.
-- (7) Obras (retirada e transferência de saldo): ano do orçamento = ano anterior ou atual, guardado na solicitação.
ALTER TABLE solicitacoes ADD COLUMN IF NOT EXISTS ano_orcamento INTEGER;

-- (8) Imobilizado passa a exigir o mês de competência: a linha de classe de imobilizado agora pode ter `mes`.
-- (Chamados antigos de Imobilizado continuam sem mês; a obrigatoriedade para os novos é validada na aplicação.)
ALTER TABLE solicitacao_lancamentos DROP CONSTRAINT IF EXISTS chk_lancamento_conta_xor_classe;
ALTER TABLE solicitacao_lancamentos ADD CONSTRAINT chk_lancamento_conta_xor_classe CHECK (
    (conta_id IS NOT NULL AND mes IS NOT NULL AND classe_imobilizado_id IS NULL)
    OR
    (conta_id IS NULL AND classe_imobilizado_id IS NOT NULL)
);
