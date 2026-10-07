# Epic 2 Context: Cadastros e Identidade

<!-- Generated from planning artifacts. Regenerate with compile-epic-context if planning docs change. -->

## Goal

Este épico dá ao administrador controle completo sobre os dados mestres do sistema (divisões, centros de custo, contas, classes de imobilizado, alçadas, papel×pessoa, feriados de SLA) e sobre a identidade de centro de custo (CC) de cada colaborador — incluindo colaboradores que precisam atuar em CCs além do próprio (exceção de CC). Importa porque todos os outros épicos dependem desses cadastros: sem CC próprio carregado, um colaborador não consegue abrir solicitação (Epic 3); sem alçadas/papel×pessoa corretos, o motor de aprovação não resolve aprovador; sem feriados cadastrados, o SLA da fila (Epic 4) não calcula corretamente. Todo cadastro administrável segue o mesmo regime uniforme de histórico e restauração de versão, sem exceção por tipo.

## Stories

- Story 2.1: Carga inicial e cadastros administráveis
- Story 2.2: Carga de colaborador × centro de custo
- Story 2.3: Exceção de centro de custo

## Requirements & Constraints

- Cadastros mestres (divisões, CCs, contas, classes de imobilizado, alçadas, papel×pessoa, feriados de SLA) nascem de carga inicial via CSV (UTF-8 BOM, separador `;`) e ficam editáveis depois pelo administrador.
- Toda alteração em qualquer cadastro administrável gera histórico (append-only), e o administrador pode restaurar uma versão anterior. Esse comportamento é idêntico entre todos os tipos de cadastro — nenhum tipo tem regra de permissão ou profundidade de histórico diferenciada.
- Colaborador = usuário autenticado via SSO; cada um tem exatamente um CC próprio, vindo da carga da base Senior (planilha de colaboradores + colaboradores-por-CC). Registros-placeholder que representam um cargo sem titular (sem pessoa física real) não devem ser carregados como colaborador.
- Colaborador sem CC próprio cadastrado não consegue abrir solicitação até a carga ser corrigida (não é erro silencioso — deve ficar claro para o usuário).
- Exceção de CC: o administrador cadastra, por colaborador, uma lista de CCs adicionais autorizados. Isso dá ao colaborador permissão para abrir e acompanhar solicitações nesses CCs, mas nunca permissão de aprovação — a aprovação continua calculada a partir do CC da solicitação, independente de quem a submeteu.
- Qualquer listagem que possa ultrapassar 1.000 linhas deve paginar obrigatoriamente (ex.: alçadas, hoje com milhares de faixas ativas). Contrato de resposta: `{items, pagina, tamanho}` — sem `total` nem cursor; parâmetros de query são `pagina`/`tamanho` (1–100).
- Nenhum exemplo, fixture ou dado ilustrativo (código, specs, stories) pode usar nome real de pessoa, matrícula ou e-mail — referenciar sempre por cargo/papel (repositório é público).

## Technical Decisions

- Regime arquitetural simples (Handler Factory, mesmo padrão já validado no sistema irmão FB_APU02): cadastros administráveis e identidade de CC não fazem parte do núcleo de domínio isolado (que é reservado para aprovação e exportação SAP) — handlers/services comuns bastam aqui.
- Autorização é single-company, sem `company_id`. Perfil (`solicitante`/`administrador`) é uma coluna própria da aplicação, não deriva de claim do Keycloak.
- Carga da base Senior é por upload manual de CSV, acionado por um administrador — nunca job agendado nem leitura direta de outro banco. Essa carga é estritamente limitada a escrever o CC próprio do colaborador; ela nunca escreve `CC_EXCECAO` — exceção de CC é um cadastro administrável separado, governado pelo mesmo regime de histórico/restauração.
- Migrations são SQL puro, numeradas (`NNN_nome.sql`), auto-executadas no boot via tabela `schema_migrations`.
- Toda tabela tem `created_at`/`updated_at`; PK é UUID (`gen_random_uuid()`).

## Cross-Story Dependencies

- Story 2.2 (colaborador × CC) depende dos cadastros mestres de CC já existirem (Story 2.1) para associar colaboradores a um CC válido.
- Story 2.3 (exceção de CC) depende de colaboradores já carregados (Story 2.2) e reutiliza exatamente o padrão de histórico/restauração estabelecido na Story 2.1 — não deve divergir nem reimplementar esse comportamento.
- Este épico inteiro é pré-requisito do Epic 3 (Solicitações): um colaborador só consegue abrir solicitação se tiver CC próprio (ou exceção) carregado; o motor de aprovação do Epic 3 depende dos cadastros de alçadas e papel×pessoa mantidos aqui.
- O calendário de feriados mantido aqui (cadastro administrável, FR-16) alimenta o módulo único de SLA consumido pela Fila do Administrador e pelos Painéis (Epic 4 e Epic 5).
