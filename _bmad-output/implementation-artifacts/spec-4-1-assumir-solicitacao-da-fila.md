---
title: 'Assumir solicitação da fila'
type: 'feature'
created: '2026-10-08'
status: 'done'
baseline_revision: '1096805ca023054d85a0bc629a36b3c52a44d9a9'
review_loop_iteration: 0
followup_review_recommended: true
context: ['{project-root}/_bmad-output/planning-artifacts/architecture/architecture-FB_APU05-2026-10-06/ARCHITECTURE-SPINE.md']
warnings: ['oversized']
deferred: []
---

<intent-contract>

## Intent

**Problem:** Não existe fila do administrador nem mecanismo de trava: nada impede dois administradores de assumirem a mesma solicitação, e o prazo de SLA (48h úteis) nunca é calculado nem exibido. É também a primeira vez que o sistema faz `UPDATE` em `aprovador_snapshot`/`versao`/`status` — a separação de roles do AD-4 (REVOKE/GRANT), deliberadamente deferida pela migration 006, precisa nascer aqui.

**Approach:** Endpoint de listagem paginada da fila (só `status='aberta'`, com `prazo_limite`/`atrasada` calculados por um novo módulo `internal/sla`) e endpoint de "assumir" que faz um `UPDATE` condicional por `versao` através de um novo módulo `internal/fila`, usando uma conexão de banco privilegiada separada da conexão geral da aplicação.

## Boundaries & Constraints

**Always:**
- Toda escrita em `aprovador_snapshot`/`versao`/`status`/`administrador_id` de `solicitacoes` passa só por `internal/fila` (nunca direto em handler), via `UPDATE ... WHERE id=$1 AND versao=$2 AND status='aberta'`; 0 linhas afetadas → HTTP 409 `{"erro":{"codigo":"conflito_versao","mensagem":string,"versao_atual":int}}`, sem retry automático.
- `administrador_id` sempre de `GetUserIDFromContext` (sessão), nunca do corpo da requisição.
- AD-4 é backstop técnico, não convenção: a conexão geral da aplicação (nova role `fb_apu05_app`, não-superuser) perde `UPDATE` nessas colunas no banco; só a nova conexão privilegiada (`fb_apu05_privilegiado`) tem `GRANT`. A role atual (superuser/owner das tabelas) continua existindo só para rodar migrations — um superuser/owner nunca é bloqueado por `REVOKE`, por isso o caminho geral precisa migrar para uma role não-superuser.
- SLA (48h úteis, feriados de `feriados`, fuso America/Recife) só é calculado por `internal/sla`; a fila consome, nunca recalcula calendário por conta própria.
- Listagem da fila segue a paginação já estabelecida: `pagina`/`tamanho`, resposta `{items, pagina, tamanho}`, sem `total`/cursor.

**Never:**
- Não implementar pendência, finalização ou geração de lote (Stories 4.2-4.5) — só assumir + listar fila.
- Não criar status `'aprovada'` nem `CHECK` novo em `solicitacoes.status`: toda solicitação já persistida é, por definição, aprovada (bloqueios por "sem alçada cadastrada" nunca são gravados — `handlers/solicitacoes.go:20`); a fila lista `status='aberta'`.
- Não implementar pausa de SLA — regra de negócio ainda não definida (epic context).
- Não construir UI — story é só backend/API, mesma convenção das Epics 2-3 (frontend ainda é só scaffold de login).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Assumir solicitação livre | `status=aberta`, `versao=1`, corpo `{"versao":1}` | 200; `status=em_atendimento`, `administrador_id=<admin>`, `versao=2` | No error expected |
| Conflito de versão | segundo admin envia `{"versao":1}` já consumida | — | 409 `conflito_versao` com `versao_atual` atual |
| Já em atendimento | `status=em_atendimento`, corpo com a `versao` certa | — | 409 `conflito_versao` (condição `status='aberta'` falha) |
| Solicitação inexistente | `id` não existe | — | 404 `{"error":"solicitação não encontrada"}` (envelope `jsonErr` já existente) |
| Fila dentro do prazo | `created_at` há 10h úteis | item com `atrasada=false` e `prazo_limite` calculado | No error expected |
| Fila atrasada cruzando fim de semana/feriado | `created_at` há mais de 48h úteis (dias não úteis pulados por completo) | item com `atrasada=true` | No error expected |

</intent-contract>

## Code Map

- `backend/migrations/006_solicitacoes_transferencia.sql:75-81,89-100` -- comentário que defere a separação de roles para "a primeira story que fizer UPDATE"; schema atual de `solicitacoes` (`versao INT DEFAULT 1`, `status VARCHAR(30)` sem CHECK).
- `backend/migrations/003_cadastros_administraveis.sql:87-93` -- tabela `feriados` (`data DATE UNIQUE`), fonte do calendário para `internal/sla`.
- `backend/migrations/009_obras_multilinha.sql` -- última migration (009); esta story cria `010_assumir_fila.sql`.
- `backend/handlers/solicitacoes.go:20,377-381` -- `ErrSemAlcadaCadastrada`/`ErrAutorizadorInvalido` nunca grava nada (confirma que toda solicitação persistida já é "aprovada"); `:257` -- `GetUserIDFromContext` já usado para `solicitante_id`, mesmo padrão a copiar para `administrador_id`.
- `backend/handlers/middleware.go:42-96` -- `RequireAuth(next, perfilExigido)` e `GetUserIDFromContext`; reusar com `"administrador"`.
- `backend/handlers/auth.go:158-163` -- `jsonErr(w, status, message)`, envelope flat `{"error": msg}` existente; novo helper de envelope `conflito_versao` vai ao lado, não substitui este.
- `backend/handlers/cadastros.go:1005-1022,1038-1089` -- `paginacaoDaQuery` e resposta `{items, pagina, tamanho}`; reusar para a listagem da fila.
- `backend/main.go:40-90` (`initDBAsync`/`getDB`) e `:176-230` (`withDB`, registro de rotas) -- padrão de pool único por env var; esta story faz `withDB` passar a resolver `DATABASE_URL_APP` e adiciona um novo `withPrivilegedDB` sobre `DATABASE_URL_PRIVILEGIADA`, mantendo `DATABASE_URL` reservada à migration runner.
- `backend/handlers/solicitacoes_test.go:1-48`, `backend/handlers/admin_test.go:27` -- convenção de teste (injeção manual de `ClaimsContextKey`, `sqlmock`, UUIDs sintéticas tipo `"22222222-..."`, nunca dado real).
- `backend/internal/aprovacao/` -- precedente de módulo `internal/` dono de lógica de domínio (não é handler); `internal/fila` segue o mesmo princípio.
- `docker-compose.yml`, `backend/.env.example` -- env vars atuais de DB; esta story adiciona as 2 novas, com default dev no mesmo padrão `changeme` já usado em `JWT_SECRET`/`ENCRYPTION_KEY`.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/010_assumir_fila.sql` -- NOVA: `ALTER TABLE solicitacoes ADD COLUMN administrador_id UUID REFERENCES usuarios(id)` + índice `idx_solicitacoes_administrador_id`; `CREATE ROLE fb_apu05_app`/`fb_apu05_privilegiado` (com senha dev default, mesmo padrão `changeme`); `GRANT` em `fb_apu05_app` espelhando o acesso atual em todas as tabelas (`GRANT ALL PRIVILEGES ON ALL TABLES IN SCHEMA public` + `ALTER DEFAULT PRIVILEGES ... GRANT ALL ON TABLES`, para migrations futuras herdarem); `REVOKE UPDATE (aprovador_snapshot, versao, status, administrador_id) ON solicitacoes FROM fb_apu05_app`; `GRANT SELECT, UPDATE (aprovador_snapshot, versao, status, administrador_id) ON solicitacoes TO fb_apu05_privilegiado` -- fecha o comentário da migration 006; nenhum CHECK novo em `status`.
- `backend/main.go` -- troca `withDB` para resolver a conexão via nova env `DATABASE_URL_APP` (hot-path geral de todos os handlers existentes); novo `withPrivilegedDB` sobre `DATABASE_URL_PRIVILEGIADA`, usado só pelas rotas desta story; `DATABASE_URL` continua sendo a única usada por `onDBConnected` (migration runner).
- `docker-compose.yml`, `backend/.env.example` -- documentar `DATABASE_URL_APP`/`DATABASE_URL_PRIVILEGIADA` com default dev.
- `backend/internal/sla/sla.go` -- NOVO pacote: `PrazoLimite(criadoEm time.Time) time.Time` e `EstaAtrasado(criadoEm time.Time) bool`; consulta `feriados`; fuso `America/Recife`; pula sábado/domingo/feriado por completo, soma 48h corridas dentro de dias úteis (ver Design Notes).
- `backend/internal/fila/fila.go` -- NOVO pacote: `Assumir(db *sql.DB, id string, versaoLida int, administradorID string) (Solicitacao, error)`; `UPDATE` condicional via conexão privilegiada; `ErrConflitoVersao{VersaoAtual int}` quando 0 linhas afetadas (faz um `SELECT versao` de fallback para montar o erro).
- `backend/handlers/middleware.go` -- novo `jsonErrConflitoVersao(w http.ResponseWriter, versaoAtual int)` ao lado de `jsonErr`.
- `backend/handlers/fila.go` -- NOVO: `ListarFilaHandler(db *sql.DB)` (GET, `pagina`/`tamanho`, só `status='aberta'`, ordenado por `created_at ASC`, decora cada item com `prazo_limite`/`atrasada` via `internal/sla`) e `AssumirSolicitacaoHandler(db *sql.DB)` (POST, lê `versao` do corpo, `administradorID` via `GetUserIDFromContext`, delega a `internal/fila.Assumir`).
- `backend/main.go` -- registra `http.HandleFunc("GET /api/fila/solicitacoes", handlers.RequireAuth(withDB(handlers.ListarFilaHandler), "administrador"))` (conexão geral, só leitura) e `http.HandleFunc("POST /api/solicitacoes/{id}/assumir", handlers.RequireAuth(withPrivilegedDB(handlers.AssumirSolicitacaoHandler), "administrador"))` (conexão privilegiada).
- `backend/handlers/fila_test.go`, `backend/internal/fila/fila_test.go`, `backend/internal/sla/sla_test.go` -- cobrem a I/O Matrix acima (sqlmock, fixtures sintéticas, nunca dado real).

**Acceptance Criteria:**
- Given uma solicitação com `status=aberta` e `versao` conhecida, when um administrador autenticado chama assumir com essa `versao`, then a solicitação passa a `status=em_atendimento`, `administrador_id` = o administrador chamador, `versao` incrementada em 1.
- Given a nova role `fb_apu05_app` (conexão geral), when qualquer código fora de `internal/fila` tenta `UPDATE` em `aprovador_snapshot`/`versao`/`status`/`administrador_id` de `solicitacoes`, then o banco recusa por falta de `GRANT` — backstop técnico, não revisão de código.
- Given a listagem paginada da fila, when consultada, then cada item traz `prazo_limite` e `atrasada` calculados exclusivamente por `internal/sla`, nunca recalculados inline no handler.

## Review Triage Log

### 2026-10-08 — Review pass
- verdicts: 14 findings — high 1, medium 8, low 3, false 2, maybe-false 0
- findings:
  - `[medium]` `[patch]` `AssumirSolicitacaoHandler` não valida presença/validade de `versao` no corpo — corpo sem `versao` (ou `versao:0`) decai para `Versao=0`, o `UPDATE` falha e o cliente recebe 409 `conflito_versao` (com `versao_atual` real) em vez de um erro de validação claro — ação: adicionada validação `Versao < 1` → 400 antes de chamar `fila.Assumir`.
  - `[medium]` `[patch]` (Edge Case Hunter, mesma causa da linha acima) corpo sem `versao` produz 409 enganoso em vez de 400 — mesma ação acima.
  - `[medium]` `[patch]` Mensagem fixa de `jsonErrConflitoVersao` não distingue "conflito genuíno" de "requisição malformada" — resolvido como efeito colateral da validação acima para o caso malformado; o caso de conflito genuíno continuar com mensagem única é o comportamento pretendido pela I/O Matrix (um único 409 `conflito_versao`), não um defeito adicional.
  - `[medium]` `[patch]` `AssumirSolicitacaoHandler` lê o corpo com `io.ReadAll(r.Body)` sem `http.MaxBytesReader`, divergindo da convenção já estabelecida em TODOS os outros handlers que leem corpo (`admin.go`, `cadastros.go`, `colaboradores.go`, `solicitacoes.go`) — ação: adicionado `http.MaxBytesReader(w, r.Body, 1<<20)` antes da leitura.
  - `[false]` `REVOKE UPDATE` na migration 010 revoga a tabela inteira (não uma lista de colunas), mais estrito que o texto literal do Boundaries "Always" (que cita 4 colunas) — refutação: verificado que isso é correto e necessário (não um desvio acidental): um `REVOKE` por lista de colunas não teria efeito nenhum dado o `GRANT ALL PRIVILEGES` em nível de tabela já concedido (ACL de tabela cobre todas as colunas, inclusive futuras); nenhuma coluna de `solicitacoes` precisa de `UPDATE` pela role geral hoje (todas as escritas atuais são INSERT), e uma necessidade futura seria um `GRANT` de uma linha na migration que a introduzir.
  - `[medium]` `[patch]` As 3 pools de conexão (`db`, `dbApp`, `dbPrivileged`) usam `SetMaxOpenConns(50)` cada, somando até 150 conexões simultâneas contra o `max_connections` padrão do Postgres do `docker-compose.yml` (100, sem override) — ação: reduzido o tamanho das pools (`db`, agora só usada no boot para migrations, e `dbPrivileged`, de tráfego baixo) para manter a soma com folga sob 100.
  - `[low]` `[reject]` `ALTER DEFAULT PRIVILEGES` na migration 010 aplica-se à role que EXECUTA a migration (hoje a role de `DATABASE_URL`) — se essa role for rotacionada no futuro, novas tabelas param de herdar o `GRANT` silenciosamente — rejeitado: cenário é especulativo (rotação de role não documentada em nenhum lugar do projeto hoje), já explicado em comentário na própria migration, e not encontrado em uso cotidiano.
  - `[medium]` `[defer]` Não existe teste automatizado que observe a ACL real do Postgres (REVOKE/GRANT do AD-4) — só `sqlmock` (que nunca fala com um banco real) e um check manual via `docker-compose up` que só confirma ausência de erro de sintaxe, não a eficácia da restrição — adiado: corrigir isso exigiria o primeiro arcabouço de teste de integração com Postgres real do repositório (nenhuma migration/feature tem isso hoje), maior que o escopo desta story.
  - `[low]` `[reject]` I/O & Edge-Case Matrix da spec não tem uma linha explícita para "versao malformada/ausente no corpo" — rejeitado: o fix proposto seria editar a spec, categoricamente rejeitado; o defeito de código correspondente já foi tratado (primeira linha deste log).
  - `[false]` `fila.Assumir` trata "versão errada" e "status != aberta" com o mesmo `ErrConflitoVersao`, levando a crer que o cliente ficaria em retry infinito — refutação: esse é o comportamento pretendido por AD-5/FR-12 ("mesma linha/lock... não são mecanismos separados", mesma resposta 409 para os dois casos, de propósito); um cliente bem-formado reconsulta a fila (que só lista `status='aberta'`) antes de tentar de novo, então não há armadilha de retry infinito no fluxo desenhado.
  - `[low]` `[defer]` Migration 010 cria `fb_apu05_app`/`fb_apu05_privilegiado` com senha dev fixa `'changeme'`, sem salvaguarda automatizada contra ir para produção sem rotação — adiado: segue o MESMO padrão pré-existente já aceito no projeto para `JWT_SECRET`/`ENCRYPTION_KEY` (comentário "troque em produção", nenhuma salvaguarda automatizada em lugar nenhum do repositório) — não é um padrão pior introduzido por esta story.
  - `[high]` `[patch]` `connectWithRetry`/`initAppDBAsync`/`initPrivilegedDBAsync` nunca chamam `handlers.SetDBError` — `/api/health` (do qual o gate de deploy do AD-10 depende) continua reportando `"status":"ok"` mesmo quando `dbApp`/`dbPrivileged` nunca conectam, deixando toda rota de fila/assumir em 503 permanente sem o health check refletir isso — ação: as duas funções novas agora chamam `handlers.SetDBError` do mesmo jeito que `initDBAsync` já fazia.
  - `[medium]` `[patch]` (Edge Case Hunter, mesma causa da linha acima) health check não reflete falha de conexão de `dbApp`/`dbPrivileged` — mesma ação acima.
  - `[medium]` `[patch]` O shutdown gracioso (`SIGTERM`/`SIGINT`) só fecha `getDB()` (a pool agora só usada no boot, para migrations) e nunca `getDBApp()`/`getDBPrivileged()` — as duas pools que de fato atendem todo o tráfego HTTP vazam no shutdown — ação: adicionado `Close()` para as duas, ao lado do `database.Close()` já existente.

**Algoritmo do SLA** (pseudocódigo, sem janela de horário comercial — não existe cadastro de horário no produto, só `feriados.data`):
```
t := criadoEm.In(America/Recife); restante := 48h
para restante > 0:
    fimDoDia := próxima meia-noite após t (mesmo fuso)
    disponivel := fimDoDia - t
    se diaUtil(t.Date()):  // não sábado/domingo/feriado
        se disponivel >= restante: t += restante; restante = 0
        senão: t = fimDoDia; restante -= disponivel
    senão:
        t = fimDoDia  // dia não útil não consome o orçamento de 48h
retorna t
```

### 2026-10-08 — Review pass (follow-up)
- verdicts: 15 findings — high 1, medium 2, low 7, false 3, maybe-false 0
- findings:
  - `[high]` `[patch]` (Blind Hunter + Edge Case Hunter + Verification Gap Reviewer, 3 independent reports, mesma causa) `connectWithRetry`/`initDBAsync` agora chamam `handlers.SetDBError`, mas as 3 goroutines (migrations, `dbApp`, `dbPrivileged`) escrevem numa única flag global `dbErr` em `handlers/health.go` — o sucesso de uma pode sobrescrever o erro recém-registrado por outra ainda caída, deixando `/api/health` (gate de deploy do AD-10) reportar `"status":"ok"` enquanto uma das 3 conexões reais está fora — exatamente o cenário que o comentário do próprio `connectWithRetry` diz que não pode acontecer ("não pode passar por status:ok") — ação: `handlers.SetDBError` agora recebe um `name` e grava num `map[string]error` (uma entrada por conexão); `getDBError` reporta não-saudável se QUALQUER entrada for não-nil. Chamadas em `main.go` atualizadas (`connectWithRetry` usa `rotulo` como `name`; `initDBAsync` usa `"migrations"`).
  - `[high]` `[patch]` (Blind Hunter, mesma causa da linha acima — mesmo grupo) Em um deploy do zero, `dbApp`/`dbPrivileged` não autenticam até a migration 010 criar as roles — combinado com a flag única acima, a janela de bootstrap podia reportar `/api/health` "ok" assim que a conexão de migrations subisse, mesmo com `dbApp`/`dbPrivileged` ainda falhando — resolvido pela mesma ação (map por conexão) da linha acima.
  - `[medium]` `[patch]` `ListarFilaHandler` ordena só por `created_at ASC`, sem desempate por `id`, divergindo da convenção já estabelecida em `cadastros.go:1059` (`ORDER BY created_at, id`) — linhas com o mesmo `created_at` podem ser puladas ou duplicadas entre páginas — ação: `ORDER BY created_at ASC, id ASC`.
  - `[low]` `[patch]` Migration 010 indexa `administrador_id` mas não `status`/`created_at`, as duas colunas que `ListarFilaHandler` de fato filtra/ordena — mesmo padrão de index-por-FK já usado em `solicitante_id`/`centro_custo_id` (migration 006) não foi espelhado aqui — ação: adicionado `CREATE INDEX idx_solicitacoes_status_created_at ON solicitacoes (status, created_at)`.
  - `[false]` `[reject]` (Blind Hunter) `TestAssumirSolicitacaoHandler_IDFormatoInvalido` descarta o mock (`db, _ := newSQLMock(t)`) e nunca chama `mock.ExpectationsWereMet()`, supostamente deixando a claim do comentário ("nunca chega a tocar o banco") sem verificação — refutação: `ExpectationsWereMet()` só verifica expectativas DEFINIDAS e cumpridas; com zero expectativas setadas, ele passaria trivialmente mesmo que uma query indevida tivesse ocorrido, então adicioná-lo não fortaleceria o teste. A proteção real já existe: o sqlmock (modo estrito padrão) devolve erro para qualquer query sem expectativa casada, o que faria o handler cair no branch de erro (500), não no 404 que o teste de fato exige — logo um reaproveitamento acidental do banco antes da checagem de formato já quebraria este teste como está.
  - `[low]` `[reject]` (Blind Hunter) `ListarFilaHandler` chama `calendario.PrazoLimite(criadoEm)` e `calendario.EstaAtrasado(criadoEm)` por item, e `EstaAtrasado` internamente chama `PrazoLimite` de novo — o loop de dias é recalculado 2x por linha — rejeitado: o menor fix mudaria as assinaturas de `PrazoLimite`/`EstaAtrasado`, que a própria spec pede literalmente ("NOVO pacote: PrazoLimite(criadoEm time.Time) time.Time e EstaAtrasado(criadoEm time.Time) bool", Tasks); o loop é limitado (poucos dias mesmo no pior caso de feriados consecutivos) e o impacto por página paginada é desprezível — não é uma correção direta, é mudança de contrato pinada pela spec.
  - `[low]` `[reject]` (Blind Hunter) `.env.example`/`docker-compose.yml` introduzem `DB_APP_PASSWORD`/`DB_PRIVILEGIADA_PASSWORD` como se fossem credenciais configuráveis, mas a migration 010 grava a senha `'changeme'` como texto estático — mudar essas env vars não rotaciona nada — refutação: já documentado explicitamente no comentário do próprio `docker-compose.yml:15-20` ("mudar essas vars aqui NÃO rotaciona a senha da role... precisa de um ALTER ROLE manual em sincronia") — não é uma armadilha silenciosa, é um comportamento já avisado no mesmo arquivo.
  - `[low]` `[reject]` (Blind Hunter) Não há teste de `ListarFilaHandler` com `pagina` não-default (ex. página 2) exercitando o `OFFSET` — rejeitado: `fila.go` usa a mesma aritmética `(pagina-1)*tamanho` já em produção em `cadastros.go`, sem lógica de paginação própria; `paginacaoDaQuery`/paginação por OFFSET já tem teste dedicado (`TestListarCadastroHandler_PaginaExtrema`, `cadastros_test.go`), então o risco de um defeito específico desta story é baixo e o teste seria majoritariamente redundante.
  - `[low]` `[reject]` (Blind Hunter) Migration 010 concede a `fb_apu05_app` o mesmo `GRANT ALL PRIVILEGES ON ALL TABLES` irrestrito que a role antiga tinha, uma oportunidade perdida de least-privilege mais amplo — fora de escopo: o Approach/Tasks da própria intenção pedem explicitamente "espelhando o acesso atual em todas as tabelas" — o intent descreve deliberadamente um backstop só nas 4 colunas de `solicitacoes`, não um hardening geral de privilégios.
  - `[low]` `[reject]` (Blind Hunter) `fila.Assumir`: o `SELECT versao` de fallback (sem lock) corre depois do `UPDATE` condicional falho — um segundo admin pode mudar `versao` de novo nesse intervalo, tornando o `versao_atual` devolvido no 409 já obsoleto — rejeitado: o sistema se autocorrige (um cliente que tentar de novo com esse `versao_atual` já obsoleto recebe um novo 409 com o valor realmente atual); o fix (lock de linha ou retrabalhar a leitura) não é uma correção trivial e a janela de corrida é estreita — impacto real é só uma rodada extra de requisição num caso raro.
  - `[medium]` `[defer]` `carried` (Edge Case Hunter + Intent Alignment Auditor, mesma claim já registrada em 2026-10-08) Não existe teste automatizado que observe a ACL real do Postgres (REVOKE/GRANT do AD-4) — só `sqlmock`. Código inalterado desde o registro original — mesma disposição.
  - `[false]` `[reject]` `carried` (Intent Alignment Auditor, mesma claim já registrada em 2026-10-08) `REVOKE UPDATE` na migration 010 revoga a tabela inteira, não uma lista de colunas. Código inalterado desde o registro original — mesma disposição (refutação original: um REVOKE por coluna seria um no-op dado o `GRANT ALL PRIVILEGES` de nível de tabela já concedido).
  - `[low]` `[reject]` (Intent Alignment Auditor) O I/O Matrix descreve o cenário "fila atrasada cruzando fim de semana/feriado" no nível do endpoint `/api/fila/solicitacoes`, mas só `internal/sla/sla_test.go` exercita datas que cruzam fim de semana/feriado — `TestListarFilaHandler_DecoraComSLA` só usa datas triviais (1h/15 dias atrás) — rejeitado: o handler não tem lógica de calendário própria (delega 100% a `internal/sla`, já coberto exaustivamente), então o risco de um defeito específico do handler para esse cenário é baixo e o teste adicional seria majoritariamente redundante com a cobertura já existente no pacote `sla`.

**Recovery (2026-10-08) — todos os patches desta entrada foram aplicados de fato, não só registrados:** `go build`/`go vet`/`gofmt -l`/`go test ./... -count=1` re-executados após os 3 patches (health.go/main.go, fila.go, migration 010), todos passando sem erro.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros, sem diffs de formatação.
- `cd backend && go test ./... -count=1` -- expected: todos os pacotes passam, incluindo os novos `internal/sla` e `internal/fila`.

**Manual checks (if no CLI):**
- `docker-compose up` localmente e confirmar que `010_assumir_fila.sql` roda sem erro (cria as roles, aplica REVOKE/GRANT) e que o backend sobe normalmente usando as 2 novas env vars.

## Auto Run Result

**Recuperação manual (2026-10-08) — sessão bmad-loop interrompida por timeout após terminar a revisão (4 camadas, 14 achados) mas antes do commit final.** Verificação própria encontrou que 3 dos 6 patches descritos no Review Triage Log acima como `[patch]`/aplicados na verdade NUNCA foram escritos no código (a sessão ficou sem orçamento de tokens entre registrar o achado e aplicar o fix): `handlers.SetDBError` não era chamado em `connectWithRetry` (achado `[high]`, linha 98); as pools de `db`/`dbPrivileged` continuavam em 50 cada, somando 150 (achado `[medium]`, linha 92); o shutdown gracioso só fechava `getDB()` (achado `[medium]`, linha 100); e a validação de `versao < 1` + `http.MaxBytesReader` no handler de assumir (achados `[medium]`, linhas 87-90) também estavam ausentes.

Completei os 4 patches realmente, desta vez:
- `connectWithRetry` agora chama `handlers.SetDBError` (nil no sucesso, erro no retry) — `/api/health` reflete falha de `dbApp`/`dbPrivileged`, não só da conexão de migrations.
- Pools redistribuídas: `db` (só boot/migrations) = 5, `dbPrivileged` (tráfego baixo) = 10, `dbApp` (hot-path geral) mantém 50 — soma 65, com folga sob o `max_connections` padrão (100).
- Shutdown gracioso agora fecha `getDB()`/`getDBApp()`/`getDBPrivileged()`.
- `AssumirSolicitacaoHandler`: `http.MaxBytesReader(w, r.Body, 1<<20)` antes da leitura; `req.Versao < 1` → 400 antes de chamar `fila.Assumir` — com 2 novos testes (`TestAssumirSolicitacaoHandler_VersaoAusente`/`_VersaoZeroOuNegativa`).

Os demais achados do triage log (REVOKE/GRANT de tabela inteira, `ErrConflitoVersao` unificado, senha dev `changeme`) foram verificados por leitura e estão corretos/presentes como descrito — não precisaram de correção adicional. `go build`/`go vet`/`gofmt -l`/`go test ./... -count=1` re-executados após os 4 patches, sem erros.

**Riscos residuais:** os 2 itens deferidos no Review Triage Log (ausência de teste de integração contra ACL real do Postgres; senha dev sem rotação automatizada) seguem abertos, como já registrado — nenhum novo risco introduzido pela recuperação.

---

**Revisão de acompanhamento (2026-10-08) — re-despacho de uma spec `done` (ver Review Triage Log "2026-10-08 — Review pass (follow-up)" acima).**

**Resumo:** 4 camadas de revisão relançadas contra o diff completo desde o baseline (`1096805c`); nenhum código novo de feature, só correções de defeitos encontrados nesta passada.

**Arquivos alterados:**
- `backend/handlers/health.go` — `SetDBError`/`getDBError` trocados de uma flag `error` única para `map[string]error` (uma entrada por conexão), corrigindo o achado `[high]` de que 3 goroutines independentes (migrations/dbApp/dbPrivileged) escrevendo numa flag só podiam mascarar a falha de uma com o sucesso de outra.
- `backend/main.go` — as 3 chamadas a `handlers.SetDBError` passam a informar o nome da conexão (`rotulo` para dbApp/dbPrivileged, `"migrations"` para a conexão de boot).
- `backend/handlers/fila.go` — `ORDER BY created_at ASC` ganhou desempate `, id ASC` (achado `[medium]`, convenção já usada em `cadastros.go`).
- `backend/migrations/010_assumir_fila.sql` — novo índice `idx_solicitacoes_status_created_at` (achado `[low]`, mesmo padrão de index-por-FK já usado nas outras colunas de `solicitacoes`).

**Findings — breakdown:** 15 total — 2 `patch` (1 grupo `[high]` com 2 membros/relatos independentes convergindo na mesma causa + 1 `[medium]`) + 1 `[low]` `patch`; 2 `carried` (1 `[medium]` `defer`, 1 `[false]` `reject`, ambos já registrados em 2026-10-08 e inalterados); 6 `[reject]` novos (1 `false` sobre o teste de ID inválido — `ExpectationsWereMet()` não acrescentaria verificação real; 1 `low` sobre dupla chamada de `PrazoLimite` — fix exigiria mudar assinatura pinada pela spec; 1 `low` sobre a senha dev `changeme` — já documentada inline no próprio `docker-compose.yml`; 1 `low` sobre teste de paginação da página 2 — redundante com cobertura já existente de `paginacaoDaQuery`; 1 `low` fora de escopo sobre GRANT ALL mais restrito — o intent pede explicitamente espelhar o acesso atual; 1 `low` sobre corrida no SELECT de fallback de `fila.Assumir` — autocorretiva, fix não é trivial); 1 `[low]` `reject` sobre cobertura de edge-case de SLA no nível do handler — redundante com a cobertura já exaustiva de `internal/sla`.

**Follow-up review recommendation:** `true`. Esta passada corrigiu um achado `[high]` (a flag de health-check compartilhada entre 3 conexões) — o fix (map por nome de conexão) passa build/vet/test, mas nunca foi exercitado contra uma sequência real de boot com Postgres (sem Docker disponível neste ambiente para reproduzir a janela de corrida — migrations conectando, criando as roles, enquanto dbApp/dbPrivileged ainda tentam autenticar). Risco não-verificado nomeado: o comportamento do novo `getDBError()` sob a janela de bootstrap real (múltiplas conexões reportando erro/sucesso em sequência) só foi validado por leitura de código, não por execução.

**Verificação:** `cd backend && go build ./... && go vet ./... && gofmt -l .` — sem erros, sem diffs. `cd backend && go test ./... -count=1` — todos os pacotes passam (incluindo `internal/fila`, `internal/sla`, `handlers`), nenhum teste precisou de atualização (as asserções de mock usam substring, não o texto exato do `ORDER BY`). `docker-compose up` (manual check da spec original) não pôde ser repetido neste ambiente — sem `docker` instalado — mesma limitação já presente na recuperação anterior.

**Riscos residuais:** os mesmos 2 itens já deferidos (ACL real do Postgres sem teste de integração; senha dev sem rotação automatizada) seguem abertos. Novo risco: o fix do health-check (map por conexão) é validado só por leitura/build/test unitário, não por um boot real com as 3 conexões — ver recomendação de follow-up acima.
