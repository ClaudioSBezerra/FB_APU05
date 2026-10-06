# Epic 1 Context: Autenticação e Acesso

<!-- Compiled from planning artifacts. Edit freely. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Este épico estabelece a base rodável do projeto e garante que qualquer usuário da empresa entre no sistema com sua conta corporativa (SSO), sem cadastro de usuário separado, enquanto protege a operação contra ficar sem nenhum administrador ativo. É o alicerce de todos os demais épicos: nenhuma solicitação, cadastro ou painel pode existir sem login funcionando e sem ao menos um administrador seguro.

## Stories

- Story 1.1: Scaffold inicial do projeto
- Story 1.2: Login via SSO corporativo
- Story 1.3: Bootstrap e proteção do administrador

## Requirements & Constraints

- Login via OIDC/PKCE contra o Keycloak corporativo real, sem cadastro de usuário separado (dois perfis de login possíveis: `solicitante` e `administrador`; aprovador é papel calculado, nunca perfil de login).
- Token do Keycloak deve ser validado via JWKS; checagem de `email_verified` é obrigatória antes de emitir token próprio — sem essa checagem, troca de e-mail no Keycloak permitiria logar como outra pessoa.
- Token do Keycloak nunca é exposto a nenhuma camada além do endpoint de troca; o sistema emite seus próprios tokens (JWT access 30min + refresh 7 dias em cookie HttpOnly).
- Perfil e ator sempre derivam da sessão autenticada, nunca de payload enviado pelo cliente.
- O primeiro administrador é provisionado diretamente no banco, por procedimento controlado fora da API.
- Concessão posterior de perfil administrador exige um administrador já autenticado e gera evento auditável.
- O sistema recusa qualquer tentativa de rebaixar ou desativar o último administrador ativo.
- Projeto deve ser rodável localmente via Docker (backend Go + frontend React + Postgres), com runner de migrations (`schema_migrations`) funcionando vazio sem erro.
- Dado sensível: nenhum exemplo/fixture pode usar nome real, matrícula ou e-mail de pessoa (repositório é público).

## Technical Decisions

- **Autenticação (AD-6):** replica exatamente o fluxo já validado em produção no sistema irmão FB_APU02 — mesmo realm Keycloak, mesma validação JWKS, mesma checagem de `email_verified`. Não reinventar o fluxo.
- **Autorização single-company (AD-7):** sem `company_id`/multitenancy. Middleware de perfil deriva exclusivamente de roles de client Keycloak via `resource_access.<client>.roles` — nunca de grupo ou claim customizada. A validação de "não rebaixar/desativar o último admin" deve viver no mesmo módulo que concede perfil administrador, nunca só na UI.
- **Stack herdado (AD-9):** versões idênticas ao FB_APU02 (Go 1.22 — débito técnico conhecido e compartilhado; golang-jwt v5.3.1; bcrypt custo 14). Inclui os middlewares de segurança HTTP já validados: `SecurityMiddleware` global (HSTS, CSP, X-Frame-Options DENY, nosniff, Referrer-Policy, Permissions-Policy), rate limiting por IP em endpoints sensíveis, CORS por whitelist, emissão/rotação de JWT próprio com blacklist no logout.
- **Estrutura de pastas:** herdada do FB_APU02 — `backend/{main.go, internal/{aprovacao,exportacao}, handlers, services, middleware, migrations}`; `frontend/src/{pages, components/ui, contexts, hooks, lib}`; `docker-compose.yml` na raiz. Migrations SQL puro, numeradas `NNN_nome.sql`, auto-executadas no boot via `schema_migrations`.
- **Deploy (AD-10):** Coolify é produção; staging separado disparado por tag `v*-rc*`; servidor AWS do cliente é espelho secundário encadeado após Coolify. Todo ambiente expõe `/api/health` com retry pós-deploy — relevante apenas se a Story 1.1 tocar pipeline/CI, não é o foco do scaffold local.
- **Em aberto (Deferred):** client/realm exato de provisionamento no Keycloak para este módulo (reaproveitar o do FB_APU02 ou criar um próprio) ainda não está decidido — o formato da claim de perfil (AD-7) já está fechado, falta só o provisionamento.

## Cross-Story Dependencies

- Story 1.1 (scaffold) é pré-requisito direto para 1.2 e 1.3 — ambas dependem da estrutura de pastas, Docker e runner de migrations existirem.
- Story 1.3 (bootstrap do admin) depende do banco/migrations de 1.1 e tipicamente também da tabela de usuários criada pelo fluxo de login de 1.2.
- Este épico inteiro é dependência transversal: todos os demais épicos (Cadastros, Solicitações, Processamento, Painéis) pressupõem login funcionando e um administrador ativo garantido por aqui.
