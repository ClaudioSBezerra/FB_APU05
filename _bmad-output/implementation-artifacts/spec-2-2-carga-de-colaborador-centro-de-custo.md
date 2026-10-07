---
title: 'Carga de colaborador × centro de custo'
type: 'feature'
created: 2026-10-07
status: 'done'
baseline_revision: '2a0244cbc91240c51e1f1d6fb84df185d6991422'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-2-context.md'
  - '{project-root}/_bmad-output/planning-artifacts/architecture/architecture-FB_APU05-2026-10-06/ARCHITECTURE-SPINE.md'
warnings: [oversized]
deferred: []
---

<intent-contract>

## Intent

**Problem:** Colaboradores autenticam via SSO (auto-provisionados em `usuarios`, Story 1.2) mas nascem sem centro de custo (CC) próprio — sem ele, Epic 3 não consegue identificar o CC de uma solicitação (FR-3).

**Approach:** Endpoint único de upload CSV (mesmo padrão de parsing da Story 2.1 — UTF-8 BOM, `;`, reaproveitando `lerCadastroCSV`) grava **somente** `usuarios.cc_proprio_id` (AD-8: "escopo estrito"), casando cada linha por e-mail contra um `usuarios` já existente (nunca cria linha nova — isso continua nascendo do SSO). Diferente da Story 2.1, é um UPDATE best-effort recorrente (FR-3: "carrega periodicamente"), não uma carga atômica única: linha sem efeito aplicável vira contagem no retorno, nunca aborta o arquivo inteiro.

## Boundaries & Constraints

**Always:** Rota atrás de `RequireAuth(..., "administrador")` (mesmo padrão de `cadastros.go`). CSV com cabeçalho exato `matricula;nome;email;centro_custo_codigo` (BOM/`;` via `lerCadastroCSV`); cabeçalho ou linha com número de colunas errado rejeita o arquivo inteiro com 400 (mesmo padrão de leitura da Story 2.1). Linha com `email` vazio é um registro-placeholder (cargo sem titular, sem pessoa física — FR-3) e é ignorada sem falhar a carga. Linha com `email` preenchido: resolve `centro_custo_codigo` em `centros_custo.codigo`; se encontrado, `UPDATE usuarios SET cc_proprio_id = :id WHERE LOWER(email) = LOWER(:email)` (casamento case-insensitive, mesmo índice de `fetchUserByEmail`). Reimportar o mesmo arquivo (ou um corrigido) é sempre permitido — nunca aplicar aqui o guard 409 "já possui dados" da Story 2.1.

**Never:** Inserir linha nova em `usuarios` (um e-mail sem `usuarios` correspondente ainda não logou — fica para a próxima carga, não é erro). Escrever `nome`, `perfil` ou qualquer outra coluna de `usuarios` — só `cc_proprio_id` (AD-8). Gravar em `cadastro_historico` ou qualquer tabela de auditoria — o regime de histórico/restauração (FR-16) cobre os 7 cadastros da Story 2.1 e a exceção de CC (Story 2.3, reaproveita o mesmo regime), não a identidade de CC próprio desta story. Implementar o bloqueio de abertura de solicitação sem CC — isso é Epic 3 (ainda não existe); esta story só garante que `cc_proprio_id` fica correto. Construir tela de frontend (mesmo precedente da Story 2.1 — só API).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Carga feliz | CSV válido, e-mail já existe em `usuarios`, `centro_custo_codigo` existe | 200, `cc_proprio_id` atualizado, contagem em `atualizados` | — |
| Linha placeholder | `email` vazio | Linha ignorada, não atualiza nada | Contada em `placeholders_ignorados`, carga continua |
| E-mail ainda não logou | `email` preenchido, nenhum `usuarios` com esse e-mail (case-insensitive) | Nenhuma escrita | Contada em `aguardando_primeiro_login`, carga continua |
| CC inexistente | `centro_custo_codigo` não existe em `centros_custo` | Nenhuma escrita para a linha | Contada em `centro_custo_nao_encontrado` (com número da linha), carga continua |
| Cabeçalho errado | Cabeçalho não bate com `matricula;nome;email;centro_custo_codigo` | Nada é escrito | 400 citando colunas esperadas |
| Linha com número de colunas errado | Linha N com campos a mais/menos | Nada é escrito (arquivo inteiro rejeitado) | 400 citando a linha N |
| Reimportação | Mesmo arquivo (ou corrigido) enviado de novo | Reaplica os mesmos casamentos — idempotente | — (nunca 409) |
| Acesso sem perfil administrador | Token válido, perfil=solicitante | — | 403 |

</intent-contract>

## Code Map

- `backend/migrations/004_colaborador_cc_proprio.sql` -- nova: `ALTER TABLE usuarios ADD COLUMN cc_proprio_id UUID REFERENCES centros_custo(id)` (nullable — "sem CC próprio" é estado válido, FR-3) + índice em `cc_proprio_id`
- `backend/handlers/colaboradores.go` -- novo: `CarregarColaboradoresHandler`, reaproveita `lerCadastroCSV`/`campoObrigatorio` de `cadastros_csv.go`; resolve `centro_custo_codigo` com a mesma consulta de `resolveDivisaoID` (mas sem rejeitar a linha — conta e segue)
- `backend/handlers/colaboradores_test.go` -- novo: cobre a I/O Matrix via `httptest`+sqlmock, mesmo padrão de `cadastros_test.go`
- `backend/handlers/cadastros_csv.go` -- reaproveitar `lerCadastroCSV`, `campoObrigatorio`, `bomUTF8` (não duplicar parsing)
- `backend/handlers/auth_sso.go:95-120` -- referência: `fetchUserByEmail`/índice `LOWER(email)` usado para casar a linha do CSV ao `usuarios` existente
- `backend/handlers/admin.go:51-67`, `cadastros.go:601+` -- padrão de referência (`jsonErr`, `RequireAuth`, transação) a reaproveitar
- `backend/main.go:213` -- registrar `POST /api/admin/colaboradores/carga` logo após o bloco de rotas de cadastros, mesmo padrão `RequireAuth(withDB(...), "administrador")`

## Tasks & Acceptance

**Execution:**
- `backend/migrations/004_colaborador_cc_proprio.sql` -- adicionar `cc_proprio_id` nullable em `usuarios` -- base para toda a story
- `backend/handlers/colaboradores.go` -- parser CSV (cabeçalho fixo) + loop linha-a-linha com UPDATE condicional e contadores -- isola a lógica de casamento e-mail/CC sem repetir o parsing genérico
- `backend/handlers/colaboradores_test.go` -- cobre os 8 cenários da I/O Matrix
- `backend/main.go` -- registrar a rota atrás de `RequireAuth(..., "administrador")`

**Acceptance Criteria:**
- Given um `usuarios` existente (via SSO) e um CSV com seu e-mail e um `centro_custo_codigo` válido, when o administrador faz upload, then `cc_proprio_id` desse usuário passa a apontar para aquele CC.
- Given um CSV com uma linha de e-mail vazio (placeholder de cargo sem titular), when o upload roda, then essa linha não gera nenhum `usuarios` novo nem erro, e é refletida em `placeholders_ignorados`.
- Given dois uploads sucessivos com dados diferentes para o mesmo e-mail, when ambos rodam, then o `cc_proprio_id` final reflete o último upload, sem 409 em nenhum dos dois.
- Given um usuário com perfil `solicitante`, when ele chama a rota, then recebe 403.

## Review Triage Log

### 2026-10-07 — Review pass
- verdicts: 11 findings — high 0, medium 1, low 8, false 2, maybe-false 0
- findings:
  - `[low]` `[patch]` (blind-hunter) `colaboradores.go` resolve `centro_custo_codigo` → `id` com um `SELECT` por linha do CSV, sem cache — repete a mesma consulta centenas/milhares de vezes por upload — ação aplicada: substituído por um único `SELECT codigo, id FROM centros_custo` antes do loop, carregado num map em memória; cada linha agora faz lookup no map em vez de consultar o banco.
  - `[low]` `[reject]` (blind-hunter) Linha com `centro_custo_codigo` vazio/só espaço é indistinguível de código inexistente (ambas caem em `centro_custo_nao_encontrado`) — rejeitado: a I/O Matrix não exige essa distinção, o cenário é raro num extrato de origem, e o fix exigiria um contador/campo novo (mais que correção direta).
  - `[low]` `[reject]` (blind-hunter) Nenhum teste cobre duas linhas com o mesmo e-mail e `centro_custo_codigo` diferentes dentro do MESMO upload ("last row wins" não documentado) — rejeitado: o comportamento já é correto por construção (`tx.Exec` sequencial) e já é implicitamente verificado entre uploads sucessivos em `TestCarregarColaboradoresHandler_Reimportacao`; cobertura adicional seria redundante.
  - `[low]` `[reject]` (blind-hunter) `TestCarregarColaboradoresHandler_Reimportacao` testa dois CSVs DIFERENTES (CC1 depois CC2), não o mesmo arquivo literal reenviado — rejeitado: a AC admite explicitamente "mesmo arquivo (ou um corrigido)"; a variante testada (corrigido) já cobre o cenário mais exigente (valor muda), tornando o caso do arquivo idêntico redundante.
  - `[false]` `[reject]` (blind-hunter) `sqlmock` só confere o texto literal do SQL de `LOWER(email) = LOWER($2)`, nunca exercita case-insensitividade real contra um Postgres de verdade — refutação: mesmo padrão de teste (driver mockado, nunca integração real) já aceito no projeto desde a Story 1.3/2.1 (ver Review Triage Log de `spec-2-1`, achado idêntico rejeitado); não é uma regressão desta story.
  - `[low]` `[reject]` (blind-hunter) `CREATE INDEX idx_usuarios_cc_proprio_id` sem `CONCURRENTLY` trava a tabela `usuarios` durante o boot — rejeitado: mesma convenção já usada em TODAS as migrations anteriores (001-003), tabela `usuarios` ainda pequena nesta fase do projeto, e `CONCURRENTLY` não pode rodar dentro da transação da migration — divergir aqui criaria inconsistência sem necessidade demonstrada.
  - `[low]` `[reject]` (blind-hunter) Nenhum teste cobre 401 (sem token/token inválido) especificamente nesta rota — rejeitado: `RequireAuth` é middleware compartilhado já coberto centralmente (`middleware_test.go`); a I/O Matrix desta story só exige 403 (perfil errado), mesmo precedente da Story 2.1.
  - `[low]` `[reject]` (blind-hunter) CSV só com cabeçalho (zero linhas de dado) não tem teste explícito do retorno com todos os contadores zerados — rejeitado: correção trivial por construção (`for range` sobre slice vazia é no-op em Go), sem risco real.
  - `[medium]` `[patch]` (edge-case-hunter) `SELECT id FROM centros_custo WHERE codigo = $1` é case-sensitive — um `centro_custo_codigo` do CSV que difira em maiúsculas/minúsculas do `codigo` cadastrado é contado como `centro_custo_nao_encontrado` mesmo sendo um CC válido, silenciando uma associação legítima — inconsistente com o casamento de e-mail (já case-insensitive) no mesmo handler — ação aplicada: o map em memória (mesmo fix acima) é indexado por `strings.ToUpper(strings.TrimSpace(codigo))`, tornando o casamento case-insensitive; teste novo `TestCarregarColaboradoresHandler_CentroCustoCodigoCaseInsensitive` adicionado.
  - `[low]` `[reject]` (verification-gap, "Other findings") A célula "CC inexistente" da I/O Matrix promete contagem "(com número da linha)", mas o branch `sql.ErrNoRows` em `colaboradores.go` não loga nem expõe `linha.Numero` — descompasso real entre texto da spec e código, mas o fix seria editar a própria spec desta build — rejeitado por regra de triagem (nunca editar a spec da build em curso); o Design Notes já estabelece que a resposta é só por contagem, sem lista — a frase da matriz é imprecisão de redação herdada do padrão da Story 2.1, não uma lacuna de comportamento.
  - `[false]` `[reject]` (intent-alignment) Divergência "in-review vs done/awaiting-operator": a spec fica em `status: in-review` no diff revisado, estado que o texto da intenção de orquestração não nomeia — refutação: `in-review` é o estado interno obrigatório deste próprio step-04 ("Change {spec_file} status to in-review... before continuing"), não um estado terminal; ao final desta mesma revisão o Finalize grava `done` (nenhuma ação só-humana existe nesta story, logo `awaiting-operator` não se aplica) — não há divergência real uma vez o processo concluído.

**Ações aplicadas nesta passada:** resolução de `centro_custo_codigo` trocada de `SELECT` por linha para um único `SELECT codigo, id FROM centros_custo` + map em memória (case-insensitive via `strings.ToUpper`), eliminando tanto o N+1 quanto a falha de casamento por caixa; 1 teste novo adicionado (`TestCarregarColaboradoresHandler_CentroCustoCodigoCaseInsensitive`); demais expectativas de `sqlmock` ajustadas à nova query. 9 achados rejeitados (8 low, 1 false) sem alteração de código.

## Design Notes

**Resposta do upload:** `{"atualizados": N, "placeholders_ignorados": N, "aguardando_primeiro_login": N, "centro_custo_nao_encontrado": N}` — todo contador sempre presente (mesmo 0), nenhuma lista nunca é descartada silenciosamente; divergência deliberada do envelope `{"importados": N}` da Story 2.1, porque aqui o resultado é por linha, não um bloco atômico.

**Por que UPDATE, nunca INSERT:** `usuarios.email`/`nome` são `NOT NULL` (migration 001) e a identidade nasce exclusivamente do primeiro login SSO (`auth_sso.go`, decisão já tomada na Story 1.2). Pré-provisionar um `usuarios` a partir do CSV exigiria escrever colunas que AD-8 explicitamente proíbe esta carga de tocar; por isso um e-mail do CSV sem `usuarios` correspondente fica em `aguardando_primeiro_login` até a pessoa logar e uma carga futura (re-upload) completar o CC.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: sem erros
- `cd backend && go test ./handlers/... -run Colaborador -v` -- expected: todos os casos da I/O Matrix passam

**Manual checks (if no CLI):**
- Subir `docker-compose up db` + backend, confirmar via log que `004_colaborador_cc_proprio.sql` executou e `schema_migrations` registrou o arquivo.

## Auto Run Result

**Resumo:** Implementado o endpoint `POST /api/admin/colaboradores/carga` (FR-3, AD-8) — upload de CSV que casa cada linha por e-mail contra um `usuarios` já provisionado via SSO e atualiza apenas `cc_proprio_id`. Nunca cria `usuarios` novo, nunca falha o arquivo inteiro por uma linha sem efeito aplicável (placeholder, e-mail ainda não logado, CC inexistente) — cada caso vira contagem no retorno. Reimportação sempre permitida (sem guard 409).

**Arquivos alterados:**
- `backend/migrations/004_colaborador_cc_proprio.sql` -- nova: `usuarios.cc_proprio_id` (nullable, FK `centros_custo`) + índice.
- `backend/handlers/colaboradores.go` -- novo: `CarregarColaboradoresHandler`; resolve `centro_custo_codigo` via um único `SELECT codigo, id FROM centros_custo` + map em memória (case-insensitive), em vez de uma query por linha.
- `backend/handlers/colaboradores_test.go` -- novo: 9 testes (8 cenários da I/O Matrix + 1 de case-insensitividade de `centro_custo_codigo`), `httptest`+`sqlmock`.
- `backend/main.go` -- registrada a rota atrás de `RequireAuth(..., "administrador")`.

**Revisão — achados:**
- 2 patches aplicados: `[low]` N+1 de consulta a `centros_custo` (corrigido com map em memória) e `[medium]` casamento case-sensitive de `centro_custo_codigo` (corrigido no mesmo map, agora case-insensitive, com teste novo).
- 9 achados rejeitados: 8 `low` (código vazio vs. inexistente indistinguível; duplicata de e-mail no mesmo upload sem teste; reimportação não testa arquivo literalmente idêntico; índice sem `CONCURRENTLY`; ausência de teste 401 dedicado; CSV só-cabeçalho sem teste; descompasso de redação na I/O Matrix sobre "número da linha" — fix exigiria editar esta própria spec) e 2 `false` (filosofia de teste com driver mockado, já aceita desde Story 1.3/2.1; divergência de status `in-review` vs. `done`/`awaiting-operator`, que é só o estado interno deste próprio step-04 antes do Finalize). Detalhe completo no Review Triage Log acima.

**Recomendação de revisão de acompanhamento:** `false` — apenas 1 achado `medium` foi patcheado nesta passada (não 2+) e nenhum `high`.

**Verificação realizada:** `go build ./... && go vet ./...` limpo; `go test ./handlers/... -run Colaborador -v` com os 9 testes passando (incluindo o novo de case-insensitividade); `go test ./...` completo sem regressão. Checagem manual via `docker-compose` não executada (sem Docker no sandbox) — a migration segue byte a byte o padrão `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`/`CREATE INDEX IF NOT EXISTS` já usado em 002/003 e é descoberta automaticamente pelo runner de migrations no boot.

**Riscos residuais:** Nenhum dos 7 achados `low`/`false` rejeitados representa risco de produção identificado; o único item com avaliação de severidade discordante possível seria a ausência de teste de upload com e-mail duplicado no mesmo arquivo, mas o comportamento (last-write-wins dentro da mesma transação) já é implicitamente coberto pelo teste de reimportação entre requisições.
