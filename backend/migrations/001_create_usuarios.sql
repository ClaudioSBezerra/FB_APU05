-- Primeira migration real do projeto (Story 1.2 — Login via SSO corporativo).
--
-- Tabela `usuarios`: perfil (`solicitante`/`administrador`) é uma coluna gerenciada
-- pela própria aplicação (AD-7, corrigido em 2026-10-06) — NUNCA derivada de
-- role/claim do Keycloak. Identidade (quem é o usuário) vem do Keycloak (e-mail
-- verificado); autorização (o que ele pode fazer) vem desta tabela.
CREATE TABLE IF NOT EXISTS usuarios (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) NOT NULL,
    perfil VARCHAR(20) NOT NULL DEFAULT 'solicitante' CHECK (perfil IN ('solicitante', 'administrador')),
    nome VARCHAR(255) NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- Unicidade e busca por e-mail (fetchUserByEmail) são ambas case-insensitive
-- (LOWER dos dois lados) — o e-mail vem de uma claim de um IdP externo
-- (Keycloak), não de digitação consistente do próprio usuário. Um UNIQUE
-- simples em `email` (case-sensitive) permitiria "Foo@x.com" e "foo@x.com"
-- coexistirem como duas pessoas diferentes; o índice único funcional em
-- LOWER(email) fecha essa brecha e também acelera a busca.
CREATE UNIQUE INDEX IF NOT EXISTS idx_usuarios_email_lower ON usuarios (LOWER(email));
