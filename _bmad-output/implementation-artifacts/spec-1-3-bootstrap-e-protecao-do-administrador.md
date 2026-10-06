---
title: 'Bootstrap e proteção do administrador'
type: 'feature'
created: 2026-10-06
status: 'ready-for-dev'
route: 'dispatch'
review_loop_iteration: 0
context:
  - '{project-root}/_bmad-output/planning-artifacts/architecture/architecture-FB_APU05-2026-10-06/ARCHITECTURE-SPINE.md'
  - '{project-root}/_bmad-output/implementation-artifacts/epic-1-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** Hoje não existe nenhum jeito de o sistema ter um primeiro administrador, nem de conceder esse perfil a outra pessoa, nem nada que impeça o sistema de ficar sem nenhum admin ativo (FR-2). Também não existe ainda nenhuma rota protegida pelo JWT próprio do FB_APU05 — a `tokenBlacklist` criada na Story 1.2 nunca é lida em lugar nenhum.

**Approach:** Criar o primeiro middleware de "exige login" (JWT próprio + checagem da blacklist, fechando a lacuna deixada pela Story 1.2), um endpoint administrativo para conceder/revogar perfil `administrador` e ativar/desativar um usuário (com trilha de auditoria), e um procedimento de bootstrap documentado e fora da API (script SQL com placeholder, nunca um e-mail real hardcoded) para o primeiro administrador. Toda mudança que reduziria o número de administradores ativos para zero é recusada.

Não existe, no FB_APU02, um padrão formal de bootstrap do primeiro admin (confirmado por investigação: só migrations ad-hoc com e-mail real hardcoded) nem o conceito de "proteger o último admin" — esta é a primeira vez que esse requisito é desenhado, não uma replicação literal.

</frozen-after-approval>

## Code Map

- `/home/claudio/projetos/FB_APU02/backend/handlers/auth.go:214-266` (`AuthMiddleware`) -- forma geral a seguir: um middleware parametrizado por perfil exigido, extrai claims do JWT próprio, injeta no contexto; adaptar para também checar a `tokenBlacklist` (gap que a Story 1.2 deixou) e usar a claim `perfil` (não `role`)
- `/home/claudio/projetos/FB_APU02/backend/handlers/admin.go:539-595` (`PromoteUserHandler`) -- modelo mais próximo pro endpoint de conceder perfil (mas lá não há proteção de "último admin" — isso é novo)
- `/home/claudio/projetos/FB_APU02/backend/migrations/021b_ensure_admin_user.sql` -- **anti-padrão a NÃO replicar**: e-mail real e hash hardcoded direto numa migration versionada. FB_APU05 usa um script separado (fora de `migrations/`) com placeholder, nunca um e-mail real no repositório (dado sensível, repositório público)
- `backend/handlers/auth.go` (desta sessão, Story 1.2) -- `GenerateToken`, `tokenBlacklist`, `jsonErr`, claims `user_id`/`perfil`/`exp`
- `backend/migrations/001_create_usuarios.sql` -- tabela `usuarios` atual (sem coluna `ativo` ainda)

## Tasks & Acceptance

**Execution:**
- [ ] `backend/migrations/002_add_ativo_and_auditoria.sql` -- adiciona `usuarios.ativo BOOLEAN NOT NULL DEFAULT true`; cria `usuarios_auditoria` (id, ator_id, usuario_alvo_id, acao, perfil_anterior, perfil_novo, ativo_anterior, ativo_novo, created_at)
- [ ] `backend/handlers/middleware.go` -- `RequireAuth(next http.HandlerFunc, perfilExigido string) http.HandlerFunc`: valida JWT próprio (mesmo `JWT_SECRET`), checa `tokenBlacklist`, injeta claims no contexto; 401 se inválido/revogado, 403 se `perfilExigido` não bate
- [ ] `backend/handlers/admin.go` -- `PATCH /api/admin/usuarios/{id}` (protegido por `RequireAuth(..., "administrador")`): body `{perfil?: "administrador"|"solicitante", ativo?: bool}`; antes de aplicar uma mudança que reduziria para zero o número de `usuarios` com `perfil='administrador' AND ativo=true`, recusa com 409; toda mudança aplicada grava uma linha em `usuarios_auditoria`
- [ ] `backend/scripts/bootstrap_admin.sql` -- script manual (fora de `migrations/`, nunca roda automaticamente): `UPDATE usuarios SET perfil='administrador' WHERE email = '<SUBSTITUA_PELO_EMAIL_REAL>'` com comentário de instrução; documentar no README a execução via `psql` após o primeiro login SSO da pessoa (ela precisa existir em `usuarios` antes — nasce via auto-provisionamento da Story 1.2)
- [ ] `backend/main.go` -- registrar a rota `PATCH /api/admin/usuarios/{id}` atrás de `RequireAuth`
- [ ] `README.md` -- seção "Primeiro acesso administrativo": login via SSO cria o usuário como `solicitante`; rodar `bootstrap_admin.sql` substituindo o placeholder para promover a pessoa a `administrador`

**Acceptance Criteria:**
- Given um banco recém-criado sem nenhum administrador, when o procedimento de bootstrap (script + instrução do README) é executado após o primeiro login da pessoa, then existe exatamente um administrador ativo
- Given um administrador autenticado, when ele concede perfil `administrador` a outro usuário via `PATCH /api/admin/usuarios/{id}`, then o perfil muda e uma linha é gravada em `usuarios_auditoria`
- Given exatamente um administrador ativo, when alguém tenta rebaixá-lo (`perfil: "solicitante"`) ou desativá-lo (`ativo: false`) via essa rota, then o sistema recusa com 409, sem aplicar a mudança
- Given um usuário com perfil `solicitante` autenticado, when ele chama `PATCH /api/admin/usuarios/{id}`, then recebe 403
- Given um token revogado (blacklist da Story 1.2), when ele é usado em qualquer rota protegida por `RequireAuth`, then recebe 401

## Implementation Notes

## Spec Change Log

## Review Triage Log

## Design Notes

A proteção do "último admin" é avaliada em uma única query antes do UPDATE, dentro da mesma transação, para evitar corrida entre duas requisições concorrentes de desativação (duas requisições lendo "ainda há 2 admins" ao mesmo tempo e ambas desativando, deixando zero) — usar `SELECT ... FOR UPDATE` ou equivalente na linha-alvo e recontagem dentro da transação.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && go test ./...` -- expected: compila sem erro, testes existentes (iam, handlers) continuam passando

**Manual checks (if no CLI):**
- Revisar `bootstrap_admin.sql` para confirmar que não contém nenhum e-mail real, só o placeholder.
