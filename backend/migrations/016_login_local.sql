-- Login local (e-mail+senha) como alternativa ao SSO Keycloak quando este
-- não está configurado (IAM_BASE_URL vazio) — decisão de negócio: contas
-- locais só são criadas por um administrador (POST /api/admin/usuarios),
-- nunca por autocadastro público (diferente do RegisterHandler do FB_APU02).
--
-- `password_hash` é NULL para todo usuário que só existe via auto-
-- provisionamento SSO (Story 1.2/1.3) — LoginHandler trata NULL como "sem
-- senha local definida", mesma mensagem genérica de erro que e-mail
-- inexistente ou senha errada (evita enumeração de contas).
ALTER TABLE usuarios ADD COLUMN IF NOT EXISTS password_hash VARCHAR(255);

-- pgcrypto (extensão "trusted" desde o Postgres 13, não exige superuser)
-- fornece crypt()/gen_salt('bf') — usado só pelo script de bootstrap do
-- primeiro administrador local (backend/scripts/bootstrap_admin_local.sql),
-- para gerar um hash compatível com bcrypt sem depender de nenhuma
-- ferramenta externa. O backend em Go nunca usa pgcrypto — sempre
-- golang.org/x/crypto/bcrypt (ver handlers/auth.go).
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- CreateUsuarioLocalHandler (admin.go) grava uma linha de auditoria com
-- acao='usuario_criado' (perfil_anterior/ativo_anterior ficam NULL — não há
-- "antes" numa criação) — precisa entrar no CHECK que hoje só permite os 3
-- valores da migration 002.
ALTER TABLE usuarios_auditoria DROP CONSTRAINT IF EXISTS usuarios_auditoria_acao_check;
ALTER TABLE usuarios_auditoria ADD CONSTRAINT usuarios_auditoria_acao_check
    CHECK (acao IN ('perfil_alterado', 'ativo_alterado', 'perfil_e_ativo_alterados', 'usuario_criado'));
