---
title: 'Finalizar solicitação'
type: 'feature'
created: '2026-10-09'
status: 'done'
baseline_revision: '5ae7c8b50b11aa80c42320581e085106e4bdfd5c'
review_loop_iteration: 0
followup_review_recommended: false
context: ['{project-root}/_bmad-output/planning-artifacts/architecture/architecture-FB_APU05-2026-10-06/ARCHITECTURE-SPINE.md']
warnings: ['oversized']
deferred:
  - summary: >-
      lerFinalizarRequest não tem teste para corpo malformado, erro de
      io.ReadAll, ou overflow do MaxBytesReader de 1 MiB.
    evidence: |-
      Mesmo padrão já presente, sem teste, em TODOS os demais lerXRequest do
      pacote handlers (lerPendenciaOuComentarioRequest,
      lerGerarLoteDespesaRequest, lerGerarLoteObraRequest — confirmado por
      grep, nenhum testa corpo malformado); não é um risco novo desta story.
    location: >-
      backend/handlers/finalizar.go (lerFinalizarRequest)
    severity: low
  - summary: >-
      ExportadorVBA.Finalizar nunca propaga o context.Context da requisição
      HTTP para a transação (sem BeginTx(ctx,...)/...Context) — uma
      requisição cancelada/com timeout no cliente não cancela a transação em
      andamento.
    evidence: |-
      Mesmo padrão em TODAS as demais escritas do repositório
      (fila.Assumir/MarcarPendencia/Comentar,
      exportacao.GerarLoteDespesa/GerarLoteObra) — nenhuma usa
      BeginTx(ctx,...)/ExecContext; convenção já estabelecida em todo o
      backend, não um risco novo desta story.
    location: >-
      backend/internal/exportacao/finalizar.go (Finalizar)
    severity: medium
---

<intent-contract>

## Intent

**Problem:** Depois de gerar o lote de exportação (Despesa ou Obra, Stories 4.3/4.4) não existe nenhuma ação que encerre a solicitação — gerar o arquivo nunca muda `status` (Epic 4 Requirements), e uma linha de Obras com `ordem_investimento='CRIAR'` nunca ganha uma ordem real (migration 009, comentário: "nasce na finalização, Epic 4").

**Approach:** Novo método `Finalizar` em `ExportadorSAP`/`ExportadorVBA` (`internal/exportacao` — AD-5 do Architecture Spine já amarra FR-14 a este pacote, mesma linha/lock de `solicitacoes` que a exportação já usa): numa ÚNICA transação pela conexão PRIVILEGIADA, muda `status` via lock otimista (`finalizada_sucesso`/`finalizada_erro`, literais novos — `status` é `VARCHAR(30)` sem `CHECK`), migra as linhas `exportacoes_sap` da solicitação de `GERADO` para `FINALIZADO`, e — só quando `resultado='sucesso'` — resolve cada linha de Obras `CRIAR` criando a ordem real em `obra_ordens` a partir do `numero_ordem` informado pelo administrador (que o obteve fora deste sistema, rodando o robô/SAP GUI; não existe `ExportadorAPI` nem integração ao vivo com o SAP nesta v1).

## Boundaries & Constraints

**Always:**
- `administrador_id` sempre de `GetUserIDFromContext`; dono da solicitação (igual Assumir/MarcarPendencia) — nunca aceito do corpo.
- As 4 escritas possíveis (`solicitacoes.status/versao`, `exportacoes_sap.status`, `obra_ordens` INSERT, `solicitacao_obras_linhas.ordem_investimento`) ocorrem na MESMA transação pela conexão PRIVILEGIADA; qualquer falha (conflito de versão, ordens_criadas inválido, `numero_ordem` duplicado) recusa a operação INTEIRA — nenhuma escrita parcial.
- Exige recálculo server-side de exportação prévia: ao menos 1 linha `exportacoes_sap` com `status='GERADO'` para a solicitação; TODAS essas linhas migram para `FINALIZADO` (não só a mais recente — a solicitação pode ter sido incluída sob mais de 1 `chave_idempotencia`).
- `ordem_investimento='CRIAR'` só é resolvida quando `resultado='sucesso'`; `numero_ordem` vem sempre do corpo (admin), nunca gerado/inventado pelo backend (02-REQUISITOS-NEGOCIO-ENVIO-TI.md: "não criar ordem automaticamente até confirmação").

**Never:**
- Nunca criar ordem real quando `resultado='erro'` — 400 se `ordens_criadas` vier preenchido nesse caso.
- Nunca aceitar `numero_ordem` duplicado (`UNIQUE obra_ordens.numero_ordem`, migration 009) nem um `linha_id` que não seja uma linha `CRIAR` da própria solicitação.
- Nunca reintroduzir o campo "Documento/referência SAP" (PRD FR-14, removido na especificação de origem).
- Não implementar `ExportadorAPI` nem qualquer chamada de rede ao SAP — fora do escopo (formato ainda desconhecido, Architecture Spine Open Questions).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Finalização com sucesso, sem linha CRIAR | `em_atendimento`, dono=sessão, export `GERADO`, `resultado=sucesso` | 200; `status=finalizada_sucesso`, `versao+1`; linhas `exportacoes_sap`→`FINALIZADO` | No error expected |
| Finalização com erro | mesmas condições, `resultado=erro` | 200; `status=finalizada_erro`, `versao+1`; linhas `exportacoes_sap`→`FINALIZADO` | No error expected |
| Linha CRIAR resolvida | Obras com 1+ linha `ordem_investimento='CRIAR'`, `resultado=sucesso`, `ordens_criadas` cobre exatamente essas linhas | 200; 1 linha nova por CRIAR em `obra_ordens`; `solicitacao_obras_linhas.ordem_investimento` atualizado | No error expected |
| `ordens_criadas` incompleto/errado | falta linha CRIAR, sobra `linha_id` extra, ou aponta linha que não é CRIAR | — | 400 |
| `ordens_criadas` com `resultado=erro` | `resultado=erro` e `ordens_criadas` não vazio | — | 400 |
| `numero_ordem` já existe | valor informado já está em `obra_ordens` | — | 409 |
| Ainda não exportada | nenhuma linha `exportacoes_sap` com `status=GERADO` para a solicitação | — | 409 (operação inteira recusada) |
| Dono errado | `administrador_id` da linha ≠ chamador | — | 403 |
| Status/versão incompatível | `status != em_atendimento` OU `versao` obsoleta | — | 409 `conflito_versao` com `versao_atual` |
| ID inexistente | UUID não encontrado | — | 404 |

</intent-contract>

## Code Map

- `backend/internal/exportacao/exportacao.go:14-107` -- interface `ExportadorSAP` (adiciona `Finalizar(db *sql.DB, solicitacaoID string, versaoLida int, administradorID, resultado string, ordensCriadas []OrdemCriada) (Solicitacao, error)`); tipos novos `OrdemCriada{LinhaID, NumeroOrdem string}` e `Solicitacao{ID, TipoSolicitacao, Status, AdministradorID string; Versao int}` (mesma projeção mínima duplicada por pacote de `fila.Solicitacao` — princípio já usado por `aprovacao`/`fila`); sentinelas novas `ErrConflitoVersao{VersaoAtual int}` (struct, mesma forma de `fila.ErrConflitoVersao`), `ErrNaoExportada`, `ErrOrdensCriadasInvalidas`, `ErrOrdemJaExiste`; reaproveita `ErrNaoEncontrada`/`ErrNaoAutorizado` sem mudança.
- `backend/internal/exportacao/finalizar.go` -- NOVO: `ExportadorVBA.Finalizar` -- 1 transação: `UPDATE solicitacoes SET status=CASE $resultado..., versao=versao+1 WHERE id=$1 AND versao=$2 AND administrador_id=$3 AND status='em_atendimento' RETURNING ...`; 0 linhas → rollback + fallback 404→403→409 (replica `fila.fallbackMarcarPendencia`, `donoID sql.NullString`, 403 só com dono preenchido diferente); em seguida `UPDATE exportacoes_sap SET status='FINALIZADO' WHERE solicitacao_id=$1 AND status='GERADO'` -- 0 linhas afetadas → rollback → `ErrNaoExportada`; se `resultado=="sucesso"`, `SELECT id,local_obra_id,subgrupo_despesa_id FROM solicitacao_obras_linhas WHERE solicitacao_id=$1 AND ordem_investimento='CRIAR'`, confere 1:1 contra `ordensCriadas` (qualquer divergência → `ErrOrdensCriadasInvalidas`), por linha `INSERT INTO obra_ordens (numero_ordem, local_obra_id, subgrupo_despesa_id) VALUES (...)` + `UPDATE solicitacao_obras_linhas SET ordem_investimento=$numeroOrdem WHERE id=$linhaID`; commit.
- `backend/internal/fila/fila.go:150-176` -- `fallbackMarcarPendencia` (`donoID sql.NullString`, 403 só quando HÁ dono preenchido diferente; NULL cai em 409 — mesmo fix do Review Triage da Story 4.2) -- padrão exato a replicar no fallback de `Finalizar`.
- `backend/handlers/cadastros.go:962-979` -- `errorMsgAmigavel`/`ehViolacaoDeConstraint` (detecção de `*pq.Error`, `Code.Name()=="unique_violation"`) -- padrão a replicar em `finalizar.go` para traduzir conflito de `obra_ordens.numero_ordem` em `ErrOrdemJaExiste` (`internal/exportacao` ainda não importa `github.com/lib/pq` em nenhum outro arquivo).
- `backend/migrations/009_obras_multilinha.sql:42-55,88-99` -- schema de `obra_ordens` (`numero_ordem UNIQUE NOT NULL`, `local_obra_id`, `subgrupo_despesa_id`) e `solicitacao_obras_linhas`, reaproveitado sem alteração — comentário da própria migration já antecipa que CRIAR só gera linha aqui na finalização.
- `backend/migrations/010_assumir_fila.sql:72-74`, `012_exportacoes_sap.sql:54-55` -- GRANTs já existentes (`UPDATE (aprovador_snapshot,versao,status,administrador_id)` em `solicitacoes`; `UPDATE` em `exportacoes_sap`) cobrem toda a escrita de `Finalizar` nessas 2 tabelas — nenhuma mudança; esta story cria `014_finalizar_solicitacao.sql` só com os 2 GRANTs que faltam (`INSERT` em `obra_ordens`; `UPDATE (ordem_investimento)` em `solicitacao_obras_linhas`, hoje só `SELECT` via migration 013).
- `backend/handlers/pendencia.go:43-69,99-120` -- padrão de leitura/validação de corpo e tradução de erro (`lerPendenciaOuComentarioRequest`, `tratarErroEscritaFila`) a replicar por analogia em `handlers/finalizar.go` (NOVO); `backend/handlers/exportacao.go:157-185` -- `tratarErroExportacao` (switch de sentinelas) -- mesmo padrão.
- `backend/handlers/middleware.go:90,99-110` -- `GetUserIDFromContext`, `jsonErrConflitoVersao`; `backend/handlers/admin.go:35` -- `uuidFormatRegexp`; `backend/handlers/auth.go:159` -- `jsonErr`.
- `backend/main.go:379-389` -- padrão de registro de rota de exportação (`withPrivilegedDB`); nova rota entra depois da linha 389.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/014_finalizar_solicitacao.sql` -- NOVA: `GRANT INSERT ON obra_ordens TO fb_apu05_privilegiado; GRANT UPDATE (ordem_investimento) ON solicitacao_obras_linhas TO fb_apu05_privilegiado;` -- único GRANT que falta (AD-4); nenhum GRANT novo em `solicitacoes`/`exportacoes_sap`.
- `backend/internal/exportacao/exportacao.go` -- adiciona `Finalizar` à interface `ExportadorSAP`; tipos `OrdemCriada`/`Solicitacao`; sentinelas `ErrConflitoVersao`/`ErrNaoExportada`/`ErrOrdensCriadasInvalidas`/`ErrOrdemJaExiste`.
- `backend/internal/exportacao/finalizar.go` -- NOVO: `ExportadorVBA.Finalizar` (ver Code Map) -- 1 transação, lock otimista + migração de `exportacoes_sap` + resolução de linhas CRIAR, atômico (qualquer erro recusa a operação inteira).
- `backend/handlers/finalizar.go` -- NOVO: `FinalizarSolicitacaoHandler(db)` -- lê corpo `{versao int, resultado string, ordens_criadas []{linha_id,numero_ordem}}`; valida `versao>=1`, `resultado` ∈ {"sucesso","erro"}, cada `ordens_criadas[i].linha_id` é UUID válido e `numero_ordem` não-vazio (TrimSpace), sem `linha_id` duplicado no corpo, 400 se `resultado=="erro"` e `ordens_criadas` não vazio; chama `exportacao.Finalizar`; traduz sentinelas (404/403/409 `conflito_versao`/409 não-exportada/400 ordens inválidas/409 ordem já existe); 200 `{id,tipo_solicitacao,status,versao,administrador_id}`.
- `backend/main.go` -- registra `POST /api/solicitacoes/{id}/finalizar` (`RequireAuth(withPrivilegedDB(handlers.FinalizarSolicitacaoHandler), "administrador")`), depois da linha 389.
- `backend/internal/exportacao/finalizar_test.go`, `backend/handlers/finalizar_test.go` -- cobrem a I/O Matrix (sqlmock, mesma convenção dos demais testes do pacote).

**Acceptance Criteria:**
- Given uma solicitação já com export `exportacoes_sap` em `GERADO`, when o administrador finaliza com sucesso, then o `status` muda para `finalizada_sucesso` via lock otimista (`versao+1`) e as linhas `exportacoes_sap` da solicitação passam a `FINALIZADO`, tudo na mesma transação.
- Given uma solicitação `obras` com 1 linha `ordem_investimento='CRIAR'` e export já `GERADO`, when o administrador finaliza com sucesso informando o `numero_ordem` real dessa linha, then `obra_ordens` ganha 1 registro novo e a própria linha passa a referenciar esse `numero_ordem`, na mesma transação da mudança de status; um conflito de versão recusa a operação inteira (nenhuma escrita parcial).

## Review Triage Log

### 2026-10-09 — Review pass
- verdicts: 13 findings — high 0, medium 2, low 10, false 1, maybe-false 0
- findings:
  - `[low]` `[patch]` (Blind Hunter) `ordens_criadas[i].numero_ordem` não tem checagem de tamanho máximo contra `obra_ordens.numero_ordem VARCHAR(50)` — um valor com mais de 50 caracteres chega ao banco e dispara um erro Postgres não tratado (500 opaco em vez de 400 claro) — ação: `lerFinalizarRequest` passa a rejeitar (400) qualquer `numero_ordem` (após TrimSpace) com mais de 50 caracteres.
  - `[low]` `[patch]` (Blind Hunter) `resolverLinhasCriar` nunca valida que `OrdemCriada.NumeroOrdem` é não-vazio por conta própria (confia inteiramente no handler) — inconsistente com a própria defesa da mesma função contra `linha_id` duplicado (AD-1: domínio nunca confia no chamador) — ação: `resolverLinhasCriar` passa a devolver `ErrOrdensCriadasInvalidas` quando `NumeroOrdem` (trimado) é vazio, antes do INSERT.
  - `[low]` `[reject]` (Blind Hunter) `Finalizar` não persiste nenhum campo "motivo"/"mensagem" explicando por que uma finalização terminou em `erro` — rejeitado: nenhuma AC do epics.md/PRD FR-14 pede isso; mesmo raciocínio já usado para rejeitar `Bloqueio.Motivo` como enum na Story 4.4 ("superfície pública nova para uma necessidade hipotética que a AC não pede") — o fix (novo campo no corpo + coluna) é mais que uma correção direta.
  - `[false]` `[reject]` (Blind Hunter) `Finalizar` não tem proteção de idempotência — um retry após commit bem-sucedido recebe 409 em vez do resultado já confirmado — refutação: esse É o comportamento pretendido do lock otimista (AD-5: "conflito de versão = HTTP 409 lógico, sem retry automático no servidor"), idêntico ao de `Assumir`/`MarcarPendencia` (Stories 4.1/4.2), nenhuma das quais tem chave de idempotência; `Finalizar` nunca precisou de uma (Design Notes/Boundaries da spec 4.5 não a pedem).
  - `[low]` `[reject]` (Blind Hunter) reusar o mesmo `numero_ordem` para 2 `linha_id` diferentes no mesmo pedido é rejeitado pela constraint UNIQUE no 2º INSERT, mas a mensagem ("o número de ordem informado já existe") não distingue esse caso de um conflito real pré-existente — rejeitado: o comportamento (recusar) já está correto nos dois casos; cenário improvável (um admin não tem motivo para submeter o mesmo número de ordem real do SAP para 2 linhas diferentes); o fix (mensagem diferenciada) é mais que uma correção direta.
  - `[low]` `[patch]` (Blind Hunter) nenhum teste em `finalizar_test.go` exercita `resolverLinhasCriar`/`Finalizar` com um `linha_id` duplicado dentro de `ordensCriadas` que ainda bate em contagem com `len(linhas)` (contornando a dedupe do handler) — a defesa interna comentada no código nunca é provada por teste — ação: novo teste em `internal/exportacao/finalizar_test.go` com 2 linhas CRIAR reais, `ordensCriadas` repetindo o id de uma e omitindo a outra, esperando `ErrOrdensCriadasInvalidas`.
  - `[low]` `[defer]` (Blind Hunter) nenhum teste cobre o corpo malformado / erro de `io.ReadAll` / overflow do `MaxBytesReader` de 1 MiB em `lerFinalizarRequest` — adiado: mesmo padrão já presente, sem teste, em TODOS os demais `lerXRequest` do pacote `handlers` (`lerPendenciaOuComentarioRequest`, `lerGerarLoteDespesaRequest`, `lerGerarLoteObraRequest` — confirmado por grep, nenhum testa corpo malformado); não é um risco novo desta story.
  - `[low]` `[reject]` (Blind Hunter) linhas de `obra_ordens` criadas pela finalização nunca recebem `tipo_despesa`/`tipo_faturamento` (sempre `NULL`) — rejeitado: nenhuma AC/PRD pede captura desses campos no fluxo de finalização; mesmo raciocínio do achado do "motivo" acima — exigiria novo campo de entrada (superfície pública nova) sem AC que sustente.
  - `[low]` `[reject]` (Blind Hunter) a I/O Matrix da própria spec não enumera "corpo malformado"/"numero_ordem excede tamanho"/"ordens_criadas malformado" como cenários — rejeitado por regra: o fix é editar esta própria spec.
  - `[medium]` `[defer]` (Blind Hunter) `ExportadorVBA.Finalizar` usa `db.Begin()`/`tx.QueryRow`/`tx.Exec` sem nunca propagar o `context.Context` da requisição HTTP (sem `BeginTx(ctx,...)`/`...Context`) — uma requisição cancelada/com timeout no cliente não cancela a transação em andamento — adiado: mesmo padrão em TODAS as demais escritas do repositório (`fila.Assumir`/`MarcarPendencia`/`Comentar`, `exportacao.GerarLoteDespesa`/`GerarLoteObra`) — convenção já estabelecida em todo o backend, não um risco novo desta story.
  - `[low]` `[reject]` (Blind Hunter) nenhuma linha de `solicitacao_comentarios` (ou outro registro) é gravada ao finalizar, sucesso ou erro — sem rastro legível de quem finalizou e quando além de `status`/`versao` — rejeitado: nenhuma AC pede isso (diferente de Pendência/Comentário, cuja própria AC exige o comentário); mesmo raciocínio dos 2 achados "motivo"/"tipo_despesa" acima.
  - `[low]` `[patch]` (Edge Case Hunter) `Finalizar` nunca valida que `resultado` é exatamente `"sucesso"` ou `"erro"` antes de usá-lo para escolher o status terminal — qualquer outro valor se torna silenciosamente `finalizada_erro` (hoje inatingível via a única rota HTTP real, que já valida isso, mas é uma lacuna de defesa em profundidade) — ação: `Finalizar` passa a devolver um erro simples para qualquer valor fora de `{"sucesso","erro"}`.
  - `[medium]` `[patch]` (Verification Gap Reviewer, achado pré-verificado com citação de código e teste) `resolverLinhasCriar` compara `OrdemCriada.LinhaID` contra o id devolvido pelo banco por igualdade exata de string, mas o Postgres devolve colunas `uuid` já canonicalizadas em minúsculas enquanto `uuidFormatRegexp` (handler) aceita `linha_id` em maiúsculas/caixa mista e o repassa sem normalizar — um `linha_id` sintaticamente válido e correto, só com caixa diferente, é rejeitado com `ErrOrdensCriadasInvalidas` (400) — ação: normaliza ambos os lados (minúsculas) antes de montar/consultar o mapa `porID`; teste de regressão adicionado com `linha_id` em maiúsculas.

## Design Notes

**Por que `Finalizar` vive em `internal/exportacao`, não `internal/fila`:** AD-5 do Architecture Spine já amarra FR-14 a este pacote ("vale também para a transição de status feita pela exportação SAP... é a mesma linha de `solicitacoes`, o mesmo lock"), e o `GRANT UPDATE` em `solicitacoes` (migration 010) não distingue `internal/fila` de `internal/exportacao` — ambos usam a MESMA conexão/role privilegiada. Por isso `internal/fila.Comentar` nunca precisou saber o nome literal dos status terminais (Design Notes da Story 4.2): seu `CASE WHEN` já trata qualquer status fora de `{aberta,em_atendimento}` como "encerrado" — `finalizada_sucesso`/`finalizada_erro` reabrem exatamente como qualquer outro status teria, sem mudança nenhuma em `fila.go`.

**Por que `numero_ordem` vem do administrador, nunca gerado pelo backend:** não existe `ExportadorAPI` nem integração ao vivo com o SAP nesta v1 (Architecture Spine, Open Question "formato final da API SAP... ainda desconhecido"); o único jeito de uma ordem nascer de fato no SAP hoje é o robô/SAP GUI, fora deste sistema — o administrador roda esse processo manual e devolve o número real ao finalizar. Inventar um número aqui violaria 02-REQUISITOS-NEGOCIO-ENVIO-TI.md ("não criar ordem automaticamente até confirmação").

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros, sem diffs.
- `cd backend && go test ./... -count=1` -- expected: todos os pacotes passam, incluindo os novos testes de `internal/exportacao` e `handlers/finalizar_test.go`.

**Manual checks (if no CLI):**
- `docker-compose up` e confirmar que `014_finalizar_solicitacao.sql` roda sem erro e que os 2 novos GRANTs permitem a conexão privilegiada inserir em `obra_ordens`/atualizar `ordem_investimento`.

## Auto Run Result

**Resumo:** Implementada a Story 4.5 (Finalizar solicitação, Epic 4, FR-14): novo método `Finalizar` em `ExportadorSAP`/`ExportadorVBA` (`internal/exportacao`) que, numa única transação pela conexão PRIVILEGIADA, muda o `status` da solicitação via lock otimista (`finalizada_sucesso`/`finalizada_erro`), migra as linhas `exportacoes_sap` da solicitação de `GERADO` para `FINALIZADO`, e — só quando o resultado é sucesso — resolve cada linha de Obras `ordem_investimento='CRIAR'` criando a ordem real em `obra_ordens` a partir do `numero_ordem` informado pelo administrador. Qualquer falha em qualquer etapa recusa a operação inteira. Nenhuma ação fora deste repositório é necessária — diferente das Stories 4.3/4.4, não há aqui nenhum ativo externo nem configuração de ambiente pendente: `numero_ordem` é um dado de negócio comum, informado pelo administrador a cada chamada, não uma dependência de infraestrutura.

**Arquivos alterados:**
- `backend/migrations/014_finalizar_solicitacao.sql` — NOVA: 2 GRANTs que faltavam para `fb_apu05_privilegiado` (`INSERT` em `obra_ordens`; `UPDATE (ordem_investimento)` em `solicitacao_obras_linhas`).
- `backend/internal/exportacao/exportacao.go` — adiciona `Finalizar` à interface `ExportadorSAP`; tipos `OrdemCriada`/`Solicitacao`; sentinelas `ErrConflitoVersao`/`ErrNaoExportada`/`ErrOrdensCriadasInvalidas`/`ErrOrdemJaExiste`.
- `backend/internal/exportacao/finalizar.go` — NOVO: `ExportadorVBA.Finalizar` (lock otimista + migração de `exportacoes_sap` + resolução de linhas CRIAR, atômico); validação defensiva de `resultado` e de `NumeroOrdem` não-vazio; normalização de caixa (lowercase) no casamento de `linha_id` contra o `uuid` canonicalizado do Postgres.
- `backend/handlers/finalizar.go` — NOVO: `FinalizarSolicitacaoHandler` (valida corpo, delega a `exportacao.Finalizar`, traduz sentinelas), incluindo checagem de tamanho máximo (50) de `numero_ordem`.
- `backend/main.go` — registra `POST /api/solicitacoes/{id}/finalizar`.
- `backend/internal/exportacao/finalizar_test.go`, `backend/handlers/finalizar_test.go` — cobrem a I/O Matrix completa (12 + 15 testes) mais 2 testes de regressão dos patches (case-insensitive `linha_id`; `linha_id` duplicado com contagem batendo).

**Review findings — breakdown:** 13 total — 5 `patch` (4 `[low]`: tamanho máximo de `numero_ordem`, `NumeroOrdem` vazio não validado no domínio, teste faltante para `linha_id` duplicado, `resultado` fora de `{sucesso,erro}` não validado no domínio; 1 `[medium]`: casamento de `linha_id` sensível a caixa rejeitando UUIDs válidos em maiúsculas — achado pré-verificado pela camada Verification Gap); 2 `defer` (1 `[low]`: corpo malformado/`io.ReadAll`/overflow do `MaxBytesReader` sem teste — mesmo padrão já presente, sem teste, em todos os demais `lerXRequest` do pacote; 1 `[medium]`: `Finalizar` não propaga `context.Context` da requisição — mesmo padrão em toda escrita do repositório, não introduzido por esta story); 5 `reject` (campo "motivo" de erro não pedido por nenhuma AC, mensagem de conflito de `numero_ordem` não diferenciada — cenário improvável, `tipo_despesa`/`tipo_faturamento` nunca populados — não pedido por nenhuma AC, falta de rastro de comentário ao finalizar — não pedido por nenhuma AC, lacunas da própria I/O Matrix da spec — fix seria editar esta spec); 1 `false` (ausência de idempotência — é o comportamento pretendido do lock otimista AD-5, idêntico a Assumir/MarcarPendencia).

**Follow-up review recommendation:** `false`. Só 1 entrada `medium` foi patcheada nesta passada (a regra exige 2+ `medium` ou qualquer `high` para recomendar follow-up); as 4 entradas restantes patcheadas são `low`.

**Verificação:** `cd backend && go build ./... && go vet ./... && gofmt -l .` — sem erros, sem diffs, re-executado após os 5 patches. `cd backend && go test ./... -count=1` — todos os pacotes passam, incluindo os 12 testes de `internal/exportacao` e 15 de `handlers` para Finalizar (27 no total, incluindo os 2 de regressão dos patches). Matrix Test Audit: as 10 linhas da I/O Matrix têm pelo menos um teste cobrindo-as, executado e aprovado, nos dois níveis (domínio e handler). `docker-compose up` (manual check) não pôde ser repetido neste ambiente — sem `docker` instalado.

**Riscos residuais:** `Finalizar` não propaga `context.Context` à transação (mesma lacuna estrutural de todo o backend, deferida); o corpo malformado de `lerFinalizarRequest` não tem teste direto (mesma lacuna estrutural de todos os `lerXRequest` do pacote, deferida). Nenhum risco novo introduzido por esta story além dos já deferidos.
