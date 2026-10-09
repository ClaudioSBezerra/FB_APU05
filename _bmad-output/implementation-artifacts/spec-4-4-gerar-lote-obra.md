---
title: 'Gerar lote Obra'
type: 'feature'
created: '2026-10-09'
status: 'awaiting-operator'
baseline_revision: 'c4013f555eed4425d246969acdf549fa74887e0a'
review_loop_iteration: 0
followup_review_recommended: false
context: ['{project-root}/_bmad-output/planning-artifacts/architecture/architecture-FB_APU05-2026-10-06/ARCHITECTURE-SPINE.md']
warnings: ['oversized']
operator_actions:
  - 'Obter o arquivo .xlsm real do robô SAP para o lote Obra (ativo de negócio externo, não existe neste repositório) e confirmar se o layout de colunas implementado em montarSheetDataXMLObra (backend/internal/exportacao/xlsm.go) — tipo_solicitacao/solicitacao_id/classificacao/local_obra_codigo/subgrupo_codigo/ordem_investimento/valor/lado/solicitante — bate com o que a macro de fato espera; ajustar a ordem/conteúdo das colunas se não bater.'
  - 'Confirmar com a equipe do robô se a aba de rascunho do template Obra se chama de fato "SAP Export" (convenção reaproveitada da Story 4.3 por falta de informação sobre o robô de Obra) ou outro nome; ajustar a constante abaSAPExportNome/a resolução via workbook.xml em backend/internal/exportacao/xlsm.go se o nome real for diferente.'
  - 'Publicar o .xlsm real do lote Obra em cada ambiente (dev/staging/produção) e configurar a variável de ambiente EXPORT_TEMPLATE_LOTE_OBRA apontando para esse caminho — sem isso, POST /api/solicitacoes/lote-obra responde 503 sempre que houver ao menos 1 solicitação incluível (comportamento correto e testado, não um defeito).'
deferred:
  - summary: >-
      GerarLoteObra não deduplica solicitacao_ids repetidos no mesmo pedido —
      uma solicitação repetida é lida/validada 2x e entra 2x em linhas/
      solicitacoes_incluidas (duplicando linhas no .xlsm e no array de
      resposta), embora exportacoes_sap grave só 1x via ON CONFLICT DO
      NOTHING.
    evidence: |-
      Mesmo formato de loop sem deduplicação já existe, idêntico, em
      GerarLoteDespesa (internal/exportacao/vba.go, Story 4.3, já revisada e
      aceita) — não é um risco novo introduzido por esta story, mesma
      categoria pré-existente.
    location: >-
      backend/internal/exportacao/obra.go (GerarLoteObra)
    severity: medium
  - summary: >-
      Nenhuma re-checagem de status/dono sob lock entre a validação por
      solicitação (fora de transação) e a transação de escrita de
      gravarLoteObra (janela TOCTOU).
    evidence: |-
      GerarLoteDespesa (internal/exportacao/vba.go, Story 4.3) tem exatamente
      a mesma forma — valida fora da transação, grava em transação separada
      — não é um risco novo desta story.
    location: >-
      backend/internal/exportacao/obra.go (GerarLoteObra)
    severity: medium
  - summary: >-
      GerarLoteObra faz 2 round-trips sequenciais ao banco por
      solicitacao_id, sem lote/paginação nem teto explícito (só o limite
      indireto de 1 MB do corpo da requisição).
    evidence: |-
      GerarLoteDespesa (internal/exportacao/vba.go, Story 4.3) tem exatamente
      o mesmo formato de loop — não é um risco novo desta story.
    location: >-
      backend/internal/exportacao/obra.go (GerarLoteObra)
    severity: medium
---

<intent-contract>

## Intent

**Problem:** Solicitações `obras` aprovadas e em atendimento não têm nenhum caminho para alimentar o robô SAP — `ExportadorSAP.GerarLoteObra` ainda não existe (Boundaries "Never" da spec 4.3).

**Approach:** `ExportadorVBA.GerarLoteObra` (`internal/exportacao`, mesma impl de `GerarLoteDespesa`): recalcula elegibilidade por solicitação (dono=sessão, `status=em_atendimento`, `tipo_solicitacao='obras'`), descarta individualmente linhas com `ordem_investimento='CRIAR'` (valor≤0 já é inatingível por `CHECK (valor > 0)`, migration 009), e quando nenhuma linha de uma solicitação sobra, ela entra em `bloqueados` com motivo em vez de barrar o lote. As solicitações restantes geram 1 linha `exportacoes_sap` cada (reaproveita a tabela da Story 4.3, nunca duplica no replay) e um `.xlsm` próprio via `montarXLSM` (xlsm.go, generalizado nesta story para aceitar o XML da planilha já montado, reaproveitado por Despesa e Obra).

## Boundaries & Constraints

**Always:**
- `administrador_id` sempre de `GetUserIDFromContext`; por `solicitacao_id` do corpo, servidor recalcula `status`/`administrador_id`/`tipo_solicitacao` no banco (mesmo princípio da Story 4.3).
- Falha de dono/status/tipo (403/409/400) aborta o LOTE INTEIRO, igual à Story 4.3 — só a ausência de linha válida (`ordem_investimento='CRIAR'`) é tolerada por solicitação, via `bloqueados`, nunca via erro.
- 1 linha `exportacoes_sap` por solicitação EFETIVAMENTE incluída (não bloqueada); `chave_idempotencia` obrigatória no corpo (coluna `NOT NULL`) e o mesmo `ON CONFLICT (administrador_id, chave_idempotencia, solicitacao_id) DO NOTHING` da Story 4.3 garante replay idempotente.
- Toda leitura de `solicitacao_obras_linhas`/`locais_obra`/`subgrupos_despesa` e toda escrita em `exportacoes_sap` passam por `internal/exportacao` via conexão PRIVILEGIADA (AD-4).
- `.xlsm` próprio (`EXPORT_TEMPLATE_LOTE_OBRA`): mesmas restrições AD-12 da Story 4.3 (nunca tocar `vbaProject.bin`, remover `xl/calcChain.xml`, aba de rascunho nunca oculta).

**Never:**
- Nunca alterar `status`/`versao` de `solicitacoes` (finalizar é a Story 4.5); nunca criar a ordem real no SAP para uma linha `CRIAR` (só acontece na finalização).
- Nunca implementar aviso de "verba duplicada"/conflito de reuso de `chave_idempotencia` aqui — exclusivo da Story 4.3 (Cross-Story Dependencies do epic context); ver Design Notes.
- Nunca gravar `exportacoes_sap` para uma solicitação bloqueada.
- Não implementar Finalizar.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Geração com sucesso | IDs `obras`, `em_atendimento`, dono=sessão, todas as linhas com ordem != `CRIAR`, template presente | 200; `lote_id`, `arquivo_base64`, `solicitacoes_incluidas`, `bloqueados: []` | No error expected |
| Linha `CRIAR` descartada, solicitação ainda válida | solicitação com 2 linhas, 1 `ordem_investimento='CRIAR'` | 200; solicitação incluída, só a linha válida entra no arquivo | No error expected |
| Solicitação só com linha(s) `CRIAR` | todas as linhas da solicitação são `CRIAR` | 200; solicitação NÃO entra no arquivo nem em `exportacoes_sap`; aparece em `bloqueados` com motivo | No error expected |
| Todo o lote bloqueado | todas as solicitações do lote caem no caso acima | 200; `solicitacoes_incluidas: []`, sem `lote_id`/`arquivo_base64`, `bloqueados` com todas | No error expected |
| Repetir mesma chave | 2ª chamada, mesma `chave_idempotencia`+mesmos IDs | 200; mesmo `lote_id`; nenhuma linha nova em `exportacoes_sap` | No error expected |
| Solicitação de outro administrador | `administrador_id` da linha ≠ chamador | — | 403 |
| Solicitação não `em_atendimento` | `status` = `aberta`/`pendente`/outro | — | 409 |
| ID inexistente | UUID não encontrado | — | 404 |
| Tipo diferente de `obras` incluído | algum ID com `tipo_solicitacao != 'obras'` | — | 400 |
| Template ausente, com ao menos 1 incluível | `EXPORT_TEMPLATE_LOTE_OBRA` não aponta para arquivo legível | — | 503 |

</intent-contract>

## Code Map

- `backend/internal/exportacao/exportacao.go:28-30,77-80` -- interface `ExportadorSAP` (adiciona `GerarLoteObra`), tipo `Bloqueio{SolicitacaoID, Motivo}` (novo), `ErrTipoNaoElegivel` (ajustar doc-comment: passa a cobrir as duas direções — `tipo='obras'` no lote Despesa E `tipo!='obras'` no lote Obra); reaproveita `ErrNaoEncontrada`/`ErrNaoAutorizado`/`ErrElegibilidadeInvalida`/`ErrTemplateAusente` sem mudança de comportamento.
- `backend/internal/exportacao/vba.go:34-36,70,128,133` -- `ExportadorVBA` struct (adiciona campo `TemplatePathObra string`, ao lado de `TemplatePath` já existente — sem renomear nada da Story 4.3); `GerarLoteDespesa` passa a chamar `montarXLSM(templateBytes, montarSheetDataXML(linhas))` (ver próximo item) em vez de `montarXLSM(templateBytes, linhas)`, mesmo resultado.
- `backend/internal/exportacao/xlsm.go:43-94,177-210,246-281` -- `montarXLSM(templateBytes []byte, linhas []linhaExportacao)` passa a ser um wrapper fino sobre uma nova `montarXLSMComLinhas(templateBytes []byte, sheetDataXML string) ([]byte, error)` (mesmo corpo de hoje, só troca `escreverAbaReescrita(zw, f, linhas)` por `escreverAbaReescrita(zw, f, sheetDataXML)`); `escreverAbaReescrita` passa a receber `sheetDataXML string` pronto em vez de `linhas`. Zero mudança de assinatura/comportamento de `montarXLSM` nem dos testes existentes (`xlsm_test.go` chama só `montarXLSM`). `montarSheetDataXML` (Despesa) fica como está; nova `montarSheetDataXMLObra` ao lado, mesmos helpers (`colunaParaLetra`/`escaparTextoXML`/`formatarValorExportacao`, já genéricos).
- `backend/internal/exportacao/vba.go:192-261` -- padrão de `buscarSolicitacaoParaExportacao`/`buscarLancamentosParaExportacao`/fallback 404→403→409→400 a replicar (atenção: `Classificacao` deve ser lida como `sql.NullString`, não `string` — uma solicitação com `tipo_solicitacao != 'obras'` tem `classificacao` NULL e o `Scan` falharia antes do 400 ser decidido).
- `backend/migrations/009_obras_multilinha.sql:88-99` -- schema de `solicitacao_obras_linhas` (`lado` nullable, `ordem_investimento` é o próprio `numero_ordem` em texto — não é FK —, `valor` já `CHECK (valor > 0)`); `locais_obra`/`subgrupos_despesa` (migration 009) para resolver os códigos de negócio das linhas.
- `backend/migrations/012_exportacoes_sap.sql` -- `exportacoes_sap` reaproveitada SEM alteração de schema (mesma `UNIQUE`/`ON CONFLICT`); esta story cria `013_lote_obra_grants.sql` só com os `GRANT SELECT` que faltam para `fb_apu05_privilegiado` (`usuarios` já concedido na 012).
- `backend/handlers/exportacao.go` (completo) -- `GerarLoteDespesaHandler`/`tratarErroExportacao`/`jsonErr`/`uuidFormatRegexp` a reaproveitar; adiciona `GerarLoteObraHandler` no mesmo arquivo.
- `backend/main.go:371-379` -- padrão de registro de rota/comentário da Story 4.3; nova rota entra depois da linha 379.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/013_lote_obra_grants.sql` -- NOVA: `GRANT SELECT ON solicitacao_obras_linhas, locais_obra, subgrupos_despesa TO fb_apu05_privilegiado`.
- `backend/internal/exportacao/exportacao.go` -- adiciona `GerarLoteObra(db *sql.DB, solicitacaoIDs []string, administradorID, chaveIdempotencia string) (Lote, []Bloqueio, error)` à interface `ExportadorSAP`; tipo `Bloqueio{SolicitacaoID, Motivo string}`; ajusta doc-comment de `ErrTipoNaoElegivel`.
- `backend/internal/exportacao/vba.go` -- adiciona campo `TemplatePathObra string` a `ExportadorVBA`; troca a chamada de `montarXLSM` em `GerarLoteDespesa` para `montarXLSM(templateBytes, montarSheetDataXML(linhas))`.
- `backend/internal/exportacao/xlsm.go` -- extrai `montarXLSMComLinhas(templateBytes []byte, sheetDataXML string) ([]byte, error)` do corpo atual de `montarXLSM` (que passa a ser um wrapper de 1 linha); `escreverAbaReescrita` passa a receber `sheetDataXML string`; nova `montarSheetDataXMLObra(linhas []linhaExportacaoObra) string` (colunas: tipo_solicitacao="obras"/solicitacao_id/classificacao/local_obra_codigo/subgrupo_codigo/ordem_investimento/valor/lado/solicitante_nome — layout melhor-esforço, mesma ressalva de confirmação da Story 4.3).
- `backend/internal/exportacao/obra.go` -- NOVO: `linhaExportacaoObra`/`infoSolicitacaoObra` (com `Classificacao sql.NullString`); `ExportadorVBA.GerarLoteObra`: por ID, recalcula elegibilidade (404→403→409→400, mesma ordem da Story 4.3) via `buscarSolicitacaoObraParaExportacao`; lê linhas via `buscarLinhasObraParaExportacao` (`solicitacao_obras_linhas JOIN locais_obra/subgrupos_despesa`, `ORDER BY created_at ASC, id ASC`), descarta as com `ordem_investimento='CRIAR'`; sem linha válida → acumula em `[]Bloqueio` (motivo fixo: "nenhuma linha com ordem de investimento já existente no SAP") e não entra no lote; com `len(incluidos)==0` devolve `Lote{}, bloqueados, nil` sem ler template; senão lê `e.TemplatePathObra` (ausente/erro → `ErrTemplateAusente`), monta `.xlsm` via `montarXLSMComLinhas(templateBytes, montarSheetDataXMLObra(linhas))`, grava `exportacoes_sap` (1 linha por `incluido`, `ON CONFLICT DO NOTHING`, mesma transação, `lote_id` relido no final — mesmo padrão de `gravarLote`, sem a parte de aviso/comentário).
- `backend/handlers/exportacao.go` -- NOVO: `gerarLoteObraRequest{SolicitacaoIDs []string; ChaveIdempotencia string}`; `lerGerarLoteObraRequest` (mesma validação de `lerGerarLoteDespesaRequest`, sem o campo de aviso); `exportTemplateLoteObraPath`/`exportTemplateLoteObraDefault = "internal/exportacao/templates/lote_obra.xlsm"`; `GerarLoteObraHandler(db)` chama `exportacao.ExportadorVBA{TemplatePathObra: exportTemplateLoteObraPath()}.GerarLoteObra(...)`, reusa `tratarErroExportacao`; resposta 200 com `{lote_id, arquivo_nome: "lote_obra_"+lote_id+".xlsm", arquivo_base64, solicitacoes_incluidas, bloqueados}` quando há ao menos 1 incluído, ou só `{solicitacoes_incluidas: [], bloqueados}` (sem `lote_id`/`arquivo_*`) quando todos bloqueados.
- `backend/main.go` -- registra `POST /api/solicitacoes/lote-obra` (`RequireAuth(withPrivilegedDB(handlers.GerarLoteObraHandler), "administrador")`), após a rota de lote-despesa.
- `backend/internal/exportacao/{obra_test.go}`, `backend/internal/exportacao/xlsm_test.go` (extensão: `montarSheetDataXMLObra`), `backend/handlers/exportacao_test.go` (extensão) -- cobrem a I/O Matrix; `obra_test.go` cobre também o caso "todo o lote bloqueado" e a leitura `sql.NullString` de `classificacao` para tipo != obras.

**Acceptance Criteria:**
- Given uma solicitação de Obras `em_atendimento` com 1 linha de ordem já existente no SAP e 1 linha `ordem_investimento='CRIAR'`, when o administrador gera o lote Obra, then ela é incluída no arquivo e em `exportacoes_sap`, mas só com a linha válida.
- Given uma solicitação de Obras cujas linhas são todas `ordem_investimento='CRIAR'`, when o administrador gera o lote Obra junto com outras solicitações válidas, then ela aparece em `bloqueados` com o motivo, sem barrar a geração das demais.

## Spec Change Log

- **Resolução de contradição interna (vba.go vs. xlsm.go, Code Map):** a entrada de `vba.go` dizia que `GerarLoteDespesa` passaria a chamar `montarXLSM(templateBytes, montarSheetDataXML(linhas))`, o que exigiria `montarXLSM` aceitar `sheetDataXML string` como 2º parâmetro; a entrada de `xlsm.go` exigia explicitamente "zero mudança de assinatura/comportamento de `montarXLSM` nem dos testes existentes (`xlsm_test.go` chama só `montarXLSM`)" — e `xlsm_test.go` de fato chama `montarXLSM(template, linhas)`/`montarXLSM(template, nil)` com `linhas []linhaExportacao`, o que não compilaria se o 2º parâmetro fosse `string`. Resolvido a favor da restrição mais forte e verificável (testes existentes intocados): `montarXLSM(templateBytes []byte, linhas []linhaExportacao) ([]byte, error)` manteve assinatura e local de chamada em `GerarLoteDespesa` inalterados, implementado como wrapper de 1 linha sobre a nova `montarXLSMComLinhas(templateBytes []byte, sheetDataXML string) ([]byte, error)` — mesmo resultado funcional, `vba.go` ficou sem alteração de chamada.

## Review Triage Log

### 2026-10-09 — Review pass
- verdicts: 14 findings — high 0, medium 8, low 3, false 3, maybe-false 0
- findings:
  - `[medium]` `[patch]` (Blind Hunter) `gravarLoteObra` nunca checa conflito de `chave_idempotencia` (ao contrário de `verificarConflitoIdempotencia` da Story 4.3) — reusar a mesma chave com um conjunto de `solicitacao_ids` diferente (ex.: uma solicitação antes bloqueada vira válida e a chamada é repetida) mescla silenciosamente em vez de 409 — ação: `gravarLoteObra` passa a reler um `lote_id` já existente para `(administrador_id, chave_idempotencia)` ANTES de gerar um candidato novo, reusando-o quando existir, para que todas as linhas da mesma chave fiquem sempre sob o mesmo `lote_id`, qualquer que seja o subconjunto incluído em cada chamada.
  - `[medium]` `[defer]` (Blind Hunter) `solicitacao_ids` duplicados no mesmo pedido não são deduplicados — a solicitação é lida/validada 2x e entra 2x em `linhas`/`solicitacoes_incluidas` (duplicando linhas no `.xlsm` e no array de resposta), embora `exportacoes_sap` grave só 1x via `ON CONFLICT DO NOTHING` — adiado: `GerarLoteDespesa` (vba.go, Story 4.3, já revisada e aceita) tem exatamente o mesmo formato de loop sem deduplicação; não é um risco novo introduzido por esta story.
  - `[low]` `[patch]` (Blind Hunter) `GerarLoteObraHandler`, ramo "todo o lote bloqueado" (`lote.LoteID == ""`), nunca loga — toda outra saída (sucesso, erros) loga, criando um ponto cego de auditoria — ação: adicionado `log.Printf` nesse ramo, mesmo padrão do ramo de sucesso.
  - `[low]` `[reject]` (Blind Hunter) mensagem de `ErrTipoNaoElegivel` generalizada perde a especificidade por direção (despesa vs. obra) — rejeitado: a própria rota chamada (`/lote-despesa` vs. `/lote-obra`) já informa a direção ao chamador; nenhuma mensagem (antes ou depois) citava o `solicitacao_id` causador; o fix exigiria reintroduzir mensagens por rota (mais que uma correção direta) para um ganho marginal.
  - `[false]` `[reject]` (Blind Hunter) `buscarSolicitacaoObraParaExportacao` faz `JOIN usuarios u ON u.id = s.solicitante_id`; um `usuarios` órfão devolveria 404 "não encontrada" mascarando um problema de integridade de dados — refutação: `solicitacoes.solicitante_id` é `NOT NULL REFERENCES usuarios(id)` sem `ON DELETE` (migration 006:93) — o Postgres recusa (RESTRICT, default) deletar um `usuarios` referenciado por qualquer `solicitacoes`; a precondição (linha órfã) não pode ocorrer neste schema.
  - `[medium]` `[defer]` (Blind Hunter) `GerarLoteObra` faz 2 round-trips sequenciais ao banco por `solicitacao_id`, sem lote/paginação nem teto explícito (só o limite indireto de 1 MB do corpo) — adiado: `GerarLoteDespesa` (vba.go, Story 4.3) tem exatamente o mesmo formato de loop; não é um risco novo desta story.
  - `[low]` `[reject]` (Blind Hunter) `Bloqueio.Motivo` é uma string PT-BR fixa, sem código estável para a API — rejeitado: só existe 1 motivo possível hoje (`motivoBloqueioSemLinhaValida`); nenhum consumidor precisa hoje distinguir "por quê" além de "foi bloqueado"; adicionar um enum é superfície pública nova para uma necessidade hipotética que a AC não pede.
  - `[false]` `[reject]` (Blind Hunter) `013_lote_obra_grants.sql` não tem `REVOKE`/rollback — refutação: nenhuma migration do repositório (001-012, conferido) tem contraparte de rollback; é a convenção já estabelecida (forward-only), não uma lacuna desta story.
  - `[medium]` `[patch]` (Blind Hunter, mesma causa da 1ª linha) nenhum teste cobre (a) `solicitacao_ids` duplicados nem (b) replay de `chave_idempotencia` com conjunto de IDs diferente — ação: teste de regressão adicionado para (b) junto do patch da 1ª linha (`TestGerarLoteObra_ReplaySubconjuntoDiferenteReusaLoteID`); a parte (a) acompanha o `defer` da 2ª linha (sem teste novo, mesma lacuna pré-existente aceita em 4.3).
  - `[medium]` `[defer]` (Edge Case Hunter, mesma causa da 2ª linha) `solicitacao_ids` repetido é processado 2x (duplica no `.xlsm`/resposta) — mesma disposição `defer` já registrada acima (padrão idêntico herdado de `GerarLoteDespesa`).
  - `[medium]` `[patch]` (Edge Case Hunter, mesma causa da 1ª linha) reusar `chave_idempotencia` com um conjunto de `solicitacao_id` diferente pode devolver um `lote_id` que não corresponde ao que foi de fato persistido nesta chamada — mesma ação já registrada (reler `lote_id` existente antes de gerar um candidato novo).
  - `[medium]` `[defer]` (Edge Case Hunter) nenhuma re-checagem de `status`/dono sob lock entre a validação por solicitação e a transação de escrita de `gravarLoteObra` (TOCTOU) — adiado: `GerarLoteDespesa` (vba.go, Story 4.3) tem exatamente a mesma forma (valida fora da transação, grava em transação separada); não é um risco novo desta story.
  - `[false]` `[reject]` (Edge Case Hunter) o motivo fixo de `Bloqueio` induziria ao erro para uma solicitação com ZERO linhas de obra (distinto de "todas CRIAR") — refutação: `validarObrasLinhasEstrutura` (handlers/solicitacoes.go:961-963) rejeita na criação qualquer solicitação de obras com zero linhas (400), e não existe caminho de UPDATE/DELETE em `solicitacao_obras_linhas` — `buscarLinhasObraParaExportacao` sempre devolve ≥1 linha para uma solicitação elegível; `len(linhasValidas)==0` só pode significar que todas as linhas existentes eram `CRIAR`, então o motivo fixo é sempre preciso.
  - `[medium]` `[patch]` (Verification Gap Reviewer, mesma causa da 1ª linha — achado pré-verificado pela própria camada, com citação de código) reusar a mesma `chave_idempotencia` com um conjunto de solicitações diferente do anterior (ex.: uma solicitação bloqueada na 1ª chamada passa a ser válida e a chamada é repetida) pode deixar 2 `lote_id` distintos sob a mesma `(administrador_id, chave_idempotencia)`, e a releitura final sem `ORDER BY` devolve um dos dois arbitrariamente, divergindo do `solicitacoes_incluidas`/`.xlsm` da resposta atual — mesma ação já registrada.

## Design Notes

**Por que `GerarLoteObra` não implementa aviso de "verba duplicada" nem conflito de reuso de `chave_idempotencia`:** o epic context atribui esse mecanismo explicitamente à Story 4.3 ("O aviso de verba duplicada em 4.3 depende..." — Cross-Story Dependencies), e nem a AC do epics.md nem o FR-15 do PRD mencionam isso para o lote Obra. Adicionar a mesma checagem de conflito da Story 4.3 (`verificarConflitoIdempotencia`, que compara o conjunto de IDs do pedido contra o já gravado) seria incorreto aqui sem reescrever a lógica: como solicitações *bloqueadas* nunca são gravadas em `exportacoes_sap`, comparar contra o pedido CRU (em vez do subconjunto incluído) faria um replay idempotente legítimo (mesma chave, mesmos IDs, algum bloqueado) ser confundido com conflito. Fora do que a AC exige — não implementado.

**Por que não há checagem de código para `valor <= 0`:** `solicitacao_obras_linhas.valor` já é `CHECK (valor > 0)` (migration 009) e não há caminho de escrita que viole isso hoje — mesmo raciocínio da Story 4.3 para "inclusão só com valores positivos". Só `ordem_investimento='CRIAR'` é, na prática, o critério de descarte de linha.

**`TemplatePathObra` como campo novo, não renomeação de `TemplatePath`:** evita qualquer mudança em `vba.go`/`vba_test.go` da Story 4.3 (já revisada, status `awaiting-operator`) só para acomodar um segundo template no mesmo struct.

**Mesma ressalva de ativo externo da Story 4.3:** o `.xlsm`/macro real do robô SAP para o lote Obra (transação de ordem de investimento, distinta de `KP06`) também não existe neste repositório nem está documentado em `docs/referencia/`. O layout de colunas de `montarSheetDataXMLObra` é melhor-esforço (mesmo nome de aba "SAP Export" por convenção, já que nenhuma outra convenção é conhecida) — PRECISA de confirmação humana antes de produção, mesmo padrão de `operator_actions` da Story 4.3.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros, sem diffs de formatação.
- `cd backend && go test ./... -count=1` -- expected: todos os pacotes passam, incluindo os testes novos/estendidos de `internal/exportacao` e `handlers/exportacao_test.go`.

**Manual checks (if no CLI):**
- `docker-compose up` localmente e confirmar que `013_lote_obra_grants.sql` roda sem erro e que os `GRANT`s permitem `internal/exportacao` ler `solicitacao_obras_linhas`/`locais_obra`/`subgrupos_despesa` pela conexão privilegiada.

## Auto Run Result

**Resumo:** Implementada a Story 4.4 (Gerar lote Obra, Epic 4): `ExportadorVBA.GerarLoteObra` recalcula elegibilidade por solicitação (dono=sessão, `em_atendimento`, `tipo_solicitacao='obras'`), descarta individualmente linhas com `ordem_investimento='CRIAR'` e, quando nenhuma linha sobra, a solicitação entra em `bloqueados` com motivo em vez de barrar o lote inteiro. Solicitações restantes geram 1 linha `exportacoes_sap` cada (reaproveitando a tabela/UNIQUE da Story 4.3) e um `.xlsm` próprio via `montarXLSM`/`montarXLSMComLinhas` (xlsm.go, generalizado nesta story para ser reaproveitado por Despesa e Obra sem mudar a assinatura pública nem os testes da Story 4.3).

**Arquivos alterados:**
- `backend/migrations/013_lote_obra_grants.sql` — NOVA: `GRANT SELECT` em `solicitacao_obras_linhas`/`locais_obra`/`subgrupos_despesa` para `fb_apu05_privilegiado`.
- `backend/internal/exportacao/exportacao.go` — `GerarLoteObra` adicionado à interface `ExportadorSAP`; tipo `Bloqueio{SolicitacaoID, Motivo}`; doc-comment/mensagem de `ErrTipoNaoElegivel` generalizada para cobrir as duas direções (despesa/obra).
- `backend/internal/exportacao/vba.go` — campo `TemplatePathObra` adicionado a `ExportadorVBA` (sem renomear `TemplatePath`); `GerarLoteDespesa` sem alteração de assinatura/comportamento.
- `backend/internal/exportacao/xlsm.go` — `montarXLSM` virou wrapper de 1 linha sobre a nova `montarXLSMComLinhas(templateBytes, sheetDataXML string)`; nova `montarSheetDataXMLObra` com o layout de colunas do lote Obra.
- `backend/internal/exportacao/obra.go` — NOVO: `GerarLoteObra` (elegibilidade, descarte de linha `CRIAR`, bloqueio por solicitação, idempotência via `ON CONFLICT DO NOTHING` com `lote_id` relido/reusado antes de gerar um candidato novo — patch desta revisão).
- `backend/handlers/exportacao.go` — NOVO: `GerarLoteObraHandler`, `gerarLoteObraRequest`, `lerGerarLoteObraRequest`, `exportTemplateLoteObraPath`, `bloqueadosParaJSON`; log adicionado no ramo "todo o lote bloqueado" (patch desta revisão).
- `backend/main.go` — registra `POST /api/solicitacoes/lote-obra`.
- `backend/internal/exportacao/obra_test.go` (novo), `backend/internal/exportacao/xlsm_test.go` (extensão), `backend/handlers/exportacao_test.go` (extensão) — cobrem a I/O Matrix, incluindo o teste de regressão do patch (`TestGerarLoteObra_ReplaySubconjuntoDiferenteReusaLoteID`).

**Review findings — breakdown:** 14 total — 4 `patch` (1 grupo `[medium]` com 4 relatos independentes de 3 camadas — Blind Hunter, Edge Case Hunter, Verification Gap Reviewer — convergindo na mesma causa: `gravarLoteObra` podia deixar 2 `lote_id` distintos sob a mesma `chave_idempotencia` quando o subconjunto incluível mudava entre chamadas; corrigido reutilizando o `lote_id` já existente antes de gerar um candidato novo, com teste de regressão novo; + 1 `[low]` — ramo "todo o lote bloqueado" nunca logava, corrigido); 4 `defer` (duplicação de `solicitacao_ids` sem dedupe, TOCTOU entre validação e escrita, round-trips sequenciais sem lote/teto — todos padrões idênticos já existentes e aceitos em `GerarLoteDespesa`/Story 4.3, não introduzidos por esta story); 6 `reject` (2 `[low]` — mensagem de erro generalizada e `Bloqueio.Motivo` sem código estável, ambos com ganho marginal/fix desproporcional — e 3 `[false]` — JOIN com `usuarios` órfão impossível por FK `RESTRICT`, migration sem rollback seguindo convenção já estabelecida do repositório, motivo de bloqueio impreciso para zero-linhas impossível por invariante de criação).

**Follow-up review recommendation:** `false`. Só 1 entrada `[medium]` foi patcheada nesta passada (a regra exige 2+ `medium` ou qualquer `high`); a outra entrada patcheada é `[low]`.

**Verificação:** `cd backend && go build ./... && go vet ./... && gofmt -l .` — sem erros, sem diffs, re-executado após os 2 patches. `cd backend && go test ./... -count=1` — todos os pacotes passam, incluindo os testes novos/estendidos de `internal/exportacao` e `handlers`. Matrix Test Audit: as 10 linhas da I/O Matrix têm pelo menos um teste cobrindo-as, executado e aprovado (verificado antes e depois dos patches). `docker-compose up` (manual check) não pôde ser repetido neste ambiente — sem `docker` instalado.

Status final `awaiting-operator` (não `done`): o `.xlsm`/macro real do robô SAP para o lote Obra é um ativo de negócio externo que não existe neste repositório nem está documentado em `docs/referencia/` (Design Notes) — o layout de colunas e o nome da aba de rascunho implementados são melhor-esforço e precisam de confirmação humana antes de uso em produção, mesmo padrão já estabelecido pela Story 4.3. Ver `operator_actions` no frontmatter.

**Riscos residuais:** os 4 itens `defer` (acima, registrados também em `deferred` no frontmatter) são lacunas estruturais já presentes e aceitas desde a Story 4.3, não específicas desta story. Fora isso, nenhum risco novo não documentado: os riscos de ativo externo já estão cobertos por `operator_actions`.
