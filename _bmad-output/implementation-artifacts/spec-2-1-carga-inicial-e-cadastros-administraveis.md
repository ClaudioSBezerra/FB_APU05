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
deferred:
  - summary: >-
      CSV de números (valor_minimo/valor_maximo de alcadas) pode rejeitar
      formato decimal pt-BR (vírgula) se os dados reais de origem usarem
      esse formato.
    evidence: |-
      parseNumeroObrigatorio/parseNumeroOpcional (cadastros_csv.go) usam
      strconv.ParseFloat diretamente sobre o texto do CSV, sem normalizar
      vírgula para ponto; um valor como "100,50" falha a conversão. O
      formato decimal real usado nos CSVs de origem (ERP) não está neste
      repositório para confirmar (mesma limitação já registrada em Design
      Notes sobre os dados reais de origem). Para resolver: obter uma
      amostra real do CSV de alcadas e confirmar se usa vírgula ou ponto
      como separador decimal antes da carga real em produção.
    location: >-
      backend/handlers/cadastros_csv.go:91-112
    severity: medium (unverified)
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

### 2026-10-07 — Review pass (follow-up)
- verdicts: 26 findings — high 3, medium 10, low 8, false 4, maybe-false 1
- findings:
  - `[false]` `[reject]` `sprint-status.yaml` marca a story como `done` enquanto o próprio spec (no mesmo diff) está `in-review`/`followup_review_recommended: true` — refutação: a contradição é um artefato do próprio processo desta passada de revisão (o status do spec foi mudado para `in-review` só para esta revisão rodar); `sprint-status.yaml` é bookkeeping do orquestrador, não um estado do código sob revisão.
  - `[low]` `[patch]` `divisoes.codigo` tem `UNIQUE` (que já cria índice implícito) e também um `CREATE INDEX idx_divisoes_codigo` redundante na mesma coluna — ação: remover o `CREATE INDEX` redundante da migration.
  - `[low]` `[reject]` `lerCadastroCSV` conta `numeroLinha` por registro lido por `reader.Read()`, e `encoding/csv` descarta silenciosamente linhas fisicamente em branco sem contá-las — uma linha em branco perdida no meio do arquivo dessincroniza "linha N" da linha física real — rejeitado: cenário possível mas incomum (exige uma linha vazia real no meio do CSV, não apenas a quebra final de arquivo); corrigir exigiria substituir a contagem por `reader.Read()` por um rastreamento de linha física próprio, mais que uma correção direta.
  - `[medium]` `[patch]` Mesmo achado-raiz (cobertura de teste): caminhos de decode de `contas`, `classes-imobilizado` e `papel-pessoa` seguem sem nenhum teste de import — ação: adicionar teste de import feliz para os 3 tipos (ver abaixo).
  - `[high]` `[patch]` O fix `[high]` da passada anterior (formatação `feriados.data` em `ListarCadastroHandler`) segue sem nenhum teste que exercite `{tipo}=feriados` em qualquer rota — uma regressão futura no scan/format de `colData` não seria pega por nada hoje — ação: adicionar `TestListarCadastroHandler_Feriados_DataFormato` (e import feliz do tipo).
  - `[high]` `[patch]` Mesmo achado-raiz (verification-gap, evidência própria): busca por `"feriados"` em todo `cadastros_test.go` não retorna nenhuma ocorrência — nenhuma das 5 rotas é exercitada para esse tipo, incluindo o scan de `colData` que carrega o fix `[high]` da passada anterior.
  - `[medium]` `[patch]` Mesmo achado-raiz (cobertura de teste): os dois fixes `[low]` patcheados na passada anterior (`validarFaixaAlcada`, `extractFloatOpcional` exigindo chave presente) seguem sem nenhum teste — o único teste de import de `alcadas` usa `valor_maximo` vazio, que nunca alcança `validarFaixaAlcada`, e não existe nenhum teste de PUT para `alcadas` — ação: adicionar teste de import com faixa invertida e testes de PUT (chave ausente/`null` explícito).
  - `[medium]` `[patch]` Mesmo achado-raiz (verification-gap, evidência própria): o ramo de rejeição de `validarFaixaAlcada` nunca é exercitado — o único teste de import de `alcadas` tem `valor_maximo` vazio, que retorna antes da comparação `vmax < valorMinimo`.
  - `[medium]` `[patch]` Mesmo achado-raiz (verification-gap, evidência própria): a regra de `extractFloatOpcional` exigir a chave `valor_maximo` presente no PUT nunca é exercitada — nenhum teste deste arquivo envia PUT para `{tipo}=alcadas`.
  - `[low]` `[patch]` O clamp de `pagina` (`paginaMaxima`, fix da passada anterior) segue sem nenhum teste que confirme o comportamento (200 clampado em vez de 500) — ação: adicionar `TestListarCadastroHandler_PaginaExtrema`.
  - `[low]` `[reject]` `parseIntDefault` trata `pagina`/`tamanho` não-numéricos (ex. `tamanho=abc`) como valor ausente, usando o default silenciosamente em vez de 400 — rejeitado: a I/O Matrix não exige 400 para parâmetro malformado, o cenário é improvável vindo dos clientes reais desta API administrativa, e o fix exigiria propagar um erro novo pela assinatura de `paginacaoDaQuery` (mais que uma correção direta).
  - `[low]` `[reject]` A nova regra de PUT de `alcadas.valor_maximo` (chave deve estar presente; `null` explícito = "sem teto") só está documentada num comentário de código, não no Code Map/Design Notes do spec — rejeitado: o fix é editar o spec desta própria build, fora do escopo de patch.
  - `[medium]` `[patch]` Mesmo achado-raiz (cobertura de teste): o critério "comportamento idêntico entre os 7 tipos" segue empiricamente não verificado para `contas`, `classes-imobilizado`, `papel-pessoa` e `feriados` (4 dos 7 tipos).
  - `[medium]` `[patch]` O caminho de rejeição de `resolveDivisaoID` ("divisão não encontrada") nunca foi exercitado por teste — só o caminho feliz de resolução de FK de `centros-custo` tem teste — ação: adicionar teste de import com `divisao_codigo` inexistente.
  - `[false]` `[reject]` `items` inicializado via `make([]map[string]interface{}, 0)` nunca testado num resultado vazio (risco de regressão para `null`) — refutação: `make(...,0)` sempre serializa como `[]` por semântica do `encoding/json` em slice não-nil; não é uma garantia frágil dependente de disciplina de teste, é uma garantia estrutural do código atual.
  - `[medium]` `[patch]` `RestaurarCadastroHandler` não mapeava violação de constraint (ex. `UNIQUE`) para 400 como `Atualizar`/`ImportarCadastroHandler` fazem — qualquer erro no `UPDATE` de restauração virava 500 — ação: aplicar o mesmo `ehViolacaoDeConstraint`/`errorMsgAmigavel` já usado em `AtualizarCadastroHandler`.
  - `[maybe-false]` `[defer]` `parseNumeroObrigatorio`/`parseNumeroOpcional` usam `strconv.ParseFloat` direto, rejeitando formato decimal pt-BR com vírgula (ex. "100,50") — não é possível confirmar se os CSVs reais de origem (ERP) usam vírgula ou ponto, pois os dados reais não estão neste repositório (mesma limitação já documentada em Design Notes) — gravado em `deferred` (severidade se verdadeiro: medium).
  - `[low]` `[reject]` Valores monetários de `alcadas` (`NUMERIC(18,2)`) trafegam como `float64` em todo o round-trip (decode/snapshot/scan) em vez de um tipo decimal-safe — rejeitado: para os limites de alçada reais deste domínio (muito abaixo de ~10^15), `float64` representa exatamente qualquer valor com 2 casas decimais; o risco só existe em magnitudes irrealistas para um teto de aprovação em reais, e o fix (trocar a representação numérica em toda a superfície de decode/snapshot/scan) é desproporcional à margem de precisão disponível.
  - `[false]` `[reject]` Import de CSV com cabeçalho válido e zero linhas de dado devolve 201 `{"importados":0}`, tabela seguindo vazia e o guard 409 "já possui dados" nunca disparando — refutação: a regra "nunca reimportar um tipo já populado" (Boundaries da spec) se aplica a um tipo com dados; um CSV de cabeçalho só não populou nada, então permitir nova tentativa de import é consistente com a intenção, não uma violação dela.
  - `[medium]` `[patch]` Mesmo achado-raiz (cobertura de teste): caminhos de decode de `contas`/`classes-imobilizado`/`papel-pessoa` seguem sem teste (achado do verification-gap, com busca de símbolo confirmando ausência em todo o arquivo de teste).
  - `[low]` `[patch]` Mesmo achado-raiz: clamp de `pagina` sem teste (achado do verification-gap).
  - `[low]` `[patch]` Mesmo achado-raiz (nota do verification-gap): a ação da passada anterior só fechou 2 dos 6 tipos sinalizados sem teste (`centros-custo`, `alcadas`), deixando `contas`/`classes-imobilizado`/`papel-pessoa`/`feriados` de fora do escopo fechado.
  - `[medium]` `[patch]` Mesmo achado-raiz (auditor de intenção, leitura "identical across 7 types"): a AC "comportamento idêntico entre os 7 tipos" é satisfeita por construção (registry único), mas só verificada empiricamente por teste para 3 dos 7 tipos.
  - `[high]` `[patch]` Mesmo achado-raiz (auditor de intenção): o único achado `[high]` da passada anterior (formatação de `feriados.data`) não tem nenhuma regressão protegendo o fix.
  - `[false]` `[reject]` Divergência R7-vs-R8 do auditor de intenção (garantias de constraint do Postgres/FK/CHECK só verificadas por teste com driver mockado, nunca por integração real) — refutação: o próprio auditor confirma por inspeção que o código atual está correto; é uma observação sobre a filosofia de teste do projeto (mesmo padrão de `admin_test.go`, já aceito desde a Story 1.3), não um defeito demonstrado num local específico.
  - `[medium]` `[patch]` Mesmo achado-raiz (auditor de intenção, divergência R6-vs-R5): a regra de ordenação "`centros_custo` só depois de `divisoes`" é implementada como resolução de FK por linha, não como checagem de sequenciamento, e o caminho de rejeição dessa resolução nunca foi exercitado por teste.

**Ações aplicadas nesta passada:** removido o índice redundante de `divisoes.codigo`; `RestaurarCadastroHandler` agora mapeia violação de constraint para 400 (mesmo padrão de `Atualizar`/`ImportarCadastroHandler`); adicionados 12 testes novos (`TestImportarCadastroHandler_CentrosCusto_DivisaoNaoEncontrada`, `_Alcadas_FaixaInvertida`, `_Contas_Success`, `_Contas_PlanoInvalido`, `_ClassesImobilizado_Success`, `_PapelPessoa_Success`, `_Feriados_Success`, `TestListarCadastroHandler_Feriados_DataFormato`, `_PaginaExtrema`, `TestAtualizarCadastroHandler_Alcadas_ValorMaximoAusente`, `_ValorMaximoNuloExplicito`, `TestRestaurarCadastroHandler_ViolacaoConstraint`). Um achado foi registrado em `deferred` (formato decimal pt-BR em CSV de `alcadas`, não verificável sem os dados reais de origem).

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

**Resumo:** Implementados os 7 cadastros mestres do Epic 2 (divisões, centros de custo, contas, classes de imobilizado, alçadas, papel×pessoa, feriados de SLA) com carga inicial via CSV, listagem paginada, edição, histórico e restauração de versão — uma única implementação Go genérica (registry `{tipo}` → tabela/colunas) serve os 7 tipos, atrás de `RequireAuth(..., "administrador")`. Esta é uma passada de revisão de follow-up sobre a story já `done`, focada em fechar a cobertura de teste deixada em aberto e um achado novo de inconsistência de tratamento de erro.

**Arquivos alterados (cumulativo desde o baseline da story):**
- `backend/migrations/003_cadastros_administraveis.sql` -- cria as 7 tabelas de cadastro + `cadastro_historico` genérica, com `UNIQUE` nas chaves naturais; nesta passada, removido um `CREATE INDEX` redundante em `divisoes.codigo` (já coberto pelo índice implícito de `UNIQUE`)
- `backend/handlers/cadastros.go` -- registry + 5 handlers genéricos; nesta passada, `RestaurarCadastroHandler` passou a mapear violação de constraint (ex. `UNIQUE`) para 400 via `ehViolacaoDeConstraint`/`errorMsgAmigavel`, mesmo padrão já usado em `Atualizar`/`ImportarCadastroHandler` (antes, qualquer erro no `UPDATE` de restauração virava 500)
- `backend/handlers/cadastros_csv.go` -- parser CSV genérico (BOM UTF-8 + `;`), decodificação/validação por tipo — sem alteração nesta passada
- `backend/handlers/cadastros_test.go` -- 16 testes da passada anterior + 12 testes novos nesta passada: `TestImportarCadastroHandler_CentrosCusto_DivisaoNaoEncontrada`, `_Alcadas_FaixaInvertida`, `_Contas_Success`, `_Contas_PlanoInvalido`, `_ClassesImobilizado_Success`, `_PapelPessoa_Success`, `_Feriados_Success`, `TestListarCadastroHandler_Feriados_DataFormato`, `_PaginaExtrema`, `TestAtualizarCadastroHandler_Alcadas_ValorMaximoAusente`, `_ValorMaximoNuloExplicito`, `TestRestaurarCadastroHandler_ViolacaoConstraint`
- `backend/main.go` -- registra as 5 rotas novas atrás de `RequireAuth(..., "administrador")` -- sem alteração nesta passada
- `_bmad-output/implementation-artifacts/epic-2-context.md` -- contexto do Epic 2 compilado para esta e as próximas stories do épico -- sem alteração nesta passada

**Revisão desta passada (follow-up) — achados (26 no total: high 3, medium 10, low 8, false 4, maybe-false 1):**
- Corrigidos (`patch`, 17 achados agrupados em 7 causas-raiz): índice redundante em `divisoes.codigo` removido; `RestaurarCadastroHandler` agora trata violação de constraint como 400; cobertura de teste fechada para `contas`/`classes-imobilizado`/`papel-pessoa` (import feliz + `plano` inválido) e para `feriados` (import + formatação de data na listagem — fecha a lacuna de verificação do único achado `[high]` da passada anterior); cobertura de teste fechada para as duas validações de `alcadas` patcheadas na passada anterior (faixa invertida, chave `valor_maximo` ausente/nula); cobertura de teste fechada para o clamp de `pagina`; cobertura de teste fechada para o caminho de rejeição de `resolveDivisaoID` ("divisão não encontrada").
- Adiado (`defer`, 1 achado): formato decimal de `valor_minimo`/`valor_maximo` em CSV de `alcadas` pode rejeitar vírgula pt-BR — não verificável sem os CSVs reais de origem (ver `deferred` no frontmatter).
- Rejeitados (8 achados, com motivo): contradição `sprint-status.yaml`/spec (artefato do próprio processo de revisão, não um defeito de código); desalinhamento de "linha N" do CSV com linha física em caso de linha em branco (incomum, fix não-trivial); `parseIntDefault` aceitando parâmetro malformado silenciosamente (improvável nos clientes reais, fix não-trivial); documentação da regra de PUT de `alcadas.valor_maximo` só em comentário de código (fix seria editar o spec, fora de escopo de patch); `items` `[]` vs `null` não testado (garantido estruturalmente por `make(...,0)`, não é uma lacuna real); precisão de `float64` para valores monetários de `alcadas` (irrealista nas magnitudes deste domínio, fix desproporcional); import de CSV com cabeçalho válido e zero linhas (consistente com a intenção, não uma violação); divergência "testes com driver mockado vs. integração real" do auditor de intenção (sem defeito demonstrado, mesma filosofia de teste já aceita desde a Story 1.3).

**Follow-up review recommendation:** `true` — esta passada corrigiu um achado `high` (lacuna de verificação do fix de formatação de `feriados.data`). Risco específico não verificado: os novos testes cobrem a leitura (`ListarCadastroHandler`) e a carga inicial (`ImportarCadastroHandler`) de `feriados` separadamente, mas nenhum teste encadeia "listar → editar via PUT → listar de novo" especificamente para `{tipo}=feriados` — um teste único de round-trip completo desse tipo ainda não existe.

**Verificação realizada:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- limpo
- `cd backend && go test ./...` -- todos os pacotes passam, incluindo os 28 testes de `cadastros_test.go` (16 da passada anterior + 12 novos)
- `cd backend && go test ./handlers/... -run Cadastro -v` -- todos os casos da I/O Matrix e dos novos achados passam

**Riscos residuais:**
- Nenhum teste de round-trip completo (listar→editar→listar) específico para `feriados` (ver follow-up acima).
- Formato decimal pt-BR (vírgula) em CSV de `alcadas` não verificado contra dados reais — ver `deferred`.
- Carga de `alcadas` com milhares de linhas é sequencial (sem batch) — aceito como custo único administrativo dentro do timeout de 300s do servidor (ver Review Triage Log da passada anterior).
- A contagem de "importados" é verificada com fixtures sintéticas, não com os CSVs reais de produção (que não estão neste repositório) — interpretação documentada em Design Notes.

