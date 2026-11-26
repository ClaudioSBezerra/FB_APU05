-- Registro de acessos (pedido do usuário, 26/11/2026): último acesso de cada usuário e o histórico de logins
-- (quando, por onde — senha ou login corporativo — e de qual IP). Começa a valer a partir desta versão.
ALTER TABLE usuarios ADD COLUMN IF NOT EXISTS ultimo_acesso TIMESTAMPTZ;
CREATE TABLE IF NOT EXISTS usuarios_acessos (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    usuario_id UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    quando TIMESTAMPTZ NOT NULL DEFAULT now(),
    via VARCHAR(10) NOT NULL CHECK (via IN ('senha', 'sso')),
    ip VARCHAR(64)
);
CREATE INDEX IF NOT EXISTS idx_usuarios_acessos_usuario ON usuarios_acessos (usuario_id, quando DESC);
