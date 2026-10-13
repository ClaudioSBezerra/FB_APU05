-- Bootstrap do primeiro administrador LOCAL (login e-mail+senha, sem SSO
-- Keycloak configurado) — Story "Login local" (companion de
-- backend/scripts/bootstrap_admin.sql, que promove quem já existe via SSO).
--
-- Diferença-chave: sem SSO não existe auto-provisionamento (auth_sso.go
-- nunca roda), então este script CRIA o usuário — não só promove um já
-- existente — com perfil='administrador' direto.
--
-- Script MANUAL — fica fora de backend/migrations/ de propósito: nunca é
-- lido pelo runner de migrations em main.go, nunca roda sozinho no boot, e
-- nunca deve ser commitado com e-mail/senha reais no lugar dos placeholders
-- abaixo (mesmo anti-padrão documentado em bootstrap_admin.sql — NÃO
-- repetir o que o FB_APU02 fez em 021b_ensure_admin_user.sql).
--
-- Pré-requisito: migration 016_login_local.sql já aplicada (cria
-- usuarios.password_hash e a extensão pgcrypto, usada só aqui para gerar um
-- hash compatível com bcrypt sem depender de nenhuma ferramenta externa).
--
-- Uso:
--   1. Edite os 3 placeholders abaixo (e-mail, nome, senha).
--   2. Rode: psql "$DATABASE_URL" -f backend/scripts/bootstrap_admin_local.sql
--   3. Confira a linha devolvida por RETURNING — "INSERT 0" (zero linhas)
--      significa que o e-mail já existia (ON CONFLICT DO NOTHING) e nada foi
--      criado; não é um 500 silencioso, mas vale checar antes de assumir que
--      deu certo.
--   4. Reverta a edição do passo 1 antes de dar `git commit` — este arquivo
--      deve voltar aos placeholders no repositório.
--   5. Faça login em /login com o e-mail e a senha reais, e troque a senha
--      por um canal seguro depois (não existe endpoint de "trocar a própria
--      senha" ainda — fora de escopo; promoção/gestão de outros usuários
--      segue via PATCH /api/admin/usuarios/{id} e POST /api/admin/usuarios).

INSERT INTO usuarios (email, nome, perfil, ativo, password_hash)
VALUES (
    '<SUBSTITUA_PELO_EMAIL_REAL>',
    '<SUBSTITUA_PELO_NOME_REAL>',
    'administrador',
    true,
    crypt('<SUBSTITUA_PELA_SENHA_REAL>', gen_salt('bf', 14))
)
ON CONFLICT (LOWER(email)) DO NOTHING
RETURNING id, email, nome, perfil, ativo;
