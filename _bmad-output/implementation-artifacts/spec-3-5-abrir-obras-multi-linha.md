---
title: 'Abrir Obras multi-linha'
type: 'feature'
created: '2026-10-08'
status: 'done'
baseline_revision: '4638b07e94feee64905d0014a49071438f679210'
review_loop_iteration: 1
followup_review_recommended: false
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-3-context.md'
  - '{project-root}/_bmad-output/planning-artifacts/architecture/architecture-FB_APU05-2026-10-06/ARCHITECTURE-SPINE.md'
warnings: [oversized]
deferred:
  - summary: >-
      `locais_obra`/`subgrupos_despesa` resolvem `codigo` case-insensitive
      (`UPPER(codigo)`) contra uma constraint UNIQUE case-sensitive, então
      duas linhas cujo código difere só na caixa produziriam resolução
      ambígua no import CSV.
    evidence: |-
      Confirmado por leitura (`resolveLocalObraIDPorCodigo`/
      `resolveSubgrupoDespesaIDPorCodigo`, cadastros_csv.go), mas é o MESMO
      padrão já existente em `centros_custo`/`resolveCentroCustoIDPorCodigo`
      (migration 003, Story 2.1), fielmente replicado aqui — não introduzido
      por esta story.
    location: 'backend/handlers/cadastros_csv.go (resolveLocalObraIDPorCodigo/resolveSubgrupoDespesaIDPorCodigo)'
    severity: low
  - summary: >-
      Nenhuma checagem de positividade em `aprovadores_obra.teto` no decode
      CSV/JSON.
    evidence: |-
      Confirmado por leitura, mas nenhum campo numérico em todo o registry
      de cadastros (`valor_minimo`/`valor_maximo`/`teto` de
      `gerentes_aprovacao`, etc.) tem essa checagem — convenção sistêmica já
      estabelecida, não uma lacuna específica desta story.
    location: 'backend/handlers/cadastros.go (decodeJSONAprovadoresObra), backend/handlers/cadastros_csv.go (decodeCSVAprovadoresObra)'
    severity: low
---

<intent-contract>

## Intent

**Problem:** FR-9/FR-11 — solicitante não tem como abrir uma solicitação de Obras; `AbrirSolicitacaoHandler` só aceita `tipo_solicitacao` em `{transferencia, inclusao_sfc, inclusao, imobilizado}`. Obras não cabe no modelo de linha existente (`solicitacao_lancamentos` exige `divisao_id`/`centro_custo_id`/`conta_id`/`mes` — Obras não tem nenhum desses); não existem as tabelas mestras `locais_obra`/`subgrupos_despesa`/`obra_ordens`/`aprovadores_obra` nem a tabela de linha própria `solicitacao_obras_linhas` (inventário de dados, 02-REQUISITOS-NEGOCIO-ENVIO-TI.md); `solicitacoes.centro_custo_id` é `NOT NULL`, mas Obras não tem CC.

**Approach:** Estender o MESMO handler/dispatch (nunca reimplementar) para `tipo_solicitacao="obras"`, com um pipeline de validação próprio (linhas por local de obra + subgrupo de despesa + ordem de investimento, sem CC/filial/conta/mês) persistido em `solicitacao_obras_linhas` (tabela nova, não `solicitacao_lancamentos`). Nova migration cria as 4 tabelas mestras + a tabela de linha, e as 4 mestras entram no MESMO registry de cadastros administráveis (CSV+PUT+histórico de graça — mesmo padrão de `regras-aprovacao`/`autorizadores-formulario`). `aprovadores_obra` é validada pela MESMA implementação `AutorizadorNominal` (AD-2 continua com exatamente 2 implementações de `Resolver`), com um branch interno tipo-aware (Design Notes) já que Obras não tem CC/faixa — só teto individual por pessoa.

## Boundaries & Constraints

**Always:** `Resolver.Resolve` continua o único ponto de cálculo (AD-1), via `AutorizadorNominal` (AD-2) para `obras` — MESMA instância Go de `inclusao`/`imobilizado`, nunca um 3º tipo de Resolver. Toda linha de Obras tem `local_obra_id`+`subgrupo_despesa_id`+`ordem_investimento` (literal `CRIAR` ou um `numero_ordem` já existente em `obra_ordens`, cujo local/subgrupo devem casar com os da linha) +`valor>0`. `classificacao` (nível da solicitação, não por linha) é obrigatória para `obras` e um dos 4 valores `{inclusao, fl, retirada_saldo, transferencia_saldo}`. `transferencia_saldo` exige ao menos 1 linha `lado="retirada"` e ao menos 1 `lado="inclusao"`, com soma(retirada)==soma(inclusao) na MESMA tolerância de R$0,01 já usada por Transferência; as outras 3 classificações ignoram `lado`. `solicitacoes.centro_custo_id` fica NULL para `obras` (nunca para os demais tipos) — `classificacao` fica NULL para os demais tipos. Autorizador de Obras validado só por `colaborador_id`+`teto>=valor_total` (sem CC/faixa) contra `aprovadores_obra`; `valor_total` é a soma de todas as linhas da solicitação. Nenhuma checagem de CC/autorização de acesso é feita para `obras` (Boundaries "Never" abaixo) — qualquer solicitante autenticado pode abrir, mesmo padrão `RequireAuth(..., "")` já usado pelo handler.

**Never:** Reimplementar `ResolverParaTipo`/o handler/`AutorizadorNominal` como um tipo novo — só estender o switch/guard e adicionar um branch interno tipo-aware em `nominal.go` (mesmo precedente de branches tipo-aware já usados em `solicitacoes.go`). Gravar linha de Obras em `solicitacao_lancamentos` (tabela errada — usar `solicitacao_obras_linhas`). Exigir `divisao_id`/`centro_custo_id`/`conta_id`/`mes` em linha de Obras, ou checar CC/autorização de acesso para `obras`. Criar a ordem real no SAP quando `ordem_investimento="CRIAR"` (isso só acontece na finalização, Epic 4 — aqui só persiste o literal). Adicionar rotas novas para os 4 cadastros mestres novos (reaproveitar as 5 rotas `{tipo}` genéricas existentes).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Envio feliz (inclusão) | `classificacao="inclusao"`, 1+ linhas válidas, `autorizador_id` com teto >= soma das linhas | 201, `solicitacao_obras_linhas` grava N linhas, `centro_custo_id` NULL, `aprovador_snapshot.tipo="pessoa"` | — |
| Transferência de saldo balanceada | `classificacao="transferencia_saldo"`, linhas com `lado` somando igual (±R$0,01) em retirada/inclusao | 201 | — |
| Transferência de saldo sem os 2 blocos | só linhas `lado="retirada"` (nenhuma `inclusao`) | Nada é gravado | 400 "transferência de saldo exige ao menos uma linha de retirada e uma de inclusão" |
| Transferência de saldo desbalanceada | soma(retirada) != soma(inclusao) | Nada é gravado | 400 (mesma mensagem/tolerância de Transferência, adaptada) |
| `classificacao` inválida/ausente | valor fora de `{inclusao,fl,retirada_saldo,transferencia_saldo}` | Nada é gravado | 400 "campo 'classificacao' inválido" |
| Zero linhas | `obras_linhas: []` | Nada é gravado | 400 "a solicitação de obras precisa de ao menos uma linha" |
| `ordem_investimento="CRIAR"` | ordem ainda não existe no SAP | 201, nenhuma linha é criada em `obra_ordens` | — |
| Ordem inexistente (não é CRIAR) | `numero_ordem` sem linha em `obra_ordens` | Nada é gravado | 400 "linha N: ordem de investimento não encontrada" |
| Ordem existe mas local/subgrupo não casam | `obra_ordens` da ordem referencia outro local/subgrupo | Nada é gravado | 400 "linha N: ordem de investimento não pertence ao local de obra/subgrupo de despesa informado" |
| `local_obra_id`/`subgrupo_despesa_id` inexistente | UUID bem formado sem linha correspondente | Nada é gravado | 400 "linha N: local de obra não encontrado" / "linha N: subgrupo de despesa não encontrado" |
| Autorizador acima do teto individual | `autorizador_id` válido mas `teto < soma das linhas` | Nada é gravado | 400 "autorizador selecionado não é válido para o teto de alçada desta pessoa" |
| Autorizador sem cadastro em `aprovadores_obra` | `autorizador_id` fora da lista fixa de Obras | Nada é gravado | mesma 400 acima (nunca distingue "não cadastrado" de "teto insuficiente") |

</intent-contract>

## Code Map

- `backend/migrations/009_obras_multilinha.sql` -- novo: (a) `ALTER TABLE solicitacoes ALTER COLUMN centro_custo_id DROP NOT NULL`, `ADD COLUMN classificacao VARCHAR(30) CHECK (classificacao IN ('inclusao','fl','retirada_saldo','transferencia_saldo'))`, `ADD CONSTRAINT chk_solicitacao_obras_cc_xor_classificacao CHECK ((tipo_solicitacao = 'obras' AND centro_custo_id IS NULL AND classificacao IS NOT NULL) OR (tipo_solicitacao != 'obras' AND centro_custo_id IS NOT NULL AND classificacao IS NULL))`; (b) `CREATE TABLE locais_obra` (`id UUID PK`, `codigo VARCHAR(50) UNIQUE NOT NULL`, `nome VARCHAR(255) NOT NULL`, `created_at`/`updated_at`); (c) `CREATE TABLE subgrupos_despesa` (mesmas colunas de `locais_obra` -- "PK não é nome", 02-REQUISITOS); (d) `CREATE TABLE obra_ordens` (`id UUID PK`, `numero_ordem VARCHAR(50) UNIQUE NOT NULL`, `local_obra_id UUID NOT NULL REFERENCES locais_obra(id)`, `subgrupo_despesa_id UUID NOT NULL REFERENCES subgrupos_despesa(id)`, `tipo_despesa VARCHAR(100)`, `tipo_faturamento VARCHAR(100)`, `created_at`/`updated_at`) + índice em `(local_obra_id, subgrupo_despesa_id)`; (e) `CREATE TABLE aprovadores_obra` (`id UUID PK`, `colaborador_id UUID NOT NULL REFERENCES usuarios(id)`, `teto NUMERIC(18,2) NOT NULL`, `ativo BOOLEAN NOT NULL DEFAULT true`, `created_at`/`updated_at`) + índice em `colaborador_id`; (f) `CREATE TABLE solicitacao_obras_linhas` (`id UUID PK`, `solicitacao_id UUID NOT NULL REFERENCES solicitacoes(id)`, `lado VARCHAR(10) CHECK (lado IS NULL OR lado IN ('retirada','inclusao'))`, `local_obra_id UUID NOT NULL REFERENCES locais_obra(id)`, `subgrupo_despesa_id UUID NOT NULL REFERENCES subgrupos_despesa(id)`, `ordem_investimento VARCHAR(50) NOT NULL`, `valor NUMERIC(18,2) NOT NULL CHECK (valor > 0)`, `created_at`) + índice em `solicitacao_id`
- `backend/handlers/cadastros.go:73` (`cadastroRegistry`) -- adicionar 4 `{tipo}` novos no MESMO registry (nenhuma rota nova, mesmo padrão do comentário de `autorizadores-formulario` linha 202-220): `"locais-obra"` (`codigo`,`nome`), `"subgrupos-despesa"` (`codigo`,`nome`), `"obra-ordens"` (`numero_ordem`,`local_obra_codigo`->`local_obra_id`,`subgrupo_despesa_codigo`->`subgrupo_despesa_id`,`tipo_despesa`,`tipo_faturamento`), `"aprovadores-obra"` (`colaborador_email`->`colaborador_id` OBRIGATÓRIO, `teto`, `ativo`) -- cada um com `DecodeCSV`/`DecodeJSON` próprios, mesmo padrão de `decodeCSVClassesImobilizado`/`decodeJSONClassesImobilizado` (tipos simples) e `decodeCSVAutorizadoresFormulario`/`decodeJSONAutorizadoresFormulario` (FK por e-mail, sempre obrigatório)
- `backend/handlers/cadastros_csv.go` -- novas `decodeCSVLocaisObra`, `decodeCSVSubgruposDespesa` (padrão `decodeCSVClassesImobilizado:286`); `decodeCSVObraOrdens` (resolve `local_obra_codigo`/`subgrupo_despesa_codigo` via novos `resolveLocalObraIDPorCodigo`/`resolveSubgrupoDespesaIDPorCodigo`, mesmo padrão de `resolveDivisaoID`/`resolveCentroCustoIDPorCodigo`); `decodeCSVAprovadoresObra` (resolve `colaborador_email` via `resolveColaboradorIDPorEmail` já existente, mesmo padrão de `decodeCSVAutorizadoresFormulario:506`)
- `backend/handlers/cadastros.go` -- novas `decodeJSONLocaisObra`, `decodeJSONSubgruposDespesa`, `decodeJSONObraOrdens`, `decodeJSONAprovadoresObra` (corpo de PUT usa nomes de coluna direto, mesmo padrão de `decodeJSONAutorizadoresFormulario:525`)
- `backend/internal/aprovacao/resolver.go:15-26` (`Solicitacao`) -- adicionar `TipoSolicitacao string` (só consumido por `AutorizadorNominal` para decidir `autorizadores_formulario` vs `aprovadores_obra`, mesmo precedente de `AutorizadorEscolhidoID`)
- `backend/internal/aprovacao/resolver.go:62-69` (`ResolverParaTipo`) -- `case "inclusao", "imobilizado": return NovoAutorizadorNominal(db), nil` -- ampliar para `case "inclusao", "imobilizado", "obras":` (MESMA instância, AD-2 continua com 2 implementações)
- `backend/internal/aprovacao/nominal.go:38-76` (`AutorizadorNominal.Resolve`) -- branch por `s.TipoSolicitacao == "obras"`: consulta `aprovadores_obra` (`WHERE ativo=true AND colaborador_id=$1 AND teto >= $2`, sem CC/faixa) em vez de `autorizadores_formulario`; sem match -> `ErrAutorizadorInvalido{}` (CentroCusto vazio); `versaoDoCadastro(db, "aprovadores-obra", id)` em vez de `"autorizadores-formulario"`; `Motivo: "autorizador nominal validado contra o teto de alçada individual"` em vez da mensagem de CC/faixa
- `backend/internal/aprovacao/aprovador.go:61-67` (`ErrAutorizadorInvalido.Error`) -- branch: `CentroCusto == ""` -> `"autorizador selecionado não é válido para o teto de alçada desta pessoa"`; caso contrário, mensagem atual inalterada
- `backend/handlers/solicitacoes.go:145` (guard de tipo) -- ampliar para aceitar também `"obras"`
- `backend/handlers/solicitacoes.go:59-71` (`abrirSolicitacaoRequest`) -- adicionar `Classificacao string \`json:"classificacao"\`` e `ObrasLinhas []obraLinhaRequest \`json:"obras_linhas"\`` (nível da solicitação; ambos ignorados pelos demais tipos, mesmo precedente de `Anexos` ignorado fora de `imobilizado`)
- `backend/handlers/solicitacoes.go` (novo, ao lado de `lancamentoRequest`) -- `type obraLinhaRequest struct { Lado, LocalObraID, SubgrupoDespesaID, OrdemInvestimento string; Valor float64 }` e `obraLinhaValidado` análogo
- `backend/handlers/solicitacoes.go:150-295` -- para `tipo=="obras"`, pular TODO o pipeline de `lancamentos`/CC/plano de conta (Boundaries "Never") e rodar em paralelo: `validarClassificacaoObras(req.Classificacao)`; `validarObrasLinhasEstrutura(req.ObrasLinhas, req.Classificacao)` (formato UUID de `local_obra_id`/`subgrupo_despesa_id`, `ordem_investimento` não vazio, `valor>0`, `lado` só exigido/validado quando `classificacao=="transferencia_saldo"`); `validarBlocosObras(linhas)` (só para `transferencia_saldo`: exige >=1 `retirada` e >=1 `inclusao`, soma bate na mesma tolerância de `validarBalanceamento`); `validarLocalSubgrupoOrdemObras(tx, linhas)` (existência de `local_obra_id`/`subgrupo_despesa_id`; para `ordem_investimento != "CRIAR"`, existência em `obra_ordens` + local/subgrupo da ordem devem casar com os da linha). Extensão do guard de `autorizador_id` (linha 165) para incluir `"obras"`. `valorTotal` para Obras = soma de todas as linhas (sem distinguir lado)
- `backend/handlers/solicitacoes.go:207-250` -- para `tipo=="obras"`, pular a busca/validação de centro de custo inteiramente (não existe); `centroCustoID` fica `""`/NULL só para este tipo
- `backend/handlers/solicitacoes.go:366-399` (INSERTs) -- para `tipo=="obras"`: `INSERT INTO solicitacoes (..., centro_custo_id, ..., classificacao) VALUES (..., NULL, ..., $n)`; loop de linhas grava em `solicitacao_obras_linhas` (não `solicitacao_lancamentos`)
- `backend/handlers/solicitacoes_test.go` -- novos testes cobrindo a I/O Matrix (`corpoObras` helper análogo a `corpoImobilizado`)
- `backend/internal/aprovacao/calculado_test.go:252-262` (`TestResolverParaTipo`) -- adicionar asserção de que `"obras"` devolve a MESMA instância `*AutorizadorNominal` que `"inclusao"`/`"imobilizado"`
- `backend/internal/aprovacao/nominal_test.go` -- novos testes do branch `aprovadores_obra` (sucesso, teto insuficiente, sem cadastro)
- `backend/handlers/cadastros_test.go` -- novos testes dos 4 `{tipo}` novos (CSV + PUT, mesmo padrão dos testes de `autorizadores-formulario`)

## Tasks & Acceptance

**Execution:**
- `backend/migrations/009_obras_multilinha.sql` -- `centro_custo_id` nulável + `classificacao` em `solicitacoes`; 4 cadastros mestres de Obras; `solicitacao_obras_linhas` -- única fonte de schema novo desta story
- `backend/handlers/cadastros.go`, `backend/handlers/cadastros_csv.go` -- registra os 4 cadastros mestres no MESMO registry genérico (CSV+PUT+histórico de graça)
- `backend/internal/aprovacao/resolver.go`, `nominal.go`, `aprovador.go` -- dispatcha `obras` para a MESMA `AutorizadorNominal`, com branch tipo-aware para `aprovadores_obra` (sem CC/faixa, só teto individual)
- `backend/handlers/solicitacoes.go` -- aceita `obras`, valida classificação/linhas/local/subgrupo/ordem, grava em `solicitacao_obras_linhas` com `centro_custo_id` NULL -- único handler estendido
- Testes: `solicitacoes_test.go`, `nominal_test.go`, `calculado_test.go`, `cadastros_test.go` -- cobrem I/O Matrix do handler, branch de `aprovadores_obra`, dispatch e os 4 cadastros novos

**Acceptance Criteria:**
- Given um solicitante autenticado, when ele monta uma solicitação de Obras com uma ou mais linhas, cada uma com local de obra + subgrupo de despesa + ordem de investimento, then a solicitação não expõe nem grava campos de filial, CC, conta ou mês (`centro_custo_id` NULL).
- Given uma solicitação de Obras classificada como "transferência de saldo", when as linhas não formam dois blocos (retirada e inclusão) que batem em valor, then o sistema recusa o envio e nada é gravado.
- Given uma linha de Obras com `ordem_investimento="CRIAR"`, when a solicitação é enviada com os demais campos válidos, then a solicitação é criada com 201 e nenhuma ordem é criada no SAP ou em `obra_ordens` nesta etapa.
- Given um `autorizador_id` sem linha ativa em `aprovadores_obra` cobrindo o valor total da solicitação (teto insuficiente ou pessoa não cadastrada), then 400, nada é gravado -- nunca um aprovador inventado, e nunca exige CC/faixa (Obras não tem CC).

## Spec Change Log

## Review Triage Log

### 2026-10-08 — Review pass

- verdicts: 14 findings — high 0, medium 3, low 10, false 1, maybe-false 0
- findings:
  - `medium` `patch` (edge-case-hunter) `validarLocalSubgrupoOrdemObras` compara `ordemLocalObraID`/`ordemSubgrupoDespesaID` (lidos do banco) contra `l.LocalObraID`/`l.SubgrupoDespesaID` (texto literal enviado pelo cliente) com `!=` estrito — confirmado por leitura direta (`backend/handlers/solicitacoes.go`, linha ~1077). Um cliente que envie o UUID em caixa alta (formato aceito por `uuidFormatRegexp`) teria uma ordem legitimamente correspondente rejeitada com "não pertence ao local de obra/subgrupo de despesa informado", só por causa da caixa. Corrigido: comparação trocada para case-insensitive (`strings.EqualFold`).
  - `medium` `patch` (blind-hunter) `somaTotalObras` soma TODAS as linhas sem distinguir lado, inclusive para `classificacao="transferencia_saldo"` — confirmado por leitura e pelo teste `TestAbrirSolicitacaoHandler_Obras_TransferenciaSaldoBalanceada_Success` (`WithArgs(..., 2000.0)` para duas linhas de R$1000 balanceadas). Isso exige que o teto do autorizador cubra o DOBRO do valor real movimentado, em vez do valor realmente transferido (mesma convenção já usada por Transferência — `math.Max` dos dois lados, não a soma). A frase do Boundaries "`valor_total é a soma de todas as linhas da solicitação`" é lida aqui como a regra padrão para as 3 classificações de bloco único — não uma intenção deliberada de dobrar o teto exigido para transferências balanceadas, caso não antecipado ao redigir o spec. Corrigido: para `transferencia_saldo`, `valorTotal = max(somaRetirada, somaInclusao)`; as outras 3 classificações continuam somando todas as linhas.
  - `medium` `patch` (blind-hunter) Nenhuma checagem de tamanho para os novos campos VARCHAR (`locais_obra.codigo`/`nome`, `subgrupos_despesa.codigo`/`nome`, `obra_ordens.numero_ordem`/`tipo_despesa`/`tipo_faturamento`) antes do INSERT — confirmado por leitura dos novos `decodeCSV*`/`decodeJSON*`. Um valor maior que a coluna estoura como erro de banco (500) em vez de um 400 de validação limpo — mesma categoria de defeito já corrigida na Story 3.4 para `nome_arquivo`/`content_type`. Corrigido: checagens de tamanho (por caractere, `utf8.RuneCountInString`) adicionadas aos 4 novos tipos de cadastro.
  - `low` `patch` (blind-hunter) O literal do `map[string]interface{}` de `aprovador_snapshot` é copiado verbatim entre o caminho de `lancamentos` existente e `abrirSolicitacaoObras` em vez de uma função compartilhada — confirmado por leitura; puramente uma duplicação de manutenção (sem bug hoje). Corrigido: extraída função auxiliar única, chamada pelos dois caminhos.
  - `low` `patch` (blind-hunter, 2 achados agrupados — mesma causa: cobertura de teste assimétrica entre os 4 cadastros novos) Só "aprovadores-obra" tem testes de PUT (`AtualizarCadastroHandler`/`decodeJSON*`); "locais-obra"/"subgrupos-despesa"/"obra-ordens" só têm teste do caminho de import CSV — confirmado por leitura de `cadastros_test.go`; as novas checagens de formato UUID de `decodeJSONObraOrdens` ficam sem nenhum teste em qualquer direção. Corrigido: adicionado ao menos 1 teste de PUT por tipo, mais um teste de UUID malformado para `obra-ordens`.
  - `low` `reject` (blind-hunter) `ErrAutorizadorInvalido.CentroCusto == ""` usado como sinalizador implícito de "isto é Obras" — real fragilidade de design para um chamador futuro hipotético, mas hoje os dois únicos pontos de construção (`nominal.go`, CC/faixa vs obras) estão verificados corretos (confirmado por verification-gap) e o tradeoff já está documentado em comentário no próprio código. Corrigir robustamente exigiria reestruturar a assinatura do erro em todos os chamadores — mais que uma correção direta, sem dano concreto hoje.
  - `low` `reject` (blind-hunter) Nenhum CHECK de banco garante que `solicitacao_obras_linhas.lado` seja NULL exceto quando a solicitação pai é `classificacao='transferencia_saldo'` — diferente do precedente citado (`chk_lancamento_conta_xor_classe`, migration 008), esse é um invariante ENTRE tabelas (linha + pai), que um CHECK simples de linha não expressa; exigiria um trigger, mecanismo não usado em nenhum outro lugar deste código-base, para um domínio de cardinalidade fixa e pequena já corretamente validado em Go.
  - `low` `reject` (blind-hunter) Nenhuma defesa contra linhas duplicadas/degeneradas em `obras_linhas` (ex. 2 pares retirada/inclusão idênticos que ainda batem em soma) — confirmado, mas nenhum FR/AC desta ou de nenhuma story anterior exige unicidade de linha; mesmo precedente já rejeitado na Story 3.4 para a ausência de limite de contagem/tamanho em `anexos` ("dano especulativo... regra de negócio não demonstrada como necessária").
  - `low` `reject` (blind-hunter) `idx_aprovadores_obra_colaborador_id` não é um índice composto cobrindo todo o predicado da query (`ativo`+`teto`) — diferente do precedente de `autorizadores_formulario` (migration 007). Mas `aprovadores_obra` é uma lista fixa de ~11-12 linhas (Epic 3 context) — a preocupação de cardinalidade que motivou o precedente de 007 (7.683+ linhas em `alcadas`) não se aplica aqui; sem dano de performance demonstrável nesta escala.
  - `false` (blind-hunter) `ordem_investimento == "CRIAR"` comparado como case-sensitive, diferente das buscas de código/e-mail (case-insensitive) — refutado: "CRIAR" é um literal de protocolo/sentinela, da MESMA natureza de `lado` ("retirada"/"inclusao") e `classificacao`, ambos também comparados case-sensitive neste mesmo diff e em todo o código-base por convenção consistente; a comparação com buscas de código/e-mail (texto livre genuinamente digitado por humanos) é uma analogia errada, não uma inconsistência real.
  - `low` `defer` (edge-case-hunter) `locais_obra`/`subgrupos_despesa` resolvidos case-insensitive (`UPPER(codigo)`) contra uma constraint UNIQUE case-sensitive, permitindo 2 linhas cujo `codigo` difere só na caixa produzirem resolução ambígua — confirmado, mas é o MESMO padrão já existente em `centros_custo`/`resolveCentroCustoIDPorCodigo` (migration 003, Story 2.1), fielmente replicado aqui — não introduzido por esta story.
  - `low` `defer` (edge-case-hunter) Nenhuma checagem de positividade em `aprovadores_obra.teto` no decode CSV/JSON — confirmado, mas NENHUM campo numérico em todo o registry de cadastros (`valor_minimo`/`valor_maximo`/`teto` de `gerentes_aprovacao`, etc.) tem essa checagem — convenção sistêmica já estabelecida, não uma lacuna específica desta story.
  - `low` `reject` (edge-case-hunter) `lado` é ignorado silenciosamente (sem 400) quando o cliente o envia para uma classificação que não é `transferencia_saldo` — mesmo precedente já estabelecido e aceito nas Stories 3.3/3.4 para outros campos ignorados-mas-presentes (ex. `conta_id`/`mes` para `imobilizado`), "sem dano concreto demonstrado".

### 2026-10-08 — Review pass (re-despacho sem mudança de código)

- verdicts: 11 findings — high 0, medium 0, low 7, false 4, maybe-false 0
- findings:
  - `low` `reject` (blind-hunter) O frontmatter desta mesma passada grava `status: in-review`, enquanto o parágrafo de `Auto Run Result` herdado da sessão de implementação (não tocado por esta passada até este ponto) ainda afirma "Status mantido/restaurado como `done`" — contradição real no snapshot revisado. O fix é editar este próprio arquivo de spec: resolvido pela reescrita integral de `Auto Run Result` no Finalize desta mesma passada (abaixo), que é o mecanismo deste workflow para esse exato propósito.
  - `low` `reject` (blind-hunter) `status: in-review` ficou sem aspas, rompendo a convenção de citar valores de `status` entre aspas simples já usada neste e em todos os outros specs de `_bmad-output/implementation-artifacts/` — estilo cosmético; o fix é editar este próprio arquivo de spec.
  - `low` `reject` (blind-hunter) A prosa diz ter encontrado o arquivo "revertido para `status: in-progress`", enquanto o diff revisado mostra `in-review` — artefato de várias edições sequenciais desta mesma passada (in-progress, definido pelo step-03 ao iniciar a implementação, depois in-review, definido pelo step-04 para a revisão) condensadas num único hunk; a prosa descreve corretamente o que foi observado no início da implementação, não o estado final. O fix, se necessário, é editar este próprio arquivo de spec.
  - `false` (blind-hunter) perda da referência explícita aos IDs `DW-9`/`DW-10` quando "Riscos residuais" passou a só parafrasear os 2 itens — refutado: `deferred-work.md` já referencia esta spec de volta via seu campo `source_spec`, então a rastreabilidade não se perde; a direção da referência é a oposta à assumida pelo achado.
  - `false` (blind-hunter) `review_loop_iteration` permanecer em `1` apesar de múltiplas passadas de revisão — refutado: por este mesmo workflow (step-04, item 5), o contador só é incrementado antes de um loopback `bad_spec`; nenhuma passada desta story jamais produziu `bad_spec`, então permanecer em `1` é o comportamento correto, não um defeito.
  - `false` (blind-hunter) a remoção integral da entrada anterior de `Auto Run Result` (sessão `bmad-loop` travada em `IdlePrompt`) apaga memória institucional do incidente — refutado: `## Auto Run Result` é, por design deste mesmo workflow (Finalize sempre a reescreve por completo a cada passada, confirmado pelo histórico desta e de outras specs do diretório), uma seção de substituição única, não um log cumulativo — diferente de `Review Triage Log`/`Spec Change Log`, que acumulam entradas datadas.
  - `low` `reject` (blind-hunter) a afirmação "a única mudança não commitada era este próprio arquivo de spec" é feita sem citar o comando que a sustenta, diferente das afirmações de build/teste no mesmo parágrafo — real lacuna de rigor evidencial na prosa; o fix é editar este próprio arquivo de spec.
  - `low` `reject` (blind-hunter) "além dos testes de import CSV" fica vago, sem nomear os testes ou linhas específicas — real vagueza de documentação; o fix é editar este próprio arquivo de spec.
  - `false` (blind-hunter) `baseline_revision` apontar para o HEAD atual (que já contém o próprio objeto desta revisão) é chamado de "circular/auto-referente" — refutado: o step-03 deste mesmo workflow define `baseline_revision` como "o HEAD atual... antes de qualquer mudança"; capturar o HEAD atual é exatamente o comportamento mandatado, e o HEAD já conter os commits anteriores desta story só reflete que nenhum código novo foi necessário nesta passada.
  - `low` `reject` (edge-case-hunter) mesma contradição `status: in-review` vs. prosa "done" do primeiro achado acima, reportada de forma independente por esta camada — mesma causa-raiz; o fix é editar este próprio arquivo de spec.
  - `low` `reject` (verification-gap) mesma contradição `status: in-review` vs. prosa "done" do primeiro achado acima, reportada de forma independente por esta camada, sob "Other findings" — mesma causa-raiz; o fix é editar este próprio arquivo de spec.

- Nota: a camada `intent-alignment` não produziu achados prescritivos (sua instrução é estritamente descritiva); relatou que o diff revisado nesta passada opera inteiramente na camada de bookkeeping/rastreamento (este arquivo de spec), sem tocar código, schema ou testes — consistente com o Code Map inteiro já estar implementado e commitado (`e871f10`/`4638b07`) antes do início desta passada.

## Design Notes

**Obras usa teto individual por pessoa, NUNCA CC/faixa — apesar do comentário já existente em `resolver.go`/do Epic 3 Context sugerirem "mesmo mecanismo de validação" de 3.3/3.4:** FR-9 (Consequences, PRD) é explícito e testável: "Autorizador validado contra lista fixa (11-12 pessoas) com teto de alçada individual; valor acima do teto da pessoa escolhida não é aceito" -- sem qualquer menção a CC/faixa, consistente com a própria Consequence "Solicitação não tem campos de filial, CC, conta ou mês". O inventário de dados (02-REQUISITOS-NEGOCIO-ENVIO-TI.md) independentemente confirma `aprovadores_obra` como tabela PRÓPRIA separada de `autorizadores_formulario`. O comentário em `resolver.go`/Epic 3 Context ("reaproveita o mesmo motor/mecanismo de validação por CC-faixa") é compilado a partir dos mesmos artefatos e, neste ponto específico, está mais solto/impreciso que a Consequence testável do FR-9 -- não há CC em nenhuma linha de Obras para uma query por CC/faixa consultar. Resolvido como: MESMA implementação Go `AutorizadorNominal` (preserva AD-2 "2 implementações"), com um branch interno tipo-aware para a tabela/critério certos -- nunca um 3º tipo de `Resolver`, nunca uma reimplementação do dispatch.

**4 classificações = `inclusao`, `fl`, `retirada_saldo`, `transferencia_saldo`:** todos os 3 artefatos de planejamento (02-REQUISITOS, PRD FR-9, epics.md) citam "4 classificações" mas só nomeiam 3 explicitamente na lista entre parênteses, com "retirada de saldo" aparecendo ao final da MESMA lista separada por vírgula. A Consequence do PRD define os 2 BLOCOS internos de "transferência de saldo" como "retirada e inclusão" (sem o sufixo "de saldo") -- nome diferente do 4º item da lista ("retirada de saldo"), o que é consistente com serem conceitos distintos: "retirada_saldo" é a 4ª classificação standalone (remove saldo, um bloco só, sem exigir contrapartida), e "retirada"/"inclusao" são só os RÓTULOS de lado usados exclusivamente dentro de `transferencia_saldo` (reaproveitados em `solicitacao_obras_linhas.lado`, análogo a `lado="origem"/"destino"` de Transferência, mas com nomenclatura própria do domínio de Obras -- tabela nova, sem CHECK compartilhado).

**`solicitacao_obras_linhas` é tabela própria, não uma extensão de `lancamentoRequest`/`solicitacao_lancamentos`:** diferente de Imobilizado (Story 3.4, que ainda é "1 linha, lado=destino, trocando só conta_id/mes por classe"), Obras não tem NENHUM dos campos que dão nome a "lançamento" (divisão/CC/conta/mês) e já é descrita no inventário de dados como uma tabela de linha separada (`solicitacao_obras_linhas`, distinta de `solicitacao_lancamentos`). Forçar os campos de Obras dentro de `lancamentoRequest` infletiria o struct com 3 campos novos sem nenhuma relação com os campos existentes, e exigiria tornar `divisao_id`/`centro_custo_id` opcionais ali (quebrando a garantia de formato hoje incondicional para os outros 4 tipos). Um campo de requisição `obras_linhas` separado, mesmo handler/endpoint/dispatch (nunca um 2º endpoint), é a continuação natural do mesmo princípio "nunca reimplementar o handler" sem forçar uma abstração artificial entre dois formatos de linha genuinamente diferentes.

**`centro_custo_id` em `solicitacoes` fica nulável só para `obras`:** a tabela é compartilhada pelos 5 tipos e hoje exige CC para todos; Obras nunca teve CC. A migration adiciona um CHECK (XOR com `classificacao`) que transforma essa relação num invariante de schema, não só de validação em Go -- mesmo padrão já estabelecido pela migration 008 (`chk_lancamento_conta_xor_classe`) para o par `conta_id`/`classe_imobilizado_id`.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros, sem diffs de formatação
- `cd backend && go test ./...` -- expected: todos os pacotes passam, incluindo `internal/aprovacao` e `handlers`

## Auto Run Result

**Resumo da mudança implementada:** nenhuma mudança de código foi necessária nesta passada. Ao ser despachado sobre esta spec como "ready-for-dev", encontrei o Code Map inteiro já presente e commitado no working tree (`e871f10`/`4638b07`) — a única divergência era uma edição não commitada deste próprio arquivo de spec (revertendo `status` para `ready-for-dev`/`in-progress` e removendo a seção `Auto Run Result` anterior), sem nenhuma mudança de código correspondente. Um subagente de implementação, lançado sem contexto prévio por exigência do step-03, confirmou de forma independente a mesma conclusão.

**Arquivos alterados:** nenhum arquivo de código (`backend/**`) foi tocado nesta passada — `git diff` contra `baseline_revision` (`4638b07`, o HEAD corrente) contém apenas a evolução deste próprio arquivo de spec (`_bmad-output/implementation-artifacts/spec-3-5-abrir-obras-multi-linha.md`: frontmatter + `Auto Run Result` + `Review Triage Log`).

**Revisão desta passada:** 4 camadas lançadas em paralelo (blind-hunter, edge-case-hunter, verification-gap, intent-alignment) sobre o diff acima (puramente bookkeeping, sem código). 11 achados reportados (0 high, 0 medium, 7 low, 4 false, 0 maybe-false) — todos rejeitados: os 7 `low` têm como único fix editar este próprio arquivo de spec (a contradição transitória `status`/prosa que esta própria reescrita do Finalize resolve, estilo de citação cosmético, lacunas de rigor evidencial na prosa); os 4 `false` foram refutados por verificação direta (rastreabilidade de `DW-9`/`DW-10` preservada via `deferred-work.md`; `review_loop_iteration` corretamente gated a loopbacks `bad_spec`; `Auto Run Result` é por design uma seção de substituição única, não um log cumulativo; `baseline_revision` capturado exatamente como o step-03 manda). Zero `patch`, zero `defer`, zero `intent_gap`, zero `bad_spec` — ver `## Review Triage Log`, entrada "2026-10-08 — Review pass (re-despacho sem mudança de código)".

**Verificação executada (do zero, sem assumir o resultado de passadas anteriores):**
- `go build ./...`, `go vet ./...`, `gofmt -l .` -- sem erros, sem diffs de formatação.
- `go test ./... -count=1` -- todos os pacotes OK (`handlers`, `iam`, `internal/anexos`, `internal/aprovacao`).
- Todos os 13 testes nomeados `TestAbrirSolicitacaoHandler_Obras_*` executados verbosamente de forma isolada (`-run "TestAbrirSolicitacaoHandler_Obras"`) -- todos PASS, confirmando cobertura de cada linha da I/O & Edge-Case Matrix (envio feliz, as 2 variantes de transferência de saldo, zero linhas, classificação inválida, `CRIAR`, ordem inexistente, ordem não casa local/subgrupo (incluindo a variante case-insensitive), local/subgrupo inexistente, autorizador fora do teto/sem cadastro).
- Confirmado por leitura direta (não só grep) que todo o Code Map está presente: migration `009_obras_multilinha.sql`; os 4 novos tipos de cadastro em `cadastros.go`/`cadastros_csv.go` com checagem de tamanho (`utf8.RuneCountInString`); dispatch `obras` em `resolver.go`/`nominal.go`/`aprovador.go` para a mesma instância `AutorizadorNominal`; pipeline `abrirSolicitacaoObras` em `solicitacoes.go` com `strings.EqualFold` (comparação local/subgrupo da ordem) e `math.Max` (valor total de `transferencia_saldo`); função compartilhada `construirAprovadorSnapshot`; testes de PUT (`TestAtualizarCadastroHandler_*`) para os 4 tipos novos de cadastro.

**Follow-up review recommendation:** `false` — zero achados `patch` nesta passada (todos os 11 achados foram `reject`/`false`), logo não há risco não verificado específico a nomear.

**Riscos residuais:** os 2 itens deferidos no frontmatter (`locais_obra`/`subgrupos_despesa` resolvidos case-insensitive contra UNIQUE case-sensitive; nenhuma checagem de positividade em `aprovadores_obra.teto`) seguem abertos, como já registrado -- padrões sistêmicos pré-existentes, não introduzidos por esta story.

