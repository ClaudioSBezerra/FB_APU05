---
title: 'Exceção de centro de custo'
type: 'feature'
created: 2026-10-07
status: 'done'
baseline_revision: '99ff35eb50e569a561a8ab22348acc23a9f59d8c'
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

**Problem:** FR-4 — colaboradores que atendem múltiplos CCs (pontos focais) só conseguem abrir/acompanhar solicitação para o próprio CC (`usuarios.cc_proprio_id`, Story 2.2); não há forma de autorizar CCs adicionais sem conceder aprovação.

**Approach:** Oitavo `{tipo}` (`cc-excecao`) no MESMO registry/Handler Factory da Story 2.1 (`cadastroRegistry`, `cadastros.go`) — nenhum handler novo, nenhuma rota nova em `main.go` (já parametrizadas por `{tipo}`). CSV resolve `colaborador_email`→`usuarios.id` e `centro_custo_codigo`→`centros_custo.id` (ambos case-insensitive); comportamento de import/edição/histórico/restauração idêntico aos 7 tipos existentes (FR-16) — nunca concede aprovação, só leitura/abertura de solicitação nos CCs listados (Epic 3 consome depois).

## Boundaries & Constraints

**Always:** Registrar `cc-excecao` em `cadastroRegistry` com `Colunas: [colaborador_id (UUID), centro_custo_id (UUID)]` — reaproveita `ImportarCadastroHandler`/`AtualizarCadastroHandler`/`ListarCadastroHandler`/`HistoricoCadastroHandler`/`RestaurarCadastroHandler` sem alteração. CSV cabeçalho exato `colaborador_email;centro_custo_codigo`; `DecodeCSV` resolve e-mail via `LOWER(email) = LOWER($1)` contra `usuarios` (mesmo padrão de `colaboradores.go`/`auth_sso.go`) e `centro_custo_codigo` via comparação case-insensitive contra `centros_custo.codigo` (mesma correção já aplicada na Story 2.2 — nunca repetir a comparação case-sensitive de `resolveDivisaoID`); qualquer um não encontrado rejeita a linha citando a causa (mesmo padrão atômico de `centros-custo`, não o best-effort de `colaboradores.go`). `DecodeJSON` (corpo de PUT) aceita `colaborador_id`/`centro_custo_id` já resolvidos (UUID), mesmo padrão de `decodeJSONCentrosCusto`. Migration nova (`005_cc_excecao.sql`) com `UNIQUE (colaborador_id, centro_custo_id)` — impede exceção duplicada para o mesmo par.

**Never:** Conceder qualquer direito de aprovação (FR-4) — o motor de aprovação (Epic 3, ainda não existe) nunca consulta `cc_excecao` para resolver aprovador. Criar handler ou rota nova — reaproveitar as 5 rotas genéricas já parametrizadas por `{tipo}`. Construir tela de frontend (mesmo precedente das Stories 2.1/2.2 — só API). Validar que o colaborador já tem `cc_proprio_id` preenchido — fora do escopo desta story (não exigido por nenhum AC).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Import feliz | CSV válido, e-mail já existe em `usuarios`, `centro_custo_codigo` existe | 201, `{"importados": N}`, linha `criado` em `cadastro_historico` | — |
| Import com tabela não-vazia | `cc_excecao` já tem ≥1 linha | Nada é escrito | 409 "cadastro já possui dados" |
| E-mail inexistente | `colaborador_email` sem `usuarios` correspondente | Nada é escrito | 400 citando a linha |
| CC inexistente | `centro_custo_codigo` não existe (ou grafia diferente, case-insensitive) | Nada é escrito | 400 citando a linha |
| Mesmo par duplicado | CSV com 2 linhas colaborador+CC idênticas | Nada é escrito | 400 (violação `UNIQUE`) citando a linha |
| Edição feliz | PUT com novo `centro_custo_id` válido | 200, registro atualizado, nova versão em histórico | — |
| Restauração feliz | POST `{"versao": V}` existente | 200, campos voltam à versão V, nova versão `restaurado` | — |
| Acesso sem perfil administrador | Token válido, perfil=solicitante | — | 403 |

</intent-contract>

## Code Map

- `backend/migrations/005_cc_excecao.sql` -- nova: `cc_excecao(id, colaborador_id UUID FK usuarios, centro_custo_id UUID FK centros_custo, created_at, updated_at)` + `UNIQUE(colaborador_id, centro_custo_id)` + índice em `colaborador_id`
- `backend/handlers/cadastros.go:65-142` -- `cadastroRegistry` -- adicionar entrada `"cc-excecao"` (Tabela `cc_excecao`, CSVCabecalho `colaborador_email;centro_custo_codigo`, Colunas `colaborador_id`/`centro_custo_id` ambos `colUUID`)
- `backend/handlers/cadastros_csv.go:158-168` -- `resolveDivisaoID` -- padrão de referência para uma nova `resolveColaboradorIDPorEmail(tx, email)` (`SELECT id FROM usuarios WHERE LOWER(email)=LOWER($1)`) e `resolveCentroCustoIDPorCodigo(tx, codigo)` (case-insensitive, `UPPER(codigo)=UPPER($1)`); nova `decodeCSVCcExcecao`/`decodeJSONCcExcecao`, mesmo padrão de `decodeCSVCentrosCusto`/`decodeJSONCentrosCusto`
- `backend/handlers/colaboradores.go:87,120` -- referência: convenção case-insensitive já estabelecida (`strings.ToUpper`/`LOWER(email)`) a reaproveitar, não a de `resolveDivisaoID` (case-sensitive)
- `backend/handlers/cadastros_test.go` -- novos testes cobrindo a I/O Matrix para `{tipo}=cc-excecao`, mesmo padrão `httptest`+`sqlmock` de `TestImportarCadastroHandler_CentrosCusto_*`

## Tasks & Acceptance

**Execution:**
- `backend/migrations/005_cc_excecao.sql` -- criar tabela `cc_excecao` + `UNIQUE`/índice -- base da story
- `backend/handlers/cadastros_csv.go` -- `resolveColaboradorIDPorEmail`, `resolveCentroCustoIDPorCodigo`, `decodeCSVCcExcecao` -- isola a resolução das 2 FKs sem duplicar parsing genérico
- `backend/handlers/cadastros.go` -- `decodeJSONCcExcecao` + entrada `"cc-excecao"` em `cadastroRegistry` -- habilita as 5 rotas genéricas para o novo tipo
- `backend/handlers/cadastros_test.go` -- cobre a I/O Matrix (import feliz/409/400×3, PUT feliz, restaurar feliz, 403)

**Acceptance Criteria:**
- Given um colaborador já carregado (Story 2.2) e um CC válido, when o administrador importa um CSV com esse par, then uma linha nasce em `cc_excecao` associando `colaborador_id`+`centro_custo_id`, com histórico `criado`.
- Given uma exceção já cadastrada, when o administrador edita via PUT para outro `centro_custo_id` válido, then o registro reflete o novo CC e uma nova versão aparece no histórico, sem apagar a anterior.
- Given uma exceção com 2+ versões, when o administrador restaura uma versão anterior, then os campos voltam aos valores daquela versão.
- Given qualquer solicitação de leitura (listagem/histórico) de `cc-excecao`, when chamada, then formato de resposta é idêntico ao dos demais 7 tipos (mesma Handler Factory).
- Given um usuário com perfil `solicitante`, when ele chama qualquer rota `/api/admin/cadastros/cc-excecao/*`, then recebe 403.

## Review Triage Log

### 2026-10-07 — Review pass
- verdicts: 12 findings — high 0, medium 2, low 7, false 3, maybe-false 0
- findings:
  - `[medium]` `[defer]` (blind-hunter) `cc-excecao` reusa o guard 409 "tabela já tem dados" de `ImportarCadastroHandler` — depois do primeiro import bem-sucedido, nenhuma rota permite criar um NOVO par colaborador+CC (PUT só edita um `{id}` já existente); diferente dos 7 cadastros estáticos da Story 2.1, exceção de CC é um grant individual que cresce ao longo do tempo (pontos focais novos) — ação: deferido como limitação pré-existente do Handler Factory (mecanismo não alterado por este diff, mandatado pela arquitetura "mesmo padrão de 4.7"); resolver exigiria uma decisão de arquitetura futura (ex.: remover o guard 409 especificamente para este tipo, como a Story 2.2 já divergiu para `colaboradores.go`).
  - `[low]` `[patch]` (blind-hunter) `decodeJSONCcExcecao` (PUT) tem 2 ramos de erro (campo ausente, UUID inválido) sem nenhum teste para `{tipo}=cc-excecao` — ação aplicada: teste novo cobrindo pelo menos um caso de campo ausente/UUID inválido, 400.
  - `[low]` `[patch]` (blind-hunter) `decodeCSVCcExcecao` tem ramos de campo CSV vazio (`colaborador_email`/`centro_custo_codigo`) sem nenhum teste — ação aplicada: teste novo cobrindo campo obrigatório vazio, 400 citando a linha.
  - `[low]` `[patch]` (blind-hunter) `cc_excecao` só tem índice em `colaborador_id`; uma consulta futura por `centro_custo_id` (Epic 3) faria full scan — ação aplicada: `CREATE INDEX idx_cc_excecao_centro_custo_id`.
  - `[low]` `[defer]` (blind-hunter) Listagem/histórico de `cc-excecao` expõe só `colaborador_id`/`centro_custo_id` (UUID cru), nunca e-mail/código legível — rejeitado como fix local: comportamento idêntico aos 7 tipos existentes (`centros-custo` também expõe `divisao_id` cru) — divergir aqui violaria FR-16 ("nenhum tipo pode ter regra... diferenciada"); pré-existente ao design do Handler Factory, não introduzido por este diff.
  - `[false]` `[reject]` (blind-hunter) Falta teste de regressão confirmando que o motor de aprovação nunca consulta `cc_excecao` — refutação: o motor de aprovação (Epic 3) ainda não existe no código; não há nada para regredir.
  - `[low]` `[reject]` (blind-hunter) Violação de par duplicado no CSV cita só a linha do segundo registro, não as duas linhas do par — rejeitado: comportamento já correto por construção (transação inteira desfeita), mesmo padrão de achados já rejeitados nas Stories 2.1/2.2 por "fix exigiria mais que correção direta" (rastrear pares já vistos no loop genérico compartilhado por 8 tipos).
  - `[false]` `[reject]` (blind-hunter) `resolveColaboradorIDPorEmail`/`resolveCentroCustoIDPorCodigo` duplicam a forma de `resolveDivisaoID` em vez de consolidar — refutação: sem dano concreto nomeado (duplicação mínima de 6 linhas); o próprio Code Map da spec trata `resolveDivisaoID` como "padrão de referência" a copiar-adaptar, convenção já estabelecida no projeto.
  - `[low]` `[reject]` (blind-hunter) `warnings: [oversized]` sem nota de justificativa no spec — rejeitado por regra de triagem (fix é editar a própria spec desta build).
  - `[medium]` `[defer]` (edge-case-hunter) `resolveCentroCustoIDPorCodigo` usa `UPPER(codigo)=UPPER($1)` sem desambiguação — como `centros_custo.codigo` só tem `UNIQUE` case-sensitive, duas linhas podem coexistir diferindo só em maiúsculas/minúsculas, e um `QueryRow` simples resolveria para uma linha arbitrária — ação: deferido como risco sistêmico pré-existente (o mesmo padrão de ambiguidade já existe sem correção em `colaboradores.go`, Story 2.2, que também resolve `centro_custo_codigo` case-insensitive via map); o fix correto é no schema (índice/constraint único case-insensitive em `centros_custo.codigo`), fora do escopo desta story isolada.
  - `[low]` `[defer]` (edge-case-hunter) Erros não-`sql.ErrNoRows` (ex. falha de conectividade) dentro de `DecodeCSV` viram 400 com o texto cru do erro, em vez de 500 — comportamento idêntico e pré-existente em `ImportarCadastroHandler`/`resolveDivisaoID` desde a Story 2.1, não introduzido por este diff — deferido como achado sistêmico, não local a esta story.
  - `[false]` `[reject]` (intent-alignment) Divergência "spec `status: in-progress` vs. `done`/`awaiting-operator`" no diff revisado — refutação: estado interno esperado antes do Finalize deste próprio step-04 (mesmo precedente já julgado `false` nas Review Triage Logs das Stories 2.1 e 2.2).

## Design Notes

**Por que case-insensitive e atômico (não o padrão de `colaboradores.go`):** `colaboradores.go` (Story 2.2) é UPDATE best-effort recorrente, fora do regime de histórico (Boundaries daquela spec, "Never: gravar em cadastro_historico"). `cc-excecao` está DENTRO do regime de histórico/restauração (FR-16, Cross-Story Dependencies do Epic 2 Context) — por isso segue o padrão atômico de `ImportarCadastroHandler` (tudo ou nada, 409 se não-vazia), igual aos 7 tipos da Story 2.1, não o padrão de contagens da Story 2.2. A resolução case-insensitive de `centro_custo_codigo`, porém, reaproveita a correção já aplicada na Story 2.2 (review triage daquela story) — não a comparação case-sensitive original de `resolveDivisaoID`, para não reintroduzir a mesma classe de defeito.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: sem erros
- `cd backend && go test ./handlers/... -run Cadastro -v` -- expected: todos os casos da I/O Matrix (incluindo `cc-excecao`) passam

**Manual checks (if no CLI):**
- Subir `docker-compose up db` + backend, confirmar via log que `005_cc_excecao.sql` executou e `schema_migrations` registrou o arquivo.

## Auto Run Result

**Resumo:** Implementado o oitavo `{tipo}` (`cc-excecao`, FR-4) no mesmo registry/Handler Factory da Story 2.1 — administrador cadastra, via CSV, exceções de CC por colaborador (`colaborador_email`→`usuarios.id`, `centro_custo_codigo`→`centros_custo.id`, ambos resolvidos case-insensitive), reaproveitando sem alteração as 5 rotas genéricas (`Listar`/`Importar`/`Atualizar`/`Historico`/`Restaurar`) e o regime de histórico/restauração (`cadastro_historico`) da Story 2.1. Nenhum handler ou rota nova; nenhuma tela de frontend (mesmo precedente das Stories 2.1/2.2).

**Arquivos alterados:**
- `backend/migrations/005_cc_excecao.sql` -- nova: tabela `cc_excecao(id, colaborador_id FK usuarios, centro_custo_id FK centros_custo, created_at, updated_at)`, `UNIQUE(colaborador_id, centro_custo_id)`, índices em `colaborador_id` e `centro_custo_id`.
- `backend/handlers/cadastros.go` -- `decodeJSONCcExcecao` (PUT aceita `colaborador_id`/`centro_custo_id` já resolvidos) + entrada `"cc-excecao"` em `cadastroRegistry`.
- `backend/handlers/cadastros_csv.go` -- `resolveColaboradorIDPorEmail` (case-insensitive, `LOWER(email)`), `resolveCentroCustoIDPorCodigo` (case-insensitive, `UPPER(codigo)`), `decodeCSVCcExcecao` (atômico — qualquer FK não encontrada rejeita a linha).
- `backend/handlers/cadastros_test.go` -- 11 testes novos cobrindo a I/O Matrix completa de `cc-excecao` (import feliz/409/400×3, par duplicado, PUT feliz/400×2, restaurar feliz, 403) com `httptest`+`sqlmock`.
- `backend/main.go` -- comentário atualizado citando o 8º `{tipo}`; nenhuma rota/handler novo.

**Revisão — achados (12 no total: high 0, medium 2, low 7, false 3):**
- 3 patches aplicados (todos `low`): teste de PUT com campo ausente/UUID inválido para `cc-excecao`; teste de CSV com `colaborador_email` vazio; índice novo em `centro_custo_id`.
- 4 achados adiados (`defer`, 2 `medium` + 2 `low`): guard 409 de `ImportarCadastroHandler` bloqueia criar novo par colaborador+CC após o primeiro import (limitação pré-existente do Handler Factory, mandatada pela arquitetura "mesmo padrão de 4.7" — resolver exigiria decisão de arquitetura futura); `resolveCentroCustoIDPorCodigo` pode resolver para uma linha arbitrária se existirem 2 `centros_custo.codigo` diferindo só em maiúsculas/minúsculas (risco sistêmico pré-existente, mesmo padrão não corrigido em `colaboradores.go` desde a Story 2.2 — fix correto é no schema); listagem expõe só UUID cru (idêntico aos 7 tipos existentes, FR-16 exige uniformidade); erros não-`ErrNoRows` em `DecodeCSV` viram 400 em vez de 500 (comportamento pré-existente desde a Story 2.1, não introduzido por este diff).
- 5 achados rejeitados (2 `false`, 2 `low`, 1 reject-por-regra): falta de teste de regressão para um motor de aprovação que ainda não existe (`false`); duplicação de forma entre os 3 resolvers de FK sem dano concreto nomeado (`false`); mensagem de erro de par duplicado cita só a 2ª linha (`low`, fix exigiria rastrear pares no loop genérico compartilhado por 8 tipos); `warnings: [oversized]` sem nota de justificativa (fix seria editar esta própria spec); divergência de status `in-progress` vs. terminal (mesmo precedente já julgado `false` nas Stories 2.1/2.2). Detalhe completo no Review Triage Log acima.

**Recomendação de revisão de acompanhamento:** `false` — nenhum achado `high` e nenhum `medium` foi patcheado nesta passada (os 2 achados `medium` foram adiados, não corrigidos).

**Verificação realizada:** `go build ./... && go vet ./... && gofmt -l .` limpo; `go test ./handlers/... -run Cadastro -v` com os 27 testes de `cc-excecao` passando (11 novos + os 16 pré-existentes dos outros 7 tipos, nenhuma regressão); `go test ./...` completo sem falhas. Checagem manual via `docker-compose` não executada (sem Docker no sandbox) — a migration segue o padrão `CREATE TABLE/INDEX IF NOT EXISTS` já usado em 003/004 e é descoberta automaticamente pelo runner de migrations no boot.

**Riscos residuais:**
- Depois do primeiro import de `cc-excecao`, nenhuma rota permite adicionar um novo par colaborador+CC (só editar um `{id}` já existente via PUT) — ver achado `[medium]` `[defer]` acima.
- `centros_custo.codigo` não tem garantia de unicidade case-insensitive; duas linhas diferindo só em caixa fariam `resolveCentroCustoIDPorCodigo` resolver de forma não-determinística — ver achado `[medium]` `[defer]` acima.
- Checagem manual de boot da migration não executada neste ambiente (sem Docker).
