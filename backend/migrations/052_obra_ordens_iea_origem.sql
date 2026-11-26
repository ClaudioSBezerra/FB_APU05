-- Obras (retorno da homologação 24/11, item 3 e seção 6): ao finalizar com sucesso uma linha "CRIAR", quem atende
-- informa a NOVA ordem de investimento e o NOVO imobilizado (IEA). A ordem entra no cadastro com a origem "chamado"
-- e o vínculo ao chamado; a linha do chamado troca "CRIAR" pelo número e guarda o IEA.
ALTER TABLE obra_ordens ADD COLUMN IF NOT EXISTS imobilizado_iea VARCHAR(20);
ALTER TABLE obra_ordens ADD COLUMN IF NOT EXISTS origem VARCHAR(20) NOT NULL DEFAULT 'carga';
ALTER TABLE obra_ordens ADD COLUMN IF NOT EXISTS solicitacao_id UUID REFERENCES solicitacoes(id);
ALTER TABLE solicitacao_obras_linhas ADD COLUMN IF NOT EXISTS imobilizado_iea VARCHAR(20);
-- a finalização roda pela conexão privilegiada (migration 014 só liberou ordem_investimento)
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'fb_apu05_privilegiado') THEN
        GRANT UPDATE (imobilizado_iea) ON solicitacao_obras_linhas TO fb_apu05_privilegiado;
    END IF;
END $$;
