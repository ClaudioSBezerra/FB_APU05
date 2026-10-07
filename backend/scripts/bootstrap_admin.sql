-- Bootstrap do primeiro administrador (Story 1.3, FR-2).
--
-- Script MANUAL — fica fora de backend/migrations/ de propósito: nunca é
-- lido pelo runner de migrations em main.go (que só varre
-- backend/migrations/*.sql), nunca roda sozinho no boot, e nunca deve ser
-- commitado com um e-mail real preenchido no lugar do placeholder abaixo.
--
-- Anti-padrão documentado no Code Map desta spec, NÃO replicado aqui:
-- FB_APU02/backend/migrations/021b_ensure_admin_user.sql grava um e-mail real
-- e um hash de senha direto numa migration versionada — isso nunca deve se
-- repetir neste repositório (é público).
--
-- Pré-requisito: a pessoa já precisa existir na tabela `usuarios` — ela nasce
-- lá via auto-provisionamento no primeiro login SSO (Story 1.2), sempre com
-- perfil 'solicitante' e ativo=true. Este script apenas PROMOVE quem já
-- existe; ele não cria usuário.
--
-- Uso:
--   1. Peça para a pessoa fazer login uma vez via SSO corporativo (cria o
--      registro em `usuarios`).
--   2. Edite a linha abaixo, substituindo o placeholder pelo e-mail real da
--      pessoa.
--   3. Rode: psql "$DATABASE_URL" -f backend/scripts/bootstrap_admin.sql
--   4. Reverta a edição do passo 2 antes de dar `git commit` — este arquivo
--      deve voltar ao placeholder no repositório.

UPDATE usuarios
SET perfil = 'administrador', ativo = true
WHERE email = '<SUBSTITUA_PELO_EMAIL_REAL>'
RETURNING id, email, perfil, ativo;
