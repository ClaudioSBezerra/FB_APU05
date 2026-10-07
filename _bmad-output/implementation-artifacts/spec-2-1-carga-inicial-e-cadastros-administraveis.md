---
title: 'Carga inicial e cadastros administráveis'
type: 'feature'
created: 2026-10-07
status: 'done'
baseline_revision: 'a23ec5799132ea98ec423ab4dd3cc17b505737ba'
review_loop_iteration: 0
followup_review_recommended: true
context:
  - '{project-root}/_bmad-output/implementation-artifacts/epic-2-context.md'
  - '{project-root}/_bmad-output/planning-artifacts/architecture/architecture-FB_APU05-2026-10-06/ARCHITECTURE-SPINE.md'
warnings: [oversized]
deferred: []
---

<intent-contract>

## Intent

**Problem:** Os 7 cadastros mestres do Epic 2 (divisões, centros de custo, contas, classes de imobilizado, alçadas, papel×pessoa, feriados de SLA) ainda não existem no banco. FR-16 exige que qualquer alteração futura em qualquer um deles gere histórico e permita restauração, com comportamento **idêntico** entre os 7 tipos — nenhum tipo pode ter regra de histórico/permissão diferente.

**Approach:** Uma migration cria as 7 tabelas de cadastro mais uma tabela genérica `cadastro_historico`. Um conjunto único de 5 rotas administrativas, parametrizadas por `{tipo}` (um dos 7 valores fixos), cobre carga inicial via CSV, listagem paginada, edição, histórico e restauração — a mesma implementação Go serve os 7 tipos, garantindo comportamento idêntico por construção, não por convenção copiada 7 vezes.

## Boundaries & Constraints

**Always:** Toda rota fica atrás de `RequireAuth(..., "administrador")` (mesmo padrão da Story 1.3, `backend/handlers/admin.go`). Carga CSV só roda contra a tabela daquele `{tipo}` ainda vazia; cabeçalho deve bater exatamente com as colunas definidas no Code Map — qualquer linha inválida ou cabeçalho divergente rejeita a carga inteira (atômica, dentro de uma transação), citando o número da linha. Toda alteração (import, edição, restauração) grava uma linha em `cadastro_historico` na mesma transação da mudança — nunca como efeito separado que pode falhar independentemente. Toda listagem usa `pagina`/`tamanho` (1–100) e devolve `{items, pagina, tamanho}` (AD-14). `centros_custo` só pode ser importado depois de `divisoes` (FK `divisao_id`).

**Never:** Implementar o motor de resolução de aprovador (Epic 3) — `alcadas`/`papel_pessoa` aqui são só cadastro versionado, ainda não consumido por nenhum resolver. Construir telas de frontend — a superfície testável desta story é a API, mesmo precedente da Story 1.3 (concessão de perfil administrador também só tem API, sem UI). Carga de colaborador×CC (Story 2.2) ou exceção de CC (Story 2.3) — fora de escopo. Reimportar um `{tipo}` já populado pela rota de importação (edição pontual usa a rota de atualização, não uma nova carga).

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Import feliz | CSV válido, UTF-8 BOM, `;`, tabela vazia | 201, `{"importados": N}`, N linhas em `cadastro_historico` (acao=criado, versao=1) | — |
| Import com tabela não-vazia | `{tipo}` já tem ≥1 linha | Nada é escrito | 409 "cadastro já possui dados" |
| Import com cabeçalho errado | Cabeçalho não bate com as colunas esperadas | Nada é escrito | 400 citando colunas esperadas |
| Import com linha inválida | Linha N com tipo/formato inválido (ex. `data` não-ISO) | Nada é escrito (transação inteira desfeita) | 400 citando a linha N |
| `{tipo}` desconhecido | `{tipo}` fora dos 7 valores fixos | — | 404 "tipo de cadastro desconhecido" |
| Edição feliz | PUT em registro existente, campos válidos | 200, linha atualizada, nova linha em `cadastro_historico` (versao+1, acao=atualizado) | — |
| Edição de id inexistente | `{id}` não existe na tabela do tipo | — | 404 |
| Restauração feliz | POST `{"versao": V}`, V existe no histórico do registro | 200, campos do registro voltam aos valores de V, nova linha (versao=max+1, acao=restaurado) | — |
| Restauração de versão inexistente | V não existe para aquele `{tipo}`+`{id}` | — | 404 |
| Acesso sem perfil administrador | Token válido, perfil=solicitante | — | 403 |

</intent-contract>

## Code Map

- `backend/migrations/003_cadastros_administraveis.sql` -- cria `divisoes`, `centros_custo`, `contas`, `classes_imobilizado`, `alcadas`, `papel_pessoa`, `feriados` + `cadastro_historico` genérica (ver colunas em Design Notes)
- `backend/handlers/cadastros.go` -- registry `{tipo}` → tabela/colunas editáveis + 5 handlers Handler Factory (`ListarCadastroHandler`, `ImportarCadastroHandler`, `AtualizarCadastroHandler`, `HistoricoCadastroHandler`, `RestaurarCadastroHandler`)
- `backend/handlers/cadastros_csv.go` -- parser CSV genérico (detecta/descarta BOM UTF-8, separador `;`), validação de cabeçalho e decodificação por tipo
- `backend/handlers/cadastros_test.go` -- testes de handler via `httptest`, mesmo padrão de `backend/handlers/admin_test.go`
- `backend/handlers/admin.go:51-67` -- padrão de referência (Handler Factory, `jsonErr`, validação de UUID do path) a reaproveitar, não duplicar
- `backend/handlers/middleware.go` -- `RequireAuth`/`GetUserIDFromContext` já existentes; usar sem alterar
- `backend/main.go:202` -- ponto de registro das novas rotas, mesmo padrão (`RequireAuth(withDB(...), "administrador")`)

## Tasks & Acceptance

**Execution:**
- `backend/migrations/003_cadastros_administraveis.sql` -- criar as 7 tabelas + `cadastro_historico` -- base de dados do épico inteiro (Code Map)
- `backend/handlers/cadastros_csv.go` -- parser/validador CSV genérico por tipo -- isola a complexidade de BOM/`;`/validação de linha do handler
- `backend/handlers/cadastros.go` -- registry + 5 handlers genéricos -- uma implementação para os 7 tipos garante comportamento idêntico (Intent)
- `backend/handlers/cadastros_test.go` -- cobre a I/O Matrix (import feliz/409/400×2/404 tipo, update feliz/404, restore feliz/404, 403) -- nenhuma regressão silenciosa no histórico
- `backend/main.go` -- registrar `GET/POST /api/admin/cadastros/{tipo}`, `PUT .../{id}`, `GET .../{id}/historico`, `POST .../{id}/restaurar`, todas atrás de `RequireAuth(..., "administrador")` -- fecha a superfície HTTP

**Acceptance Criteria:**
- Given as 7 tabelas vazias, when os 7 CSVs corretos são importados (divisões antes de centros de custo), then todas ficam populadas e cada linha importada gera uma linha `criado` em `cadastro_historico`.
- Given um cadastro já importado, when o administrador edita um registro via PUT, then o valor é atualizado e uma nova linha (`versao`+1, `atualizado`) aparece no histórico sem apagar a anterior.
- Given um registro com 2+ versões no histórico, when o administrador restaura uma versão anterior, then os campos do registro voltam aos valores daquela versão e uma nova linha `restaurado` é anexada (nunca reaproveita o número de versão antigo).
- Given qualquer um dos 7 tipos, when a mesma operação (importar/editar/listar/histórico/restaurar) é chamada, then formato de resposta e comportamento de histórico são idênticos entre tipos.
- Given um usuário com perfil `solicitante`, when ele chama qualquer rota `/api/admin/cadastros/*`, then recebe 403.

## Review Triage Log

### 2026-10-07 — Review pass
- verdicts: 18 findings — high 1, medium 7, low 5, false 5, maybe-false 0
- findings:
  - `[medium]` `[patch]` Nenhuma constraint `UNIQUE` em `divisoes.codigo` — um `codigo` duplicado faz `resolveDivisaoID` (cadastros_csv.go) resolver `centros-custo` para uma divisão arbitrária e não-determinística — ação: adicionar `UNIQUE` em `divisoes.codigo` na migration 003.
  - `[medium]` `[patch]` Mesmo achado (duplicata entre camadas) — `divisoes.codigo` sem `UNIQUE`, mesma causa-raiz e mesma ação acima.
  - `[medium]` `[patch]` `ListarCadastroHandler` não tem nenhum teste (envelope `{items,pagina,tamanho}`, clamp de `tamanho`) — ação: adicionar `TestListarCadastroHandler_Success`.
  - `[medium]` `[patch]` `HistoricoCadastroHandler` não tem nenhum teste — ação: adicionar `TestHistoricoCadastroHandler_Success`/`_RegistroInexistente`.
  - `[medium]` `[patch]` Mesmo achado (verification-gap, mesma causa-raiz): `HistoricoCadastroHandler` sem teste de leitura/paginação/`ator_id` nulo.
  - `[medium]` `[patch]` O critério "comportamento idêntico entre os 7 tipos" só é exercitado para `divisoes`; os caminhos `DecodeCSV`/`DecodeJSON` de `centros-custo`, `contas`, `alcadas`, `classes-imobilizado`, `papel-pessoa`, `feriados` não têm nenhum teste — ação: adicionar teste de import feliz para `centros-custo` (FK) e `alcadas` (nulo/booleano).
  - `[medium]` `[patch]` Mesmo achado (verification-gap, mesma causa-raiz): caminhos de decode de 6/7 tipos nunca exercitados por teste.
  - `[low]` `[patch]` Nenhuma validação rejeita `valor_maximo < valor_minimo` em `alcadas` (import e PUT), permitindo faixa de alçada invertida — ação: validar e rejeitar quando ambos presentes e `valor_maximo < valor_minimo`.
  - `[low]` `[patch]` `extractFloatOpcional` trata campo ausente (ex. erro de digitação na chave) igual a `null` explícito em PUT de `alcadas` — ação: exigir a chave presente, só aceitar `null` explícito como "sem teto".
  - `[false]` `[reject]` Falta `CHECK` em `cadastro_historico.tipo_cadastro` — refutação: toda escrita só ocorre após `cadastroRegistry[tipo]` confirmar que `tipo` é uma chave válida (senão 404 antes de qualquer INSERT); nenhum caminho hoje alcançável grava um `tipo_cadastro` desconhecido.
  - `[false]` `[reject]` Falta `UNIQUE` em `cadastro_historico (tipo_cadastro, registro_id, versao)` — refutação: `AtualizarCadastroHandler`/`RestaurarCadastroHandler` travam a linha-alvo com `FOR UPDATE` antes de calcular `MAX(versao)+1`, serializando concorrência por registro; a importação só grava `versao=1` para ids recém-criados, que nunca colidem com histórico existente.
  - `[low]` `[reject]` Import sequencial (sem batch) para `alcadas` (milhares de linhas) pode levar dezenas de segundos — rejeitado: `WriteTimeout`/`ReadTimeout` de 300s em `main.go` cobrem folgadamente o pior caso citado (~12,6 mil linhas); é uma carga administrativa única, não um caminho quente, e o fix (reestruturar para INSERT em lote) é mais que uma correção direta.
  - `[false]` `[reject]` `err == sql.ErrNoRows` em vez de `errors.Is` — refutação: `database/sql` garante que `QueryRow().Scan()` devolve o sentinel `sql.ErrNoRows` sem encapsular; o mesmo idioma já é usado em `admin.go:118` (padrão pré-existente, não uma regressão desta story).
  - `[high]` `[patch]` `colData` (campo `feriados.data`) escaneia a coluna `DATE` do Postgres para `*string`; `database/sql` converte `time.Time` para `*string` via `Format(time.RFC3339Nano)`, então `GET /api/admin/cadastros/feriados` devolve `"data":"2024-01-02T00:00:00Z"` em vez de `YYYY-MM-DD` — quebra o formato usado no CSV/histórico e impede reenviar o mesmo valor num PUT (falha em `parseDataISO`) — ação: escanear `colData` como `time.Time`/`sql.NullTime` e formatar como `"2006-01-02"` na resposta.
  - `[low]` `[reject]` Contagem de "linha N" no erro de CSV conta registros lidos, não linhas físicas de texto — só diverge se um campo citado contiver quebra de linha embutida — rejeitado: campos desta carga (`codigo`,`nome`,`filial`,`papel`,`pessoa_nome`,`descricao`) são textos curtos de um ERP, sem expectativa real de quebra de linha embutida; tratar "linha" como número de registro é o modelo mental correto para quem exporta de planilha.
  - `[low]` `[patch]` `pagina` não tem teto (diferente de `tamanho`) — um valor extremo (próximo de `math.MaxInt64`) faz `(pagina-1)*tamanho` estourar `int64` e virar OFFSET negativo, gerando 500 em vez de 400 — ação: aplicar o mesmo clamp superior já usado em `tamanho`.
  - `[false]` `[reject]` Risco especulativo de colisão de chave do advisory lock (`hashtext`) com lock namespace de stories futuras (2.2/2.3) — refutação: nenhum código hoje usa outra chave de advisory lock; é um risco hipotético sobre código ainda não escrito, não um defeito alcançável nesta mudança.
  - `[false]` `[reject]` Leitura "B" do auditor de intenção (dados reais de origem ausentes = ação só-humana, exigiria `status: awaiting-operator`) — refutação: os exemplos do próprio texto da intenção ("comprar domínio, publicar DNS, conceder API key, clicar em console de fornecedor") são todos ações de provisionamento de infraestrutura/implantação; carregar os CSVs reais de produção é o uso normal pós-implantação da própria funcionalidade entregue (feita por um administrador através da API já implementada), não um bloqueio de conclusão da story — a Story 2.1 não tem nenhuma ação que só um humano possa realizar fora do repositório.

## Design Notes

**`cadastro_historico` (genérica, append-only):** `id UUID PK`, `tipo_cadastro VARCHAR` (um dos 7 valores de rota), `registro_id UUID`, `versao INT`, `acao VARCHAR CHECK IN ('criado','atualizado','restaurado')`, `dados JSONB` (snapshot completo das colunas editáveis após a mudança), `ator_id UUID REFERENCES usuarios(id)`, `created_at TIMESTAMPTZ DEFAULT now()`. Índice em `(tipo_cadastro, registro_id, versao)`. Restaurar NUNCA deleta/edita uma linha existente — só lê o `dados` da versão alvo, aplica no registro vivo e grava uma versão nova.

**Colunas CSV por tipo** (cabeçalho exato, ordem exata; `id`/`created_at`/`updated_at` nunca vêm do CSV):

| `{tipo}` | tabela | colunas CSV |
|---|---|---|
| `divisoes` | `divisoes` | `codigo;nome` |
| `centros-custo` | `centros_custo` | `codigo;nome;divisao_codigo` (resolvido para `divisao_id` via `divisoes.codigo`; não encontrado → rejeita a carga citando a linha) |
| `contas` | `contas` | `plano;codigo;nome` (`plano` ∈ {BIFC,SFC}; PK composta `(plano,codigo)`) |
| `classes-imobilizado` | `classes_imobilizado` | `codigo;nome` |
| `alcadas` | `alcadas` | `filial;centro_custo_codigo;valor_minimo;valor_maximo;papel_aprovador;ativo` (`valor_maximo` pode vir vazio = sem teto; `centro_custo_codigo`/`papel_aprovador` são texto livre, sem FK — o pacote de origem documenta pares filial×CC e papéis sem estratégia cadastrada no SAP) |
| `papel-pessoa` | `papel_pessoa` | `papel;pessoa_nome` |
| `feriados` | `feriados` | `data;descricao` (`data` formato `YYYY-MM-DD`) |

**Interpretação de "contagem bate com a tabela de conferência de origem" (AC):** o pacote de origem com a tabela de conferência e os CSVs reais (dados sensíveis — nomes/matrículas) não está neste repositório (`docs/referencia/02-REQUISITOS-NEGOCIO-ENVIO-TI.md` só resume contagens agregadas). Interpretação adotada e implementada: `importados` sempre igual ao número de linhas de dado do CSV recebido (nenhuma linha descartada silenciosamente) — testável com fixtures sintéticas em `cadastros_test.go`, sem depender dos dados reais.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./...` -- expected: sem erros
- `cd backend && go test ./handlers/... -run Cadastro -v` -- expected: todos os casos da I/O Matrix passam

**Manual checks (if no CLI):**
- Subir `docker-compose up db` + backend, confirmar via log que `003_cadastros_administraveis.sql` executou e `schema_migrations` registrou o arquivo.

## Auto Run Result

Status: done

**Resumo:** Implementados os 7 cadastros mestres do Epic 2 (divisões, centros de custo, contas, classes de imobilizado, alçadas, papel×pessoa, feriados de SLA) com carga inicial via CSV, listagem paginada, edição, histórico e restauração de versão — uma única implementação Go genérica (registry `{tipo}` → tabela/colunas) serve os 7 tipos, atrás de `RequireAuth(..., "administrador")`.

**Arquivos alterados:**
- `backend/migrations/003_cadastros_administraveis.sql` -- cria as 7 tabelas de cadastro + `cadastro_historico` genérica, com `UNIQUE` nas chaves naturais (`divisoes.codigo`, `centros_custo.codigo`, `classes_imobilizado.codigo`, `papel_pessoa.papel`, `feriados.data`, `contas (plano,codigo)`)
- `backend/handlers/cadastros.go` -- registry + 5 handlers genéricos (`ListarCadastroHandler`, `ImportarCadastroHandler`, `AtualizarCadastroHandler`, `HistoricoCadastroHandler`, `RestaurarCadastroHandler`), validação de faixa de alçada, clamp de paginação, formatação de data ISO
- `backend/handlers/cadastros_csv.go` -- parser CSV genérico (BOM UTF-8 + `;`), decodificação/validação por tipo
- `backend/handlers/cadastros_test.go` -- 16 testes cobrindo a I/O Matrix + os achados de revisão (Listar, Histórico, `centros-custo`, `alcadas`)
- `backend/main.go` -- registra as 5 rotas novas atrás de `RequireAuth(..., "administrador")`
- `_bmad-output/implementation-artifacts/epic-2-context.md` -- contexto do Epic 2 compilado para esta e as próximas stories do épico

**Revisão — achados (18 no total: high 1, medium 7, low 5, false 5):**
- Corrigidos (`patch`, 11 achados): `UNIQUE` nas chaves naturais de 5 tabelas (nondeterminismo em `resolveDivisaoID`); `feriados.data` saía em RFC3339Nano em vez de `YYYY-MM-DD` na listagem (`high`); faixa de alçada invertida (`valor_maximo < valor_minimo`) não era rejeitada; `extractFloatOpcional` escondia erro de digitação na chave `valor_maximo`; `pagina` sem teto superior podia estourar `int64`; `ListarCadastroHandler` e `HistoricoCadastroHandler` sem nenhum teste; 6 dos 7 tipos sem teste de import.
- Rejeitados (7 achados, com motivo): falta de `CHECK` em `cadastro_historico.tipo_cadastro` (nenhum caminho alcançável grava valor inválido); falta de `UNIQUE` em `cadastro_historico(tipo,registro,versao)` (já serializado por `FOR UPDATE` por registro); import sequencial de `alcadas` sem batch (tolerável dentro do timeout de 300s do servidor, custo único administrativo); `err == sql.ErrNoRows` em vez de `errors.Is` (padrão pré-existente em `admin.go`, não regressão); contagem de "linha N" por registro, não por linha física (campos desta carga não têm quebra de linha embutida); risco especulativo de colisão de advisory lock com stories futuras (nenhum código concorrente existe hoje); leitura alternativa do auditor de intenção sugerindo `status: awaiting-operator` por falta dos CSVs reais de produção (refutada — ver linha correspondente no Review Triage Log: carregar dados reais é uso normal pós-implantação da própria funcionalidade entregue, não uma ação de infraestrutura bloqueante; esta story não tem nenhuma ação que só um humano possa realizar fora do repositório).

**Follow-up review recommendation:** `true` — um achado `high` foi corrigido nesta passada (formatação de data de `feriados` em `ListarCadastroHandler`). Risco específico não verificado: o fix (escanear `colData` como `time.Time` e formatar como `2006-01-02`) foi inspecionado e confere com o código, mas nenhum teste automatizado cobre especificamente a listagem do tipo `feriados` (os novos testes de listagem/histórico usam o tipo `divisoes`, que não tem coluna `colData`) — uma regressão futura nesse formato não seria pega por nenhum teste hoje.

**Verificação realizada:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- limpo
- `cd backend && go test ./...` -- todos os pacotes passam, incluindo os 16 testes de `cadastros_test.go`
- Smoke test manual end-to-end contra Postgres real (feito pelo agente de implementação antes da revisão): import → list → update → histórico → restore → re-restore-404 para múltiplos tipos, incluindo resolução de FK de `centros-custo` e campos nulos/booleanos de `alcadas`

**Riscos residuais:**
- Falta teste automatizado específico para a formatação de `feriados.data` na listagem (ver follow-up acima).
- Carga de `alcadas` com milhares de linhas é sequencial (sem batch) — aceito como custo único administrativo dentro do timeout de 300s do servidor (ver Review Triage Log).
- A contagem de "importados" (AC "bate com a tabela de conferência de origem") é verificada com fixtures sintéticas, não com os CSVs reais de produção (que não estão neste repositório) — interpretação documentada em Design Notes.
