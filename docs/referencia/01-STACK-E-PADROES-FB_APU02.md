# Stack, Segurança, Telas e Banco — Referência FB_APU02

> Fonte: levantamento técnico do repositório `/home/claudio/projetos/FB_APU02` (FBTax Cloud, módulo fiscal Ferreira Costa), feito em 2026-10-06 para servir de base arquitetural ao FB_APU05 (Controladoria). Este documento é insumo para as fases de Analyst/PM/Architect do BMAD — não é a arquitetura final do FB_APU05, que deve ser decidida nessas fases levando em conta as diferenças de domínio.

## 1. Stack tecnológica

**Backend:** Go 1.22.0, `net/http` stdlib (sem framework de rotas), `database/sql` + `github.com/lib/pq` v1.11.2 (SQL puro, sem ORM). Auth: `golang-jwt/jwt/v5` v5.3.1 (HS256), bcrypt custo 14 (`golang.org/x/crypto`), `joho/godotenv` para `.env` em dev. Build com `CGO_ENABLED=0`, binário estático. Padrão "Handler Factory": `func XHandler(db *sql.DB) http.HandlerFunc`, DI manual no `main.go`.

**Frontend:** React 18.3.1 + TypeScript 5.2.2 (strict) + Vite 5.2 (SWC). React Router DOM 6.22.3, TanStack Query 5.90, React Hook Form 7.71 + Zod 4.3.6, Tailwind CSS 3.4.3 + shadcn/ui (Radix UI + CVA), Lucide React, Recharts, Sonner (toasts), xlsx (export Excel), date-fns.

**Banco:** PostgreSQL 15 — **decisão confirmada para o FB_APU05 também é Postgres** (ver resposta do usuário em 2026-10-06). Sem Redis em uso real.

**Infra:** Docker multi-stage (builder `golang:1.22-alpine` / `node:18-alpine` → runtime `alpine` / `nginx:alpine`), Coolify (PaaS self-hosted) + Traefik (TLS automático), deploy em AWS EC2 ou Hostinger VPS, imagens no GHCR.

## 2. Segurança

- JWT access token 30min (claims: user_id, role, exp) + refresh token 7 dias em cookie HttpOnly/SameSite=Strict, rotacionado a cada uso. Blacklist de tokens no logout.
- Senhas: bcrypt custo 14, nunca retornadas pela API.
- Segredos sensíveis criptografados com AES-256-GCM via `ENCRYPTION_KEY` separado do `JWT_SECRET` (falha fatal em prod se ausente).
- Rate limiting em memória por IP (login 5/15min, registro 10/h, forgot-password 3/h) — não distribuído.
- Webhooks externos validados por HMAC-SHA256.
- `SecurityMiddleware` global: HSTS, CSP, X-Frame-Options DENY, nosniff, Referrer-Policy, Permissions-Policy.
- CORS com whitelist via `ALLOWED_ORIGINS`.
- Multi-tenancy por `company_id` UUID explícito no `WHERE` de cada query (RLS existe mas não é aplicado em produção — anti-padrão documentado, **evitar repetir no FB_APU05**).
- Retenção de dados com TTL + goroutine de limpeza.

> Nota para o FB_APU05: o domínio de Controladoria usa identidade via SSO/Entra corporativo (conforme pacote envio TI), diferente do login próprio do FB_APU02. A camada de autorização por alçadas/aprovadores é o equivalente funcional do multi-tenancy do FB_APU02 e precisa de desenho próprio na fase de Architecture.

## 3. Padrão de telas / frontend

- Design system shadcn/ui (Radix + Tailwind + CVA), ~46 componentes base em `src/components/ui/`, dark mode via `class` + `next-themes`.
- 1 página por rota em `src/pages/`, export default.
- Data-fetching recomendado para código novo: TanStack Query (`useQuery`/`useMutation`); existe padrão legado com `useEffect` que não deve ser replicado em projeto novo.
- Headers de auth injetados automaticamente via monkey-patch de `window.fetch` no `AuthContext`.
- Navegação centralizada em `src/lib/navigation.ts`.
- Formulários: React Hook Form + Zod (padrão recomendado).
- Toasts via Sonner.

## 4. Banco de dados

- Sem ORM, SQL puro, migrations sequenciais numeradas (`001_...sql`), auto-executadas no boot via tabela `schema_migrations`.
- Convenções: UUID PK (`gen_random_uuid()`), `created_at`/`updated_at` em toda tabela, `UNIQUE` em chaves naturais para idempotência de import.
- Views materializadas para relatórios pesados, `REFRESH CONCURRENTLY`.

## 5. Estrutura de pastas (replicável)

```
raiz/        CLAUDE.md, docs/, Bmad-output/, _bmad/, _bmad-output/, .github/workflows/,
             docker-compose.yml, docker-compose.prod.yml, Dockerfile.production
backend/     main.go, go.mod, Dockerfile, handlers/ (1 arquivo/domínio), services/,
             middleware/, migrations/ (NNN_nome.sql)
frontend/    package.json, vite.config.ts, tailwind.config.js,
             src/{main.tsx,App.tsx,contexts/,pages/,components/ui/,hooks/,lib/}, public/, Dockerfile
```

Documentação viva em `docs/` (ARCHITECTURE, architecture-backend, architecture-frontend, data-models-backend, api-contracts-backend, component-inventory-frontend, integration-architecture, deployment-guide, project-overview, source-tree-analysis) é o padrão a reproduzir no FB_APU05 conforme as fases do BMAD forem gerando esses artefatos.

## 6. BMAD no FB_APU02

Instalado via `npx bmad-method` v6.10.0, módulos: core, bmm, bmb, cis, bmad-loop, tea, wds. Estrutura: `_bmad/_config/` (manifest, agent-manifest, workflow-manifest), `_bmad/<module>/config.yaml`, skills expostos em `.claude/skills/bmad-*`. Saída de trabalho em `_bmad-output/` (planning-artifacts, implementation-artifacts). Os 5 documentos executivos de entrega ficam em `Bmad-output/` (maiúsculo, convenção própria do projeto, não do framework): 01-ESCOPO-NEGOCIO, 02-STACK-TECNOLOGICA, 03-SEGURANCA, 04-INSTALACAO-AWS, 05-EPICS-SPRINTS.

O FB_APU05 replica essa mesma instalação (v6.12.1, módulos equivalentes) em 2026-10-06.

## 7. CI/CD

Workflows GitHub Actions: `deploy-production.yml` (push em `main`/tags `v*`: testa, builda imagens API/Web, deploy self-hosted com health-check), `deploy-staging.yml` (tags `v*-rc*`, SSH), `deploy-cliente-aws.yml` (sincroniza docker-compose no servidor do cliente). Padrão a seguir: sempre testar antes de buildar, sempre health-check pós-deploy com retry, backup pré-deploy.
