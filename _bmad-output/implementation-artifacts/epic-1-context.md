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
- Identidade (quem é o usuário) vem do Keycloak (e-mail verificado); perfil e ator sempre derivam da sessão autenticada no nosso próprio banco, nunca de payload enviado pelo cliente nem de claim do Keycloak.
- O primeiro administrador é provisionado diretamente no banco, por procedimento controlado fora da API.
- Concessão posterior de perfil administrador exige um administrador já autenticado, é uma escrita feita por um módulo único, e gera evento auditável.
- O sistema recusa qualquer tentativa de rebaixar ou desativar o último administrador ativo.
- Projeto deve ser rodável localmente via Docker (backend Go + frontend React + Postgres), com runner de migrations (`schema_migrations`) funcionando vazio sem erro.
- Dado sensível: nenhum exemplo/fixture pode usar nome real, matrícula ou e-mail de pessoa (repositório é público).

## Technical Decisions

- **Autenticação (AD-6):** replica exatamente o fluxo já validado em produção no sistema irmão FB_APU02 — mesmo realm Keycloak, mesma validação JWKS, mesma checagem de `email_verified`. Não reinventar o fluxo.
- **Autorização single-company (AD-7, corrigido):** sem `company_id`/multitenancy. Perfil (`solicitante`/`administrador`) é uma coluna gerenciada pela própria aplicação na tabela `usuarios` (mesmo padrão de `users.role` já validado no FB_APU02) — **não** deriva de role/claim do Keycloak (`resource_access.<client>.roles`). Identidade vem do Keycloak; autorização vem do nosso banco. Concessão de perfil administrador é uma escrita nessa coluna feita por um módulo único, que também aplica a proteção do último admin — nunca só na UI, nunca via chamada à Admin API do Keycloak (fora de escopo). Esta correção substitui a versão original do AD-7 (que derivava perfil de claim do Keycloak), incompatível com o FR-2: uma claim somente-legível não permite que o próprio sistema conceda/revogue perfil nem proteja o último admin.
- **Stack herdado (AD-9):** versões idênticas ao FB_APU02 (Go 1.22 — débito técnico conhecido e compartilhado; golang-jwt v5.3.1; bcrypt custo 14). Inclui os middlewares de segurança HTTP já validados: `SecurityMiddleware` global (HSTS, CSP, X-Frame-Options DENY, nosniff, Referrer-Policy, Permissions-Policy), rate limiting por IP em endpoints sensíveis, CORS por whitelist, emissão/rotação de JWT próprio com blacklist no logout.
- **Estrutura de pastas:** herdada do FB_APU02 — `backend/{main.go, internal/{aprovacao,exportacao}, handlers, services, middleware, migrations}`; `frontend/src/{pages, components/ui, contexts, hooks, lib}`; `docker-compose.yml` na raiz. Migrations SQL puro, numeradas `NNN_nome.sql`, auto-executadas no boot via `schema_migrations`.
- **Deploy (AD-10):** Coolify é produção; staging separado disparado por tag `v*-rc*`; servidor AWS do cliente é espelho secundário encadeado após Coolify. Todo ambiente expõe `/api/health` com retry pós-deploy — relevante apenas se a Story 1.1 tocar pipeline/CI, não é o foco do scaffold local.
- **Em aberto (Deferred):** client/realm exato de provisionamento no Keycloak para este módulo (reaproveitar o do FB_APU02 ou criar um próprio) ainda não está decidido — isso não afeta o perfil (já fixado na tabela `usuarios` pelo AD-7), só a identidade/provisionamento do client em si.

## Cross-Story Dependencies

- Story 1.1 (scaffold) é pré-requisito direto para 1.2 e 1.3 — ambas dependem da estrutura de pastas, Docker e runner de migrations existirem.
- Story 1.3 (bootstrap do admin) depende do banco/migrations de 1.1 e da coluna de perfil na tabela de usuários criada/usada pelo fluxo de login de 1.2.
- Este épico inteiro é dependência transversal: todos os demais épicos (Cadastros, Solicitações, Processamento, Painéis) pressupõem login funcionando e um administrador ativo garantido por aqui.
