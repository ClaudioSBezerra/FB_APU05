# Epic 4 Context: Processamento e Exportação SAP

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Administradores da Controladoria processam a fila de solicitações já aprovadas (assumir → atender/marcar pendência → finalizar) e geram os dois lotes de exportação (Despesa e Obra) que alimentam o robô SAP (VBA/.xlsm) hoje, e uma futura API SAP depois. O que importa aqui é a integridade operacional: nenhuma solicitação pode ser processada por dois administradores ao mesmo tempo, gerar o arquivo de exportação nunca finaliza por si só, a elegibilidade para lote é sempre recalculada no servidor, e a geração do Lote-Despesa é idempotente. Esta é a etapa final do ciclo de vida de uma solicitação, depois do motor de aprovação (Epic 3) já ter resolvido o aprovador.

## Stories

- Story 4.1: Assumir solicitação da fila
- Story 4.2: Pendência e reabertura
- Story 4.3: Gerar lote Despesa
- Story 4.4: Gerar lote Obra
- Story 4.5: Finalizar solicitação

## Requirements & Constraints

- Administrador assume uma solicitação aprovada da fila; a trava é por lock otimista no campo `versão` — se outro admin já assumiu antes, a segunda tentativa recebe conflito de versão, sem retry automático do servidor.
- A fila mostra o prazo de SLA (48h úteis, calendário de feriados PE/federais, fuso America/Recife) e sinaliza solicitações já atrasadas.
- Pendência devolve a solicitação ao solicitante com comentário visível no histórico; uma solicitação já finalizada que recebe novo comentário reabre automaticamente.
- Finalizar é uma ação transacional separada de gerar o arquivo de exportação — gerar o arquivo nunca muda o status por si só. Não existe campo "Documento/referência SAP" no modelo; não deve ser reintroduzido.
- Lote-Despesa (transferência/inclusão/SFC/imobilizado): só inclui solicitações atribuídas ao administrador ativo, em `em_atendimento`, com todos os campos obrigatórios preenchidos; inclusão só com valores positivos; transferência balanceada; um único exercício orçamentário por lote. Geração é idempotente e grava snapshot — gerar de novo não duplica.
- Lote-Obra: só inclui linhas cuja ordem já existe no SAP e valor positivo; linha com ordem `CRIAR` ou valor ≤ 0 é descartada individualmente, sem barrar o lote inteiro; solicitação sem nenhuma linha válida vai para "Bloqueados" com o motivo.
- O servidor sempre recalcula elegibilidade e cruza IDs com o responsável da sessão — nunca confia num `admin_id` enviado pelo cliente.
- Sistema exibe aviso de "verba duplicada" quando há risco de dupla contagem (inclui exportação em status `GERADO` ainda não finalizada); se o administrador optar por ignorar o aviso, isso é gravado no histórico da solicitação.
- Uma linha de Obras com ordem `CRIAR` tem sua ordem real criada no SAP na mesma transação de finalização (não antes, nunca na abertura/aprovação).
- Contra-métrica: volume de bloqueios por "sem alçada cadastrada" não é indicador de sucesso do sistema — não otimizar reduzindo rigor da checagem.
- Dados sensíveis: nunca usar nome real, matrícula ou e-mail de pessoa em exemplos/fixtures/stories — sempre referenciar por cargo/papel.

## Technical Decisions

- Toda escrita em `solicitacoes` usa `WHERE versao = :versao_lida`; zero linhas afetadas retorna HTTP 409 no envelope padronizado `{"erro": {"codigo": "conflito_versao", "mensagem": string, "versao_atual": int}}`. Essa mesma linha/lock cobre assumir, pendência, finalização e a transição de status feita pela exportação — não são mecanismos separados.
- Handlers/services nunca geram o arquivo de exportação nem escrevem status de exportação diretamente — sempre delegam à interface única `ExportadorSAP` (`backend/internal/exportacao`), com métodos por lote (`GerarLoteDespesa(solicitacaoIDs) (Lote, error)`, `GerarLoteObra(...)`), nunca por solicitação individual. A própria implementação grava `exportacoes_sap`; o handler só devolve `ArquivoBytes` ao navegador. v1 implementa `ExportadorVBA`; troca futura para `ExportadorAPI` não deve exigir mudar handlers.
- Writer path único e tecnicamente forçado: `REVOKE UPDATE` nos campos `aprovador_snapshot`, `versao`, `status`, `exportacoes_sap.*` para a role padrão da aplicação; só a role usada pelos módulos `aprovacao`/`exportacao` tem `GRANT`. Não é só convenção de code review.
- Módulo único de SLA (`backend/internal/sla`) expõe `EstaAtrasado(solicitacao) bool` e `PrazoLimite(solicitacao) time.Time`; Fila do Administrador e Painéis consomem esse módulo — nenhum recalcula o calendário por conta própria. Regra de pausa do SLA ainda não definida com o negócio (fica fora do mecanismo por ora).
- Geração do `.xlsm`: manipular como ZIP/XML via `archive/zip` + reescrita de partes XML, sem lib externa de OOXML. Nunca tocar `vbaProject.bin` (copiar byte a byte). Todo texto livre do usuário passa por função de escape antes de entrar em fórmula/texto gerado — nunca substituição de string não escapada (risco de corromper fórmulas tipo `$B$100`). Remover `xl/calcChain.xml` ao reescrever linhas. Aba de rascunho (`SAP Export` ou equivalente) sempre `Visible`, nunca `veryHidden`.
- Entidades centrais relevantes: `SOLICITACAO` grava `APROVADOR_SNAPSHOT` (1:1) e gera `EXPORTACAO_SAP` (1:N). UUID como PK; `created_at`/`updated_at` em toda tabela.
- Listagens (ex. fila) seguem paginação obrigatória: parâmetros `pagina`/`tamanho`, resposta `{items, pagina, tamanho}`, sem `total` nem cursor.

## Cross-Story Dependencies

- Epic 4 consome diretamente o resultado do motor de aprovação do Epic 3 (snapshot de aprovador já gravado) — a fila só trabalha com solicitações já aprovadas.
- Story 4.1 (assumir) é pré-requisito operacional para 4.2 (pendência), 4.3/4.4 (gerar lote) e 4.5 (finalizar) — todas assumem uma solicitação já travada para o administrador ativo.
- Story 4.5 (finalizar) depende de 4.3 ou 4.4 já terem gerado a exportação correspondente; finalizar e gerar o arquivo são transações separadas, mas finalizar é o único ponto em que uma linha de Obras com ordem `CRIAR` materializa a ordem real no SAP.
- O aviso de "verba duplicada" em 4.3 depende do estado de exportações já geradas (inclusive em status `GERADO` ainda não finalizado) de outras solicitações — risco cross-request, não isolado por solicitação.
