-- Resposta da Controladoria (26/11/2026):
--   §2 nota interna (só atendimento e administração escrevem e veem);
--   §3 edição do chamado pela Controladoria, com o que mudou no histórico;
--   §9 visualizadores (pessoas em cópia, já em solicitacao_compartilhamentos) e GRUPOS de pessoas com gestor.
ALTER TABLE solicitacao_comentarios DROP CONSTRAINT IF EXISTS solicitacao_comentarios_tipo_check;
ALTER TABLE solicitacao_comentarios ADD CONSTRAINT solicitacao_comentarios_tipo_check
    CHECK (tipo IN ('pendencia', 'comentario', 'encerramento', 'evento', 'nota_interna', 'edicao'));

CREATE TABLE IF NOT EXISTS grupos (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    nome VARCHAR(120) NOT NULL,
    descricao VARCHAR(500),
    gestor_id UUID NOT NULL REFERENCES usuarios(id),
    ativo BOOLEAN NOT NULL DEFAULT true,
    criado_por UUID NOT NULL REFERENCES usuarios(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_grupos_nome ON grupos (lower(nome));

CREATE TABLE IF NOT EXISTS grupo_membros (
    grupo_id UUID NOT NULL REFERENCES grupos(id) ON DELETE CASCADE,
    usuario_id UUID NOT NULL REFERENCES usuarios(id),
    incluido_por UUID REFERENCES usuarios(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (grupo_id, usuario_id)
);
CREATE INDEX IF NOT EXISTS idx_grupo_membros_usuario ON grupo_membros (usuario_id);

CREATE TABLE IF NOT EXISTS grupo_historico (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    grupo_id UUID NOT NULL REFERENCES grupos(id) ON DELETE CASCADE,
    autor_id UUID REFERENCES usuarios(id),
    texto TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- o grupo é REFERÊNCIA: quem está no grupo hoje acompanha o chamado (não é cópia dos membros)
CREATE TABLE IF NOT EXISTS solicitacao_grupos (
    solicitacao_id UUID NOT NULL REFERENCES solicitacoes(id),
    grupo_id UUID NOT NULL REFERENCES grupos(id),
    incluido_por UUID NOT NULL REFERENCES usuarios(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (solicitacao_id, grupo_id)
);
CREATE INDEX IF NOT EXISTS idx_solicitacao_grupos_grupo ON solicitacao_grupos (grupo_id);

-- edição pela Controladoria: roda pela conexão privilegiada (a geral não faz UPDATE em solicitacoes, migration 010)
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'fb_apu05_privilegiado') THEN
        GRANT UPDATE (descricao, fornecedor, fornecedor_cnpj, fornecedor_nao_cadastrado, tipo_compra, quantidade_material, valor_unitario)
            ON solicitacoes TO fb_apu05_privilegiado;
        GRANT UPDATE (conta_id, mes, valor, centro_custo_id, divisao_id) ON solicitacao_lancamentos TO fb_apu05_privilegiado;
        GRANT UPDATE (valor) ON solicitacao_obras_linhas TO fb_apu05_privilegiado;
        GRANT SELECT ON obra_ordens TO fb_apu05_privilegiado;
        -- avisos e checagem de visualizador nas rotas que rodam pela conexão privilegiada (comentar, pendência, assumir, finalizar)
        GRANT SELECT ON solicitacao_compartilhamentos, solicitacao_grupos, grupos, grupo_membros TO fb_apu05_privilegiado;
    END IF;
END $$;
