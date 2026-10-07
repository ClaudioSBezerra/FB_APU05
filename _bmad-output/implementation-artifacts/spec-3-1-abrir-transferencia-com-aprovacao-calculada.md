---
title: 'Abrir Transferência com aprovação calculada'
type: 'feature'
created: 2026-10-07
status: 'done'
baseline_revision: 'be8a99791df6d90064a3ea490de6b07b8fb43733'
review_loop_iteration: 0
followup_review_recommended: true
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-3-context.md'
  - '{project-root}/_bmad-output/planning-artifacts/architecture/architecture-FB_APU05-2026-10-06/ARCHITECTURE-SPINE.md'
  - '{project-root}/docs/referencia/02-REQUISITOS-NEGOCIO-ENVIO-TI.md'
warnings: [oversized]
deferred: []
---

<intent-contract>

## Intent

**Problem:** FR-5/FR-10 — solicitante não tem como abrir uma Transferência de saldo sem calcular de cor a matriz de alçadas (quem aprova depende de filial+CC+valor, janela do gerente, regras especiais curadas, fallback GR); o núcleo de aprovação (`backend/internal/aprovacao`) ainda não existe (só `.gitkeep`) e `solicitacoes` ainda não existe no banco.

**Approach:** Nova rota `POST /api/solicitacoes` (autenticada, qualquer perfil — primeira rota do sistema que não exige `administrador`) grava `solicitacoes`+`solicitacao_lancamentos` e delega o cálculo do aprovador a um novo módulo `backend/internal/aprovacao` (`Resolver`/`ResolverCalculado`/`Aprovador`/`ErrSemAlcadaCadastrada`, conforme AD-1/AD-2) que encadeia: regras especiais curadas → matriz de alçadas (`alcadas`, já existe) → janela do gerente do CC → fallback GR — as duas últimas lidas de uma nova tabela `gerentes_aprovacao`. Dados reais dessas 2 tabelas novas (`regras_aprovacao`, `gerentes_aprovacao`) entram como 2 novos `{tipo}` no MESMO registry de cadastros administráveis da Story 2.1 (CSV+PUT+histórico de graça) — mesmo padrão que a Story 2.3 já usou para `cc-excecao`.

## Boundaries & Constraints

**Always:** `Resolver.Resolve(solicitacao) → (Aprovador, error)` é o único ponto de cálculo; o handler HTTP nunca decide aprovador diretamente (AD-1). `Aprovador{Tipo:"pessoa"|"cargo", Nome, ColaboradorID *string, Motivo, RegraID, RegraVersao}` — `ColaboradorID` nulo exatamente quando `Tipo="cargo"` (cobre tanto papel colegiado sem titular quanto grupo colegiado de gerentes — ambos são "cargo", nunca uma lista de pessoas). Falta de alçada retorna `ErrSemAlcadaCadastrada{CentroCusto, Filial}` — o handler traduz para 422 "sem alçada cadastrada" citando os dois; o resolver nunca sintetiza um `Aprovador`. Os 2 lados (`origem`/`destino`) devem somar o mesmo total (tolerância R$ 0,01) e todas as linhas devem cair no mesmo ano-competência (`mes`) — "exercício orçamentário" é o ano extraído de `mes`, nunca um campo submetido à parte (não existe em nenhum artefato de planejamento). Todas as linhas de uma mesma solicitação de Transferência devem referenciar o MESMO `centro_custo_id` (e portanto a mesma `divisao_id`, derivada do CC) — é esse valor único, copiado para o cabeçalho `solicitacoes.centro_custo_id`, que alimenta a checagem de autorização e o resolver; linhas com CC divergente são 400. CC da solicitação precisa bater com `usuarios.cc_proprio_id` do solicitante OU existir em `cc_excecao` para o par — 403 caso contrário. Conta selecionada precisa ser do plano `BIFC` (nunca `SFC` — Transferência não é Inclusão SFC).

**Never:** Inventar aprovador fictício ou escalonamento acima de R$ 20.000 — isso fica bloqueado com "sem alçada cadastrada" (fora do fluxo nesta fase, FR-10 Out of Scope). Implementar os outros 4 tipos de solicitação (`inclusao`, `inclusao_sfc`, `imobilizado`, `obras`) — a rota aceita só `tipo_solicitacao="transferencia"`, qualquer outro valor é 400 "tipo de solicitação não suportado ainda" (histórias 3.2–3.5 estendem o mesmo handler, nunca reimplementam). Construir tela de frontend (mesmo precedente backend-only das Stories 2.1–2.3; nenhum componente de UI existe ainda no repo). Implementar o REVOKE/GRANT de role dupla do AD-4 agora — esta story só faz INSERT em `aprovador_snapshot`/`versao` (nunca UPDATE), então a restrição do AD-4 (que é especificamente sobre UPDATE) ainda não tem caminho de escrita para proteger; registrar essa decisão em Design Notes, não implementar a infraestrutura de role antes de a primeira story que faça UPDATE nessas colunas existir. Validar `rel_cc_conta` (filtro desativado por desenho, Epic 3 context).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Envio feliz, alçada normal | 2 lados batendo, CC autorizado, valor dentro de uma faixa ativa de `alcadas` | 201, `aprovador_snapshot` com `Tipo="cargo"`, `regra_id`/`regra_versao` da linha de `alcadas` casada | — |
| Lados desbalanceados | soma(origem) ≠ soma(destino) fora de R$0,01 | Nada é gravado | 400 citando o lado desbalanceado |
| Mais de um exercício | linhas com `mes` em anos diferentes | Nada é gravado | 400 "mais de um exercício orçamentário" |
| CC não autorizado | `centro_custo_id` ≠ `cc_proprio_id` e fora de `cc_excecao` | Nada é gravado | 403 |
| CC divergente entre linhas | linha `origem` e linha `destino` com `centro_custo_id` diferentes | Nada é gravado | 400 |
| Conta do plano SFC | `conta_id` resolve para `contas.plano='SFC'` | Nada é gravado | 400 |
| Regra especial curada casa | linha ativa em `regras_aprovacao` cobre filial+CC+valor, precedência mais alta que `alcadas` | 201, snapshot com `regra_id` da regra curada (não de `alcadas`) | — |
| Janela do gerente | nenhuma linha de `alcadas`/`regras_aprovacao` casa, valor ≤ R$20.000, `gerentes_aprovacao` tem linha para o CC | 201, snapshot `Tipo` conforme a linha (pessoa ou cargo) | — |
| Fallback GR | idem acima mas só existe linha de `gerentes_aprovacao` por `divisao_id` (nenhuma específica do CC) | 201, snapshot citando a linha de divisão | — |
| Sem alçada cadastrada | nada casa em nenhuma etapa (valor >R$20.000 sem `alcadas`, ou ≤R$20.000 sem nenhuma linha aplicável) | Nada é gravado | 422 "sem alçada cadastrada" citando CC e filial |
| Cargo colegiado sem titular | linha casada tem `papel_aprovador` preenchido e `colaborador_id` nulo | 201, snapshot `Tipo="cargo"`, `colaborador_id=null` — aceito, não bloqueia | — |

</intent-contract>

## Code Map

- `backend/migrations/006_solicitacoes_transferencia.sql` -- nova: `ALTER TABLE centros_custo ADD COLUMN filial VARCHAR(50)` (nullable, mesmo padrão de dado incompleto de `rel_cc_conta`, migration 003); `regras_aprovacao` (precedencia, filial, centro_custo_codigo, valor_minimo, valor_maximo nulo, colaborador_id FK nulo, papel_aprovador nulo, ativo); `gerentes_aprovacao` (centro_custo_id FK nulo, divisao_id FK nulo, colaborador_id FK nulo, papel_aprovador nulo, teto default 20000, ativo); `solicitacoes` (tipo_solicitacao CHECK 5 valores, solicitante_id FK usuarios, centro_custo_id FK, status, aprovador_snapshot JSONB, versao int default 1); `solicitacao_lancamentos` (solicitacao_id FK, lado CHECK origem/destino, divisao_id FK, centro_custo_id FK, conta_id FK, mes DATE, valor CHECK >0)
- `backend/handlers/cadastros.go:65-152` -- `cadastroRegistry` -- adicionar `"regras-aprovacao"` e `"gerentes-aprovacao"` (mesmo padrão de `"alcadas"`/`"cc-excecao"`: FKs opcionais resolvidas por e-mail/código no CSV, aceitas como UUID já resolvido no PUT); estender a entrada `"centros-custo"` com a coluna opcional `filial`
- `backend/handlers/cadastros.go:33-42` -- `colKind`/`colunaDef` -- adicionar `colStringNulo` (mesmo padrão de `colNumericoNulo`, mas para string) para a coluna `filial` opcional
- `backend/handlers/cadastros_csv.go:158-168` -- `resolveDivisaoID` -- padrão de referência para resolver `colaborador_email`→`usuarios.id` (reaproveitar o case-insensitive de `cc_excecao`, não o case-sensitive de `resolveDivisaoID`) nos CSVs de `regras-aprovacao`/`gerentes-aprovacao`
- `backend/internal/aprovacao/aprovador.go` -- novo: tipo `Aprovador` e erro sentinela `ErrSemAlcadaCadastrada{CentroCusto, Filial string}` (AD-2)
- `backend/internal/aprovacao/resolver.go` -- novo: interface `Resolver`, fábrica `ResolverParaTipo(tipo string) Resolver` (AD-2) — só `"transferencia"` mapeado nesta story
- `backend/internal/aprovacao/calculado.go` -- novo: `ResolverCalculado` — encadeia `regras_aprovacao` → `alcadas` → `gerentes_aprovacao` (CC → divisão → global) → `ErrSemAlcadaCadastrada`; `RegraVersao` lido de `MAX(cadastro_historico.versao)` para `(tipo_cadastro, registro_id)` da linha casada (reaproveita o histórico já existente em vez de versionar em paralelo)
- `backend/handlers/admin.go:51-67` -- referência de padrão de transação (`db.Begin`/`defer Rollback`/validação de UUID do path) a copiar-adaptar para o INSERT de `solicitacoes`+`solicitacao_lancamentos`
- `backend/handlers/middleware.go:42` -- `RequireAuth(next, "")` -- reaproveitar com `perfilExigido=""` (primeira rota autenticada sem exigir perfil `administrador`)
- `backend/handlers/solicitacoes.go` -- novo: `AbrirSolicitacaoHandler` — valida corpo, checa autorização de CC (`cc_proprio_id`/`cc_excecao`), valida balanceamento/exercício único/plano de conta, chama `aprovacao.ResolverParaTipo("transferencia").Resolve(...)`, grava tudo numa transação
- `backend/main.go:215` (após as rotas de cadastros) -- registrar `POST /api/solicitacoes` atrás de `RequireAuth(withDB(...), "")`
- `backend/handlers/solicitacoes_test.go`, `backend/internal/aprovacao/calculado_test.go` -- novos: cobrem a I/O Matrix acima (sqlmock, mesmo padrão de `cadastros_test.go`)

## Tasks & Acceptance

**Execution:**
- `backend/migrations/006_solicitacoes_transferencia.sql` -- criar as 4 tabelas/alteração descritas no Code Map -- base de dados da story
- `backend/handlers/cadastros.go` -- `colStringNulo` + entradas `regras-aprovacao`/`gerentes-aprovacao` + coluna `filial` em `centros-custo` -- habilita carga/edição administrável das 2 tabelas novas e da coluna nova, de graça via as 5 rotas genéricas já existentes
- `backend/internal/aprovacao/{aprovador,resolver,calculado}.go` -- motor de resolução -- isola a lógica de maior risco do produto fora de HTTP/driver (AD-1/AD-2)
- `backend/handlers/solicitacoes.go` -- `AbrirSolicitacaoHandler` -- valida, autoriza CC, chama o resolver, grava em transação
- `backend/main.go` -- registra `POST /api/solicitacoes` -- único ponto de entrada
- `backend/internal/aprovacao/calculado_test.go` -- testa as 5 etapas do encadeamento + `ErrSemAlcadaCadastrada` + cargo colegiado sem titular
- `backend/handlers/solicitacoes_test.go` -- testa a I/O Matrix (balanceamento, exercício único, autorização de CC, plano de conta, 422 sem alçada, 201 feliz)

**Acceptance Criteria:**
- Given um solicitante autenticado com CC válido (próprio ou por exceção), when ele envia os 2 lados batendo (tolerância R$0,01) num único exercício, then a solicitação é criada com 201 e `aprovador_snapshot` gravado.
- Given os 2 lados fora da tolerância ou linhas de exercícios diferentes, when o envio ocorre, then 400, nada é gravado.
- Given nenhuma `alcadas`/`regras_aprovacao`/`gerentes_aprovacao` cobre o caso, when o servidor resolve o aprovador, then 422 "sem alçada cadastrada" citando CC e filial — nunca um aprovador inventado.
- Given o papel resolvido é um cargo sem titular (ex. Superintendência) ou um grupo colegiado de gerentes, when resolvido, then `Tipo="cargo"`/`ColaboradorID=null` é aceito como válido, não bloqueia.
- Given uma regra especial curada ativa cobre o caso, when o resolver roda, then ela prevalece sobre a matriz de alçadas (precedência).

## Spec Change Log

## Review Triage Log

### 2026-10-07 — Review pass
- verdicts: 22 findings — high 0, medium 7, low 13, false 2, maybe-false 0
- findings:
  - `[medium]` `[patch]` `solicitacao_lancamentos.divisao_id` aceito por linha sem checar que bate com a divisão real do CC (blind-hunter) — confirmado: `validarMesmoCentroCusto` só checa `centro_custo_id`; `divisao_id` só tinha validação de formato UUID, nunca comparado a `ccDivisaoID`. Corrigido: nova `validarDivisaoDerivadaDoCC` rejeita com 400 quando alguma linha diverge (`handlers/solicitacoes.go`).
  - `[medium]` `[patch]` `divisao_id` bem-formado porém inexistente estoura FK no INSERT e vira 500 em vez de 400 (blind-hunter) — mesma causa raiz do item acima; a nova validação rejeita antes do INSERT, então o 500 nunca é alcançado por essa via.
  - `[false]` `[reject]` `montarAprovador` não tem guarda própria contra `colaborador_id`/`papel_aprovador` ambos nulos, "segunda linha de defesa" ausente (blind-hunter) — verificado: `validarAutorRegra` é chamado nos 4 caminhos de escrita (JSON e CSV de `regras-aprovacao`/`gerentes-aprovacao`, `cadastros.go`/`cadastros_csv.go`) e já bloqueia essa linha antes de qualquer persistência; sem uma linha gravável com os dois nulos, `montarAprovador` nunca recebe esse estado pela aplicação.
  - `[low]` `[reject]` `solicitacoes.status` sem `CHECK` (blind-hunter) — real, mas sem caminho alcançável nesta story (o único INSERT grava `'aberta'` fixo); os valores válidos futuros (Epic 4) ainda não estão definidos em nenhum artefato, então um `CHECK` agora seria adivinhação.
  - `[low]` `[patch]` falta índice composto em `regras_aprovacao(filial, centro_custo_codigo)`, caminho de consulta de todo `POST /api/solicitacoes` (blind-hunter) — confirmado (só há índice em `colaborador_id`, fora do caminho de consulta). Corrigido: índice adicionado na migration 006.
  - `[low]` `[patch]` `valorTotal := somaPorLado(lancamentos, "origem")` é uma escolha arbitrária entre os 2 lados, que só precisam bater com tolerância de R$0,01 — na borda de uma faixa de alçada isso pode mudar o aprovador resolvido (blind-hunter) — confirmado. Corrigido: usa `math.Max(origem, destino)`, escolha conservadora e documentada.
  - `[low]` `[patch]` sem teste para a guarda estrutural "menos de 2 linhas"/falta um lado (blind-hunter) — confirmado, nenhum teste cobria `len(linhas) < 2`. Corrigido: `TestAbrirSolicitacaoHandler_MenosDeDuasLinhas`.
  - `[low]` `[patch]` sem teste para a convenção "dia 1" de `mes` (blind-hunter) — confirmado. Corrigido: `TestAbrirSolicitacaoHandler_MesForaDaConvencaoDia1`.
  - `[medium]` `[patch]` `decodeJSONCentrosCusto` passa a exigir a chave `filial` no corpo de PUT, tornando a edição de um CC pré-existente (sem conhecer o campo novo) um 400 onde antes era 200, e nenhum teste no repo exercitava `AtualizarCadastroHandler` com `tipo="centros-custo"` para pinar esse contrato antes ou depois (blind-hunter + verification-gap, mesma causa raiz) — confirmado nos dois sentidos: comportamento real e ausência total de teste para este endpoint. Corrigido: `TestAtualizarCadastroHandler_CentrosCusto_FilialAusente` e `_FilialPreenchida` pinam os dois lados do contrato novo.
  - `[low]` `[patch]` `gerente()` monta a query com `fmt.Sprintf` concatenando a condição WHERE extra, em vez de 3 literais fixos como o resto do pacote (blind-hunter) — confirmado; hoje os 3 chamadores só passam literais fixos (não é explorável), mas é um padrão fora do comum no módulo de maior risco do produto. Corrigido: 3 consts de query fixos, helper recebe a query já pronta.
  - `[low]` `[reject]` I/O Matrix da spec não fixa a mensagem exata esperada para "CC divergente entre linhas" (blind-hunter) — reject: o fix seria editar a spec, nunca o diff (regra de triagem).
  - `[medium]` `[patch]` `divisao_id` por linha nunca comparado a `ccDivisaoID` (edge-case-hunter, mesma causa raiz dos 2 primeiros itens) — carried into the same fix as acima.
  - `[medium]` `[patch]` linha de `regras_aprovacao`/`gerentes_aprovacao` com `colaborador_id` E `papel_aprovador` preenchidos ao mesmo tempo não é rejeitada; `montarAprovador` prioriza `colaborador_id` silenciosamente, descartando o `papel_aprovador` curado (edge-case-hunter) — o guard_snippet citado pelo reviewer não existe no código (não há guarda nenhuma para esse caso, ao contrário do que o snippet sugeria), mas a consequência descrita é real e verificada: `validarAutorRegra` só rejeita quando os dois são nulos, nunca quando os dois estão preenchidos. Corrigido: nova `validarAutorExclusivo`, chamada nos 4 caminhos de decode (JSON/CSV × regras/gerentes).
  - `[low]` `[reject]` PUT de `gerentes-aprovacao` exige `teto` presente (sem default) enquanto o CSV usa default 20000 quando a coluna vem vazia (edge-case-hunter) — confirmado como diferença real, mas consistente com a semântica de substituição completa do PUT já aplicada a todo campo editável deste registry (nenhum outro campo tem default no PUT); mudar isso reverteria um padrão deliberado, não corrigiria um defeito.
  - `[low]` `[reject]` CSV de `gerentes-aprovacao` aceita `teto<=0`, tornando a linha permanentemente inerte (nunca casa, pois `valor>0` sempre) sem feedback (edge-case-hunter) — confirmado que não há guarda de positividade, mas o guard_snippet citado pelo reviewer não existe (fabricado); a falha é silenciosa porém segura (nunca aprova nada, só cai para a próxima etapa / "sem alçada cadastrada"), e o caso é um erro de digitação de carga administrativa, não um caminho do usuário final.
  - `[medium]` `[patch]` mesma causa raiz do `filial` obrigatório sem teste, achada independentemente pela camada verification-gap com evidência de busca por `grep` confirmando zero testes de `AtualizarCadastroHandler("centros-custo")` — carried into the same fix as o item de `filial`/blind-hunter acima.
  - `[low]` `[reject]` mesma causa raiz do `teto` obrigatório no PUT vs default no CSV, achada independentemente pela camada verification-gap ("Other findings") — carried into o mesmo reject acima.
  - `[low]` `[reject]` teto de `gerentes_aprovacao` (janela do gerente/fallback GR) não tem limite superior reforçado em código — um administrador pode curar `teto` acima de R$20.000, e a Boundaries "Never" da spec lista "escalonamento acima de R$20.000" como algo que nunca deve ocorrer (intent-alignment) — avaliado: o próprio código documenta a decisão ("teto é por linha, não uma constante") e ela é consistente com o padrão do resto do sistema, onde TODO teto de alçada (`alcadas.valor_maximo`, `regras_aprovacao.valor_maximo`) é dado curada pelo negócio, sem limite de código — a leitura mais defensável é que R$20.000 é o default documentado da janela do gerente, não um invariante de sistema a ser travado em código; travar agora contradiria esse padrão em vez de corrigir um defeito.
  - `[low]` `[reject]` 3 das 5 etapas do encadeamento do resolver (regra curada, janela do gerente, fallback GR, cargo colegiado) só têm teste no nível do pacote `internal/aprovacao`, nunca através do handler HTTP (intent-alignment) — avaliado: pirâmide de testes razoável (lógica de negócio testada na camada mais baixa, handler testado para roteamento/wiring + happy path/erro); testes HTTP redundantes para cada ramo já coberto no nível do resolver teriam custo desproporcional ao ganho.
  - `[low]` `[patch]` teste do 422 "sem alçada cadastrada" não afirma que a mensagem cita o CC e a filial, só a frase genérica, embora o handler já monte os dois valores na mensagem (intent-alignment) — confirmado. Corrigido: asserção adicional em `TestAbrirSolicitacaoHandler_SemAlcadaCadastrada`.
  - `[false]` `[reject]` branch de erro de `aprovacao.ResolverParaTipo` no handler é inalcançável e sem teste (intent-alignment) — confirmado inalcançável, mas é defensivo e documentado no próprio comentário do código como preparação para as Stories 3.2-3.5; código que nunca é exercitado por uma condição que o guard anterior já filtra não é defeito.

## Design Notes

**`filial` em `centros_custo` (coluna nova, nullable):** nenhum artefato de planejamento modela "filial" como cadastro próprio — é sempre citada em par com CC (`alcadas.filial`, já free-text desde a Story 2.1). Resolver calculado precisa de filial+CC+valor (FR-10); a única fonte estrutural existente que liga a um CC é o próprio `centros_custo`, então a coluna entra ali (mesmo padrão de `usuarios.cc_proprio_id` chegar via `ALTER TABLE` numa story posterior à tabela original). Nullable porque, como todo cadastro mestre deste produto, a carga real é tarefa do negócio (mesma natureza de `rel_cc_conta`); ausência de filial apenas garante que nenhuma linha de `alcadas`/`regras_aprovacao` casa — cai corretamente em "sem alçada cadastrada", não é tratado como bug.

**`mes` como competência, "exercício orçamentário" derivado:** FR-5 não lista "exercício orçamentário" como campo de linha (só divisão/CC/conta/mês/valor) — nenhum artefato diz como o sistema SABE o exercício. Interpretação adotada: `mes` é uma competência completa (`DATE`, convenção dia 1), e o exercício orçamentário é `EXTRACT(YEAR FROM mes)`; a AC "recusa se houver mais de um exercício" vira "recusa se as linhas tiverem anos de `mes` diferentes" — sem precisar de um campo extra que nenhum FR pede.

**`regras_aprovacao`/`gerentes_aprovacao` unificam pessoa/cargo/colegiado numa única linha:** em vez de uma tabela `dono_cc` separada de uma tabela de hierarquia de GR (como o pacote de origem tinha, `docs/referencia/02-REQUISITOS-NEGOCIO-ENVIO-TI.md`), uma única tabela `gerentes_aprovacao` com `centro_custo_id` XOR `divisao_id` (NULL em ambos = fallback global) já cobre "janela do gerente" (linha por CC) e "fallback GR mais próximo acima" (linha por divisão, ou global) com a MESMA estrutura — subir a hierarquia é só tentar CC→divisão→global em ordem. O caso colegiado (3 gerentes de Logística, qualquer um aprova) e o cargo vago (Superintendência) são o MESMO conceito do ponto de vista do snapshot: `papel_aprovador` preenchido, `colaborador_id` nulo — "qualquer pessoa que ocupe esse papel aprova" é exatamente a semântica de `Tipo="cargo"` do AD-2, não precisa de uma tabela pessoa↔regra à parte.

**AD-4 (REVOKE/GRANT) adiado:** esta story só faz `INSERT` em `solicitacoes.aprovador_snapshot`/`versao` (valor inicial, nunca alterado depois aqui) — a regra do AD-4 é especificamente sobre `UPDATE`. Implementar a role dupla agora seria infraestrutura para um caminho de escrita que ainda não existe nesta story; a primeira story que fizer `UPDATE` nessas colunas (ex. reprocessamento, Fila do Administrador) é quem deve introduzir a separação de roles.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros, sem diffs de formatação
- `cd backend && go test ./...` -- expected: todos os pacotes passam, incluindo `internal/aprovacao` e os novos testes de `solicitacoes_test.go`/`cadastros_test.go`

## Auto Run Result

**Resumo da mudança implementada:** motor de aprovação calculada (`backend/internal/aprovacao` — `Resolver`/`ResolverCalculado`/`Aprovador`/`ErrSemAlcadaCadastrada`, encadeando regra curada → alçada → janela do gerente do CC → fallback divisão → fallback global) + `POST /api/solicitacoes` (`AbrirSolicitacaoHandler`), primeira rota do sistema sem exigir perfil `administrador`. Base de dados nova: `regras_aprovacao`, `gerentes_aprovacao`, `solicitacoes`, `solicitacao_lancamentos`, coluna `centros_custo.filial`. `regras-aprovacao`/`gerentes-aprovacao` entram no registry genérico de cadastros administráveis (CSV+PUT+histórico), mesmo padrão da Story 2.1/2.3. Esta é uma passagem de revisão de follow-up sobre trabalho já commitado (commit `c6f7324`): a história já estava com `status: in-review` quando este run começou; o run executou o pass de revisão (4 camadas em paralelo), triou os achados e aplicou os patches abaixo.

**Arquivos alterados nesta passagem (sobre o commit `c6f7324`):**
- `backend/handlers/solicitacoes.go` -- nova `validarDivisaoDerivadaDoCC` (rejeita `divisao_id` por linha que diverge da divisão real do CC, 400 em vez do 500 que a FK causaria); `valorTotal` agora usa `math.Max(soma origem, soma destino)` em vez da escolha arbitrária de "origem".
- `backend/handlers/cadastros.go` -- nova `validarAutorExclusivo` (rejeita `colaborador_id` E `papel_aprovador` preenchidos ao mesmo tempo em `regras-aprovacao`/`gerentes-aprovacao`), chamada nos 2 decoders JSON.
- `backend/handlers/cadastros_csv.go` -- `validarAutorExclusivo` chamada também nos 2 decoders CSV (mesma checagem, caminho de import).
- `backend/internal/aprovacao/calculado.go` -- as 3 queries de `gerentes_aprovacao` passam de 1 template com `fmt.Sprintf` para 3 consts de literal fixo (sem concatenação), removendo um padrão fora do comum no módulo de maior risco do produto.
- `backend/migrations/006_solicitacoes_transferencia.sql` -- novo índice composto `idx_regras_aprovacao_filial_cc (filial, centro_custo_codigo)`, no caminho de consulta de todo `POST /api/solicitacoes`.
- `backend/handlers/solicitacoes_test.go` -- 3 testes novos (`MenosDeDuasLinhas`, `MesForaDaConvencaoDia1`, `DivisaoDivergenteDoCC`) + asserção adicional no teste de "sem alçada cadastrada" (cita CC e filial na mensagem).
- `backend/handlers/cadastros_test.go` -- 4 testes novos: `CentrosCusto_FilialAusente`/`_FilialPreenchida` (pinam o contrato de PUT depois da coluna `filial` nova) e `RegrasAprovacao_AutorExclusivo`/`GerentesAprovacao_AutorExclusivo`.

**Achados da revisão:** 22 achados das 4 camadas (blind-hunter, edge-case-hunter, verification-gap, intent-alignment) — ver `## Review Triage Log` acima para o detalhe completo, verificação e evidência de cada um. Resumo:
- **Patches aplicados (7 causas-raiz, agrupando achados duplicados das 4 camadas):** `divisao_id` por linha não derivado/validado contra o CC (medium); PUT de `centros-custo` exigindo `filial` sem nenhum teste pinando o contrato (medium); `colaborador_id`/`papel_aprovador` sem exclusividade mútua (medium); falta de índice composto em `regras_aprovacao` (low); escolha arbitrária de lado para `valorTotal` (low); 2 lacunas de teste (`<2 linhas`, convenção dia 1) (low); `fmt.Sprintf` na montagem de SQL do resolver (low); teste do 422 não citava CC/filial na asserção (low).
- **Rejeitados (9):** guarda ausente em `montarAprovador` para ambos nulos (false — já bloqueado em todo caminho de escrita); `status` sem `CHECK` (low — sem caminho alcançável nesta story, valores futuros ainda não definidos); ambiguidade da spec sobre a mensagem de "CC divergente" (low — fix seria editar a spec); `teto` obrigatório no PUT vs default no CSV (low — consistente com o padrão de substituição completa do PUT); `teto<=0` sem validação (low — falha de forma segura); teto do gerente sem limite de código acima de R$20.000 (low — consistente com o padrão de alçada 100% curada por dado em todo o resto do sistema); cobertura só no nível do resolver para 3 das 5 etapas (low — pirâmide de testes razoável); branch `ResolverParaTipo` inalcançável sem teste (false — defensivo e documentado).
- Nenhum achado foi roteado como `intent_gap` ou `bad_spec` — nenhuma mudança na `<intent-contract>` ou nas demais seções da spec foi necessária.

**Verificação realizada:** `cd backend && go build ./... && go vet ./... && gofmt -l .` -- sem erros, sem diffs. `cd backend && go test ./...` -- todos os pacotes passam (`handlers`, `iam`, `internal/aprovacao`), incluindo os 7 testes novos desta passagem.

**Recomendação de revisão de follow-up:** `true`. 3 entradas `medium` foram patchadas nesta primeira passagem (regra de "2 ou mais medium patchados" do workflow). Risco residual nomeado: a troca de `valorTotal` para `math.Max(origem, destino)` é uma escolha conservadora e documentada, mas não é dictada literalmente pela spec (que não antecipava a ambiguidade de qual lado usar quando os 2 batem só dentro da tolerância de R$0,01) — não há teste cobrindo o caso-limite em que os 2 lados divergem por ~R$0,01 bem na borda de uma faixa de alçada/teto, então o comportamento exato nessa borda específica fica sustentado por raciocínio, não por teste.
