---
title: 'Gerar lote Despesa'
type: 'feature'
created: '2026-10-08'
status: 'awaiting-operator'
baseline_revision: 'ece3547291bde4bff00bb0159db2d4d7c781e4f7'
review_loop_iteration: 0
followup_review_recommended: false
context: ['{project-root}/_bmad-output/planning-artifacts/architecture/architecture-FB_APU05-2026-10-06/ARCHITECTURE-SPINE.md']
warnings: ['oversized']
deferred: []
operator_actions:
  - 'Obter o arquivo `.xlsm`/`modRoboOrcamento.bas` real do robô SAP (ativo de negócio externo, não existe neste repositório) e confirmar se o layout de colunas da planilha "SAP Export" implementado em `montarSheetDataXML` (internal/exportacao/xlsm.go) — tipo/solicitação/divisão/CC/conta-ou-classe/mês/valor/lado/solicitante — bate com o que a macro de fato espera; ajustar a ordem/conteúdo das colunas se não bater.'
  - 'Confirmar com a equipe do robô se o campo de mês/competência deve ser texto ISO (YYYY-MM-DD, implementação atual) ou valor serial do Excel — formarMesExportacao em xlsm.go precisa ser ajustado se o robô esperar o segundo formato.'
  - 'Publicar o `.xlsm` real em cada ambiente (dev/staging/produção) e configurar `EXPORT_TEMPLATE_LOTE_DESPESA` apontando para esse caminho — sem isso, POST /api/solicitacoes/lote-despesa responde 503 (comportamento correto e testado, não um defeito).'
---

<intent-contract>

## Intent

**Problem:** Solicitações `transferencia`/`inclusao`/`inclusao_sfc`/`imobilizado` aprovadas e em atendimento não têm nenhum caminho para alimentar o robô SAP — não existe `exportacoes_sap`, nem módulo `internal/exportacao` (hoje vazio), nem rota de geração de lote.

**Approach:** Nova interface `ExportadorSAP`/impl `ExportadorVBA` (`internal/exportacao`) com `GerarLoteDespesa`: recalcula elegibilidade (dono=sessão, `status=em_atendimento`), valida exercício único (linhas com `mes`), detecta risco de "verba duplicada" (solicitação já com export `GERADO` não finalizado), grava 1 linha de `exportacoes_sap` por solicitação (idempotente por `(administrador_id, chave_idempotencia)`) e monta o `.xlsm` via ZIP/XML reescrevendo um TEMPLATE base (AD-12) lido de um caminho configurável — **o arquivo `.xlsm` real do robô (`modRoboOrcamento.bas`) é um ativo de negócio externo, não existe neste repositório; sem ele, `GerarLoteDespesa` devolve `ErrTemplateAusente` (503) em vez de inventar um arquivo**.

## Boundaries & Constraints

**Always:**
- `administrador_id` sempre de `GetUserIDFromContext`; para cada `solicitacao_id` do corpo, servidor recalcula `status`/`administrador_id`/`tipo_solicitacao` no banco — nunca confia em nada vindo do cliente além dos próprios IDs.
- Toda leitura/escrita de `exportacoes_sap`/`solicitacao_comentarios` (aviso ignorado) passa por `internal/exportacao` via conexão PRIVILEGIADA (mesmo princípio AD-4 de `internal/fila`).
- Idempotência por `(administrador_id, chave_idempotencia)`: repetir a mesma chave nunca insere 2ª linha por solicitação (`ON CONFLICT DO NOTHING` + re-`SELECT`); reusar a chave com um conjunto de `solicitacao_ids` diferente é 409.
- "Verba duplicada": qualquer `solicitacao_id` do lote que já tenha linha `exportacoes_sap` com `status='GERADO'` de uma chave DIFERENTE é aviso; só prossegue com `ignorar_aviso_verba_duplicada=true`, e só então grava 1 `solicitacao_comentarios` (`tipo='aviso_verba_duplicada'`) por solicitação em risco.
- Sem template `.xlsm` legível no caminho configurado (`EXPORT_TEMPLATE_LOTE_DESPESA`, default `internal/exportacao/templates/lote_despesa.xlsm`): `ErrTemplateAusente` -> 503, nenhuma linha gravada.

**Never:**
- Nunca alterar `status`/`versao` de `solicitacoes` aqui — finalizar é a Story 4.5 (backlog); gerar o arquivo não finaliza (Epic 4 Requirements).
- Nunca aceitar `tipo_solicitacao='obras'` no lote Despesa (Lote-Obra é a Story 4.4).
- Nunca tocar o part `vbaProject.bin` do template (copiado byte a byte); nunca reescrever sem remover `xl/calcChain.xml`; nunca marcar a aba de rascunho como oculta.
- Não implementar Lote-Obra nem Finalizar.

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Geração com sucesso | IDs válidos, todas `em_atendimento`, dono=sessão, mesmo exercício, sem risco duplicado, template presente | 200; `lote_id`, `arquivo_base64`, `solicitacoes_incluidas`; 1 linha `exportacoes_sap` por solicitação | No error expected |
| Repetir mesma chave | 2ª chamada, mesma `chave_idempotencia`+mesmos IDs | 200; mesmo `lote_id`; nenhuma linha nova em `exportacoes_sap` | No error expected |
| Chave reusada com IDs diferentes | mesma `chave_idempotencia`, `solicitacao_ids` diferente do 1º uso | — | 409 |
| Solicitação de outro administrador | `administrador_id` da linha ≠ chamador | — | 403 |
| Solicitação não `em_atendimento` | `status` = `aberta`/`pendente`/outro | — | 409 |
| ID inexistente | UUID não encontrado | — | 404 |
| Tipo `obras` incluído | algum ID com `tipo_solicitacao='obras'` | — | 400 |
| Exercícios mistos | linhas com `mes` em anos diferentes entre as solicitações do lote | — | 400 |
| Risco de verba duplicada, sem ignorar | solicitação já em export `GERADO` (chave diferente), `ignorar_aviso_verba_duplicada=false` | 200; `{"aviso":{"codigo":"verba_duplicada","solicitacoes_em_risco":[...]}}`; nada gravado | No error expected |
| Risco de verba duplicada, ignorado | mesmo cenário, `ignorar_aviso_verba_duplicada=true` | 200; gera normalmente + 1 `solicitacao_comentarios` por solicitação em risco | No error expected |
| Template ausente | `EXPORT_TEMPLATE_LOTE_DESPESA` não aponta para arquivo legível | — | 503 |

</intent-contract>

## Code Map

- `backend/internal/exportacao/` -- hoje só `.gitkeep`; criar `exportacao.go` (interface `ExportadorSAP`, `Lote{LoteID,ArquivoBytes}`, `Aviso`, sentinelas `ErrTemplateAusente`/`ErrNaoAutorizado`/`ErrNaoEncontrada`/`ErrElegibilidadeInvalida`/`ErrChaveIdempotenciaConflitante`/`ErrTipoNaoElegivel`/`ErrExercicioMisto`), `vba.go` (`ExportadorVBA.GerarLoteDespesa`: elegibilidade+validação+idempotência+aviso, tudo via conexão privilegiada), `xlsm.go` (reescrita ZIP/XML do template: `archive/zip`, substitui linhas da aba "SAP Export", remove `xl/calcChain.xml`, preserva `vbaProject.bin`, mantém aba `Visible`).
- `backend/internal/fila/fila.go:63-92` -- padrão de fallback 404→403→409 a replicar (SELECT de fallback após UPDATE/INSERT não afetar linhas).
- `backend/handlers/pendencia.go:36-120` -- padrão de leitura/validação de corpo, `tratarErroEscritaFila`, helpers `jsonErr`/`nullStringOuNil` (middleware.go/auth.go) a reaproveitar por analogia (novo helper equivalente em `exportacao.go` do pacote `handlers`).
- `backend/handlers/solicitacoes.go:679-730` -- `validarBalanceamento`/`somaPorLado`/`validarExercicioUnico` (lógica de referência; `internal/exportacao` replica versão própria sobre dados lidos do banco, já que não pode importar `handlers`).
- `backend/migrations/010_assumir_fila.sql`, `011_pendencia_e_reabertura.sql` -- padrão GRANT/REVOKE (AD-4) e `GRANT INSERT ... TO fb_apu05_privilegiado`; esta story cria `012_exportacoes_sap.sql`.
- `backend/main.go:283-308,356,367-369` -- `withPrivilegedDB`; nova rota entra após a linha 369.

## Tasks & Acceptance

**Execution:**
- `backend/migrations/012_exportacoes_sap.sql` -- NOVA: `exportacoes_sap(id UUID PK, lote_id UUID NOT NULL, solicitacao_id UUID NOT NULL REFERENCES solicitacoes(id), administrador_id UUID NOT NULL REFERENCES usuarios(id), chave_idempotencia VARCHAR(255) NOT NULL, status VARCHAR(20) NOT NULL DEFAULT 'GERADO' CHECK (status IN ('GERADO','FINALIZADO')), created_at, updated_at)` + `UNIQUE (administrador_id, chave_idempotencia, solicitacao_id)` + índice em `solicitacao_id` (lookup de verba duplicada); `ALTER TABLE solicitacao_comentarios DROP CONSTRAINT solicitacao_comentarios_tipo_check, ADD CONSTRAINT solicitacao_comentarios_tipo_check CHECK (tipo IN ('pendencia','comentario','aviso_verba_duplicada'))`; `REVOKE ALL ON exportacoes_sap FROM fb_apu05_app` + `GRANT SELECT, INSERT, UPDATE ON exportacoes_sap TO fb_apu05_privilegiado` (AD-4, mesmo motivo do REVOKE em `solicitacoes`); `GRANT SELECT ON solicitacao_lancamentos, contas, centros_custo, divisoes, classes_imobilizado, usuarios TO fb_apu05_privilegiado`.
- `backend/internal/exportacao/exportacao.go` -- NOVO: tipos/interface/sentinelas acima.
- `backend/internal/exportacao/vba.go` -- NOVO: `ExportadorVBA{TemplatePath string}.GerarLoteDespesa(db *sql.DB, solicitacaoIDs []string, administradorID, chaveIdempotencia string, ignorarAvisoVerbaDuplicada bool) (Lote, *Aviso, error)`: para cada ID, `SELECT tipo_solicitacao,status,administrador_id FROM solicitacoes WHERE id=$1` (404/403/409/400-tipo-obras); junta `solicitacao_lancamentos` para exercício único (só linhas com `mes` não nulo) e conteúdo do arquivo; verifica linhas `exportacoes_sap` existentes por `solicitacao_id` com `status='GERADO'` e chave diferente (aviso); se aviso e não ignorado, retorna `*Aviso` sem gravar; senão lê o template (`os.ReadFile`; ausente/erro -> `ErrTemplateAusente`), monta `.xlsm` (`xlsm.go`), `INSERT ... ON CONFLICT (administrador_id, chave_idempotencia, solicitacao_id) DO NOTHING` por solicitação + (se aviso ignorado) 1 `INSERT solicitacao_comentarios` por solicitação em risco, tudo numa transação; re-`SELECT lote_id` para devolver o mesmo em retries.
- `backend/internal/exportacao/xlsm.go` -- NOVO: `montarXLSM(templateBytes []byte, linhas []linhaExportacao) ([]byte, error)` -- abre o template como `archive/zip.Reader`, copia todas as partes sem alterar exceto a planilha "SAP Export" (reescreve `<row>`s a partir de `linhas`, escapando texto livre) e remove `xl/calcChain.xml`; `vbaProject.bin` sempre copiado byte a byte; falha se o template não tiver uma planilha "SAP Export".
- `backend/handlers/exportacao.go` -- NOVO: `GerarLoteDespesaHandler(db *sql.DB)` -- lê corpo `{solicitacao_ids []string, chave_idempotencia string, ignorar_aviso_verba_duplicada bool}` (400 se `solicitacao_ids` vazio ou `chave_idempotencia` vazia ou algum ID não é UUID), chama `exportacao.GerarLoteDespesa`, traduz sentinelas (404/403/409/400/503), devolve 200 com `{lote_id, arquivo_nome, arquivo_base64, solicitacoes_incluidas}` ou `{aviso:{...}}`.
- `backend/main.go` -- registra `POST /api/solicitacoes/lote-despesa` (`RequireAuth(withPrivilegedDB(handlers.GerarLoteDespesaHandler), "administrador")`), após a linha 369.
- `backend/internal/exportacao/{vba_test.go,xlsm_test.go}`, `backend/handlers/exportacao_test.go` -- cobrem a I/O Matrix; `xlsm_test.go` constrói um `.xlsm` sintético de teste (não o arquivo de produção) para provar preservação byte a byte de `vbaProject.bin` e remoção de `calcChain.xml`.

**Acceptance Criteria:**
- Given um `EXPORT_TEMPLATE_LOTE_DESPESA` apontando para um `.xlsm` válido e solicitações elegíveis de 1+ tipos do lote Despesa, when o administrador gera o lote, then recebe um `.xlsm` com as linhas corretas e 1 registro `exportacoes_sap` por solicitação, sem duplicar ao repetir a mesma `chave_idempotencia`.
- Given nenhum arquivo legível no caminho configurado, when o administrador tenta gerar o lote, then recebe 503 e nada é gravado.

## Spec Change Log

## Review Triage Log

## Design Notes

**Gap de ativo externo (não é bug, é dependência de negócio):** o `.xlsm` real do robô (`modRoboOrcamento.bas`, planilha "SAP Export", macro que roda `S_ALR_87013620`/`KP06`) nunca foi commitado neste repositório nem está disponível neste ambiente (só referenciado em `docs/referencia/02-REQUISITOS-NEGOCIO-ENVIO-TI.md` como artefato de outro pacote). O layout de colunas da planilha "SAP Export" usado por `montarXLSM` é melhor esforço (tipo/solicitação/divisão/CC-ou-classe/conta-ou-classe/mês/valor/lado) e PRECISA ser confirmado contra o `.bas` real antes de produção — ver `operator_actions` no resultado final desta story.

**Exercício único:** calculado só sobre linhas com `mes` preenchido (transferência/inclusão/inclusão SFC) — linhas de Imobilizado não têm `mes` (migration 008) e ficam fora dessa checagem.

**"Inclusão só com valores positivos"/"transferência balanceada" (AC):** já são invariantes de schema (`CHECK (valor > 0)`, migration 006) e de validação na criação (`validarBalanceamento`, imutável depois); não há caminho de escrita que viole isso hoje, então nenhuma revalidação redundante é adicionada aqui além da releitura normal dos dados para montar o arquivo.

## Verification

**Commands:**
- `cd backend && go build ./... && go vet ./... && gofmt -l .` -- expected: sem erros, sem diffs.
- `cd backend && go test ./... -count=1` -- expected: todos os pacotes passam, incluindo os novos testes de `internal/exportacao` e `handlers/exportacao_test.go`.

**Manual checks (if no CLI):**
- `docker-compose up` e confirmar que `012_exportacoes_sap.sql` roda sem erro e que os GRANTs permitem `internal/exportacao` ler/escrever pela conexão privilegiada.

## Auto Run Result

**Recuperação manual (2026-10-09) — sessão bmad-loop interrompida por stall (limite de uso) antes de rodar sua própria revisão ou commitar.** Implementação já estava completa ao parar; nenhum patch de código foi necessário. Verificação própria cobriu:
- Build/vet/gofmt/test (`go test ./... -count=1`): todos os pacotes OK, incluindo `internal/exportacao` (novo).
- Nomes de coluna/tabela usados nas queries SQL de `vba.go` (sqlmock não valida contra um schema real) checados contra as migrations 003/008/009/010/011/012: `classes_imobilizado.codigo`, `solicitacao_lancamentos.classe_imobilizado_id`, `solicitacao_comentarios.autor_id`/`texto`, GRANTs de `fb_apu05_privilegiado` em todas as 6 tabelas lidas por `internal/exportacao` — todos corretos.
- Lógica de `xlsm.go` (manipulação ZIP/XML, a parte de maior risco técnico desta story): resolução do caminho real da aba "SAP Export" via `workbook.xml`+`.rels` (nunca nome de arquivo fixo), `zip.Writer.Copy` preservando `vbaProject.bin` byte a byte, remoção de `calcChain.xml`, escape de texto XML em toda coluna — tudo confirmado por leitura E pelos testes sintéticos de `xlsm_test.go` (`TestMontarXLSM_PreservaVBAERemoveCalcChain` et al.).
- Cobertura de teste nas 3 camadas (`internal/exportacao/xlsm_test.go`, `internal/exportacao/vba_test.go`, `handlers/exportacao_test.go`) confirmada contra a I/O & Edge-Case Matrix da spec — todas as 11 linhas têm teste correspondente.

Status final `awaiting-operator` (não `done`): o `.xlsm`/`.bas` real do robô é um ativo de negócio externo que não existe neste ambiente (Intent/Design Notes da spec) — o layout de colunas implementado é melhor esforço e precisa de confirmação humana contra o artefato real antes de uso em produção. Ver `operator_actions` no frontmatter.

**Riscos residuais:** nenhum novo; os riscos já documentados no Design Notes (formato de data texto vs. serial do Excel, layout de colunas melhor-esforço) são exatamente o que `operator_actions` cobre.
