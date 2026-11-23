-- Papel × Pessoa passa a poder ter VÁRIAS pessoas por papel (ex.: "Diretor": qualquer um decide) e cada
-- pessoa pode apontar para o usuário do sistema (colaborador), que é quem recebe o e-mail de aprovação.
ALTER TABLE papel_pessoa DROP CONSTRAINT IF EXISTS papel_pessoa_papel_key;
ALTER TABLE papel_pessoa ADD COLUMN IF NOT EXISTS colaborador_id UUID REFERENCES usuarios(id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_papel_pessoa_papel_pessoa ON papel_pessoa (LOWER(papel), LOWER(pessoa_nome));
