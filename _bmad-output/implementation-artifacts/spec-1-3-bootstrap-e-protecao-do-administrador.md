---
title: 'Bootstrap e proteção do administrador'
type: 'feature'
created: 2026-10-06
status: 'done'
route: 'dispatch'
review_loop_iteration: 0
baseline_revision: 'dce0b84f0ae961ab161ae678a02d09dbdcf9bd53'
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
- [x] `backend/migrations/002_add_ativo_and_auditoria.sql` -- adiciona `usuarios.ativo BOOLEAN NOT NULL DEFAULT true`; cria `usuarios_auditoria` (id, ator_id, usuario_alvo_id, acao, perfil_anterior, perfil_novo, ativo_anterior, ativo_novo, created_at)
- [x] `backend/handlers/middleware.go` -- `RequireAuth(next http.HandlerFunc, perfilExigido string) http.HandlerFunc`: valida JWT próprio (mesmo `JWT_SECRET`), checa `tokenBlacklist`, injeta claims no contexto; 401 se inválido/revogado, 403 se `perfilExigido` não bate
- [x] `backend/handlers/admin.go` -- `PATCH /api/admin/usuarios/{id}` (protegido por `RequireAuth(..., "administrador")`): body `{perfil?: "administrador"|"solicitante", ativo?: bool}`; antes de aplicar uma mudança que reduziria para zero o número de `usuarios` com `perfil='administrador' AND ativo=true`, recusa com 409; toda mudança aplicada grava uma linha em `usuarios_auditoria`
- [x] `backend/scripts/bootstrap_admin.sql` -- script manual (fora de `migrations/`, nunca roda automaticamente): `UPDATE usuarios SET perfil='administrador' WHERE email = '<SUBSTITUA_PELO_EMAIL_REAL>'` com comentário de instrução; documentar no README a execução via `psql` após o primeiro login SSO da pessoa (ela precisa existir em `usuarios` antes — nasce via auto-provisionamento da Story 1.2)
- [x] `backend/main.go` -- registrar a rota `PATCH /api/admin/usuarios/{id}` atrás de `RequireAuth`
- [x] `README.md` -- seção "Primeiro acesso administrativo": login via SSO cria o usuário como `solicitante`; rodar `bootstrap_admin.sql` substituindo o placeholder para promover a pessoa a `administrador`

**Acceptance Criteria:**
- Given um banco recém-criado sem nenhum administrador, when o procedimento de bootstrap (script + instrução do README) é executado após o primeiro login da pessoa, then existe exatamente um administrador ativo
- Given um administrador autenticado, when ele concede perfil `administrador` a outro usuário via `PATCH /api/admin/usuarios/{id}`, then o perfil muda e uma linha é gravada em `usuarios_auditoria`
- Given exatamente um administrador ativo, when alguém tenta rebaixá-lo (`perfil: "solicitante"`) ou desativá-lo (`ativo: false`) via essa rota, then o sistema recusa com 409, sem aplicar a mudança
- Given um usuário com perfil `solicitante` autenticado, when ele chama `PATCH /api/admin/usuarios/{id}`, then recebe 403
- Given um token revogado (blacklist da Story 1.2), when ele é usado em qualquer rota protegida por `RequireAuth`, then recebe 401

## Implementation Notes

Implementado em sessão única. Resumo das decisões de implementação:

- `RequireAuth` (`backend/handlers/middleware.go`) adaptado de `AuthMiddleware` do FB_APU02 com duas diferenças deliberadas: (1) checa `tokenBlacklist` antes de validar a assinatura, fechando o gap documentado no Problem desta spec; (2) exige igualdade exata da claim `perfil` (não `role`, AD-7) com `perfilExigido`, sem o bypass implícito "admin sempre passa" do modelo original — evita um bug sutil se um perfil intermediário surgir no futuro.
- `AdminUpdateUsuarioHandler` (`backend/handlers/admin.go`) roda inteiro dentro de uma transação: `SELECT perfil, ativo FROM usuarios WHERE id = $1 FOR UPDATE` trava a linha-alvo e lê o estado anterior; quando a mudança tiraria o usuário-alvo da condição "administrador ativo", uma segunda query (`SELECT id FROM usuarios WHERE perfil='administrador' AND ativo=true FOR UPDATE`) trava **todo o conjunto** de administradores ativos antes de recontar — não um `COUNT(*)` solto. Isso importa porque o Postgres recusa `FOR UPDATE` combinado com função agregada, e porque travar só a linha-alvo não fecharia a corrida entre duas requisições mirando **dois administradores diferentes** ao mesmo tempo (cada uma travaria sua própria linha sem bloquear a outra, e ambas poderiam ler "ainda há 2 admins" antes de qualquer commit). Com o conjunto inteiro travado, a segunda requisição concorrente bloqueia até a primeira commitar ou abortar (e, no caso raro de deadlock entre duas linhas-alvo diferentes, o Postgres aborta uma delas — o cliente recebe 500 e pode reenviar; o reenvio então enxerga o estado já consistente e é corretamente aceito ou recusado com 409). PATCH sem nenhum campo alterado (no-op) não grava auditoria.
- `backend/scripts/bootstrap_admin.sql` segue exatamente o padrão pedido: fora de `migrations/`, só placeholder (`<SUBSTITUA_PELO_EMAIL_REAL>`), nunca um e-mail real — confirmado por `grep -n "@"` no arquivo (zero ocorrências).
- `PATCH /api/admin/usuarios/{id}` registrado em `main.go` usando o roteamento por método+wildcard do `net/http` nativo do Go 1.22 (`"PATCH /api/admin/usuarios/{id}"`), sem introduzir nenhuma dependência de router externo.

Verificação real executada (sem Docker/credenciais de Postgres disponíveis neste sandbox — ver Verification):
- `go build ./... && go vet ./... && go test ./...` — compila sem erro, suíte completa (incluindo a nova) passa.
- Testes novos adicionados cobrindo os trechos que não dependem de banco: `backend/handlers/middleware_test.go` (401 sem token, 403 perfil incorreto, 401 token revogado via blacklist, caminho feliz injeta `user_id` no contexto) e `backend/handlers/admin_test.go` (400 corpo vazio, 400 perfil inválido, 400 id ausente, 405 método errado, mapeamento de `auditAction`).
- A lógica transacional (proteção do último admin, auditoria, FOR UPDATE) foi verificada por leitura cuidadosa do código e por um traçado manual do cenário de corrida de duas requisições concorrentes mirando dois administradores diferentes (documentado acima) — **não foi exercitada contra um Postgres real** neste sandbox (sem Docker disponível e sem credenciais para o Postgres local já em execução na máquina). Esse é o principal risco residual desta implementação — ver seção de riscos abaixo.

Riscos/incompleto sinalizados pelo implementador:
- **Não testado ponta a ponta contra Postgres real**: as 5 Acceptance Criteria desta story (bootstrap, concessão de perfil, proteção do último admin, 403 para solicitante, 401 para token revogado) foram verificadas por unit test (para as partes sem DB) e por leitura cuidadosa do SQL/transação (para as partes com DB), mas não por um teste de integração rodando de fato contra Postgres 15 — recomendo rodar manualmente via `docker-compose up` antes de considerar esta story pronta para produção.
- Cenário de deadlock entre duas requisições concorrentes mirando dois administradores-alvo diferentes ao mesmo tempo resulta em 500 (erro genérico) para uma delas, não em 409 — tecnicamente correto (o cliente pode reenviar e recebe a resposta certa), mas um 409 explícito seria uma experiência melhor; não implementado por ser uma janela de corrida extremamente estreita e por manter o tratamento de erro consistente com o resto do arquivo.
- `backend/handlers/middleware.go` não tem endpoint de "whoami"/`/api/auth/me` para o frontend consumir claims — fora do escopo desta story (Code Map não pede isso).
- Nenhuma tela de frontend foi criada para a rota administrativa (gestão de usuários) — a spec só pede o endpoint backend + script + README; UI fica para uma story futura.

## Spec Change Log

## Review Triage Log

> Nota: o dev-pass original (sessão automática do bmad-loop) travou por limite de uso da conta antes de concluir o review gate. Retomado manualmente em 2026-10-07 — rodei os 3 reviewers do zero contra o diff já produzido.

- **high** `backend/handlers/admin.go`: a proteção do último administrador (409) — a garantia central desta story — não tem nenhum teste que a exercite de verdade; os 5 testes existentes passam `db=nil` e só cobrem os ramos anteriores ao acesso ao banco. Uma inversão/off-by-one em `totalAdminsAtivos <= 1` ou nas flags `eraAdminAtivo`/`seraAdminAtivo` passaria despercebida. → **patch**
- **medium** `backend/handlers/admin.go`: a escrita em `usuarios_auditoria` (e o skip no caminho no-op) também não tem nenhum teste que a exercite — mesma causa raiz (precisa de banco real ou mock). → **patch**
- **medium** `backend/handlers/admin.go`: `{id}` do path vai direto pra `WHERE id = $1` contra coluna `UUID` sem validar formato antes — um id malformado vira erro de sintaxe do Postgres, caindo no `err != nil` genérico → 500 em vez de 400. → **patch**
- **low** `backend/handlers/admin.go`: a trava `FOR UPDATE` do conjunto de admins ativos não usa `ORDER BY` — duas requisições concorrentes mirando dois admins-alvo diferentes podem travar linhas em ordens diferentes (risco de deadlock que o próprio implementador já documentou como resultando em 500, retentável). `ORDER BY id` reduz a janela. → **patch**
- **low** `backend/scripts/bootstrap_admin.sql`: `UPDATE` sem `RETURNING` — um e-mail digitado errado no placeholder vira "UPDATE 0" silencioso, sem sinal de que o bootstrap falhou. → **patch**
- **low** `backend/migrations/002_add_ativo_and_auditoria.sql`: falta índice em `ator_id` (só `usuario_alvo_id` tem) — "o que o admin X fez" é uma consulta de auditoria tão natural quanto "o que aconteceu com Y". → **patch**
- **low** `backend/migrations/002_add_ativo_and_auditoria.sql`: `acao VARCHAR(50)` sem `CHECK` amarrado aos 3 valores que `auditAction()` realmente produz — `usuarios.perfil` já tem esse padrão (migration 001), ficou inconsistente aqui. → **patch**
- **low** `backend/handlers/admin.go`: `json.NewDecoder(r.Body)` sem limite de tamanho (`http.MaxBytesReader`) — hygiene padrão, sem downside. → **patch**
- **low** `backend/handlers/middleware.go`: prefixo `"Bearer "` checado case-sensitive — como só nosso próprio frontend chama essa rota (sempre manda `Bearer` com B maiúsculo), impacto prático é baixo, mas o fix é trivial e sem downside. → **patch**
- **false** `backend/handlers/middleware.go` + `auth.go`: alegação de "algorithm confusion" por `jwt.Parse` não restringir `WithValidMethods`. Verificado: o sistema só assina/verifica com um único segredo simétrico (HMAC) em toda a aplicação — não existe nenhuma chave assimétrica (RSA/EC) nossa para um atacante "confundir" (a verificação RS256 do Keycloak é um caminho de código totalmente separado, em `iam/`, nunca usado para os tokens próprios). `alg: none` já é rejeitado pelo golang-jwt v5 por padrão (exige o marcador `UnsafeAllowNoneSignatureType`, que o keyfunc nunca retorna). Sem chave pública para abusar e sem bypass de `none`, não há escalonamento real possível aqui. Vou adicionar `jwt.WithValidMethods(["HS256"])` mesmo assim, como higiene — mas fora do fluxo formal de triagem (fix de graça, não é correção de um achado real).
- **defer** `backend/handlers/middleware.go`: quando um admin é rebaixado/desativado, o JWT que essa pessoa já tem continua válido até expirar (até 30min) — não existe hoje um jeito de invalidar "todos os tokens do usuário X" (a blacklist é por token individual, não por `user_id`), e fazer `RequireAuth` reconsultar o banco a cada requisição anula o propósito de um JWT sem estado. Troca-off já existente desde a Story 1.2 (mesmo motivo do token de acesso durar só 30min); não é escopo desta story resolver.

## Design Notes

A proteção do "último admin" é avaliada em uma única query antes do UPDATE, dentro da mesma transação, para evitar corrida entre duas requisições concorrentes de desativação (duas requisições lendo "ainda há 2 admins" ao mesmo tempo e ambas desativando, deixando zero) — usar `SELECT ... FOR UPDATE` ou equivalente na linha-alvo e recontagem dentro da transação.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && go test ./...` -- expected: compila sem erro, testes existentes (iam, handlers) continuam passando -- **executado, passou** (build OK, vet OK, `ok fb_apu05/handlers`, `ok fb_apu05/iam`, incluindo os testes novos de `RequireAuth` e `AdminUpdateUsuarioHandler` que não dependem de banco)

**Manual checks (if no CLI):**
- Revisar `bootstrap_admin.sql` para confirmar que não contém nenhum e-mail real, só o placeholder. -- **feito**: `grep -n "@" backend/scripts/bootstrap_admin.sql` não retornou nenhuma ocorrência.
- **Pendente**: teste de integração ponta a ponta contra Postgres real (`docker-compose up` + exercitar as 5 Acceptance Criteria com `curl`/`psql`) — este sandbox não tinha Docker nem credenciais para o Postgres local já em execução na máquina; a lógica transacional (FOR UPDATE no conjunto de admins ativos, proteção do último admin, trilha de auditoria) foi verificada por leitura de código e traçado manual de cenário de corrida, não por execução real. Recomendo rodar isso antes do merge.
