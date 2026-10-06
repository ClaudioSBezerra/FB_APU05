---
title: 'Scaffold inicial do projeto'
type: 'chore'
created: 2026-10-06
status: 'done'
route: 'dispatch'
review_loop_iteration: 0
baseline_commit: '248308df8de1dfaf75528febc2a5da8af2cc9861'
context:
  - '{project-root}/_bmad-output/planning-artifacts/architecture/architecture-FB_APU05-2026-10-06/ARCHITECTURE-SPINE.md'
  - '{project-root}/_bmad-output/implementation-artifacts/epic-1-context.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** O repositório FB_APU05 não tem nenhum código ainda — só documentação de planejamento (BMAD). Não há como implementar qualquer story funcional sem uma base rodável.

**Approach:** Criar o scaffold inicial (backend Go + frontend React + Postgres via `docker-compose`), replicando literalmente os padrões já validados em produção no projeto irmão FB_APU02 (module layout, runner de migrations, Dockerfile, stack de frontend) — exceto o fluxo de dev local, que ganha um `docker-compose.yml` de verdade (com portas mapeadas), já que o FB_APU02 não tem um e usa Postgres nativo via WSL em vez disso.

## Boundaries & Constraints

**Always:**
- Replicar exatamente o padrão do FB_APU02 para: `go.mod` (versões de dependência), lógica do runner de migrations (`onDBConnected`, tabela `schema_migrations`, leitura de `migrations/*.sql`), `Dockerfile` multi-stage, `vite.config.ts` (proxy `/api`, alias `@`).
- Seguir a árvore de diretórios do Architecture Spine (`backend/{handlers,services,middleware,migrations,internal}`, `frontend/src/{pages,components/ui,contexts,hooks,lib}`).
- Expor `GET /api/health` (200 `{"status":"ok"}`) — requisito do AD-10 para health-check de deploy.
- Criar `backend/.env.example` (o FB_APU02 não tem um versionado).

**Never:**
- Não implementar login, autenticação ou qualquer FR de negócio — isso é Story 1.2/1.3 em diante.
- Não criar nenhuma tabela de domínio (`solicitacoes`, `colaboradores`, etc.) — `migrations/` começa vazia; a primeira migration real nasce na Story 2.1.
- Não copiar o `README.md` do FB_APU02 (está desatualizado, ainda fala de "FB_APU01").
- Não vendorizar dependências Go (`vendor/`) — o FB_APU02 vendoriza por razão histórica; aqui, `go mod download` direto no Dockerfile é suficiente e mais simples.
- Não criar `docker-compose.prod.yml` nem workflow de CI/CD nesta story — isso é escopo de uma story de deploy futura (ainda não hádesenhada em nenhum dos 5 épicos atuais).

</frozen-after-approval>

## Code Map

- `/home/claudio/projetos/FB_APU02/backend/go.mod` -- base de dependências estruturais a replicar (module name trocado para `fb_apu05`)
- `/home/claudio/projetos/FB_APU02/backend/main.go` (função `onDBConnected`) -- padrão do runner de migrations a replicar literalmente (ReadFile + Exec, tabela `schema_migrations`, skip de já-executadas)
- `/home/claudio/projetos/FB_APU02/backend/Dockerfile` -- padrão de build multi-stage a replicar (golang:1.22-alpine builder → alpine runtime, tzdata America/Sao_Paulo)
- `/home/claudio/projetos/FB_APU02/frontend/vite.config.ts` -- padrão de proxy `/api` + alias `@` a replicar
- `/home/claudio/projetos/FB_APU02/frontend/package.json` -- padrão de scripts/deps a replicar (corrigindo o campo `"name"`, que no FB_APU02 ficou errado como `"fb_apu01-frontend"`)
- `ARCHITECTURE-SPINE.md` § Structural Seed -- árvore de diretórios alvo e Stack (versões pinadas)

## Tasks & Acceptance

**Execution:**
- [x] `backend/go.mod` -- criar com `module fb_apu05`, `go 1.22.0`, deps `golang-jwt/jwt/v5 v5.3.1`, `lib/pq v1.11.2`, `golang.org/x/crypto v0.17.0`, `joho/godotenv v1.5.1` -- base estrutural já validada em produção
- [x] `backend/main.go` -- bootstrap `net/http` stdlib, conexão Postgres via `DATABASE_URL`, runner de migrations (`onDBConnected`, idêntico ao padrão FB_APU02) -- reaproveita mecanismo já validado
- [x] `backend/handlers/health.go` -- `GET /api/health` → 200 `{"status":"ok"}` -- requisito de deploy (AD-10)
- [x] `backend/migrations/.gitkeep` -- pasta vazia; primeira migration real é da Story 2.1
- [x] `backend/Dockerfile` -- multi-stage, replicando o padrão do FB_APU02 (sem vendor)
- [x] `backend/.env.example` -- chaves `DATABASE_URL, JWT_SECRET, ENCRYPTION_KEY, PORT, COOKIE_SECURE, APP_URL, ALLOWED_ORIGINS`
- [x] `frontend/package.json` -- `"name": "fb_apu05-frontend"`, scripts `dev/build/lint/preview/test`, deps React 18.3.1 + TS 5.2.2 + Vite 5.2 + Tailwind 3.4.x + shadcn/ui + TanStack Query + react-hook-form + zod
- [x] `frontend/vite.config.ts` -- porta 3000, proxy `/api` → `http://localhost:8081` (configurável via `VITE_API_TARGET`), alias `@` → `src`
- [x] `frontend/src/main.tsx`, `frontend/src/App.tsx` -- skeleton mínimo: renderiza "FB_APU05" e faz `fetch('/api/health')` para provar o pipeline ponta a ponta
- [x] `frontend/src/{pages,components/ui,contexts,hooks,lib}/.gitkeep` -- estrutura de pastas do Architecture Spine
- [x] `frontend/tailwind.config.js`, `frontend/tsconfig.json` -- config mínima padrão Vite+React+TS+Tailwind
- [x] `docker-compose.yml` -- dev local: `api` (porta 8081), `web` (porta 3000), `db` (`postgres:15-alpine`, porta 5432, db/user/senha de dev)
- [x] `README.md` -- reescrever com instruções reais de setup (`docker-compose up`) -- não herdar o erro de nome do FB_APU02

**Acceptance Criteria:**
- Given o repositório FB_APU05 vazio de código, when o desenvolvedor roda `docker-compose up`, then os três serviços (api, web, db) sobem sem erro
- Given o backend subindo, when ele conecta ao Postgres, then o runner de migrations executa sem erro mesmo com `migrations/` vazia
- Given o frontend servido, when a página carrega, then ela exibe "FB_APU05" e o resultado do `fetch('/api/health')`
- Given a estrutura de pastas criada, when comparada ao Architecture Spine § Structural Seed, then bate exatamente

## Implementation Notes

Implementado em sessão única (subagente dedicado). Decisões não cobertas literalmente pelo spec, tomadas por julgamento durante a implementação:
- `main.go`: conexão ao Postgres com retry (5s) em goroutine própria, para não travar a subida do processo HTTP enquanto o `db` do compose ainda não respondeu — necessário porque `depends_on: condition: service_healthy` no compose não é garantia suficiente de que o Postgres já aceita conexões no instante exato.
- `onDBConnected`: removida a lógica de correção de schema legado do FB_APU02 (ela só existe para consertar uma tabela antiga já em produção lá) — irrelevante para um schema greenfield.
- `docker-compose.yml`: serviço `web` roda Vite em modo dev dentro de `node:20-alpine` com bind mount (sem Dockerfile de frontend) — mais simples que buildar uma imagem de produção, que é escopo de uma story de deploy futura.
- `go.mod`: `golang-jwt/jwt/v5` e `golang.org/x/crypto` ficam sem uso ainda (nada importa login/bcrypt nesta story) — `go mod tidy` tende a querer removê-los; mantidos propositalmente porque a Story 1.2/1.3 os usa.

Verificação executada: `go build ./...`, `go vet ./...`, `gofmt -l .` (limpos); `npm install && npm run build` (limpo); YAML do compose validado por parser; variáveis do compose conferidas contra `.env.example`; árvore de diretórios conferida contra o Architecture Spine. `docker-compose up` não foi executado (Docker não instalado neste sandbox).

## Review Triage Log

- **high** `backend/.env.example` é excluído do git pelo padrão pré-existente `.gitignore:12` (`.env.*`), confirmado por `git check-ignore -v`. Num clone limpo o arquivo não existe, quebrando o passo `cp .env.example .env` do README. → **patch**
- **medium** `backend/handlers/health.go`: `HealthHandler` sempre retorna 200, nunca lê `dbErr` (que `main.go` mantém só para isso) — health-check não reflete o estado real do banco, contrariando o propósito do AD-10. → **patch**
- **low** `backend/handlers/health.go`: não restringe o método HTTP (aceita POST/DELETE/etc. com 200). → **patch**
- **medium** `backend/main.go` (laço de retry de conexão): quando `conn.Ping()` falha a conexão aberta por `sql.Open` não é fechada antes de tentar de novo — vazamento de `*sql.DB` durante uma indisponibilidade prolongada do banco. → **patch**
- **medium** `backend/main.go`: desligamento usa `os.Exit(0)` direto, sem `server.Shutdown(ctx)` — requisições em andamento são derrubadas em vez de drenadas. → **patch**
- **low** `frontend/src/App.tsx`: `fetch('/api/health')` não checa `res.ok` antes de tratar o corpo como saudável — um 503 (depois do patch acima) apareceria na tela como se estivesse ok. → **patch**
- **medium** `frontend/package.json` declara `"lint": "eslint ."` mas `eslint` não está em `devDependencies` e não há config — `npm run lint` falha imediatamente. → **patch**
- **medium** `backend/main.go` (`onDBConnected`): migration que falha só loga e segue para a próxima, sem parar — padrão herdado deliberadamente do FB_APU02 (Always-boundary do spec exige replicar a lógica literalmente); sem risco hoje (`migrations/` vazia), mas relevante quando a Story 2.1 trouxer migrations reais com dependência de ordem. → **defer**
- **maybe-false** Runner de migrations e stack `docker-compose` nunca foram exercitados de ponta a ponta (Docker ausente neste sandbox) — a spec já reconhece essa limitação e proíbe explicitamente montar CI/CD nesta story (seção Never). → **defer**
- **low** `vite.config.ts` configura `test.environment: 'node'`, mas um futuro teste de componente React precisaria de `jsdom`/`happy-dom` + `@testing-library/react`, ausentes; hoje não há nenhum teste, então não quebra nada ainda. → **defer**
- **false** `.gitignore` "faltando" em `backend/`/`frontend/` — o `.gitignore` da raiz já cobre `node_modules/`, `dist/`, `backend/server`, `.env*` para o repo inteiro; não precisa de um por subpasta.
- **false** `components/ui/` só com `.gitkeep`, sem `components.json`/`cn()` — é exatamente o que o spec pediu (Tasks só lista `.gitkeep`); construir componentes shadcn é fora do escopo desta story, não uma omissão.
- **false** `tailwind.config.js` usa `export default` (ESM) e chama `require("tailwindcss-animate")` — levantei a hipótese de quebra, mas rodei `npm run build` de verdade e o build passa limpo; o loader de config do Tailwind/PostCSS não se importa com `"type":"module"` do package.json para esse arquivo.
- **false** `go.mod` tem `golang-jwt`/`x/crypto` sem uso ainda, um futuro `go mod tidy` poderia remover — já documentado como intencional nas Implementation Notes; não há correção trivial que não seja um hack artificial (ex. blank import), e o risco é hipotético, não um defeito atual.
- **false** `docker-compose.yml` não documenta um `.env` de raiz para as variáveis de override — redundante: depois do patch do `.env.example`, as mesmas chaves já ficam documentadas lá; criar um segundo arquivo seria duplicação, não correção.
- **false** `sprint-status.yaml` marca a story `in-progress` enquanto o spec já está `in-review` com tudo `[x]` — é o estado esperado neste ponto do workflow; a sincronização para `review` acontece no Step 5 (Present), ainda não executado.

## Verification

**Commands:**
- `cd backend && go build ./...` -- expected: compila sem erro
- `cd frontend && npm install && npm run build` -- expected: build sem erro
- `python3 -c "import yaml, sys; yaml.safe_load(open('docker-compose.yml'))"` -- expected: YAML válido (Docker não está instalado neste sandbox, então `docker-compose up` não pode ser executado aqui; validação de sintaxe + build individual dos dois serviços é o que esta sessão consegue verificar)

**Manual checks (if no CLI):**
- Revisar `docker-compose.yml` linha a linha conferindo que as portas/variáveis batem com `backend/.env.example`
