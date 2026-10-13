-- Recuperação de senha (e-mail com link de redefinição) — companion do
-- login local (migration 016). Mesmo modelo do FB_APU02
-- (migrations/015_create_auth_system.sql + 045_add_used_to_verification_tokens.sql),
-- já com a coluna `used` desde o início (lá foi uma migration separada
-- posterior; aqui nasce junto).
CREATE TABLE IF NOT EXISTS verification_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
    token VARCHAR(255) NOT NULL UNIQUE,
    type VARCHAR(50) NOT NULL CHECK (type IN ('password_reset')),
    used BOOLEAN NOT NULL DEFAULT false,
    expires_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_verification_tokens_token ON verification_tokens (token);
