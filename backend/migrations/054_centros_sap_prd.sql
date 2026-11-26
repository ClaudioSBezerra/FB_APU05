-- Centros (Plant) lidos do SAP pelo gateway (API em produção desde 26/11/2026): serviço "wk" nas Configurações,
-- carga diária na tabela centros, que passa a guardar também CNPJ, cidade, UF, grupo de contas e empresa.
ALTER TABLE centros ADD COLUMN IF NOT EXISTS cnpj VARCHAR(14);
ALTER TABLE centros ADD COLUMN IF NOT EXISTS cidade VARCHAR(100);
ALTER TABLE centros ADD COLUMN IF NOT EXISTS uf VARCHAR(2);
ALTER TABLE centros ADD COLUMN IF NOT EXISTS grupo_contas VARCHAR(10);
ALTER TABLE centros ADD COLUMN IF NOT EXISTS empresa VARCHAR(10);
INSERT INTO configuracoes (chave, valor) VALUES
    ('sap_wk_ativo',            'false'),
    ('sap_wk_url_base',         'http://10.131.1.80:8000/sap/prd/centros'),
    ('sap_wk_ultima_execucao',  ''),
    ('sap_wk_ultimo_resultado', '')
ON CONFLICT (chave) DO NOTHING;
