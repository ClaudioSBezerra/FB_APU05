---
status: final
stepsCompleted: ["step-01", "step-02", "step-03", "step-04"]
inputDocuments:
  - _bmad-output/planning-artifacts/prds/prd-FB_APU05-2026-10-06/prd.md
  - _bmad-output/planning-artifacts/prds/prd-FB_APU05-2026-10-06/addendum.md
  - _bmad-output/planning-artifacts/architecture/architecture-FB_APU05-2026-10-06/ARCHITECTURE-SPINE.md
  - _bmad-output/planning-artifacts/briefs/brief-FB_APU05-2026-10-06/addendum.md
  - docs/referencia/02-REQUISITOS-NEGOCIO-ENVIO-TI.md
---

# FB_APU05 — Módulo Controladoria - Epic Breakdown

## Overview

This document provides the complete epic and story breakdown for FB_APU05 — Módulo Controladoria, decomposing the requirements from the PRD and Architecture Spine into implementable stories. No UX design contract exists yet (`bmad-ux` has not run).

## Requirements Inventory

### Functional Requirements

FR-1: Qualquer usuário pode autenticar via OIDC/PKCE contra o Keycloak real da Ferreira Costa, com validação JWKS e checagem obrigatória de `email_verified`, sem cadastro de usuário separado.
FR-2: O primeiro administrador é provisionado diretamente no banco por procedimento controlado; concessões posteriores exigem admin autenticado e geram evento auditável; o último administrador ativo não pode ser rebaixado nem desativado.
FR-3: Sistema carrega periodicamente uma planilha/CSV de colaboradores e colaboradores-por-CC (fonte Senior); registros-placeholder de cargo sem titular não são carregados como colaborador.
FR-4: Administrador pode cadastrar, para um colaborador específico, uma lista de CCs adicionais autorizados (exceção); colaborador com exceção pode abrir/acompanhar solicitações nesses CCs mas não aprová-las; segue padrão de histórico/restauração (FR-16).
FR-5: Solicitante pode abrir uma Transferência de saldo entre contas do CC autorizado, com dois lados que devem bater (tolerância R$ 0,01), campos obrigatórios por linha, exercício orçamentário único.
FR-6: Solicitante pode abrir uma Inclusão de orçamento, valor positivo, com autorizador nominal.
FR-7: Solicitante pode abrir uma Inclusão SFC (RPA), idêntica à Inclusão exceto pelo plano de contas exclusivo `CONTAS_SFC`; usa resolver calculado.
FR-8: Solicitante pode abrir aquisição de Imobilizado por classe de imobilizado (não conta contábil), sem campos de fornecedor/competência, com anexo de cotação obrigatório; autorizador nominal por CC/faixa.
FR-9: Solicitante pode abrir solicitação de Obras multi-linha por local de obra + subgrupo de despesa + ordem de investimento, com 4 classificações; ordem aceita literal `CRIAR` (nasce na finalização); autorizador de lista fixa com teto de alçada por pessoa.
FR-10: Para transferência e inclusão SFC, sistema calcula o aprovador no servidor (importação filial≥9000 → regras especiais curadas → matriz de alçadas → janela do gerente → fallback GR); grava snapshot de auditoria; nunca inventa aprovador — bloqueia com "sem alçada cadastrada" quando não há alçada cadastrada; escalonamento acima de R$ 20.000 fica fora do fluxo do sistema nesta fase.
FR-11: Para inclusão, imobilizado e obras, sistema valida o autorizador escolhido contra lista fixa por CC/faixa de valor; grava snapshot no mesmo formato do resolver calculado.
FR-12: Administrador pode assumir uma solicitação aprovada da fila, travada via lock otimista por campo `versão`.
FR-13: Administrador pode marcar pendência (devolve ao solicitante com comentário); solicitação finalizada que recebe novo comentário reabre automaticamente.
FR-14: Administrador pode finalizar uma solicitação após gerar sua exportação SAP — ação transacional separada da geração do arquivo, via lock otimista; campo "Documento/referência SAP" não existe no modelo.
FR-15: Administrador pode gerar o lote Despesa (transferência/inclusão/SFC/imobilizado) ou o lote Obra (obras), cada um com elegibilidade própria; servidor recalcula elegibilidade e nunca confia em `admin_id` do cliente; geração do Lote-Despesa é idempotente e grava snapshot; aviso de "verba duplicada" quando há risco de dupla contagem.
FR-16: Qualquer alteração em um cadastro administrável gera histórico; administrador pode restaurar uma versão anterior; comportamento uniforme por design entre todos os tipos de cadastro.
FR-17: Usuário pode visualizar painéis com indicadores agregados de solicitações, lidos de snapshots pré-calculados — escopo exato de indicadores ainda a definir com o negócio.

### NonFunctional Requirements

NFR-1 (SLA): Toda solicitação tem prazo de 48 horas úteis para processamento, contado pelo calendário de feriados PE/federais cadastrado (FR-16), em fuso America/Recife. Regra de pausa do SLA ainda não definida com o negócio.
NFR-2 (Paginação obrigatória): Qualquer listagem que possa ultrapassar 1.000 linhas deve paginar obrigatoriamente; contrato de resposta é `{items, pagina, tamanho}`, sem `total` nem cursor.
NFR-3 (Prazo/Risco): Prazo-alvo outubro/2026 é flexível; escopo completo dos 5 tipos de solicitação é a variável fixa — não cortar tipo nem regra para encaixar no prazo.

### Additional Requirements

- **Sem starter template formal.** A Architecture Spine não aponta um scaffold pronto — o ponto de partida é replicar manualmente a estrutura de pastas e padrões já validados no FB_APU02 (`backend/`, `frontend/`, `docker-compose.yml` na raiz, migrations numeradas). Isso deve ser a base da Epic de Infraestrutura/Setup, Story 1.
- Paradigma híbrido: núcleo de domínio isolado (`backend/internal/aprovacao`, `backend/internal/exportacao`, `backend/internal/sla`, `backend/internal/anexos`) sem dependência de HTTP/driver específico; resto do sistema segue o padrão Handler Factory simples do FB_APU02 (AD-1).
- Interface `Resolver` única, com tipo `Aprovador` explícito (`{Tipo, Nome, ColaboradorID, Motivo, RegraID, RegraVersao}`), dispatch compartilhado por tipo de solicitação, erro sentinela `ErrSemAlcadaCadastrada` (nunca aprovador fictício) (AD-2).
- Interface `ExportadorSAP` única, métodos por lote (não por solicitação), a própria implementação grava `exportacoes_sap` (AD-3).
- Writer path único para `aprovador_snapshot`/`versao`/`exportacoes_sap`, com enforcement técnico via `REVOKE`/`GRANT` no Postgres, não só convenção de code review (AD-4).
- Lock otimista com envelope de erro HTTP 409 padronizado: `{"erro": {"codigo","mensagem","versao_atual"}}` (AD-5).
- Autenticação replica exatamente o fluxo Keycloak do FB_APU02 (OIDC/PKCE, JWKS, `email_verified`) (AD-6).
- Autorização single-company (sem `company_id`); perfil deriva de role de client Keycloak via `resource_access.<client>.roles` (client/realm exato ainda em aberto) (AD-7).
- Carga Senior via upload manual de CSV; escopo delimitado — nunca escreve `CC_EXCECAO` (AD-8).
- Stack herdado do FB_APU02: Go 1.22 (débito técnico conhecido, fora da janela de suporte — avaliar upgrade em conjunto com o FB_APU02, não isoladamente), PostgreSQL 15, React 18.3.1, TypeScript 5.2.2, Vite 5.2, Tailwind+shadcn/ui, React Router 6.22.3, TanStack Query 5.90, React Hook Form+Zod, golang-jwt v5.3.1, bcrypt custo 14, `lib/pq` v1.11.2 — além dos middlewares de segurança HTTP (SecurityMiddleware, rate limiting por IP, CORS whitelist, emissão/rotação de JWT próprio) (AD-9).
- Deploy: Coolify = produção; staging separado (ex. Azure, disparado por tags `v*-rc*`); servidor AWS do cliente = espelho secundário encadeado após o Coolify; todo ambiente expõe `/api/health` com retry pós-deploy (AD-10).
- Módulo único de SLA/calendário (`EstaAtrasado`, `PrazoLimite`) consumido por Fila do Administrador e Painéis (AD-11).
- Geração do `.xlsm` via `archive/zip` + reescrita de partes XML, nunca tocar `vbaProject.bin`, escapar todo texto livre antes de substituição, remover `calcChain.xml`, manter aba de rascunho sempre visível (AD-12).
- Módulo único de anexos (`Salvar`/`Recuperar`); antivírus/DLP/quarentena fora de escopo por ora (AD-13).
- Paginação obrigatória com parâmetros de query padronizados `pagina`/`tamanho`; painéis respondem em formato agregado separado, não paginado (AD-14).
- **Dados sensíveis**: nenhum exemplo, fixture ou dado ilustrativo em código, specs ou stories pode usar nome real de pessoa, matrícula ou e-mail — sempre referenciar por cargo/papel (repositório é público).

### UX Design Requirements

Nenhum — não há UX design contract (`bmad-ux` não foi executado para este projeto). Decisões de UI ficam a critério de cada story, referenciando o protótipo de referência citado no PRD (`docs/referencia/02-REQUISITOS-NEGOCIO-ENVIO-TI.md`, seção "App de referência") como guia de padrão visual.

### FR Coverage Map

FR-1: Epic 1 - Login via SSO corporativo (Keycloak)
FR-2: Epic 1 - Bootstrap e proteção do administrador
FR-3: Epic 2 - Carga de colaborador × CC (Senior)
FR-4: Epic 2 - Exceção de CC
FR-5: Epic 3 - Transferência
FR-6: Epic 3 - Inclusão
FR-7: Epic 3 - Inclusão SFC (RPA)
FR-8: Epic 3 - Imobilizado
FR-9: Epic 3 - Obras
FR-10: Epic 3 - Resolução de aprovador (calculada)
FR-11: Epic 3 - Resolução de aprovador (nominal)
FR-12: Epic 4 - Assumir e processar
FR-13: Epic 4 - Pendência e reabertura
FR-14: Epic 4 - Finalização
FR-15: Epic 4 - Geração dos lotes Despesa e Obra
FR-16: Epic 2 - Histórico e restauração de cadastro
FR-17: Epic 5 - Painéis consolidados

## Epic List

### Epic 1: Autenticação e Acesso
Qualquer usuário entra no sistema via SSO corporativo; administrador é provisionado com segurança.
**FRs covered:** FR-1, FR-2

### Epic 2: Cadastros e Identidade
Administrador gerencia todos os dados mestres (divisões, CCs, contas, alçadas, papel×pessoa, feriados) e a identidade de CC de cada colaborador, incluindo exceções multi-CC — tudo com histórico/restauração.
**FRs covered:** FR-3, FR-4, FR-16

### Epic 3: Solicitações Orçamentárias e Motor de Aprovação
Solicitante abre qualquer um dos 5 tipos de solicitação e tem o aprovador resolvido automaticamente (calculado ou nominal), com trilha de auditoria — nunca um aprovador inventado.
**FRs covered:** FR-5, FR-6, FR-7, FR-8, FR-9, FR-10, FR-11

### Epic 4: Processamento e Exportação SAP
Administrador processa a fila (assumir, pendência, finalizar) e gera os lotes de exportação para o robô SAP, com elegibilidade e idempotência corretas.
**FRs covered:** FR-12, FR-13, FR-14, FR-15

### Epic 5: Painéis e Indicadores
Usuário visualiza indicadores agregados de solicitações.
**FRs covered:** FR-17

## Epic 1: Autenticação e Acesso

Qualquer usuário entra no sistema via SSO corporativo; administrador é provisionado com segurança.

### Story 1.1: Scaffold inicial do projeto

Como desenvolvedor,
Eu quero um projeto rodável localmente (backend Go + frontend React + Postgres via Docker), replicando a estrutura de pastas do FB_APU02,
Para que eu possa começar a implementar as demais stories sobre uma base consistente.

**Acceptance Criteria:**

**Given** o repositório FB_APU05 vazio de código
**When** o desenvolvedor segue o guia de setup
**Then** `docker-compose up` sobe backend + frontend + Postgres localmente
**And** a estrutura de pastas segue o padrão documentado no Architecture Spine (`backend/{handlers,services,middleware,migrations,internal}`, `frontend/src/{pages,components/ui,contexts,hooks,lib}`)
**And** o runner de migrations (`schema_migrations`) existe e roda vazio sem erro

### Story 1.2: Login via SSO corporativo

Como usuário da Ferreira Costa,
Eu quero entrar no sistema com minha conta corporativa (SSO),
Para não precisar de um cadastro separado.

**Acceptance Criteria:**

**Given** um usuário com conta válida no Keycloak da Ferreira Costa
**When** ele completa o fluxo OIDC/PKCE de login
**Then** o sistema valida o token via JWKS e emite seu próprio JWT (access 30min + refresh 7 dias em cookie HttpOnly)
**And** se `email_verified=false` no token do Keycloak, o login é recusado
**And** o token do Keycloak nunca é exposto a nenhuma camada além do endpoint de troca (AD-6)

### Story 1.3: Bootstrap e proteção do administrador

Como responsável pela implantação,
Eu quero provisionar o primeiro administrador de forma controlada e impedir que o sistema fique sem nenhum admin,
Para manter a operação segura desde o primeiro dia.

**Acceptance Criteria:**

**Given** um banco recém-criado, sem nenhum administrador
**When** o procedimento controlado de bootstrap é executado (fora da API)
**Then** existe exatamente um administrador ativo
**And** esse administrador pode conceder perfil administrador a outro usuário autenticado, gerando evento auditável
**And** uma tentativa de rebaixar ou desativar o único administrador ativo é recusada pelo sistema (FR-2)

## Epic 2: Cadastros e Identidade

Administrador gerencia todos os dados mestres e a identidade de CC de cada colaborador, incluindo exceções multi-CC — tudo com histórico/restauração.

### Story 2.1: Carga inicial e cadastros administráveis

Como administrador,
Eu quero que os cadastros mestres (divisões, CCs, contas, classes de imobilizado, alçadas, papel×pessoa, feriados de SLA) sejam carregados a partir dos arquivos de origem e fiquem editáveis com histórico,
Para que eu possa manter os dados corretos ao longo do tempo sem perder rastro de mudanças.

**Acceptance Criteria:**

**Given** os CSVs de carga inicial (UTF-8 BOM, separador `;`) no formato já especificado
**When** o processo de carga é executado contra um banco vazio
**Then** todos os cadastros mestres são populados e a contagem bate com a tabela de conferência de origem
**And** qualquer alteração posterior em um cadastro gera um registro de histórico
**And** o administrador pode restaurar uma versão anterior de qualquer cadastro
**And** o comportamento de histórico/restauração é idêntico entre todos os tipos de cadastro (FR-16)

### Story 2.2: Carga de colaborador × centro de custo

Como administrador,
Eu quero subir uma planilha de colaboradores e seus centros de custo (fonte Senior),
Para que cada usuário tenha seu CC próprio identificado no sistema.

**Acceptance Criteria:**

**Given** um arquivo CSV de colaboradores e colaboradores-por-CC no formato acordado
**When** o administrador faz upload manual do arquivo
**Then** cada colaborador fica associado ao seu CC próprio
**And** registros-placeholder de cargo sem titular (sem pessoa física real) não são carregados como colaborador
**And** um colaborador sem CC próprio cadastrado não consegue abrir solicitação até a carga ser corrigida

### Story 2.3: Exceção de centro de custo

Como administrador,
Eu quero autorizar um colaborador específico a abrir solicitações para centros de custo além do seu próprio,
Para que pontos focais administrativos (que atendem múltiplos setores) consigam trabalhar sem contornar o sistema.

**Acceptance Criteria:**

**Given** um colaborador já carregado com seu CC próprio
**When** o administrador cadastra uma exceção de CC para esse colaborador
**Then** esse CC adicional passa a aparecer como opção autorizada para aquele colaborador
**And** a exceção não concede nenhum direito de aprovação sobre solicitações nesse CC
**And** a tela de exceções segue o mesmo padrão de histórico/restauração da Story 2.1

## Epic 3: Solicitações Orçamentárias e Motor de Aprovação

Solicitante abre qualquer um dos 5 tipos de solicitação e tem o aprovador resolvido automaticamente (calculado ou nominal), com trilha de auditoria — nunca um aprovador inventado.

### Story 3.1: Abrir Transferência com aprovação calculada

Como solicitante,
Eu quero abrir uma transferência de saldo entre contas do meu CC e ter o aprovador calculado automaticamente,
Para não depender de saber de cor a matriz de alçadas.

**Acceptance Criteria:**

**Given** um solicitante autenticado com CC válido
**When** ele preenche os dois lados do lançamento (conta origem/destino, valor, mês) e envia
**Then** o sistema recusa o envio se os lados não baterem (tolerância R$ 0,01) ou se houver mais de um exercício orçamentário
**And** o servidor calcula o aprovador (regras especiais curadas → matriz de alçadas → janela do gerente → fallback GR) e grava snapshot de auditoria (nome/cargo, motivo, regra/versão)
**And** quando não há alçada cadastrada acima da janela do gerente, a solicitação é bloqueada com "sem alçada cadastrada" citando CC e filial — nunca um aprovador fictício
**And** quando o papel resolvido é um cargo colegiado sem titular (ex. Superintendência), o sistema aceita o cargo como aprovador válido

### Story 3.2: Abrir Inclusão SFC com aprovação calculada

Como solicitante,
Eu quero abrir uma inclusão SFC usando o plano de contas correto,
Para que o lançamento nunca seja confundido com o plano BIFC.

**Acceptance Criteria:**

**Given** um solicitante autenticado
**When** ele abre uma Inclusão SFC
**Then** o combo de conta lista exclusivamente contas do plano `CONTAS_SFC`, mesmo quando o código coincide com uma conta BIFC
**And** o valor deve ser positivo — o sistema recusa valor zero ou negativo
**And** o aprovador é resolvido pelo mesmo motor calculado da Story 3.1 (reaproveitado, não reimplementado)

### Story 3.3: Abrir Inclusão com autorizador nominal

Como solicitante,
Eu quero abrir uma inclusão de orçamento e escolher um autorizador validado,
Para que a aprovação não dependa de cálculo de alçada.

**Acceptance Criteria:**

**Given** um solicitante autenticado
**When** ele preenche a inclusão (CC, conta, mês, valor positivo) e escolhe um autorizador
**Then** o combo de autorizador só oferece pessoas validadas para o CC/faixa de valor daquela solicitação
**And** a decisão do autorizador é gravada como snapshot de auditoria no mesmo formato do motor calculado
**And** autorizador fora da lista para aquele CC/faixa não aparece como opção

### Story 3.4: Abrir Imobilizado com anexo de cotação

Como solicitante,
Eu quero abrir uma aquisição de ativo fixo por classe de imobilizado, anexando a cotação,
Para registrar o pedido sem precisar de uma conta contábil ou fornecedor formal.

**Acceptance Criteria:**

**Given** um solicitante autenticado
**When** ele abre um Imobilizado, escolhe uma classe de imobilizado (das 8 existentes) e tenta enviar sem anexo
**Then** o sistema recusa o envio por falta de anexo de cotação
**And** o formulário não expõe campos de fornecedor nem de mês/competência
**And** o autorizador escolhido é validado contra CC/faixa, reaproveitando o motor da Story 3.3

### Story 3.5: Abrir Obras multi-linha

Como solicitante,
Eu quero abrir uma solicitação de obras com várias linhas (local + subgrupo + ordem),
Para cobrir inclusão, FL ou transferência de saldo numa única solicitação.

**Acceptance Criteria:**

**Given** um solicitante autenticado
**When** ele monta uma solicitação de Obras com uma ou mais linhas, cada uma com local de obra + subgrupo de despesa + ordem de investimento
**Then** a solicitação não expõe campos de filial, CC, conta ou mês
**And** uma classificação "transferência de saldo" exige dois blocos (retirada e inclusão) que devem bater em valor
**And** uma linha com ordem `CRIAR` não gera a ordem no SAP agora — só na finalização (Epic 4), na mesma transação
**And** o autorizador é validado contra a lista fixa de Obras, com teto de alçada individual por pessoa

## Epic 4: Processamento e Exportação SAP

Administrador processa a fila (assumir, pendência, finalizar) e gera os lotes de exportação para o robô SAP, com elegibilidade e idempotência corretas.

### Story 4.1: Assumir solicitação da fila

Como administrador,
Eu quero assumir uma solicitação aprovada da fila vendo seu prazo de SLA,
Para processar com prioridade certa e sem conflito com outro administrador.

**Acceptance Criteria:**

**Given** uma solicitação aprovada aguardando processamento
**When** um administrador assume essa solicitação
**Then** ela fica travada para ele via lock otimista por campo `versão`
**And** se outro administrador tentar assumir a mesma solicitação antes, recebe HTTP 409 (`{"erro":{"codigo":"conflito_versao",...}}`), sem retry automático
**And** a fila mostra o prazo de SLA (48h úteis, calendário de feriados, fuso America/Recife) e sinaliza solicitações já atrasadas

### Story 4.2: Pendência e reabertura

Como administrador,
Eu quero devolver uma solicitação ao solicitante com um comentário quando falta algo, e ter o chamado reaberto automaticamente se ele comentar depois de finalizado,
Para manter o histórico de idas e vindas completo.

**Acceptance Criteria:**

**Given** uma solicitação assumida por um administrador
**When** ele marca pendência com um comentário
**Then** a solicitação volta ao solicitante, com o comentário visível no histórico
**And** se o solicitante comentar numa solicitação já finalizada, ela reabre automaticamente

### Story 4.3: Gerar lote Despesa

Como administrador,
Eu quero gerar o lote de exportação Despesa (transferência/inclusão/SFC/imobilizado),
Para alimentar o robô SAP com os lançamentos aprovados.

**Acceptance Criteria:**

**Given** solicitações atribuídas ao administrador ativo, em `em_atendimento`, com todos os campos obrigatórios preenchidos
**When** ele gera o lote Despesa
**Then** o servidor recalcula a elegibilidade e cruza os IDs com o responsável da sessão — nunca confia em `admin_id` enviado pelo cliente
**And** inclusão só entra com valores positivos; transferência só entra balanceada; o lote é de um único exercício orçamentário
**And** a geração é idempotente e grava snapshot — gerar de novo não cria um segundo registro
**And** o sistema exibe aviso de "verba duplicada" quando há risco de dupla contagem, e grava esse aviso no histórico se o administrador optar por ignorá-lo

### Story 4.4: Gerar lote Obra

Como administrador,
Eu quero gerar o lote de exportação Obra,
Para alimentar o robô SAP com as linhas de investimento aprovadas.

**Acceptance Criteria:**

**Given** solicitações de Obras com linhas cuja ordem já existe no SAP e valor positivo
**When** ele gera o lote Obra
**Then** linhas inválidas (ordem `CRIAR` ou valor ≤ 0) são descartadas individualmente, sem barrar o lote inteiro
**And** uma solicitação sem nenhuma linha válida aparece em "Bloqueados" com o motivo

### Story 4.5: Finalizar solicitação

Como administrador,
Eu quero finalizar uma solicitação depois de gerar sua exportação, como uma ação separada,
Para que gerar o arquivo nunca mude o status sozinho.

**Acceptance Criteria:**

**Given** uma solicitação já exportada (lote Despesa ou Obra)
**When** o administrador finaliza com sucesso ou com erro
**Then** o status muda via lock otimista (mesmo mecanismo da Story 4.1) — conflito de versão recusa a operação inteira
**And** gerar o arquivo de exportação, por si só, nunca muda o status da solicitação
**And** uma linha de Obras com ordem `CRIAR` tem sua ordem real criada no SAP nesta mesma transação de finalização

## Epic 5: Painéis e Indicadores

Usuário visualiza indicadores agregados de solicitações.

### Story 5.1: Visualizar painel consolidado de solicitações

Como usuário,
Eu quero ver um painel com indicadores agregados de solicitações (volume por tipo, por status, por CC),
Para acompanhar o andamento sem precisar abrir cada solicitação individualmente.

**Acceptance Criteria:**

**Given** solicitações processadas com snapshots de painel já calculados
**When** o usuário abre o painel
**Then** os indicadores são lidos de `painel_snapshots` — nunca por consulta direta às tabelas transacionais em tempo real
**And** o escopo exato de quais indicadores aparecem é o mínimo definido aqui (volume por tipo/status/CC); indicadores adicionais ficam para iteração futura, a definir com o negócio (PRD Questão Aberta 14)
