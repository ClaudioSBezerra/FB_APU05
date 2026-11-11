-- Pedidos de melhoria de 11/11/2026 (lista do negócio, itens 4 e 8).
--
-- Item 4: a consulta ao cadastro de fornecedores passa a trazer o CNPJ junto
-- com a razão social, e o solicitante pode marcar "fornecedor não
-- cadastrado" quando o fornecedor ainda não existe no cadastro (o campo
-- continua texto livre). Ambos no nível da solicitação, como `fornecedor`
-- (migration 021).
ALTER TABLE solicitacoes ADD COLUMN IF NOT EXISTS fornecedor_cnpj VARCHAR(20);
ALTER TABLE solicitacoes ADD COLUMN IF NOT EXISTS fornecedor_nao_cadastrado BOOLEAN NOT NULL DEFAULT false;

-- Item 8: Imobilizado informa a quantidade do material. Imobilizado tem
-- sempre exatamente 1 linha (validarContagemELadoPorTipo), então a
-- quantidade fica na solicitação; NULL para os demais tipos.
ALTER TABLE solicitacoes ADD COLUMN IF NOT EXISTS quantidade_material INTEGER;
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_solicitacoes_quantidade_material_positiva') THEN
        ALTER TABLE solicitacoes
            ADD CONSTRAINT chk_solicitacoes_quantidade_material_positiva
            CHECK (quantidade_material IS NULL OR quantidade_material > 0);
    END IF;
END $$;
