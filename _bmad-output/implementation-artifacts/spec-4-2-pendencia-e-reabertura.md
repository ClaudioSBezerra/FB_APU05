---
title: 'Pendência e reabertura'
type: 'feature'
created: '2026-10-08'
status: 'done'
baseline_revision: 'b4cf4d850e072cae360a5a20ef708cc7e57e04a3'
review_loop_iteration: 0
followup_review_recommended: false
context: ['{project-root}/_bmad-output/planning-artifacts/architecture/architecture-FB_APU05-2026-10-06/ARCHITECTURE-SPINE.md']
warnings: ['oversized']
deferred:
  - summary: >-
      Os testes de internal/fila.Comentar/MarcarPendencia usam sqlmock, que nunca
      executa o SQL real contra um Postgres — o CASE de reabertura (o mecanismo
      central desta story) nunca é verificado contra um banco de verdade.
    evidence: |-
      mock.ExpectQuery(...).WillReturnRows(...) fixa o resultado pós-transição
      independente do texto SQL executado; um CASE invertido (ex. fechar ativas
      em vez de reabrir encerradas) passaria TestComentar_ReabreDePendente,
      TestComentar_StatusJaAtivoPermanece e o teste de handler equivalente sem
      detecção. Resolver isso exige o primeiro arcabouço de teste de integração
      com Postgres real do repositório (nenhuma feature/migration tem isso hoje)
      — mesma categoria já deferida no triage log da Story 4.1 para a ACL real
      do AD-4. SQL conferido manualmente por leitura nesta passada: válido e
      correto.
    location: >-
      backend/internal/fila/fila.go (Comentar, MarcarPendencia)
    severity: medium
---

<intent-contract>

## Intent

**Problem:** Depois de assumir uma solicitação (Story 4.1), o administrador não tem como devolvê-la ao solicitante quando falta algo, e não existe nenhuma estrutura de comentário/histórico em `solicitacoes` nem endpoint para o solicitante interagir com uma solicitação já aberta.

**Approach:** Nova tabela `solicitacao_comentarios` + 2 novas escritas em `internal/fila` (mesma linha/lock do AD-4): `MarcarPendencia` (administrador dono: `em_atendimento`→`pendente`) e `Comentar` (solicitante dono: insere comentário e, por uma única expressão `CASE`, devolve o status a `em_atendimento` sempre que ele não for `aberta`/`em_atendimento` — cobre tanto a resposta a uma pendência quanto a reabertura automática de uma solicitação finalizada, já que nenhum status `finalizada` existe ainda no sistema). Mais um `GET` de detalhe para expor o histórico e a `versao` atual a quem é dono da solicitação.

## Boundaries & Constraints

**Always:**
- As duas novas escritas em `status`/`versao` de `solicitacoes` passam por `internal/fila` (nunca direto no handler), via conexão privilegiada (`withPrivilegedDB`), mesmo UPDATE condicional por `versao` do AD-4; 0 linhas afetadas → SELECT de fallback distingue 404 (id inexistente) → 403 (`administrador_id`/`solicitante_id` não é o do chamador, `jsonErr` com `http.StatusForbidden`, mesmo padrão de `solicitacoes.go:289`) → 409 `conflito_versao` (versão obsoleta, ou para pendência, status já não é `em_atendimento`).
- `administrador_id`/comentarista sempre de `GetUserIDFromContext`, nunca do corpo.
- `Comentar` NÃO exige `status='em_atendimento'` no `WHERE` (precisa funcionar em qualquer status) — só `id`+`versao`+`solicitante_id`; a reabertura é por exclusão (`CASE WHEN status IN ('aberta','em_atendimento') THEN status ELSE 'em_atendimento' END`), nunca por um literal `'finalizada'` que não existe no código.
- Toda escrita em `solicitacao_comentarios` ocorre na MESMA transação do UPDATE em `solicitacoes` (atomicidade: comentário nunca existe sem a transição de status correspondente, e vice-versa).
- Migration `011` dá `GRANT SELECT, INSERT ON solicitacao_comentarios TO fb_apu05_privilegiado` explicitamente (esta role só tem o que é concedido linha a linha — ao contrário de `fb_apu05_app`, que já herda `ALTER DEFAULT PRIVILEGES` da migration 010).

**Never:**
- Não criar o status literal `'finalizada'` nem implementar Stories 4.3-4.5 (geração de lote, finalização) — a reabertura trata qualquer status fora de `{aberta, em_atendimento}` como "encerrado", sem saber o nome exato que a Story 4.5 vai usar.
- Não limpar `administrador_id` em nenhuma transição desta story — pendência e reabertura mantêm o mesmo administrador (continuidade do histórico de idas e vindas).
- Não construir UI — story é só backend/API, mesma convenção das Epics 2-4.
- Não adicionar `CHECK` de tamanho em `solicitacao_comentarios.texto` — só validação de não-vazio no handler (mesmo nível de rigor de outros campos de texto livre do projeto).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Pendência com sucesso | `status=em_atendimento`, `administrador_id`=chamador, corpo `{"versao":N,"comentario":"texto"}` | 200; `status=pendente`, `versao=N+1`; comentário `tipo=pendencia` no histórico | No error expected |
| Pendência por admin errado | `administrador_id` da linha ≠ chamador | — | 403 |
| Pendência com status/versão incompatível | `status=aberta` OU `versao` obsoleta | — | 409 `conflito_versao` com `versao_atual` |
| Comentário reabre de pendente | `status=pendente`, solicitante dono, corpo `{"versao":N,"comentario":"texto"}` | 200; `status=em_atendimento`, `versao=N+1`; comentário `tipo=comentario` | No error expected |
| Comentário reabre de status "encerrado" simulado | `status='qualquer-coisa-fora-de-aberta-em_atendimento'` (simula finalizada, inexistente ainda) | 200; `status=em_atendimento` | No error expected |
| Comentário em status já ativo | `status=aberta` ou `em_atendimento` | 200; `status` inalterado, `versao=N+1`, comentário registrado | No error expected |
| Comentário por quem não é o solicitante dono | `solicitante_id` da linha ≠ chamador | — | 403 |
| Comentário sem texto (vazio/whitespace) | corpo `{"versao":N,"comentario":"  "}` | — | 400 |
| GET detalhe por dono (solicitante ou administrador) | id existente | 200; `{id,status,versao,solicitante_id,administrador_id,comentarios:[...]}` ordenado por `created_at ASC` | No error expected |
| GET detalhe por terceiro | chamador não é `solicitante_id` nem `administrador_id` | — | 403 |

</intent-contract>

## Code Map

- `backend/internal/fila/fila.go:11-71` -- `Solicitacao`, `ErrNaoEncontrada`, `ErrConflitoVersao`, `Assumir` (padrão UPDATE condicional + SELECT de fallback a replicar); adiciona aqui `ErrNaoAutorizado`, `MarcarPendencia`, `Comentar`.
- `backend/handlers/fila.go:1-26,99-182` -- cabeçalho do pacote (atualizar comentário para citar esta story) e `AssumirSolicitacaoHandler` (padrão exato de leitura de corpo/validação/erro a replicar); `uuidFormatRegexp` vem de `handlers/admin.go:35`.
- `backend/handlers/middleware.go:43,90,99-110` -- `RequireAuth`, `GetUserIDFromContext`, `jsonErrConflitoVersao`; `backend/handlers/auth.go:159` -- `jsonErr`; `backend/handlers/solicitacoes.go:289` -- precedente de `jsonErr(w, http.StatusForbidden, ...)` para erro de dono.
- `backend/migrations/010_assumir_fila.sql` -- última migration; esta story cria `011_pendencia_e_reabertura.sql`; segue o mesmo padrão de `GRANT`/índice por FK.
- `backend/main.go:283-345` -- `withDB`/`withPrivilegedDB` e bloco de registro de rotas (`http.HandleFunc(...)`); novas rotas entram depois da linha 345 (fila/Story 4.1).
- `backend/handlers/fila_test.go`, `backend/internal/fila/fila_test.go` -- convenção de teste (sqlmock com `regexp.QuoteMeta`, UUIDs sintéticas por arquivo, claims injetadas manualmente via `ClaimsContextKey`).

## Tasks & Acceptance

**Execution:**
- `backend/migrations/011_pendencia_e_reabertura.sql` -- NOVA: `CREATE TABLE solicitacao_comentarios (id UUID PK gen_random_uuid(), solicitacao_id UUID NOT NULL REFERENCES solicitacoes(id), autor_id UUID NOT NULL REFERENCES usuarios(id), tipo VARCHAR(20) NOT NULL CHECK IN ('pendencia','comentario'), texto TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT now())` + índice `(solicitacao_id, created_at)` + `GRANT SELECT, INSERT ON solicitacao_comentarios TO fb_apu05_privilegiado` -- `fb_apu05_app` já herda `ALL` via `ALTER DEFAULT PRIVILEGES` da migration 010.
- `backend/internal/fila/fila.go` -- `ErrNaoAutorizado` (sentinela); `MarcarPendencia(db, id, versaoLida, administradorID, comentario) (Solicitacao, error)` -- transação: `UPDATE ... SET status='pendente', versao=versao+1 WHERE id=$1 AND versao=$2 AND status='em_atendimento' AND administrador_id=$3` + `INSERT ... tipo='pendencia'`; 0 linhas → fallback `SELECT administrador_id, status, versao` para escolher `ErrNaoEncontrada`/`ErrNaoAutorizado`/`ErrConflitoVersao`. `Comentar(db, id, versaoLida, solicitanteID, comentario) (Solicitacao, error)` -- transação: `UPDATE ... SET status = CASE WHEN status IN ('aberta','em_atendimento') THEN status ELSE 'em_atendimento' END, versao=versao+1 WHERE id=$1 AND versao=$2 AND solicitante_id=$3` + `INSERT ... tipo='comentario'`; mesmo fallback (sem checagem de `status` aqui).
- `backend/handlers/pendencia.go` -- NOVO: `MarcarPendenciaHandler(db)` (POST, corpo `{versao,comentario}`, administrador via `GetUserIDFromContext`, valida `versao>=1` e `comentario` não-vazio após `TrimSpace` → 400, delega a `fila.MarcarPendencia`); `ComentarSolicitacaoHandler(db)` (POST, mesmo corpo/validação, solicitante via contexto, delega a `fila.Comentar`); `ObterSolicitacaoHandler(db)` (GET, `SELECT` direto de `solicitacoes` + `solicitacao_comentarios` ordenado por `created_at ASC`, 403 se chamador não é `solicitante_id` nem `administrador_id`, 404 se id não existe). Os 3 tratam `fila.ErrNaoAutorizado` → 403, `fila.ErrConflitoVersao` → `jsonErrConflitoVersao`, `fila.ErrNaoEncontrada` → 404, formato UUID inválido → 404 (mesmo padrão de `fila.go:126`).
- `backend/main.go` -- registra `POST /api/solicitacoes/{id}/pendencia` (`RequireAuth(withPrivilegedDB(handlers.MarcarPendenciaHandler), "administrador")`), `POST /api/solicitacoes/{id}/comentarios` (`RequireAuth(withPrivilegedDB(handlers.ComentarSolicitacaoHandler), "")`), `GET /api/solicitacoes/{id}` (`RequireAuth(withDB(handlers.ObterSolicitacaoHandler), "")`).
- `backend/handlers/pendencia_test.go`, `backend/internal/fila/fila_test.go` (extensão) -- cobrem a I/O Matrix acima.

**Acceptance Criteria:**
- Given uma solicitação `em_atendimento` assumida por um administrador, when ele marca pendência com um comentário, then a solicitação fica `pendente` e o comentário aparece no histórico retornado pelo `GET` de detalhe, com `administrador_id` inalterado.
- Given uma solicitação cujo status não é `aberta` nem `em_atendimento` (simula já finalizada, já que esse status real ainda não existe), when o solicitante dono comenta, then o status volta a `em_atendimento` automaticamente, mantendo o `administrador_id` anterior.

## Spec Change Log

## Review Triage Log

### 2026-10-08 — Review pass
- verdicts: 19 findings — high 0, medium 4, low 12, false 3, maybe-false 0
- findings:
  - `[medium]` `[patch]` (Edge Case Hunter) `fallbackMarcarPendencia` (`internal/fila/fila.go`) devolve `ErrNaoAutorizado` (403) quando `administrador_id` é `NULL` (solicitação `status=aberta`, nunca assumida) — contradiz a própria I/O Matrix da spec ("Pendência com status/versão incompatível: status=aberta... 409 conflito_versao"), já que uma linha `aberta` nunca tem dono para "não bater" — ação: invertida a checagem para `if donoID.Valid && donoID.String != administradorID` (só 403 quando HÁ um dono diferente; `NULL` cai no 409 de versão/status). Adicionado teste cobrindo o caso real (`administrador_id=NULL`, `status=aberta`).
  - `[medium]` `[patch]` (Edge Case Hunter, mesma causa da linha acima, 2º relato independente convergindo) mesma claim, mesma ação.
  - `[medium]` `[defer]` (Blind Hunter) os testes de `Comentar`/`MarcarPendencia` usam `sqlmock`, que nunca executa o SQL real — o `CASE WHEN status IN (...) THEN status ELSE 'em_atendimento' END` (o mecanismo central desta story) nunca é verificado contra um Postgres real; um `CASE` invertido passaria os testes atuais sem detecção — adiado: mesma categoria já registrada e deferida no triage log da Story 4.1 ("ausência de teste de integração contra ACL real do Postgres... só sqlmock"), exigiria o primeiro arcabouço de teste de integração do repositório, fora do escopo desta story. SQL conferido manualmente por leitura: sintaticamente válido e semanticamente correto.
  - `[medium]` `[defer]` (Verification Gap Reviewer, mesma causa da linha acima) achado pré-verificado pela própria camada (leu os testes, confirmou que `WillReturnRows` fixa o resultado pós-transição independente do SQL real executado) — mesma disposição `defer` já registrada pela camada.
  - `[low]` `[patch]` (Blind Hunter) comentário da migration 011 ("Sem UPDATE/DELETE... histórico imutável") sugere imutabilidade garantida no banco, mas `fb_apu05_app` herda `ALL PRIVILEGES` nesta tabela nova via `ALTER DEFAULT PRIVILEGES` (migration 010) — nenhum `REVOKE` foi adicionado para essa role, então a garantia não é tecnicamente aplicada, só convencional (nenhum código hoje escreve UPDATE/DELETE) — ação: comentário reescrito para não sugerir um backstop técnico que não existe.
  - `[low]` `[patch]` (Blind Hunter) `GRANT SELECT, INSERT ON solicitacao_comentarios TO fb_apu05_privilegiado` concede `SELECT` que nenhum código usa (o único `SELECT` desta tabela é `ObterSolicitacaoHandler`, na conexão geral) — ação: reduzido para `GRANT INSERT`.
  - `[low]` `[patch]` (Blind Hunter) `administradorIDOuNil` (`pendencia.go`) duplica a lógica de `nullStringOuNil` (`fila.go`) só para aceitar uma string simples em vez de `sql.NullString` — ação: `administradorIDOuNil` agora delega a `nullStringOuNil`, sem duplicar a condição.
  - `[low]` `[patch]` (Blind Hunter) `pendencia.go` loga com a tag `"[Pendencia]"` enquanto o próprio cabeçalho do arquivo descreve esta story como continuação do MESMO Writer path de `fila.go` (tag `"[Fila]"`) — dificulta rastrear o ciclo assumir→pendência→comentário nos logs com uma tag só — ação: unificado para `"[Fila]"`.
  - `[low]` `[reject]` (Blind Hunter) `ComentarSolicitacaoHandler` não tem teste de handler dedicado para o 409 de versão obsoleta (só `internal/fila`) — rejeitado: `tratarErroEscritaFila` é a MESMA função já exercitada pelo teste equivalente de `MarcarPendenciaHandler` (`TestMarcarPendenciaHandler_StatusIncompativel`); risco de regressão não detectada é desprezível, e o fix (novo teste) é mais que uma correção direta.
  - `[low]` `[reject]` (Blind Hunter) linha "Comentário em status já ativo" da I/O Matrix não tem teste de handler dedicado (só `internal/fila/TestComentar_StatusJaAtivoPermanece`) — rejeitado: a linha já está coberta por pelo menos um teste (regra do Matrix Test Audit do step-03), o handler é passagem direta já exercitada por `ReabreDePendente`/`ReabreDeEncerradoSimulado`, e o fix é mais que uma correção direta.
  - `[low]` `[reject]` (Blind Hunter) nenhum teste alimenta `comentario` com espaço em branco nas bordas e confere o valor TRIMADO persistido — rejeitado: `strings.TrimSpace` é chamada direta de biblioteca padrão sem transformação adicional, risco de regressão desprezível; o fix é mais que uma correção direta.
  - `[low]` `[reject]` (Blind Hunter) `ComentarSolicitacaoHandler` não tem teste de `IDFormatoInvalido` dedicado (`MarcarPendenciaHandler`/`ObterSolicitacaoHandler` têm) — rejeitado: mesma checagem `uuidFormatRegexp` idêntica já exercitada 2x nos handlers irmãos; risco desprezível, fix mais que uma correção direta.
  - `[low]` `[reject]` (Blind Hunter) nenhum dos 3 novos handlers tem teste para o branch "Method not allowed" — rejeitado: mesmo padrão já aceito sem teste no precedente direto desta story (`AssumirSolicitacaoHandler`/`ListarFilaHandler`, `fila_test.go`, Story 4.1); fix mais que uma correção direta.
  - `[low]` `[patch]` (Edge Case Hunter) `ORDER BY created_at ASC` em `ObterSolicitacaoHandler` (comentários) sem desempate por `id`, mesmo padrão já corrigido na Story 4.1 (`ListarFilaHandler`) para o mesmo motivo (linhas com `created_at` idêntico podem trocar de ordem entre chamadas) — ação: `ORDER BY created_at ASC, id ASC`.
  - `[low]` `[reject]` (Edge Case Hunter) `GET /api/solicitacoes/{id}` não pagina/limita `comentarios` — rejeitado: nenhuma linha da I/O Matrix exige paginação aqui, threads de pendência/resposta são naturalmente pequenas (ciclo humano assumir→pendência→resposta), cenário de crescimento sem limite é improvável na prática; o fix (paginação, mudança de contrato) é mais que uma correção direta.
  - `[low]` `[reject]` (Edge Case Hunter) `ObterSolicitacaoHandler` lê `solicitacoes` e `solicitacao_comentarios` em 2 queries separadas, não atômicas (janela de corrida teórica entre as duas leituras) — rejeitado: endpoint só-leitura, janela de corrida estreita e sem garantia de consistência forte exigida pela spec; o fix (transação/JOIN) é mais que uma correção direta.
  - `[false]` `[reject]` (Blind Hunter) bullet de Tasks da spec escreve `CHECK IN ('pendencia','comentario')` (SQL inválido, falta referência de coluna/parênteses) — refutação: a migration real (`011_pendencia_e_reabertura.sql`) usa a sintaxe correta `CHECK (tipo IN (...))`; é só a prosa da spec que está imprecisa, e corrigir isso exigiria editar esta própria spec (rejeitado por regra).
  - `[false]` `[reject]` (Blind Hunter) por design, um administrador não tem como deixar uma nota sem forçar a transição para `pendente` — refutação: nenhuma AC ou linha da I/O Matrix pede essa capacidade; a intenção descreve só 2 cenários (pendência do admin, comentário do solicitante), ambos implementados.
  - `[false]` `[reject]` (Edge Case Hunter) bullet de Tasks da spec diz que "os 3" handlers tratam os 3 sentinelas de `internal/fila` (incluindo `ErrConflitoVersao`→409), mas `ObterSolicitacaoHandler` nunca chama `internal/fila` e não tem caminho para 409 — refutação: nenhuma linha da I/O Matrix exige 409 para o GET (só 200/403/404); o código do GET está correto, só a prosa coletiva da spec é imprecisa, e corrigir isso exigiria editar esta própria spec (rejeitado por regra).

## Design Notes

A reabertura é por EXCLUSÃO (`CASE ... ELSE 'em_atendimento'`), não por um literal `'finalizada'`: como a Story 4.5 (Finalizar) ainda está em backlog e vai decidir o(s) nome(s) exato(s) de status terminal (ex. sucesso/erro), esta story não pode nem deve antecipar esse literal. O mesmo `CASE` resolve, sem ramificação extra, tanto "solicitante responde a uma pendência" (status `pendente`→`em_atendimento`) quanto "comentário reabre uma solicitação finalizada" (qualquer outro status→`em_atendimento`) — a AC só descreve o segundo caso explicitamente, mas o primeiro é necessário para o ciclo `assumir→pendência→(resposta)→finalizar` fechar, já que `4.3`/`4.4` (geração de lote, epic context) só processam solicitações em `em_atendimento`.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros, sem diffs de formatação.
- `cd backend && go test ./... -count=1` -- expected: todos os pacotes passam, incluindo os novos testes de `internal/fila` e `handlers/pendencia_test.go`.

**Manual checks (if no CLI):**
- `docker-compose up` localmente e confirmar que `011_pendencia_e_reabertura.sql` roda sem erro e que o `GRANT` para `fb_apu05_privilegiado` permite o `INSERT` em `solicitacao_comentarios` pela conexão privilegiada.

## Auto Run Result

**Resumo:** Implementada a Story 4.2 (Pendência e reabertura, Epic 4): administrador devolve uma solicitação `em_atendimento` ao solicitante com comentário (`status=pendente`), e o solicitante reabre automaticamente qualquer solicitação fora de `{aberta, em_atendimento}` ao comentar — reabertura por exclusão, sem literal `'finalizada'` (Story 4.5 ainda em backlog). Novo histórico de comentários (`solicitacao_comentarios`) exposto por um `GET` de detalhe para quem é dono da solicitação.

**Arquivos alterados:**
- `backend/migrations/011_pendencia_e_reabertura.sql` — NOVA: tabela `solicitacao_comentarios`, índice `(solicitacao_id, created_at, id)`, `GRANT INSERT` para `fb_apu05_privilegiado`.
- `backend/internal/fila/fila.go` — `ErrNaoAutorizado`; `MarcarPendencia`/`Comentar` (transação UPDATE condicional + INSERT de comentário) com fallback 404→403→409.
- `backend/handlers/pendencia.go` — NOVO: `MarcarPendenciaHandler`, `ComentarSolicitacaoHandler`, `ObterSolicitacaoHandler`.
- `backend/handlers/fila.go` — nota no cabeçalho apontando para a continuação em `pendencia.go`.
- `backend/main.go` — 3 novas rotas registradas.
- `backend/handlers/pendencia_test.go` (novo), `backend/internal/fila/fila_test.go` (extensão) — cobrem a I/O Matrix.

**Review findings — breakdown:** 19 total — 6 `patch` (1 grupo `[medium]` com 2 relatos independentes convergindo na mesma causa — fallback de `MarcarPendencia` devolvia 403 em vez do 409 exigido pela I/O Matrix para `administrador_id` nulo; + 5 `[low]`: comentário de migration overclaiming imutabilidade, `GRANT SELECT` não usado, duplicação de helper `administradorIDOuNil`/`nullStringOuNil`, tag de log inconsistente, `ORDER BY` sem desempate); 2 `defer` (1 grupo `[medium]` com 2 relatos — Blind Hunter + Verification Gap Reviewer — convergindo na mesma causa: lógica central do `CASE` de reabertura só verificada por `sqlmock`, nunca contra Postgres real; mesma categoria já deferida no triage da Story 4.1); 11 `reject` (7 `[low]` sobre lacunas de teste pontuais em caminhos já exercitados por código/testes compartilhados — risco desprezível, fix maior que correção direta; 2 `[low]` sobre paginação/atomicidade do `GET` de detalhe — fora do uso realista, fix maior que correção direta; 3 `[false]` sobre imprecisões de prosa da própria spec ou capacidade não exigida pela intenção — refutados).

**Follow-up review recommendation:** `false`. Só 1 entrada `medium` foi patcheada nesta passada (a regra exige 2+ `medium` ou qualquer `high` para recomendar follow-up); as 5 entradas restantes patcheadas são `low`.

**Verificação:** `cd backend && go build ./... && go vet ./... && gofmt -l .` — sem erros, sem diffs, re-executado após os 6 patches. `cd backend && go test ./... -count=1` — todos os pacotes passam, incluindo o novo teste de regressão (`administrador_id` nulo + `status=aberta` → 409). Matrix Test Audit: as 10 linhas da I/O Matrix têm pelo menos um teste cobrindo-as, executado e aprovado. `docker-compose up` (manual check) não pôde ser repetido neste ambiente — sem `docker` instalado.

**Riscos residuais:** o `CASE` de reabertura (mecanismo central desta story) só foi verificado por leitura manual e por testes `sqlmock` — nunca executado contra um Postgres real (ver `deferred` acima); mesma lacuna estrutural já presente em todo o repositório (nenhuma feature tem teste de integração com banco real hoje).
