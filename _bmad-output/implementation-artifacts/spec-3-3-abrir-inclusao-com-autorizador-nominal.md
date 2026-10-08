---
title: 'Abrir Inclusão com autorizador nominal'
type: 'feature'
created: '2026-10-07'
status: 'in-review'
baseline_revision: 'c0dcde369b4b5c6b4b882ee3cda42429777b7b35'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-3-context.md'
  - '{project-root}/_bmad-output/planning-artifacts/architecture/architecture-FB_APU05-2026-10-06/ARCHITECTURE-SPINE.md'
warnings: [oversized]
deferred: []
---

<intent-contract>

## Intent

**Problem:** FR-6/FR-11 — solicitante não tem como abrir uma Inclusão (plain); `AbrirSolicitacaoHandler` só aceita `tipo_solicitacao` em `{transferencia, inclusao_sfc}`, e `aprovacao.ResolverParaTipo` só conhece `ResolverCalculado` — não existe nenhum motor de autorizador nominal nem tabela de lista fixa de autorizadores por CC/faixa de valor.

**Approach:** Estender o MESMO handler/dispatch (nunca reimplementar) para `tipo_solicitacao="inclusao"`: novo `AutorizadorNominal` (2ª implementação de `Resolver`, AD-2) valida um `autorizador_id` escolhido pelo solicitante contra a nova tabela `autorizadores_formulario` (CC/faixa de valor, sempre pessoa — FR-11), registrada como novo `{tipo}` no MESMO registry de cadastros administráveis (CSV+PUT+histórico de graça, mesmo padrão de `regras-aprovacao`/`gerentes-aprovacao` na Story 3.1). Inclusão usa plano `BIFC` (FR-7: só Inclusão SFC usa `SFC`) e, como Inclusão SFC, é sempre 1 linha `lado="destino"`.

## Boundaries & Constraints

**Always:** `Resolver.Resolve` continua o único ponto de cálculo (AD-1), agora também via `AutorizadorNominal` para `inclusao`. Toda linha de `autorizadores_formulario` é sempre uma pessoa (`colaborador_id NOT NULL`) — FR-11 só fala de "pessoa escolhida", nunca cargo/colegiado para este motor. `autorizador_id` é um campo no nível da solicitação (não por linha — só há 1 linha). Validação: existe linha ativa em `autorizadores_formulario` com `centro_custo_codigo` da solicitação, `colaborador_id = autorizador_id` e `valor_minimo <= valor <= valor_maximo` (ou `valor_maximo` nulo = sem teto) — AutorizadorNominal NUNCA sintetiza um Aprovador quando isso falha (mesmo princípio AD-2 do calculado). `aprovador_snapshot` no mesmo formato (`Tipo="pessoa"`, `ColaboradorID` preenchido).

**Never:** Reimplementar `ResolverParaTipo`/o handler — só estender o switch/guard existentes. Aceitar `papel_aprovador`/cargo em `autorizadores_formulario` (fora de escopo — FR-11 é sempre pessoa). Implementar `imobilizado`/`obras` (Stories 3.4/3.5) — qualquer tipo fora de `{transferencia, inclusao_sfc, inclusao}` continua 400. Expor endpoint de combo (ex. `/autorizadores-formulario` público) — mesmo precedente backend-only de 3.1/3.2 (nenhum frontend consome). Duplicar as queries de `versaoDoCadastro`/`nomeColaborador` — extrair como funções compartilhadas por `ResolverCalculado` e `AutorizadorNominal`.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Envio feliz | 1 linha `lado="destino"`, conta plano `BIFC`, CC autorizado, `autorizador_id` com linha ativa em `autorizadores_formulario` cobrindo CC+valor | 201, `aprovador_snapshot` `Tipo="pessoa"` | — |
| Conta do plano SFC | `conta_id` resolve para `contas.plano='SFC'` | Nada é gravado | 400 citando que só `BIFC` é permitido em Inclusão |
| `autorizador_id` ausente ou não-UUID | campo vazio/malformado | Nada é gravado | 400 "campo 'autorizador_id' inválido" |
| Autorizador fora do CC/faixa | `autorizador_id` válido mas sem linha ativa casando CC+valor | Nada é gravado | 400 "autorizador selecionado não é válido para este centro de custo e faixa de valor" |
| Mais de 1 linha / `lado` != destino | mesmo padrão de Inclusão SFC | Nada é gravado | 400 "Inclusão aceita apenas uma linha" / lado="destino" exigido |
| Valor zero/negativo | linha com `valor<=0` | Nada é gravado | 400 (validação estrutural já existente) |

</intent-contract>

## Code Map

- `backend/internal/aprovacao/resolver.go:49-58` -- `ResolverParaTipo` -- adicionar `case "inclusao": return NovoAutorizadorNominal(db), nil`
- `backend/internal/aprovacao/aprovador.go` -- adicionar `ErrAutorizadorInvalido{CentroCusto string}` (sentinela, mesmo padrão de `ErrSemAlcadaCadastrada`)
- `backend/internal/aprovacao/calculado.go:210-263` -- extrair `versaoDoCadastro`/`nomeColaborador` de métodos de `*ResolverCalculado` para funções livres `versaoDoCadastro(db DBTX, tipoCadastro, registroID string)`/`nomeColaborador(db DBTX, colaboradorID string)` -- reaproveitadas por `AutorizadorNominal`, nunca duplicadas; atualizar as 2 chamadas existentes em `montarAprovador`
- `backend/internal/aprovacao/nominal.go` -- novo: `AutorizadorNominal`/`NovoAutorizadorNominal(db DBTX)`; `Resolve` consulta `autorizadores_formulario` (`ativo=true AND centro_custo_codigo=$1 AND colaborador_id=$2 AND valor_minimo<=$3 AND (valor_maximo IS NULL OR valor_maximo>=$3)`) usando `s.CentroCustoCodigo`/`s.AutorizadorEscolhidoID`/`s.Valor`; sem match -> `ErrAutorizadorInvalido`; com match -> `Aprovador{Tipo:TipoPessoa, ColaboradorID:&s.AutorizadorEscolhidoID, Nome: nomeColaborador(...), Motivo:"autorizador nominal validado para o CC/faixa de valor", RegraID:id, RegraVersao: versaoDoCadastro(db,"autorizadores-formulario",id)}`
- `backend/internal/aprovacao/resolver.go:15-21` -- `Solicitacao` -- adicionar campo `AutorizadorEscolhidoID string` (vazio/ignorado por `ResolverCalculado`)
- `backend/handlers/solicitacoes.go:50-53` -- `abrirSolicitacaoRequest` -- adicionar `AutorizadorID string \`json:"autorizador_id"\`` (nível da solicitação, não por linha)
- `backend/handlers/solicitacoes.go:106-109` -- guarda de `tipo_solicitacao` -- aceitar também `"inclusao"`
- `backend/handlers/solicitacoes.go:117-120` (logo após `validarContagemELadoPorTipo`) -- nova checagem: se `tipo_solicitacao=="inclusao"`, exigir `AutorizadorID` casando `uuidFormatRegexp`; senão 400 "campo 'autorizador_id' inválido"
- `backend/handlers/solicitacoes.go:188-191` -- mapa plano/rótulo -- virar `switch`: `"inclusao_sfc"→("SFC","Inclusão SFC")`, `"inclusao"→("BIFC","Inclusão")`, default `("BIFC","Transferência")`
- `backend/handlers/solicitacoes.go:224-230` -- `aprovacao.Solicitacao{...}` -- incluir `AutorizadorEscolhidoID: req.AutorizadorID`
- `backend/handlers/solicitacoes.go:231-242` -- tratamento de erro do resolver -- adicionar `errors.As` para `*aprovacao.ErrAutorizadorInvalido` -> 400 (mesmo formato de mensagem do sentinela)
- `backend/handlers/solicitacoes.go:350-365` -- `validarContagemELadoPorTipo` -- adicionar `case "inclusao"` (mesma regra de `inclusao_sfc`: exatamente 1 linha, lado="destino", mensagem citando "Inclusão")
- `backend/migrations/007_autorizadores_formulario.sql` -- novo: `autorizadores_formulario` (`centro_custo_codigo` VARCHAR sem FK -- mesmo padrão de `alcadas`; `valor_minimo` NUMERIC; `valor_maximo` NUMERIC nulo; `colaborador_id UUID NOT NULL REFERENCES usuarios`; `ativo` BOOLEAN; índices em `centro_custo_codigo` e `colaborador_id`)
- `backend/handlers/cadastros.go:73-202` -- `cadastroRegistry` -- adicionar `"autorizadores-formulario"` (`CSVCabecalho`: centro_custo_codigo, valor_minimo, valor_maximo, colaborador_email, ativo; `Colunas`: centro_custo_codigo/valor_minimo/valor_maximo/colaborador_id(colUUID)/ativo)
- `backend/handlers/cadastros.go` -- `decodeJSONAutorizadoresFormulario` -- `colaborador_id` obrigatório (não opcional, diferente de `regras-aprovacao`), valida UUID; reaproveita `extractString`/`extractFloat`/`extractFloatOpcional`/`validarFaixaAlcada`/`extractBool`
- `backend/handlers/cadastros_csv.go` -- `decodeCSVAutorizadoresFormulario` -- reaproveita `campoObrigatorio`/`parseNumeroObrigatorio`/`parseNumeroOpcional`/`validarFaixaAlcada`/`resolveColaboradorIDPorEmail` (variante obrigatória, não a `...Opcional`)/`parseBoolCSV`
- `backend/internal/aprovacao/calculado_test.go:252-262` -- `TestResolverParaTipo` -- adicionar asserção de que `"inclusao"` devolve `*AutorizadorNominal` não-nil
- `backend/handlers/solicitacoes_test.go` -- novos testes cobrindo a I/O Matrix (`corpoInclusao` helper análogo a `corpoInclusaoSFC`)
- `backend/handlers/cadastros_test.go` -- `TestImportarCadastroHandler_AutorizadoresFormulario_Success` e `..._EmailNaoEncontrado` (mesmo padrão dos testes de `regras-aprovacao`)

## Tasks & Acceptance

**Execution:**
- `backend/internal/aprovacao/{resolver,aprovador,calculado,nominal}.go` -- novo `AutorizadorNominal` + dispatch + sentinela + extração de helpers compartilhados -- 2ª implementação de `Resolver`, nunca reimplementa o motor calculado
- `backend/handlers/solicitacoes.go` -- aceita `inclusao`, valida `autorizador_id`, plano BIFC, contagem/lado, mapeia `ErrAutorizadorInvalido` -- único handler estendido
- `backend/migrations/007_autorizadores_formulario.sql` + `backend/handlers/cadastros{,_csv}.go` -- novo cadastro administrável `autorizadores-formulario` (CSV+PUT+histórico de graça)
- Testes: `calculado_test.go`, `solicitacoes_test.go`, `cadastros_test.go` -- cobrem dispatch, I/O Matrix do handler e o novo cadastro

**Acceptance Criteria:**
- Given um solicitante autenticado, when ele abre uma Inclusão com `autorizador_id` que tem linha ativa em `autorizadores_formulario` cobrindo o CC e o valor da solicitação, then o aprovador é resolvido como `Tipo="pessoa"` com esse `ColaboradorID` e a solicitação é criada com 201.
- Given um solicitante autenticado, when ele abre uma Inclusão com `autorizador_id` que NÃO tem nenhuma linha ativa cobrindo aquele CC/faixa de valor, then 400, nada é gravado — nunca um aprovador inventado.
- Given uma conta do plano SFC, when usada numa Inclusão (plain), then 400 recusando por plano, nada é gravado.
- Given um administrador, when ele importa um CSV de `autorizadores-formulario` com `colaborador_email` não cadastrado, then a linha é rejeitada (mesmo padrão de `cc-excecao`/`regras-aprovacao`).

## Spec Change Log

## Review Triage Log

### 2026-10-07 — Review pass
- verdicts: 12 findings — high 0, medium 6, low 5, false 1, maybe-false 0
- findings:
  - `medium` `patch` (blind-hunter) `decodeJSONAutorizadoresFormulario` (PUT path) tem zero cobertura de teste — confirmado: `grep` em `cadastros_test.go` só acha o símbolo na definição e no registry wiring, os 2 testes novos de Story 3.3 cobrem só o caminho CSV/import. Corrigido: 2 novos testes de PUT cobrindo `colaborador_id` ausente/malformado (a regra nova e exclusiva deste tipo: campo obrigatório, diferente de `regras-aprovacao`).
  - `low` `patch` (blind-hunter) `ErrAutorizadorInvalido.CentroCusto` é preenchido em `nominal.go` mas nunca lido (nem em `Error()`, nem em log) — confirmado por leitura do handler e do sentinela. Corrigido: adicionado `log.Printf` citando `CentroCusto` antes do 400, mesmo padrão do branch de erro genérico ao lado.
  - `medium` `patch` (blind-hunter) Não existe `nominal_test.go`; os branches de erro genérico de banco (`fmt.Errorf` em `Resolve`) e as falhas de `versaoDoCadastro`/`nomeColaborador` pós-match nunca são exercitados por nenhum teste — confirmado, cobertura é só indireta via HTTP. Corrigido: novo `nominal_test.go` (mesmo padrão sqlmock de `calculado_test.go`).
  - `low` `patch` (blind-hunter) Branch de erro genérico de `Resolve` em `solicitacoes.go` (`ErrAutorizadorInvalido`) não loga nada no servidor, ao contrário do branch de erro genérico vizinho — mesmo root cause do finding de `CentroCusto` acima; corrigido junto no mesmo `log.Printf`.
  - `low` `reject` (blind-hunter) `autorizador_id` é aceito e ignorado sem validação para `transferencia`/`inclusao_sfc` — confirmado como fato, mas sem dano concreto demonstrado ("pode mascarar bug de integração" é especulativo); o fix exigiria novos guards/branches por tipo, não exigidos pela spec (Boundaries: `autorizador_id` só é contrato para `inclusao`) — mais que correção direta, não vale o custo para um dano não demonstrado.
  - `false` `reject` (blind-hunter) `decodeJSONAutorizadoresFormulario` não verifica se `colaborador_id` existe em `usuarios` antes do INSERT, soaria como erro de FK bruto — refutado: `errorMsgAmigavel`/`ehViolacaoDeConstraint` (cadastros.go:776-796) já traduz `foreign_key_violation` do Postgres em 400 "referência inválida (chave estrangeira não encontrada)" para TODO o registry, inclusive `regras-aprovacao`/`gerentes-aprovacao`, que também não fazem esse pré-check — mesmo padrão já estabelecido, não uma regressão desta story.
  - `medium` `patch` (blind-hunter) Nenhum teste cobre o branch `valor_maximo IS NULL` (sem teto) do novo predicado SQL — confirmado, mesmo root cause do finding de `nominal_test.go` acima; corrigido no mesmo arquivo novo.
  - `medium` `patch` (blind-hunter) Nenhum teste cobre os limites inclusivos (`valor == valor_minimo`/`valor == valor_maximo`) do range — confirmado, mesmo root cause do finding de `nominal_test.go` acima; corrigido no mesmo arquivo novo.
  - `low` `patch` (blind-hunter) A migration cria 2 índices de coluna única (`centro_custo_codigo`, `colaborador_id`) mas a query de `AutorizadorNominal.Resolve` filtra os dois juntos (+`ativo`+faixa) — nenhum índice composto cobre o acesso real. Confirmado por leitura da query e da migration; mesmo padrão de lacuna já corrigido para `regras_aprovacao` na Story 3.1. Corrigido: índice composto `idx_autorizadores_formulario_busca (centro_custo_codigo, colaborador_id)` adicionado à migration 007.
  - `low` `reject` (blind-hunter) Os números de linha citados no Code Map do spec não batem com onde os hunks realmente caem no diff — real, mas o fix é editar este próprio spec (regra explícita de rejeição).
  - `medium` `patch` (edge-case-hunter) `AutorizadorNominal.Resolve` usa `LIMIT 1` sem `ORDER BY` — se mais de uma linha ativa de `autorizadores_formulario` casar o mesmo CC+colaborador+faixa de valor, o `RegraID`/`RegraVersao` gravado no snapshot de auditoria é não-determinístico. Confirmado por leitura direta da query em `nominal.go` (sem ORDER BY nenhum — pior que o guard_snippet citado pelo reviewer, que já sugeria um ORDER BY que não existe no código real); todo outro resolver do pacote (`regraCurada`, `alcada`, `gerente`) já usa `ORDER BY ..., id` como tie-break determinístico — esta é a única consulta do módulo sem esse padrão. Corrigido: `ORDER BY valor_minimo DESC, id` adicionado antes do `LIMIT 1`, mesmo padrão de `alcada`.
  - `medium` `patch` (verification-gap) `decodeJSONAutorizadoresFormulario`/PUT de `autorizadores-formulario` sem nenhum teste — achado pré-verificado pela camada verification-gap (grep confirmado: só aparece na definição e no wiring do registry); mesmo root cause do finding de PUT do blind-hunter acima — corrigido junto com os 2 novos testes de PUT.

## Design Notes

**`autorizadores_formulario` sempre pessoa, nunca cargo:** diferente de `alcadas`/`regras_aprovacao` (que aceitam `papel_aprovador` para cargo sem titular), FR-6/FR-11 só descrevem "pessoa escolhida pelo solicitante... validada contra lista fixa de autorizadores" — não há noção de cargo colegiado para autorizador nominal nos artefatos de planejamento. `colaborador_id NOT NULL` torna essa regra um invariante de schema, não só de validação em Go.

**Falha de validação do autorizador é 400, não 422:** `ErrSemAlcadaCadastrada` (calculado) é 422 porque representa uma lacuna de configuração num cálculo sem entrada do cliente. `AutorizadorNominal` sempre valida um `autorizador_id` que o próprio cliente enviou — mesma natureza de "conta do plano errado" (400), não de "sem alçada cadastrada". Sem UI neste repo (mesmo precedente de 3.1/3.2), a mensagem nunca distingue "ninguém cadastrado para este CC/faixa" de "esta pessoa não é a cadastrada" — evita expor a lista de autorizadores por CC via tentativa e erro.

**Sem `filial` em `autorizadores_formulario`:** diferente de `alcadas`/`regras_aprovacao` (filial+CC+valor), FR-6/FR-11 e a AC da Story 3.3 (epics.md) só citam "CC/faixa de valor" — nenhum artefato de planejamento menciona filial para autorizador nominal.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros, sem diffs de formatação
- `cd backend && go test ./...` -- expected: todos os pacotes passam, incluindo `internal/aprovacao`, `handlers` (solicitacoes + cadastros)
