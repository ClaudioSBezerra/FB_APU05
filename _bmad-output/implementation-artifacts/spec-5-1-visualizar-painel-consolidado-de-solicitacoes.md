---
title: 'Visualizar painel consolidado de solicitações'
type: 'feature'
created: '2026-10-09'
status: 'done'
baseline_revision: 'b6e5c7b0d96f87491d3ce63dd940e3375d837604'
review_loop_iteration: 0
followup_review_recommended: false
context: ['{project-root}/_bmad-output/planning-artifacts/architecture/architecture-FB_APU05-2026-10-06/ARCHITECTURE-SPINE.md']
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** Usuários não têm como acompanhar o volume agregado de solicitações (por tipo, por status, por centro de custo) sem abrir cada uma individualmente; FR-17/Epic 5 prevê um painel consolidado, mas lido de snapshots pré-calculados (`painel_snapshots`), nunca por agregação ao vivo nas tabelas transacionais.

**Approach:** Novo endpoint somente-leitura `GET /api/paineis/{painel}` (contrato já citado em `02-REQUISITOS-NEGOCIO-ENVIO-TI.md` e no PRD §4.8) que, para `painel="consolidado"`, lê e agrupa linhas de uma nova tabela `painel_snapshots` (3 dimensões: tipo, status, cc) e devolve um objeto agregado único; nova página autenticada no frontend renderiza os 3 agrupamentos como listas simples. O cálculo/preenchimento de `painel_snapshots` a partir das tabelas transacionais (job upstream) não está especificado em nenhum artefato de planejamento (PRD §7.3/Questão Aberta 14) e fica fora do escopo desta story — ver Boundaries.

## Boundaries & Constraints

**Always:**
- O handler lê exclusivamente de `painel_snapshots` — nunca consulta/agrega `solicitacoes` ou tabelas relacionadas diretamente (Architecture Spine: "Handlers de Painéis leem exclusivamente de `painel_snapshots`").
- Rota aberta a qualquer usuário autenticado (`RequireAuth(..., "")`) — nenhuma AC/Epic Context pede restrição a administrador.
- Resposta é um objeto único pré-calculado (`por_tipo`/`por_status`/`por_cc`/`gerado_em`) — nunca o contrato paginado `{items,pagina,tamanho}` usado pelas demais listagens (Architecture Spine, regra explícita de não confundir os dois formatos).
- Quando `painel_snapshots` está vazia (nenhum snapshot calculado ainda), a rota devolve 200 com os 3 arrays vazios e `gerado_em: null` — nunca erro.

**Never:**
- Não implementar o job/rotina que calcula e grava `painel_snapshots` a partir das tabelas transacionais — trigger/periodicidade não está especificado em nenhum artefato de planejamento (PRD §7.3, Questão Aberta 14); inventar um mecanismo (cron, trigger síncrono em cada escrita, etc.) seria uma decisão de negócio/arquitetura não pedida por nenhuma AC desta story.
- Não expandir o escopo de indicadores além de volume por tipo/status/CC — Epic 5 explicita que indicadores adicionais ficam para iteração futura, a definir com o negócio.
- Não adicionar biblioteca de gráficos ao frontend (nenhuma existe hoje no `package.json`); "painel"/"indicadores" é satisfeito por listas/tabelas agregadas, nenhuma AC pede visualização gráfica.
- Não reaproveitar `internal/sla` nesta story — o escopo mínimo (volume por tipo/status/cc) não inclui indicador de atraso.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Painel consolidado com dados | `painel_snapshots` tem linhas nas 3 dimensões para `painel='consolidado'` | 200; `por_tipo`/`por_status`/`por_cc` preenchidos com `chave`/`rotulo`/`quantidade` exatos da tabela; `gerado_em` = o mais recente entre as linhas | No error expected |
| Painel consolidado sem dados | `painel_snapshots` vazia | 200; os 3 arrays vazios; `gerado_em: null` | No error expected |
| Nome de painel desconhecido | `GET /api/paineis/qualquer-outro` | — | 404 |
| Sem autenticação | `GET /api/paineis/consolidado` sem `Authorization` válido | — | 401 (mesmo middleware `RequireAuth` de toda rota autenticada) |

</intent-contract>

## Code Map

- `backend/handlers/fila.go:42-110` (`ListarFilaHandler`) -- padrão exato a replicar: SQL plano dentro do handler (sem camada de repositório — convenção já estabelecida: leituras simples ficam no handler), `json.NewEncoder(w).Encode(...)` ao final; aqui adaptado para um objeto agregado em vez de `{items,pagina,tamanho}`.
- `backend/handlers/middleware.go:43-85` (`RequireAuth`) -- `RequireAuth(next, "")` = qualquer usuário autenticado (mesmo uso de `POST /api/solicitacoes`, `main.go:347`); `perfilExigido=""` nunca checa `perfil` claim.
- `backend/handlers/auth.go:159-163` (`jsonErr`) -- helper de erro `{"error": mensagem}` a reaproveitar para o 404 de painel desconhecido.
- `backend/main.go:283-292` (`withDB`) -- closure de conexão GERAL (role `fb_apu05_app`, só leitura aqui); `main.go:390` (depois da rota de `Finalizar`, Story 4.5) -- ponto de inserção da nova rota.
- `backend/migrations/010_assumir_fila.sql:45-54` (`ALTER DEFAULT PRIVILEGES ... GRANT ALL ON TABLES TO fb_apu05_app`) -- toda tabela nova já nasce com GRANT ALL para `fb_apu05_app`; a migration 015 **não precisa** de um `GRANT` explícito para a conexão geral usada por este handler.
- `backend/migrations/003_cadastros_administraveis.sql:24-31` (`centros_custo`: `codigo`,`nome`) -- referência só para saber que `codigo`/`nome` existem; não é consultada por este handler (snapshot já carrega o rótulo denormalizado).
- `backend/handlers/fila_test.go` -- convenção de teste a replicar: `httptest.NewRequest` + `sqlmock.New()` + claims injetadas via `context.WithValue(req.Context(), ClaimsContextKey, claims)` (sem JWT real).
- `frontend/src/App.tsx:17-44` (`Home`) -- padrão de auth-gate manual (`useAuth()` + `useEffect` redireciona para `/login` se `!isAuthenticated`) a replicar na nova página; `App.tsx:72-82` (`<Routes>`) -- ponto de inserção da rota `/painel`.
- `frontend/src/contexts/AuthContext.tsx:58-82` -- interceptor global de `fetch`: já injeta `Authorization: Bearer <token>` em toda chamada a `/api/*` e redireciona para `/login` em 401 — a nova página usa `fetch('/api/paineis/consolidado')` puro, sem montar o header manualmente (mesmo padrão do `fetch('/api/health')` em `Home`).

## Tasks & Acceptance

**Execution:**
- `backend/migrations/015_painel_snapshots.sql` -- NOVA: `CREATE TABLE painel_snapshots (id UUID PK default gen_random_uuid(), painel VARCHAR(50) NOT NULL, dimensao VARCHAR(20) NOT NULL CHECK (dimensao IN ('tipo','status','cc')), chave VARCHAR(255) NOT NULL, rotulo VARCHAR(255) NOT NULL, quantidade INTEGER NOT NULL CHECK (quantidade >= 0), gerado_em TIMESTAMPTZ NOT NULL DEFAULT now(), UNIQUE (painel, dimensao, chave))` + índice `idx_painel_snapshots_painel ON painel_snapshots (painel)` -- schema genérico (1 tabela, 3 dimensões) suficiente para o escopo mínimo do Epic 5; sem `GRANT` (migration 010 já cobre `fb_apu05_app` via `ALTER DEFAULT PRIVILEGES`).
- `backend/handlers/painel.go` -- NOVO: `ObterPainelHandler(db)` -- lê `r.PathValue("painel")`; se `!= "consolidado"` -> `jsonErr(w, 404, ...)`; senão `SELECT dimensao, chave, rotulo, quantidade, gerado_em FROM painel_snapshots WHERE painel = 'consolidado' ORDER BY dimensao, chave`; agrupa em `por_tipo`/`por_status`/`por_cc` por `dimensao`, cada item `{chave, rotulo, quantidade}`; `gerado_em` = o maior valor observado entre as linhas, `nil` se nenhuma linha.
- `backend/main.go` -- registra `http.HandleFunc("GET /api/paineis/{painel}", handlers.RequireAuth(withDB(handlers.ObterPainelHandler), ""))`, depois da rota de `Finalizar` (linha 390).
- `backend/handlers/painel_test.go` -- cobre a I/O Matrix (sqlmock, mesma convenção de `fila_test.go`: com dados, vazio, painel desconhecido, sem autenticação).
- `frontend/src/pages/PainelConsolidado.tsx` -- NOVO: página autenticada (mesmo auth-gate manual de `Home`) que busca `/api/paineis/consolidado` via `fetch` puro em `useEffect` e renderiza 3 listas (por tipo, por status, por cc) com `rotulo`/`quantidade`; trata estado vazio e erro de forma visível (mesmo padrão de mensagem de `Home`).
- `frontend/src/App.tsx` -- adiciona `<Route path="/painel" element={<PainelConsolidado />} />` dentro de `<Routes>`.

**Acceptance Criteria:**
- Given linhas em `painel_snapshots` calculadas para `painel='consolidado'` nas 3 dimensões, when um usuário autenticado faz `GET /api/paineis/consolidado`, then a resposta é 200 com `por_tipo`/`por_status`/`por_cc` refletindo exatamente os valores gravados na tabela, sem nenhuma consulta a `solicitacoes`/tabelas relacionadas.
- Given `painel_snapshots` vazia, when o usuário abre o painel, then a resposta é 200 com os 3 arrays vazios e `gerado_em: null` (nunca erro).
- Given um nome de painel diferente de `"consolidado"`, when o `GET` é feito, then a resposta é 404.
- Given um usuário sem sessão válida, when ele acessa `/painel` no frontend, then é redirecionado para `/login` antes de qualquer chamada à API (mesmo padrão de `Home`).

## Design Notes

**Por que uma única tabela `painel_snapshots` com coluna `dimensao`, em vez de 3 tabelas:** o Epic 5 Context e o PRD citam `painel_snapshots` (singular) como o nome já previsto no contrato de API original (`02-REQUISITOS-NEGOCIO-ENVIO-TI.md`); uma tabela genérica `(painel, dimensao, chave, rotulo, quantidade, gerado_em)` cobre as 3 dimensões do escopo mínimo (tipo/status/cc) sem comprometer esse nome, e já é extensível a um futuro segundo `painel` (rota `/paineis/{painel}`) sem migration adicional de schema.

**Por que o cálculo/preenchimento de `painel_snapshots` não entra nesta story:** nenhum artefato de planejamento especifica o trigger (síncrono a cada mudança de status? job periódico? manual?) — decisão de negócio/arquitetura explicitamente aberta (PRD §7.3, "Escopo a Definir com o Negócio", Questão Aberta 14). A própria AC do Epic 5 (epics.md) já assume snapshots "já calculados" como precondição, não como comportamento sob teste desta story. Até essa decisão ser tomada, o painel em produção mostrará os 3 arrays vazios — comportamento correto e testado (ver I/O Matrix), não um bug.

**Por que `rotulo` é denormalizado em `painel_snapshots` em vez de o handler fazer `JOIN` com `centros_custo`:** a regra do Architecture Spine ("Handlers de Painéis leem exclusivamente de `painel_snapshots`") existe para impedir qualquer consulta cruzada no caminho de leitura do painel; quem grava o snapshot (job futuro, fora desta story) resolve o nome legível uma única vez na gravação, não a cada leitura.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l . && go test ./... -count=1` -- expected: compila sem erro, sem diffs de `gofmt`, todos os pacotes passam incluindo `handlers/painel_test.go`.
- `cd frontend && npm run build && npm run lint` -- expected: build e lint sem erro.

**Manual checks (if no CLI):**
- `docker-compose up` e confirmar que `015_painel_snapshots.sql` roda sem erro; inserir manualmente algumas linhas de teste em `painel_snapshots` e confirmar que `/painel` no frontend renderiza os 3 agrupamentos.

## Auto Run Result

**Recuperação manual (2026-10-09) — sessão bmad-loop interrompida por stall (limite de uso) antes de rodar sua própria revisão ou commitar.** Implementação já estava completa ao parar (primeira story do projeto a tocar o frontend além do scaffold de login); nenhum patch de código foi necessário. Verificação própria cobriu:
- Backend: `go build`/`go vet`/`gofmt -l`/`go test ./... -count=1` — todos OK, incluindo os 3 testes novos de `handlers/painel_test.go` (com dados, sem dados, painel desconhecido).
- Frontend: `npx tsc --noEmit` (sem erros de tipo), `npm run build` (sucesso), `npm run lint` (0 erros — 1 warning pré-existente em `AuthContext.tsx`, não relacionado a esta story). `npm run test` confirma a ausência conhecida de ambiente de teste de componente no frontend (nenhum arquivo de teste existe no projeto); a spec já previa isso no próprio comando de Verification (só `build && lint`, nunca `test`), então não é uma lacuna introduzida por esta story.
- Leitura de `painel.go`/`015_painel_snapshots.sql`/`PainelConsolidado.tsx`/`App.tsx`/`main.go`: todos batem exatamente com o Code Map e os Tasks & Acceptance da spec — rota só-leitura via conexão geral, sem GRANT explícito (herdado de `ALTER DEFAULT PRIVILEGES`), `gerado_em` calculado como o máximo entre as linhas, auth-gate manual replicado de `Home`, nenhuma biblioteca de gráficos adicionada.

Esta é a última story do backlog (Epic 5 é o último epic). Status final `done`.

**Riscos residuais:** nenhum novo. O job que calcula/preenche `painel_snapshots` continua fora do escopo (decisão de negócio/arquitetura aberta, PRD §7.3) — em produção, o painel mostrará os 3 arrays vazios até essa decisão ser tomada, comportamento correto e já testado.
