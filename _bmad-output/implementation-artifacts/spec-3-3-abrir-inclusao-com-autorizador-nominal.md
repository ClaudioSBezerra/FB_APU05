---
title: 'Abrir Inclusão com autorizador nominal'
type: 'feature'
created: '2026-10-07'
status: 'done'
baseline_revision: 'c0dcde369b4b5c6b4b882ee3cda42429777b7b35'
review_loop_iteration: 0
followup_review_recommended: false
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-3-context.md'
  - '{project-root}/_bmad-output/planning-artifacts/architecture/architecture-FB_APU05-2026-10-06/ARCHITECTURE-SPINE.md'
warnings: [oversized]
deferred:
  - summary: >-
      Nenhuma validação impede 2 linhas ativas com faixas de valor
      sobrepostas para o mesmo centro_custo_codigo+colaborador_id em
      autorizadores_formulario.
    evidence: |-
      Confirmado por leitura de decodeJSONAutorizadoresFormulario/
      decodeCSVAutorizadoresFormulario: nenhuma checagem de sobreposição
      existe. Mesmo padrão de alcadas (migration 003), que também não
      valida sobreposição no decode; só regras_aprovacao tem um desempate
      explícito (precedencia), e mesmo essa não valida sobreposição na
      carga. Não introduzido por esta story.
    location: >-
      backend/handlers/cadastros.go (decodeJSONAutorizadoresFormulario) /
      backend/handlers/cadastros_csv.go (decodeCSVAutorizadoresFormulario)
    severity: medium
  - summary: >-
      AutorizadorNominal.Resolve nunca checa se o colaborador_id escolhido
      ainda é um usuarios.ativo=true.
    evidence: |-
      Confirmado: a query em nominal.go só filtra
      autorizadores_formulario.ativo, nunca usuarios.ativo. Lacuna sistêmica
      do pacote aprovacao — nenhum resolver existente (alcada, gerente,
      regraCurada) checa usuarios.ativo hoje. Não introduzido por esta
      story.
    location: >-
      backend/internal/aprovacao/nominal.go:38-56 (AutorizadorNominal.Resolve)
    severity: medium
  - summary: >-
      TestResolverParaTipo só assevera "imobilizado" como não suportado,
      nunca "obras".
    evidence: |-
      Confirmado por leitura de calculado_test.go. A asserção de
      "imobilizado" é da Story 3.1; esta story só adicionou as asserções de
      "inclusao" sem tocar essa linha — não introduzido por 3.3. Ambos os
      valores caem no mesmo branch default/ErrTipoNaoSuportado em
      ResolverParaTipo, então testar um é representativo do outro.
    location: >-
      backend/internal/aprovacao/calculado_test.go (TestResolverParaTipo)
    severity: low
  - summary: >-
      TestAbrirSolicitacaoHandler_Inclusao_Success nunca assevera que
      regra_id/regra_versao/motivo aparecem no corpo da resposta HTTP.
    evidence: |-
      Confirmado por leitura do teste — só tipo/nome do aprovador são
      checados no body. Mesmo padrão em todos os testes de sucesso do
      arquivo, para todos os tipos (transferencia/inclusao_sfc/inclusao);
      não introduzido por esta story.
    location: >-
      backend/handlers/solicitacoes_test.go
      (TestAbrirSolicitacaoHandler_Inclusao_Success)
    severity: low
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

### 2026-10-08 — Review pass

- verdicts: 13 findings — high 0, medium 1, low 10, false 1, maybe-false 0
- findings:
  - `low` `patch` (blind-hunter) Comentário da migration 007 afirma que `idx_autorizadores_formulario_busca` cobre "ativo + centro_custo_codigo + colaborador_id, mais a faixa de valor", mas o `CREATE INDEX` real só tem `(centro_custo_codigo, colaborador_id)` — confirmado por leitura direta do SQL. Corrigido: comentário reescrito para descrever só as 2 colunas de igualdade que o índice de fato cobre, deixando claro que `ativo`/faixa são filtrados sobre os candidatos já restritos por ele, não indexados em si.
  - `low` `reject` (blind-hunter) `autorizadores_formulario` não tem `CHECK` de banco para `valor_maximo >= valor_minimo` — confirmado, mas é o mesmo padrão de `alcadas`/`regras_aprovacao` (migrations 003/006), que também não têm esse `CHECK` e dependem só da validação Go (`validarFaixaAlcada`, já aplicada nos 2 caminhos de decode desta story); não é uma lacuna nova nem uma regressão desta story, e um `CHECK` só aqui romperia a consistência já estabelecida com as 2 tabelas irmãs.
  - `low` `defer` (blind-hunter) Nada impede 2 linhas ativas com faixas de valor sobrepostas para o mesmo `centro_custo_codigo`+`colaborador_id` em `autorizadores_formulario` — confirmado, nenhuma validação de sobreposição existe em `decodeJSONAutorizadoresFormulario`/`decodeCSVAutorizadoresFormulario`. Se verdadeiro (e é), a gravidade seria `medium` (o `RegraID`/`RegraVersao` resolvido passaria a depender de um critério de desempate arbitrário em vez de dados administrativos não-ambíguos) — mas o mesmo padrão (nenhuma validação de sobreposição/precedência na carga) já existe para `alcadas`; só `regras_aprovacao` tem desempate explícito (`precedencia`) e mesmo assim não valida sobreposição no decode. Não é uma lacuna introduzida por esta story.
  - `medium` `patch` (blind-hunter) `nominal_test.go` não tem nenhum teste exercitando o `ORDER BY valor_minimo DESC, id` (o próprio fix de determinismo da revisão de 2026-10-07, achado abaixo) com 2 linhas candidatas — confirmado por leitura de `nominal_test.go`: nenhum teste configura mais de 1 linha de retorno, e o único teste que toca a query usa `regexp.QuoteMeta("FROM autorizadores_formulario")`, um match parcial que nem exige a presença do `ORDER BY` no texto da query enviada ao mock. Um regress futuro que removesse o `ORDER BY` passaria por todos os testes existentes sem ser detectado. Corrigido: novo teste `TestAutorizadorNominal_DesempateDeterministico` que exige (via regex) que a query enviada contenha `ORDER BY valor_minimo DESC, id` antes do `LIMIT 1` — como sqlmock não executa SQL de fato, esta é a forma de trava de regressão disponível no nível de unidade (a correção real do `ORDER BY` em si só é verificável contra um Postgres de verdade).
  - `low` `defer` (blind-hunter) `AutorizadorNominal.Resolve` nunca checa se o `colaborador_id` escolhido ainda é um `usuarios.ativo=true` — confirmado, a query só filtra `autorizadores_formulario.ativo`. Se verdadeiro, a gravidade seria `medium` (um colaborador desligado cuja linha de autorizador ainda esteja `ativo=true` continuaria sendo um aprovador válido) — mas nenhum resolver do pacote (`alcada`, `gerente`, `regraCurada`) checa `usuarios.ativo` hoje; é uma lacuna sistêmica do pacote `aprovacao`, não introduzida por esta story.
  - `low` `reject` (blind-hunter) Dispatch de `tipo_solicitacao` espalhado por 4+ pontos switch/if hand-maintained, com risco de uma story futura (3.4/3.5) esquecer um dos pontos e cair silenciosamente no `default` do switch de plano/rótulo (rotulado "Transferência"). Verificado: hoje o `default` é inalcançável para qualquer valor fora de `{transferencia, inclusao_sfc, inclusao}`, porque o guard de tipo no topo do handler já rejeita esses valores com 400 antes de chegar ali — o `default` atual só é de fato alcançado por `"transferencia"`, seu comportamento correto e intencional. O dano citado é inteiramente especulativo sobre código ainda não escrito, e o fix proposto (centralizar o dispatch num registry de tipos) é bem mais que uma correção direta — introduziria abstração não exigida pela spec desta story.
  - `false` `reject` (blind-hunter) Mensagens do novo `case "inclusao"` em `validarContagemELadoPorTipo` têm capitalização inconsistente ("Inclusão aceita apenas uma linha" vs. `inclusão exige lado="destino"`), soando como inconsistência introduzida por este diff — refutado: é exatamente o mesmo padrão de capitalização já usado verbatim pelo `case "inclusao_sfc"` da Story 3.2 ("Inclusão SFC aceita apenas uma linha" / `inclusão SFC exige lado="destino"`, linhas 398/401) — Story 3.3 só replica o precedente, não introduz nenhuma inconsistência nova.
  - `low` `patch` (blind-hunter) Nenhum teste cobre o caso de 0 linhas (`lancamentos: []`) para `tipo_solicitacao="inclusao"` — só o branch de "mais de uma linha" tem teste. Confirmado; o código já trata 0 corretamente (`len(linhas) != 1` cobre 0 e >1 com a mesma mensagem), mas sem teste de regressão. Corrigido: novo `TestAbrirSolicitacaoHandler_Inclusao_ZeroLinhas`.
  - `low` `defer` (blind-hunter) `TestResolverParaTipo` só assevera `"imobilizado"` como não suportado, nunca `"obras"` — confirmado por leitura do teste. Mas essa lacuna já existia antes desta story (a asserção de `"imobilizado"` é da Story 3.1; esta story só adicionou as asserções de `"inclusao"`, sem tocar essa linha) — não é causada por 3.3, e ambos os valores caem no mesmo branch `default`/`ErrTipoNaoSuportado`, então testar um é representativo do outro.
  - `low` `defer` (blind-hunter) `TestAbrirSolicitacaoHandler_Inclusao_Success` nunca assevera que `regra_id`/`regra_versao`/`motivo` aparecem no corpo da resposta HTTP (só `tipo`/nome do aprovador) — confirmado por leitura do teste; um regress que removesse esses campos do `aprovador_snapshot` serializado não seria pego no nível do handler. Mas o mesmo padrão (nenhum teste de sucesso do arquivo, para nenhum tipo — transferencia/inclusao_sfc/inclusao — assevera `regra_id`/`regra_versao`) já existia antes desta story; não é uma lacuna introduzida por 3.3.
  - `low` `reject` `carried` (edge-case-hunter) `autorizador_id` é aceito e ignorado sem validação para `transferencia`/`inclusao_sfc` — mesma localização e mesma alegação da entrada de 2026-10-07 ("autorizador_id é aceito e ignorado sem validação para transferencia/inclusao_sfc... sem dano concreto demonstrado"), e o código ainda lê exatamente como aquela linha descreve (`AutorizadorEscolhidoID` segue decodificado incondicionalmente e só lido por `AutorizadorNominal`). Verdict e rota mantidos sem nova verificação.
  - `low` `reject` `carried` (intent-alignment) Nenhum teste confirma que `autorizador_id` é de fato ignorado por `ResolverCalculado` quando enviado para `transferencia`/`inclusao_sfc` — mesma raiz e localização do achado do edge-case-hunter acima (o comportamento de "ignorar" é garantido estruturalmente por `ResolverCalculado.Resolve` nunca ler o campo, não por um guard); agrupado com aquele achado, mesmo verdict e rota.
  - `medium` `patch` `carried` (intent-alignment) O fix de `ORDER BY valor_minimo DESC, id` (determinismo) está correto no texto da query mas não é verificado por nenhum teste (nenhum teste configura 2 linhas candidatas, e o match de regex existente nem exige a cláusula) — mesma raiz do achado do blind-hunter acima sobre `nominal_test.go`; agrupado com aquele achado, mesmo verdict e correção.

## Design Notes

**`autorizadores_formulario` sempre pessoa, nunca cargo:** diferente de `alcadas`/`regras_aprovacao` (que aceitam `papel_aprovador` para cargo sem titular), FR-6/FR-11 só descrevem "pessoa escolhida pelo solicitante... validada contra lista fixa de autorizadores" — não há noção de cargo colegiado para autorizador nominal nos artefatos de planejamento. `colaborador_id NOT NULL` torna essa regra um invariante de schema, não só de validação em Go.

**Falha de validação do autorizador é 400, não 422:** `ErrSemAlcadaCadastrada` (calculado) é 422 porque representa uma lacuna de configuração num cálculo sem entrada do cliente. `AutorizadorNominal` sempre valida um `autorizador_id` que o próprio cliente enviou — mesma natureza de "conta do plano errado" (400), não de "sem alçada cadastrada". Sem UI neste repo (mesmo precedente de 3.1/3.2), a mensagem nunca distingue "ninguém cadastrado para este CC/faixa" de "esta pessoa não é a cadastrada" — evita expor a lista de autorizadores por CC via tentativa e erro.

**Sem `filial` em `autorizadores_formulario`:** diferente de `alcadas`/`regras_aprovacao` (filial+CC+valor), FR-6/FR-11 e a AC da Story 3.3 (epics.md) só citam "CC/faixa de valor" — nenhum artefato de planejamento menciona filial para autorizador nominal.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros, sem diffs de formatação
- `cd backend && go test ./...` -- expected: todos os pacotes passam, incluindo `internal/aprovacao`, `handlers` (solicitacoes + cadastros)

## Auto Run Result

**Resumo:** Story 3.3 implementa `tipo_solicitacao="inclusao"` estendendo o MESMO handler/dispatch de 3.1/3.2: novo `AutorizadorNominal` (2ª implementação de `Resolver`, AD-2) valida um `autorizador_id` escolhido pelo solicitante contra a nova tabela administrável `autorizadores_formulario` (CC/faixa de valor, sempre pessoa — FR-11). Inclusão usa plano `BIFC` e é sempre 1 linha `lado="destino"`, mesmo padrão de Inclusão SFC. Esta execução do build-auto retomou uma recuperação manual de sessão (commit `2112fe7`, já continha o dev completo + as 12 correções da revisão de 2026-10-07) e rodou uma nova passada de revisão (2026-10-08) sobre o estado atual.

**Arquivos alterados (commit `2112fe7`, já na baseline deste run):**
- `backend/internal/aprovacao/nominal.go` -- novo `AutorizadorNominal`/`NovoAutorizadorNominal`, `Resolve` com o predicado CC+colaborador+faixa de valor e `ORDER BY valor_minimo DESC, id`/`LIMIT 1`
- `backend/internal/aprovacao/resolver.go` -- `ResolverParaTipo` dispatcha `"inclusao"` para `AutorizadorNominal`; `Solicitacao.AutorizadorEscolhidoID`
- `backend/internal/aprovacao/aprovador.go` -- sentinela `ErrAutorizadorInvalido`
- `backend/internal/aprovacao/calculado.go` -- `versaoDoCadastro`/`nomeColaborador` extraídos de métodos para funções livres, reaproveitados por `AutorizadorNominal`
- `backend/handlers/solicitacoes.go` -- aceita `"inclusao"`, valida formato de `autorizador_id`, plano BIFC, contagem/lado (1 linha, destino), mapeia `ErrAutorizadorInvalido` para 400
- `backend/migrations/007_autorizadores_formulario.sql` -- nova tabela `autorizadores_formulario`
- `backend/handlers/cadastros.go` / `cadastros_csv.go` -- novo cadastro administrável `autorizadores-formulario` (CSV+PUT+histórico de graça)
- `backend/internal/aprovacao/{calculado_test,nominal_test}.go`, `backend/handlers/{solicitacoes_test,cadastros_test}.go` -- cobertura da I/O Matrix, dispatch e do novo cadastro

**Arquivos alterados nesta passada de revisão (patches, não commitados ainda):**
- `backend/migrations/007_autorizadores_formulario.sql` -- comentário do índice composto corrigido para não overclaim cobertura de `ativo`/faixa de valor
- `backend/internal/aprovacao/nominal_test.go` -- novo `TestAutorizadorNominal_DesempateDeterministico`, trava de regressão para o `ORDER BY` de determinismo
- `backend/handlers/solicitacoes_test.go` -- novo `TestAbrirSolicitacaoHandler_Inclusao_ZeroLinhas`

**Revisão (2026-10-08) -- 13 achados:**
- Patches aplicados (3): comentário do índice da migration 007 corrigido (low); novo teste travando o `ORDER BY valor_minimo DESC, id` contra regressão silenciosa (medium, achado agrupado blind-hunter+intent-alignment); novo teste cobrindo 0 linhas em Inclusão (low).
- Deferidos (4, ver frontmatter `deferred`): falta de validação de sobreposição de faixas em `autorizadores_formulario` (medium, mesmo padrão de `alcadas`); `AutorizadorNominal.Resolve` não checa `usuarios.ativo` (medium, lacuna sistêmica do pacote `aprovacao`); `TestResolverParaTipo` não testa `"obras"` (low, pré-existente desde 3.1); teste de sucesso de Inclusão não assevera `regra_id`/`regra_versao` na resposta (low, mesmo padrão em todos os tipos).
- Rejeitados (4): falta de `CHECK` de banco para `valor_maximo>=valor_minimo` (mesmo padrão de `alcadas`/`regras_aprovacao`, mitigado por validação Go já existente); dispatch espalhado por switch/if (dano puramente especulativo sobre stories futuras, `default` hoje inalcançável); capitalização de mensagens -- `false`, refutado (replica verbatim o padrão de 3.2); `autorizador_id` aceito/ignorado para outros tipos -- `carried` de 2026-10-07, mesma verdict/rota, 2 achados (edge-case-hunter + intent-alignment) agrupados na mesma raiz.

**Recomendação de revisão de acompanhamento:** `false`. Nesta passada só 1 achado `medium` foi patcheado (não 2+), e nenhum `high`. Trabalho convergiu.

**Verificação:** `go build ./...`, `go vet ./...`, `gofmt -l .` sem erros/diffs; `go test ./...` -- todos os pacotes OK (`handlers`, `iam`, `internal/aprovacao`), incluindo os 2 novos testes desta passada.

**Riscos residuais:** os 4 itens deferidos acima (ver frontmatter `deferred` para detalhes e severidade) -- nenhum é específico desta story; todos replicam padrões sistêmicos já presentes em `alcadas`/`regras_aprovacao` ou no pacote `aprovacao` como um todo.
