# FB_APU05 — Módulo Controladoria (Ferreira Costa)

Sistema de gestão de solicitações orçamentárias/investimento (transferência, inclusão, inclusão SFC, imobilizado e obras), com fluxo de aprovação por alçadas e exportação para SAP.

## Stack

- Backend: Go 1.22 + PostgreSQL 15 (SQL puro, sem ORM) — padrões já validados em produção no projeto irmão FB_APU02.
- Frontend: React 18 + TypeScript + Vite + Tailwind CSS + shadcn/ui.
- Infra (dev local): Docker Compose (`api`, `web`, `db`).

## Rodando localmente

Pré-requisito: Docker e Docker Compose instalados.

```bash
docker-compose up
```

Isso sobe três serviços:

| Serviço | Porta | Descrição |
| --- | --- | --- |
| `web` | http://localhost:3000 | Frontend (Vite dev server) |
| `api` | http://localhost:8081 | Backend (Go) |
| `db` | localhost:5432 | PostgreSQL 15 |

Na primeira conexão do `api` com o Postgres, o runner de migrations roda automaticamente (`backend/migrations/*.sql`, registradas em `schema_migrations`). Com `migrations/` vazia — estado inicial do projeto — ele sobe sem erro e sem criar nenhuma tabela de domínio; a primeira migration real nasce na Story 2.1.

Verifique o backend com:

```bash
curl http://localhost:8081/api/health
# {"status":"ok"}
```

E o frontend em http://localhost:3000 — a página deve exibir "FB_APU05" e o resultado do `fetch('/api/health')`.

## Desenvolvimento sem Docker

### Backend

```bash
cd backend
cp .env.example .env   # ajuste DATABASE_URL para seu Postgres local
go run main.go
```

### Frontend

```bash
cd frontend
npm install
npm run dev
```

## Primeiro acesso administrativo

Não existe cadastro de usuário separado (AD-6/AD-7): o login via SSO corporativo
cria automaticamente o registro em `usuarios` no primeiro acesso de cada
pessoa, sempre com perfil `solicitante`. Para promover a primeira pessoa a
`administrador`:

1. Peça para a pessoa fazer login uma vez via SSO (isso a cria em `usuarios`).
2. Edite `backend/scripts/bootstrap_admin.sql`, substituindo o placeholder
   `<SUBSTITUA_PELO_EMAIL_REAL>` pelo e-mail real dela.
3. Rode o script manualmente (ele nunca roda sozinho — fica fora de
   `backend/migrations/` de propósito):
   ```bash
   psql "$DATABASE_URL" -f backend/scripts/bootstrap_admin.sql
   ```
4. Reverta a edição do passo 2 antes de commitar — o arquivo deve voltar ao
   placeholder no repositório (dado sensível, repositório público).

Depois do primeiro administrador criado, qualquer administrador autenticado
pode conceder/revogar o perfil `administrador` e ativar/desativar outros
usuários via `PATCH /api/admin/usuarios/{id}` (protegido por `RequireAuth`,
grava trilha de auditoria em `usuarios_auditoria`). O sistema sempre recusa
(`409`) qualquer mudança que deixaria zero administradores ativos.

## Estrutura de diretórios

```text
backend/
  main.go              # bootstrap HTTP + runner de migrations
  handlers/            # 1 arquivo por domínio HTTP
  services/            # integrações (Keycloak, upload Senior, ...)
  middleware/
  internal/
    aprovacao/         # motor de aprovação (Resolver) — Story 1.4+
    exportacao/        # exportação SAP — Story 1.4+
  migrations/          # NNN_nome.sql, auto-executadas no boot
frontend/
  src/
    pages/
    components/ui/
    contexts/
    hooks/
    lib/
docker-compose.yml
```

Ver `_bmad-output/planning-artifacts/architecture/architecture-FB_APU05-2026-10-06/ARCHITECTURE-SPINE.md` para a árvore completa e as decisões de arquitetura (AD-1 a AD-14).

## Documentação

- `docs/referencia/01-STACK-E-PADROES-FB_APU02.md` — stack, segurança, padrão de telas e banco de dados de referência (projeto irmão FB_APU02).
- `docs/referencia/02-REQUISITOS-NEGOCIO-ENVIO-TI.md` — síntese dos requisitos de negócio recebidos da área de TI/Controladoria.
- `_bmad-output/planning-artifacts/` — Project Brief, PRD, Arquitetura e Epics gerados pelo BMAD.

## Gestão do projeto

Gestão conduzida via [BMAD Method](https://bmadcode.com/) (`_bmad/`) e Kanban no [GitHub Projects #6](https://github.com/users/ClaudioSBezerra/projects/6).

Instalado via `npx bmad-method` (v6.12.1), módulos: `core`, `bmm`, `bmb`, `cis`, `bmad-loop`, `tea`. Skills disponíveis em `.claude/skills/` (gerado localmente, não versionado — rode `npx bmad-method install` para recriar).
