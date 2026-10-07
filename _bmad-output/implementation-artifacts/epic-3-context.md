# Epic 3 Context: Solicitações Orçamentárias e Motor de Aprovação

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Solicitantes autenticados podem abrir qualquer um dos cinco tipos de solicitação orçamentária/de investimento (transferência, inclusão, inclusão SFC, imobilizado, obras) e ter o aprovador resolvido automaticamente pelo servidor — por cálculo de alçada ou por autorizador nominal validado, conforme o tipo — com a decisão sempre gravada como snapshot de auditoria imutável. Este é o núcleo de maior risco de esforço do produto: nunca deve inventar ou sintetizar um aprovador quando o cadastro de alçada não cobre o caso; nesses casos o sistema bloqueia com mensagem explícita. Substitui o cálculo manual/de memória hoje feito em planilha.

## Stories

- Story 3.1: Abrir Transferência com aprovação calculada
- Story 3.2: Abrir Inclusão SFC com aprovação calculada
- Story 3.3: Abrir Inclusão com autorizador nominal
- Story 3.4: Abrir Imobilizado com anexo de cotação
- Story 3.5: Abrir Obras multi-linha

## Requirements & Constraints

- Transferência: dois lados do lançamento (origem/destino) devem bater em valor, tolerância R$ 0,01; campos obrigatórios por linha (divisão, CC, conta, mês, valor); um único exercício orçamentário por solicitação.
- Inclusão / Inclusão SFC: valor sempre positivo (zero ou negativo é recusado). Inclusão SFC usa exclusivamente o plano de contas `CONTAS_SFC`, que nunca pode se misturar com o plano BIFC mesmo quando os códigos de conta coincidem.
- Imobilizado: campo obrigatório é a classe de imobilizado (lista fixa de 8), não conta contábil; formulário não expõe fornecedor nem mês/competência; exige ao menos um anexo de cotação para poder ser enviado.
- Obras: multi-linha por local de obra + subgrupo de despesa + ordem de investimento; sem campos de filial/CC/conta/mês; classificação "transferência de saldo" exige dois blocos (retirada + inclusão) balanceados em valor; linha com ordem `CRIAR` não gera a ordem real no SAP nesta etapa (isso só acontece na finalização, Epic 4, mesma transação).
- Resolução calculada (Transferência, Inclusão SFC): cadeia regras especiais curadas → matriz de alçadas SAP (filial+CC+valor) → janela do gerente do CC (até R$ 20.000) → fallback GR mais próximo acima. Quando nenhuma alçada cobre o caso, a solicitação é bloqueada com "sem alçada cadastrada" citando CC e filial — nunca aprovador fictício. Escalonamento acima de R$ 20.000 é fora de escopo desta fase. Papel que resolve para um cargo colegiado sem titular é um aprovador válido por desenho, não uma lacuna.
- Resolução nominal (Inclusão, Imobilizado, Obras): autorizador escolhido pelo solicitante é validado contra lista fixa por CC/faixa de valor; autorizador fora da lista não aparece como opção. Obras usa lista fixa própria (11-12 pessoas) com teto de alçada individual por pessoa.
- O filtro de conta disponível por CC (`rel_cc_conta`) fica desativado nos formulários até o cadastro mestre ser completado pelo negócio (hoje só 27 de ~21 mil combinações existem) — não tratar como defeito.
- Exceção de CC (Epic 2) só autoriza abrir/acompanhar solicitação no CC adicional; nunca concede aprovação — o aprovador é sempre calculado a partir do CC da solicitação, independente de quem a submeteu.
- Volume de solicitações bloqueadas por "sem alçada cadastrada" não é uma métrica de sucesso do sistema; não reduzir o rigor da checagem para diminuir esse número.
- Nenhum exemplo, fixture ou dado de teste pode usar nome real, matrícula ou e-mail de pessoa — sempre referenciar por cargo/papel (repositório público).

## Technical Decisions

- Núcleo de aprovação vive isolado em `backend/internal/aprovacao`, sem dependência de HTTP ou driver de banco específico; handlers nunca calculam aprovador diretamente, sempre delegam a esse módulo.
- Porta única `Resolver` com duas implementações (`ResolverCalculado`, `AutorizadorNominal`); dispatch tipo→resolver é uma única função/fábrica compartilhada (`ResolverParaTipo`), nunca reimplementada por handler. Todo handler chama apenas `Resolver.Resolve(solicitacao) → (Aprovador, error)`.
- Tipo `Aprovador` é explícito, nunca string livre: `{Tipo: "pessoa"|"cargo", Nome, ColaboradorID *string, Motivo, RegraID, RegraVersao}` — `ColaboradorID` é nulo exatamente quando `Tipo="cargo"`.
- Falta de alçada retorna erro sentinela `ErrSemAlcadaCadastrada` (com CC e filial); é o chamador (handler) que traduz isso para a mensagem ao usuário — o resolver nunca inventa um `Aprovador`.
- Escrita de `aprovador_snapshot` e `versao` em `solicitacoes` passa exclusivamente pelo módulo `aprovacao`; é tecnicamente bloqueada para outras roles via `REVOKE`/`GRANT` no Postgres, não apenas convenção de code review.
- Upload de anexo (Imobilizado) passa exclusivamente pelo módulo único `backend/internal/anexos` (`Salvar`/`Recuperar`), nunca por lógica própria do handler.
- `tipo_solicitacao` usa exatamente os valores do glossário: `transferencia`, `inclusao`, `inclusao_sfc`, `imobilizado`, `obras`.
- Entidades centrais: `SOLICITACAO` contém `SOLICITACAO_LANCAMENTO` (linhas) e grava uma `APROVADOR_SNAPSHOT`.

## Cross-Story Dependencies

- Story 3.2 (Inclusão SFC) reaproveita o mesmo motor `ResolverCalculado` da Story 3.1 — não reimplementa a cadeia de regras.
- Stories 3.3, 3.4 e 3.5 reaproveitam o mesmo motor `AutorizadorNominal`/validação por CC-faixa; 3.4 e 3.5 dependem do mesmo mecanismo de validação introduzido em 3.3.
- Todos os tipos dependem da identidade de CC do solicitante e das exceções de CC (Epic 2) para saber em quais CCs cada solicitante pode abrir solicitação.
- A linha de Obras com ordem `CRIAR` (Story 3.5) só tem sua ordem real criada no SAP durante a finalização (Epic 4, Story 4.5) — esta epic apenas aceita e persiste o literal `CRIAR`.
- O snapshot de auditoria e o campo `versao` gravados aqui são a base consumida pela Fila do Administrador e pela geração de lotes (Epic 4).
