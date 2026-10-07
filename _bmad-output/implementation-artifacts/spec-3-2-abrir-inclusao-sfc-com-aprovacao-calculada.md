---
title: 'Abrir Inclusão SFC com aprovação calculada'
type: 'feature'
created: '2026-10-07'
status: 'in-progress'
baseline_revision: 'eeffdd9904ac1ac588845f853f3ed27c7b55d005'
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

**Problem:** FR-7/FR-10 — solicitante não tem como abrir uma Inclusão SFC; `AbrirSolicitacaoHandler` (Story 3.1) só aceita `tipo_solicitacao="transferencia"`, só valida contas do plano `BIFC`, e `aprovacao.ResolverParaTipo` só mapeia `"transferencia"` — qualquer outro tipo, inclusive `inclusao_sfc`, hoje é 400 "tipo de solicitação não suportado ainda".

**Approach:** Estender o MESMO handler/dispatch da Story 3.1 (nunca reimplementar) para aceitar `tipo_solicitacao="inclusao_sfc"`: `ResolverParaTipo` passa a mapear esse tipo para o MESMO `ResolverCalculado` (Epic 3 Cross-Story Dependencies — nenhuma lógica nova em `internal/aprovacao`); a validação de conta passa a exigir plano `SFC` (nunca `BIFC`) para este tipo; e, como Inclusão não tem o conceito de "2 lados" de Transferência, a única linha aceita usa `lado="destino"` fixo e pula as checagens de balanceamento/exercício-único (que só fazem sentido com 2 lados).

## Boundaries & Constraints

**Always:** `Resolver.Resolve` continua o único ponto de cálculo (AD-1), agora também para `inclusao_sfc`, via o MESMO `ResolverCalculado` de 3.1. Conta de uma linha de Inclusão SFC precisa ser do plano `SFC` (nunca `BIFC`, mesmo quando o código numérico colide — FR-7). Valor da linha deve ser `>0` (zero/negativo recusado) — já garantido pela validação estrutural existente (`validarLancamentosEstrutura`), que continua valendo para este tipo sem alteração. Uma solicitação `inclusao_sfc` é sempre exatamente 1 linha em `lancamentos`, com `lado="destino"` — mais de 1 linha ou `lado="origem"` é 400. CC da solicitação segue a mesma checagem de autorização (`cc_proprio_id`/`cc_excecao`) e a mesma derivação de divisão do CC já existentes, sem alteração. `aprovador_snapshot` gravado com o mesmo formato (`Tipo`/`Nome`/`ColaboradorID`/`Motivo`/`RegraID`/`RegraVersao`).

**Never:** Reimplementar a cadeia do resolver calculado (regra curada → alçada → janela do gerente → fallback divisão → fallback global) — é o MESMO `ResolverCalculado`, sem lógica nova em `internal/aprovacao`. Validar balanceamento entre 2 lados ou exercício orçamentário único para `inclusao_sfc` — são conceitos de Transferência, não se aplicam a uma única linha. Implementar `inclusao` (plain), `imobilizado` ou `obras` (Stories 3.3–3.5) — qualquer `tipo_solicitacao` fora de `{transferencia, inclusao_sfc}` continua 400 "tipo de solicitação não suportado ainda". Construir tela de frontend (mesmo precedente backend-only de 2.1–3.1; nenhum componente de UI existe no repo). Criar um endpoint de lookup público novo (ex. `/contas-sfc`) — nenhum frontend consome isso hoje; a exclusividade do plano `SFC` é garantida na validação de submissão do servidor, mesmo padrão já usado para `BIFC` em 3.1.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Envio feliz, alçada normal | 1 linha `lado="destino"`, conta plano `SFC`, CC autorizado, valor em faixa ativa de `alcadas` | 201, `aprovador_snapshot` gravado (mesmo motor de 3.1) | — |
| Conta do plano BIFC | `conta_id` resolve para `contas.plano='BIFC'` | Nada é gravado | 400 citando que só `SFC` é permitido para Inclusão SFC |
| Valor zero/negativo | linha com `valor<=0` | Nada é gravado | 400 (validação estrutural já existente, sem alteração) |
| Mais de 1 linha | `lancamentos` com 2+ itens | Nada é gravado | 400 "Inclusão SFC aceita apenas uma linha" |
| `lado` diferente de destino | linha única com `lado="origem"` | Nada é gravado | 400 citando que `inclusao_sfc` exige `lado="destino"` |
| Sem alçada cadastrada | nenhuma etapa do resolver casa (mesma cadeia de 3.1) | Nada é gravado | 422 "sem alçada cadastrada" citando CC e filial |
| `tipo_solicitacao="inclusao"` (plain) | tipo ainda não implementado (Story 3.3) | Nada é gravado | 400 "tipo de solicitação não suportado ainda" (comportamento existente, preservado) |

</intent-contract>

## Code Map

- `backend/internal/aprovacao/resolver.go:47-54` -- `ResolverParaTipo` -- adicionar `case "inclusao_sfc": return NovoResolverCalculado(db), nil` (reaproveita o mesmo resolver de `"transferencia"`, Epic 3 Cross-Story Dependencies)
- `backend/handlers/solicitacoes.go:102-105` -- guarda de `tipo_solicitacao` -- aceitar também `"inclusao_sfc"`; qualquer outro valor continua 400 "tipo de solicitação não suportado ainda" (preserva `TestAbrirSolicitacaoHandler_TipoNaoSuportado`, que usa `"inclusao"` plain)
- `backend/handlers/solicitacoes.go:289-326` -- `validarLancamentosEstrutura` -- hoje tem `len(linhas) < 2` fixo (linha 290-292); extrair a regra de contagem/lado para uma nova função tipo-aware chamada logo após o parse estrutural: `"transferencia"` mantém `>=2` linhas (mesma mensagem "ao menos uma linha de origem e uma de destino"); `"inclusao_sfc"` exige exatamente 1 linha com `Lado=="destino"`
- `backend/handlers/solicitacoes.go:119-127` -- chamadas a `validarBalanceamento`/`validarExercicioUnico` -- só executar quando `req.TipoSolicitacao=="transferencia"` (não se aplicam a uma única linha)
- `backend/handlers/solicitacoes.go:174,433-448` -- `validarPlanoContaBIFC` -- generalizar para `validarPlanoConta(tx, linhas, planoEsperado, rotuloTipo string)`, parametrizado por tipo: `("BIFC", "Transferência")` para `transferencia`, `("SFC", "Inclusão SFC")` para `inclusao_sfc`; mensagem de erro passa a citar o plano/tipo recebidos como parâmetro em vez de literais fixos
- `backend/internal/aprovacao/calculado_test.go:252-262` -- `TestResolverParaTipo` -- adicionar asserção de que `"inclusao_sfc"` também devolve um `Resolver` não-nil, mantendo a asserção existente de que `"inclusao"` (plain) continua `ErrTipoNaoSuportado`
- `backend/handlers/solicitacoes_test.go` -- novos testes cobrindo a I/O Matrix acima, mesmo padrão sqlmock de `TestAbrirSolicitacaoHandler_*` já existentes (ex. `TestAbrirSolicitacaoHandler_ContaPlanoSFC` é o espelho a seguir, mas para `transferencia`/BIFC — aqui o espelhado é o inverso)

## Tasks & Acceptance

**Execution:**
- `backend/internal/aprovacao/resolver.go` -- dispatch `inclusao_sfc` → `ResolverCalculado` -- habilita o motor calculado para o 2º tipo, sem lógica nova no pacote
- `backend/handlers/solicitacoes.go` -- aceita o tipo, validação de contagem/lado por tipo, pula balanceamento/exercício-único para `inclusao_sfc`, generaliza a validação de plano -- único handler estendido, nunca reimplementado (Boundaries "Never" de 3.1)
- `backend/internal/aprovacao/calculado_test.go` -- pina o dispatch de `inclusao_sfc`
- `backend/handlers/solicitacoes_test.go` -- cobre a I/O Matrix (plano errado, contagem/lado inválidos, sem alçada, sucesso) para `inclusao_sfc`

**Acceptance Criteria:**
- Given um solicitante autenticado, when ele abre uma Inclusão SFC com conta do plano `BIFC` (mesmo que o código coincida com uma conta `SFC`), then 400 recusando por plano, nada é gravado.
- Given um solicitante autenticado, when ele abre uma Inclusão SFC com conta do plano `SFC` válida, CC autorizado e valor positivo dentro de uma faixa de alçada, then o aprovador é resolvido pelo MESMO motor calculado da Story 3.1 (sem lógica nova em `internal/aprovacao`) e a solicitação é criada com 201.
- Given valor zero ou negativo, when enviado numa Inclusão SFC, then 400, nada é gravado.
- Given nenhuma etapa do resolver calculado cobre o caso, when a Inclusão SFC é enviada, then 422 "sem alçada cadastrada" citando CC e filial — nunca um aprovador inventado (mesmo comportamento de 3.1, agora alcançável também por este tipo).

## Spec Change Log

## Review Triage Log

## Design Notes

**`lado="destino"` fixo para Inclusão SFC:** nenhum artefato de planejamento modela "lado" para Inclusão — só Transferência tem "2 lados" explícito (FR-5). Reutilizar a coluna `lado CHECK(origem,destino)` já existente evita uma migration nova; `"destino"` é a leitura mais direta (inclusão adiciona um lançamento, análogo ao lado que recebe valor em Transferência) e é EXIGIDA explicitamente no payload (`lado="origem"` é 400), não um valor silenciosamente sobrescrito pelo servidor — mantém o contrato observável e testável.

**Sem endpoint `/contas-sfc` novo:** a documentação de referência do sistema completo (`docs/referencia/02-REQUISITOS-NEGOCIO-ENVIO-TI.md`) lista um lookup `/contas-sfc`, mas nenhum frontend existe neste repo (mesmo precedente backend-only de 2.1–3.1). A exclusividade "combo lista exclusivamente contas do plano SFC" (AC da Story 3.2) é garantida pela validação de submissão do servidor — mesmo padrão já usado para `BIFC` em 3.1 — não por um endpoint de combo sem nenhum consumidor.

**Cobertura do motor calculado não duplicada via HTTP:** como `ResolverCalculado` é 100% reaproveitado (nenhuma lógica nova em `internal/aprovacao`), as 5 etapas do encadeamento já estão cobertas pelos testes de pacote da Story 3.1. Esta story adiciona só 1 teste de sucesso + 1 de "sem alçada cadastrada" no nível do handler (wiring/roteamento), mesmo raciocínio de pirâmide de testes já aceito na review de 3.1 (não duplicar as 5 etapas do resolver via HTTP para um 2º tipo que usa o mesmo código).

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros, sem diffs de formatação
- `cd backend && go test ./...` -- expected: todos os pacotes passam, incluindo `internal/aprovacao` e os novos testes de `solicitacoes_test.go`
