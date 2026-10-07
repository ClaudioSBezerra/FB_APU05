-- Story 1.3 — Bootstrap e proteção do administrador (FR-2).
--
-- `usuarios.ativo`: permite desativar um usuário sem apagar o registro
-- (preserva histórico/auditoria). Gerenciado pela mesma rota que concede/
-- revoga perfil `administrador` (PATCH /api/admin/usuarios/{id} —
-- backend/handlers/admin.go), nunca por SQL direto fora dela.
ALTER TABLE usuarios ADD COLUMN IF NOT EXISTS ativo BOOLEAN NOT NULL DEFAULT true;

-- `usuarios_auditoria`: trilha de auditoria de toda mudança de perfil e/ou
-- `ativo` aplicada via PATCH /api/admin/usuarios/{id} — quem mudou (ator_id),
-- em quem (usuario_alvo_id), o que mudou (acao) e os valores antes/depois.
-- Sem UPDATE/DELETE previstos sobre esta tabela a partir da aplicação —
-- registro append-only.
CREATE TABLE IF NOT EXISTS usuarios_auditoria (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ator_id UUID NOT NULL REFERENCES usuarios(id),
    usuario_alvo_id UUID NOT NULL REFERENCES usuarios(id),
    acao VARCHAR(50) NOT NULL CHECK (acao IN ('perfil_alterado', 'ativo_alterado', 'perfil_e_ativo_alterados')),
    perfil_anterior VARCHAR(20),
    perfil_novo VARCHAR(20),
    ativo_anterior BOOLEAN,
    ativo_novo BOOLEAN,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_usuarios_auditoria_usuario_alvo_id ON usuarios_auditoria (usuario_alvo_id);
CREATE INDEX IF NOT EXISTS idx_usuarios_auditoria_ator_id ON usuarios_auditoria (ator_id);
