-- Story 3.4 — Abrir Imobilizado com anexo de cotação (FR-8/FR-11, AD-13).
--
-- (a) `solicitacao_lancamentos` ganha `classe_imobilizado_id` (Imobilizado
-- usa classe de imobilizado, não conta contábil/mês de competência — Epic 3
-- context). `conta_id`/`mes` deixam de ser NOT NULL (continuam obrigatórios
-- para os demais 4 tipos, mas nunca preenchidos para Imobilizado) e o CHECK
-- XOR abaixo torna essa exclusividade um invariante de SCHEMA, não só de
-- validação em Go (mesmo princípio de `colaborador_id NOT NULL` em
-- `autorizadores_formulario`, migration 007): exatamente um dos dois pares
-- (conta_id+mes) XOR (classe_imobilizado_id) é preenchido por linha.
ALTER TABLE solicitacao_lancamentos
    ALTER COLUMN conta_id DROP NOT NULL,
    ALTER COLUMN mes DROP NOT NULL,
    ADD COLUMN classe_imobilizado_id UUID REFERENCES classes_imobilizado(id);

ALTER TABLE solicitacao_lancamentos
    ADD CONSTRAINT chk_lancamento_conta_xor_classe CHECK (
        (conta_id IS NOT NULL AND mes IS NOT NULL AND classe_imobilizado_id IS NULL)
        OR
        (conta_id IS NULL AND mes IS NULL AND classe_imobilizado_id IS NOT NULL)
    );

-- (b) `solicitacao_anexos`: metadado + hash SHA-256 + bytes em BYTEA (AD-13
-- -- mecanismo base de storage é o Postgres já existente, nenhuma
-- infraestrutura nova de disco/S3/bucket). `hash_sha256` e `tamanho_bytes`
-- são sempre calculados pelo módulo `internal/anexos` (Salvar), nunca
-- recebidos do cliente -- garante que o hash gravado corresponde de fato ao
-- conteúdo decodificado, não a um valor que o solicitante poderia forjar.
CREATE TABLE IF NOT EXISTS solicitacao_anexos (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    solicitacao_id UUID NOT NULL REFERENCES solicitacoes(id),
    nome_arquivo VARCHAR(255) NOT NULL,
    content_type VARCHAR(100) NOT NULL DEFAULT '',
    tamanho_bytes INT NOT NULL,
    hash_sha256 VARCHAR(64) NOT NULL,
    dados BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_solicitacao_anexos_solicitacao_id ON solicitacao_anexos (solicitacao_id);
