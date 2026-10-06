---
title: FB_APU05 — Módulo Controladoria (Gestão de Solicitações Orçamentárias)
status: final
created: 2026-10-06
updated: 2026-10-06
---

# PRD: FB_APU05 — Módulo Controladoria

## 0. Propósito do Documento

Este PRD é para o time que vai desenhar a arquitetura e quebrar o trabalho em épicos/stories do FB_APU05. Ele parte do [Product Brief](../../briefs/brief-FB_APU05-2026-10-06/brief.md) (+ [addendum](../../briefs/brief-FB_APU05-2026-10-06/addendum.md)) e da documentação de negócio em `docs/referencia/`. Estrutura: vocabulário fixo no Glossário (seção 3), features agrupadas com FRs numerados globalmente (seção 4), suposições marcadas inline com `[ASSUMPTION]` e indexadas no final (seção 9). Detalhe técnico de implementação (mecanismo do gerador de exportação, decisões de reuso de código) vive no `addendum.md` deste PRD, não aqui.

## 1. Visão

FB_APU05 é o hub único de solicitações orçamentárias/de investimento da Ferreira Costa, substituindo o processo hoje operado em planilha ("Novo JIRA — Gestão de Orçamento") e robô VBA. Cobre cinco tipos de solicitação — transferência, inclusão, inclusão SFC, imobilizado e obras — com dois motores de aprovação (resolver calculado por alçada SAP, e autorizador nominal por formulário), trilha de auditoria por snapshot, e exportação governada para o SAP.

A v1 entrega os cinco tipos completos de uma vez para os ~500 usuários internos da empresa, mantendo a exportação atual (robô VBA) como ponte até que outro time entregue uma API SAP própria — momento em que a camada de integração é trocada sem reescrever o núcleo do domínio.

## 2. Usuário-Alvo

### 2.1 Jobs To Be Done

- Como **solicitante**, preciso abrir um pedido de movimentação orçamentária sem depender de planilha compartilhada ou de saber de cor quem aprova.
- Como **solicitante com múltiplos centros de custo** (ex.: ponto focal administrativo de uma área), preciso abrir e acompanhar pedidos para setores que não são o meu, sem poder aprová-los.
- Como **aprovador** (calculado ou nominal), preciso decidir um pedido sabendo exatamente por que ele caiu pra mim, sem precisar entender a matriz de alçada de cabeça.
- Como **administrador da Controladoria**, preciso processar uma fila com trava de concorrência, sinalizar pendência, e gerar a exportação SAP sem duplicar verba nem perder rastro de quem fez o quê.

### 2.2 Jornadas-Chave do Usuário

- **UJ-1. Renata abre uma solicitação para um CC que não é o dela.**
  - **Persona + contexto:** Renata é ponto focal administrativo de TI, atende vários setores com CCs diferentes do seu (Governança de TI).
  - **Estado de entrada:** autenticada via SSO corporativo; vai abrir uma solicitação para o setor de Infraestrutura.
  - **Caminho:** novo pedido → formulário oferece seu CC próprio + CCs cadastrados como exceção (Infraestrutura, Segurança, Suporte) → seleciona Infraestrutura → preenche tipo/valor/descrição/anexos → envia.
  - **Clímax:** sistema calcula o aprovador com base no CC **escolhido** (Infraestrutura), não no CC dela — o gestor responsável por aquele CC recebe para aprovar.
  - **Resolução:** Renata acompanha status, histórico e responde pendências como se fosse dela — mas não vê botão de aprovação nessa solicitação.
  - **Edge case:** se ela tentar selecionar um CC fora da sua lista de exceções cadastradas, o formulário não oferece a opção.

- **UJ-2. Bruno abre uma Transferência (resolver calculado).**
  - **Persona + contexto:** Bruno, colaborador do Comercial, precisa mover saldo entre contas do próprio CC.
  - **Estado de entrada:** autenticado, CC = o seu próprio (sem exceção).
  - **Caminho:** abre tipo "Transferência" → preenche dois lados do lançamento (conta origem/destino, valor, mês), que precisam bater → envia.
  - **Clímax:** servidor calcula o aprovador (regras especiais curadas → matriz de alçadas → janela do gerente → escalonamento) e grava snapshot de auditoria.
  - **Resolução:** acompanha status; se faltar alçada cadastrada para o par filial×CC, o pedido é **bloqueado com "sem alçada cadastrada"** (nunca inventa aprovador).
  - **Edge case:** aprovador pede ajuste → pedido volta como pendência para o solicitante.

- **UJ-3. Patrícia, administradora da Controladoria, processa a fila.**
  - **Persona + contexto:** Patrícia é administradora, dona do processamento operacional.
  - **Estado de entrada:** autenticado como administrador, olhando a fila de solicitações aprovadas aguardando processamento.
  - **Caminho:** assume um pedido (lock otimista evita dois admins pegando o mesmo) → confere dados → marca pendência (volta ao solicitante com comentário) ou segue.
  - **Clímax:** finaliza o pedido, que gera o lote (Despesa ou Obra) para o robô VBA — gerar o arquivo **não** finaliza o chamado, finalizar é ação separada.
  - **Resolução:** chamado finalizado; se o solicitante comentar depois, reabre automaticamente.
  - **Edge case:** se outro admin já assumiu o pedido entre a leitura e a ação, o sistema recusa com conflito de versão (409), sem retry automático.

- **UJ-4. Diego, autorizador nominal, decide um Imobilizado.**
  - **Persona + contexto:** Diego está numa lista fixa de autorizadores, validado por CC/faixa de valor (sem cálculo de alçada).
  - **Estado de entrada:** recebe notificação de um pedido de aquisição de ativo fixo pendente de aprovação.
  - **Caminho:** abre o pedido → confere classe de imobilizado, cotação em anexo (sem fornecedor/competência no formulário) → aprova ou rejeita.
  - **Clímax:** decisão gravada como snapshot de auditoria, igual ao resolver calculado.
  - **Resolução:** pedido segue para a fila do administrador.

## 3. Glossário

- **Solicitação** — registro raiz de um pedido orçamentário/de investimento. Tem um `tipo` (ver abaixo), um CC, um valor, um histórico append-only e uma `versão` para lock otimista.
- **Tipo de solicitação** — um de: `transferência`, `inclusão`, `inclusão SFC`, `imobilizado`, `obras`.
- **CC (Centro de Custo)** — unidade orçamentária. Todo colaborador tem um CC próprio (carregado da Senior); pode ter CCs adicionais via **Exceção de CC**.
- **Exceção de CC** — autorização cadastrada que permite a um colaborador abrir e acompanhar solicitações para um CC que não é o seu. Não concede direito de aprovação.
- **Resolver calculado** — motor de aprovação que calcula o aprovador a partir de regras especiais curadas + matriz de alçadas SAP + janela do gerente + escalonamento. Usado por `transferência` e `inclusão SFC`.
- **Autorizador nominal** — motor de aprovação por pessoa de lista fixa, validada por CC/faixa de valor, sem cálculo de alçada. Usado por `inclusão`, `imobilizado` e `obras`.
- **Alçada** — faixa de valor + papel aprovador, por filial/CC, espelhando a matriz do SAP.
- **Snapshot de auditoria** — registro imutável de quem/por que uma solicitação foi aprovada, gravado no momento da decisão (nunca referência viva a um cadastro que pode mudar depois).
- **Lock otimista** — controle de concorrência por campo `versão`; conflito de edição simultânea retorna HTTP 409, sem retry automático.
- **Fila do administrador** — lista de solicitações aprovadas aguardando processamento pela Controladoria (assumir → atender/pendência → finalizar → reabrir).
- **Lote-Despesa** / **Lote-Obra** — os dois pacotes de exportação para o robô SAP, com templates e regras de elegibilidade distintos, nunca misturados.
- **Finalização** — ação transacional que encerra uma solicitação após a exportação SAP; distinta de "gerar o arquivo de exportação".
- **Cargo sem titular** — papel aprovador que resolve para o próprio nome do cargo (ex.: um cargo colegiado), de propósito — não é lacuna de cadastro.
- **SLA** — prazo de 48 horas úteis para processamento de uma solicitação, contado pelo calendário de feriados PE/federais, fuso America/Recife.
- **`rel_cc_conta`** — relação entre centro de custo e conta contábil. Cadastro incompleto por desenho (27 de ~21 mil combinações possíveis); o filtro de conta por CC fica desativado em formulário até o cadastro ser completado pelo negócio.
- **Colaborador** / **Usuário** — mesma entidade: qualquer pessoa autenticada via SSO corporativo. Os dois termos são usados de forma intercambiável neste documento.
- **Painel** — visão consolidada e pré-calculada (`painel_snapshots`) de indicadores agregados de solicitações, exposta via rotas `/paineis/{painel}`.

## 4. Features

### 4.1 Autenticação e Identidade

**Descrição:** Login via SSO corporativo (Keycloak), reaproveitando o padrão já validado em produção no FB_APU02. Perfil nasce da sessão corporativa, nunca do payload. Dois perfis de login: `solicitante` e `administrador` — aprovador é papel calculado, não perfil de login.

#### FR-1: Login via SSO corporativo
Qualquer usuário pode autenticar via OIDC/PKCE contra o Keycloak real da Ferreira Costa (`https://iam.fcxlabs.com/realms/ferreiracosta`), sem cadastro de usuário separado.

**Consequences (testable):**
- Sistema valida o token do Keycloak via JWKS e checa `email_verified` antes de emitir token próprio (replica achado de segurança do FB_APU02: sem essa checagem, troca de e-mail no Keycloak permite logar como outra pessoa).
- Token do Keycloak nunca é exposto a nenhuma camada além do endpoint de troca; sistema emite seus próprios tokens (JWT + refresh).

#### FR-2: Bootstrap e proteção do administrador
O primeiro administrador é provisionado diretamente no banco por procedimento controlado, fora da API.

**Consequences (testable):**
- Concessões posteriores de perfil administrador exigem um administrador autenticado e geram evento auditável.
- Sistema rejeita qualquer tentativa de rebaixar ou desativar o último administrador ativo.

### 4.2 Identidade de Solicitante e Centro de Custo

**Descrição:** Cada colaborador tem um CC próprio, carregado da base de RH (Senior). Exceções permitem que um colaborador específico abra/acompanhe solicitações para CCs adicionais. Realiza UJ-1.

#### FR-3: Carga de colaborador × CC
Sistema carrega periodicamente uma planilha de colaboradores e de colaboradores-por-CC (fonte: Senior, via extração manual nesta fase — ver addendum). `[ASSUMPTION: a cadência de recarga — diária, semanal — ainda não foi definida; assumo recarga manual sob demanda até confirmação.]`

**Consequences (testable):**
- Usuário sem CC próprio cadastrado não consegue abrir solicitação até que a carga seja corrigida.
- Carga de pessoas segue a mesma régua da carga inicial de cadastros: registros-placeholder que representam um cargo sem titular (não uma pessoa real) não são carregados como colaborador.

#### FR-4: Exceção de CC
Um administrador pode cadastrar, para um colaborador específico, uma lista de CCs adicionais autorizados (além do próprio).

**Consequences (testable):**
- Formulário de nova solicitação oferece apenas: CC próprio do colaborador + CCs em suas exceções ativas.
- Colaborador com exceção pode abrir e acompanhar (ver status, responder pendência) solicitações nos CCs de exceção.
- Colaborador com exceção **não** ganha direito de aprovação nesses CCs — aprovação continua calculada normalmente a partir do CC da solicitação, independente de quem a submeteu.
- Tela de cadastro de exceções segue o mesmo padrão de histórico/restauração de versão dos demais cadastros administráveis (FR-16).

### 4.3 Solicitações — os 5 Tipos

**Descrição:** Os cinco tipos de solicitação, cada um com formulário e regras próprias. Realiza UJ-2, UJ-4.

**Feature-specific NFRs:**
- O relacionamento conta×CC (`rel_cc_conta`) está incompleto por desenho (27 de ~21 mil combinações cadastradas); o filtro de contas disponíveis por CC permanece **desativado** nos formulários até o negócio completar esse cadastro — não é defeito, é decisão de escopo herdada da fonte original.

#### FR-5: Transferência
Solicitante pode abrir uma transferência de saldo entre contas do CC autorizado, com dois lados do lançamento que devem bater (tolerância R$ 0,01). Realiza UJ-2.

**Consequences (testable):**
- Campos obrigatórios em cada linha: divisão, CC, conta, mês, valor.
- Lado origem e lado destino devem ser ambos positivos e somar o mesmo total (tolerância R$ 0,01) — fora da tolerância, sistema recusa o envio com erro apontando o lado desbalanceado.
- Um único exercício orçamentário por solicitação.
- Conta disponível para seleção respeita o filtro `rel_cc_conta` quando o cadastro existir (ver §4.3 NFR); enquanto incompleto, filtro fica desativado.

#### FR-6: Inclusão
Solicitante pode abrir uma inclusão de orçamento, com autorizador nominal (desde 2026-08-04 — antes usava resolver calculado).

**Consequences (testable):**
- Campos obrigatórios: divisão, CC, conta, mês, valor.
- Valor deve ser positivo — sistema recusa valor zero ou negativo.
- Autorizador selecionado é validado contra a lista fixa de autorizadores do CC/faixa de valor daquela solicitação (FR-11); autorizador fora da lista não aparece como opção.

#### FR-7: Inclusão SFC (RPA)
Idêntica à Inclusão, exceto pelo plano de contas (`CONTAS_SFC` — 143 contas que colidem em código com o plano BIFC; sistema nunca deve misturar os dois planos). Usa resolver calculado.

**Consequences (testable):**
- Combo de conta desta solicitação lista exclusivamente contas do plano `CONTAS_SFC`, nunca do plano BIFC, mesmo quando os códigos coincidem.
- Valor deve ser positivo.
- Aprovador resolvido pelo motor calculado (FR-10), não pelo nominal.

#### FR-8: Imobilizado
Solicitante pode abrir aquisição de ativo fixo por classe de imobilizado (não conta contábil), sem campo de fornecedor ou competência/mês — cotação vai em anexo. Autorizador nominal validado por CC/faixa. Realiza UJ-4.

**Consequences (testable):**
- Campo obrigatório é a classe de imobilizado (lista fixa de 8 classes), não uma conta contábil.
- Formulário não expõe campos de fornecedor nem de mês/competência.
- Solicitação sem ao menos um anexo (cotação) não pode ser enviada.
- Autorizador selecionado é validado contra CC/faixa de valor (FR-11).

**Out of Scope:** formulário não inclui fluxo de cotação/fornecedor — isso permanece em anexo não estruturado.

#### FR-9: Obras
Solicitante pode abrir solicitação multi-linha por local de obra + subgrupo de despesa + ordem de investimento (sem filial/CC/conta/mês), com 4 classificações (inclusão, FL, transferência de saldo — blocos balanceados, retirada de saldo). Ordem aceita o literal `CRIAR` quando ainda não existe no SAP, nascendo na finalização na mesma transação. Autorizador de lista fixa com teto de alçada por pessoa.

**Consequences (testable):**
- Solicitação não tem campos de filial, CC, conta ou mês — apenas local de obra + subgrupo de despesa + ordem de investimento por linha.
- Classificação "transferência de saldo" exige dois blocos (retirada e inclusão) que devem bater em valor — fora disso, sistema recusa o envio.
- Linha com ordem `CRIAR` só gera a ordem real no SAP no momento da finalização (FR-14), na mesma transação — nunca antes.
- Autorizador validado contra lista fixa (11-12 pessoas) com teto de alçada individual; valor acima do teto da pessoa escolhida não é aceito.

**Feature-specific NFRs:**
- `CD Jaboatão` não pode receber ordem nova enquanto sua estrutura de obra não for confirmada pelo negócio (ver Questão Aberta 7).

### 4.4 Motor de Aprovação

**Descrição:** Os dois motores (resolver calculado, autorizador nominal) e as regras que os cercam. Núcleo de maior risco de esforço do produto (ver §5.3 Riscos e Mitigações).

#### FR-10: Resolução de aprovador (calculada)
Para `transferência` e `inclusão SFC`, sistema calcula o aprovador no servidor: camada de importação (filial ≥ 9000) → regras especiais curadas (por precedência) → matriz de alçadas SAP (filial+CC+valor) → janela do gerente do CC (até R$ 20.000, com caso colegiado para Logística) → fallback para o GR mais próximo acima.

**Consequences (testable):**
- Aprovador resolvido é gravado como **snapshot** (nome ou cargo, motivo legível, regra/versão usada) — nunca como referência viva a um cadastro.
- Quando um papel resolve para "cargo sem titular" por desenho (ex.: papel colegiado), sistema aceita o cargo como aprovador válido — não bloqueia.
- Quando não há alçada cadastrada para o par filial×CC acima da janela do gerente, sistema **bloqueia com "sem alçada cadastrada"**, citando CC e filial — nunca inventa aprovador ou escalonamento fictício.

**Out of Scope:** escalonamento para diretoria/superintendência acima de R$ 20.000 fica fora do fluxo deste sistema nesta fase — o sistema bloqueia e sinaliza, não implementa esse fluxo de decisão.

#### FR-11: Resolução de aprovador (nominal)
Para `inclusão`, `imobilizado` e `obras`, sistema valida a pessoa escolhida pelo solicitante contra uma lista fixa de autorizadores por CC/faixa de valor — sem cálculo de alçada.

**Consequences (testable):**
- Combobox de autorizador só oferece pessoas autorizadas para o CC/faixa daquela solicitação.
- Decisão gravada como snapshot, no mesmo formato do resolver calculado.

### 4.5 Fila do Administrador

**Descrição:** Processamento operacional pela Controladoria. Realiza UJ-3.

#### FR-12: Assumir e processar
Administrador pode assumir uma solicitação aprovada da fila, que fica travada para ele via lock otimista por campo `versão`.

**Consequences (testable):**
- Dois administradores tentando assumir o mesmo pedido: o segundo recebe conflito de versão (409), sem retry automático.

#### FR-13: Pendência e reabertura
Administrador pode marcar pendência (devolve ao solicitante com comentário); solicitação finalizada que recebe novo comentário reabre automaticamente.

#### FR-14: Finalização
Administrador pode finalizar uma solicitação após gerar sua exportação SAP — ação transacional separada da geração do arquivo, via lock otimista. Realiza UJ-3.

**Consequences (testable):**
- Gerar o arquivo de exportação não muda o status da solicitação.
- Campo "Documento/referência SAP" não existe no modelo (removido na especificação de origem) — não deve ser reintroduzido.

### 4.6 Exportação SAP

**Descrição:** Dois lotes de exportação, nunca misturados, alimentando o robô VBA (v1) e futuramente uma API SAP (fora do escopo deste PRD — ver Non-Goals).

#### FR-15: Geração dos lotes Despesa e Obra
Administrador pode gerar o lote **Despesa** (transferência/inclusão/SFC/imobilizado) ou o lote **Obra** (obras), cada um com template e regras de elegibilidade próprias.

**Consequences (testable):**
- Lote-Despesa só inclui chamados: atribuídos ao administrador ativo, em `em_atendimento`, com todos os campos obrigatórios preenchidos, inclusão só com valores positivos, transferência balanceada, um único exercício orçamentário por lote.
- Lote-Obra inclui apenas linhas cuja ordem já existe no SAP e valor positivo; linha inválida é descartada individualmente (não barra o lote inteiro); chamado sem nenhuma linha válida vai para "Bloqueados" com motivo.
- Servidor recalcula a elegibilidade e cruza os IDs com o responsável da sessão — nunca confia em um `admin_id` vindo do cliente.
- Geração do Lote-Despesa é idempotente e grava snapshot; alterações posteriores na solicitação não mudam um arquivo já gerado.
- Sistema exibe aviso de "verba duplicada" quando há risco de incluir a mesma verba duas vezes, e grava esse aviso no histórico da solicitação quando o administrador opta por ignorá-lo. Exportação em status `GERADO` (ainda não finalizada) conta como risco de duplicidade para efeito desse aviso.

**Feature-specific NFRs:**
- Arquivo gerado deve preservar a estrutura binária das macros do robô VBA (ver addendum — restrições técnicas do gerador).

### 4.7 Cadastros Administráveis

**Descrição:** Telas de administração para os cadastros mestres (divisões, CCs, contas, alçadas, papel×pessoa, exceções de CC, feriados de SLA, etc.), todos carregados inicialmente via planilha e editáveis depois.

#### FR-16: Histórico e restauração de cadastro
Qualquer alteração em um cadastro administrável gera histórico; administrador pode restaurar uma versão anterior.

**Consequences (testable):**
- Comportamento de histórico/restauração é uniforme por design para todos os tipos de cadastro desta feature (divisões, CCs, contas, alçadas, papel×pessoa, exceções de CC, feriados de SLA) — nenhum tipo tem regra de permissão ou profundidade de histórico diferenciada nesta versão.

### 4.8 Painéis e Indicadores

**Descrição:** Visões consolidadas do estado das solicitações para acompanhamento gerencial, espelhando os `painel_snapshots` e rotas `/paineis/{painel}` já previstos no contrato de API original.

#### FR-17: Painéis consolidados
Usuário pode visualizar painéis com indicadores agregados de solicitações (ex.: volume por tipo, por status, por CC).

**Consequences (testable):**
- Painéis leem de snapshots pré-calculados, não de consulta direta às tabelas transacionais em tempo real.

**Notes:** `[NOTE FOR PM]` escopo exato de quais painéis e indicadores entram na v1 ainda não foi detalhado com o negócio — ver Questões Abertas.

## 5. Riscos e Requisitos Não-Funcionais Cross-Cutting

### 5.1 SLA
Toda solicitação tem prazo de 48 horas úteis para processamento, contado pelo calendário de feriados PE/federais cadastrado (FR-16), em fuso America/Recife. `[NOTE FOR PM]` regra de pausa do SLA (quando o relógio para de contar, ex. solicitação em pendência) está entre as decisões de negócio ainda não detalhadas — ver Questão Aberta 10.

### 5.2 Paginação obrigatória
Qualquer listagem que possa ultrapassar 1.000 linhas (ex.: `alçadas`, hoje com 7.683 faixas ativas) deve paginar obrigatoriamente — nunca retornar lista completa de uma vez. Contrato de resposta é `{items, pagina, tamanho}`, sem `total` nem cursor.

### 5.3 Riscos e Mitigações
Prazo-alvo é outubro/2026, mas é um alvo flexível: o patrocinador já confirmou que prefere estender o prazo a cortar qualquer um dos 5 tipos de solicitação ou das regras já especificadas — prazo é a variável flexível, escopo completo é a variável fixa.

O motor de aprovação (§4.4) é o maior fator de incerteza de esforço do produto: duas camadas de resolução, regras especiais curadas por precedência, casos colegiados e snapshots de auditoria. Mitigantes já em mãos: modelo de dados especificado (dicionário + DDL prontos), autenticação resolvida por reuso do Keycloak do FB_APU02, padrões de tela e código já validados em produção. Recomenda-se que o dimensionamento de esforço do motor de aprovação seja tratado com cuidado extra na Arquitetura, antes de comprometer publicamente a data de outubro.

Risco de primeira ordem, distinto dos riscos de esforço técnico acima: a especificação de negócio de origem nunca passou por UAT autenticada nem aceite formal (Questão Aberta 13). Este PRD formaliza uma especificação de construção, não um requisito já homologado em produção — qualquer decisão de arquitetura ou cronograma que trate o conteúdo deste PRD como definitivo sem essa validação carrega esse risco residual.

## 6. Non-Goals (Explícito)

- Este produto **não** implementa a integração via API SAP — ela é construída por outro time, fora deste projeto; o FB_APU05 apenas consumirá quando existir.
- Este produto **não** implementa consulta online de saldo/bloqueio de ordem no SAP — depende da API acima.
- Este produto **não** resolve lacunas de cadastro no SAP (ex. pares filial×CC sem estratégia) — isso é trabalho do negócio/SAP; o sistema apenas sinaliza com clareza quando o cadastro não existe.
- Este produto **não** é uma migração de histórico — carga inicial é só cadastro mestre; solicitações, anexos e exportações nascem zeradas.

## 7. Escopo do MVP

### 7.1 Dentro do Escopo
- Os 5 tipos de solicitação completos (FR-5 a FR-9), lançados de uma vez para todo o público (~500 usuários), sem piloto faseado — completos quanto à regra de negócio especificada; Obras carrega pendências pontuais de dados ainda não confirmadas (Questões Abertas 7-10), não de lógica de sistema.
- Autenticação SSO (FR-1, FR-2).
- Identidade de CC + exceções (FR-3, FR-4).
- Motor de aprovação completo, calculado e nominal (FR-10, FR-11).
- Fila do administrador completa, incluindo reabertura (FR-12 a FR-14).
- Exportação para o robô VBA, os dois lotes, com persistência do Lote-Obra no backend — hoje client-side no protótipo (FR-15).
- Cadastros administráveis com histórico/restauração (FR-16).

### 7.2 Fora do Escopo do MVP
- Integração via API SAP e consulta online de saldo/bloqueio — dependência externa real (outro time), não escolha de escopo.
- Qualquer resolução de lacuna de cadastro SAP (ex. os ~877 pares filial×CC sem alçada) — trabalho do negócio, não de engenharia.

### 7.3 Escopo a Definir com o Negócio
- Painéis e Indicadores (FR-17) — feature nomeada e com contrato de API já previsto, mas sem escopo de indicadores fechado com o negócio (ver Questão Aberta 14). Não entra em 7.1 nem 7.2 até essa definição.

## 8. Métricas de Sucesso

**Primária**
- **SM-1**: Abandono prático do processo atual (planilha + robô VBA) pelos usuários — qualitativo, sem meta numérica definida nesta fase (decisão do patrocinador). `[NOTE FOR PM]` considerar propor 1-2 métricas operacionais simples (ex.: % de solicitações processadas fora da planilha após N semanas) sem reabrir a decisão do patrocinador.

**Contra-métrica**
- **SM-C1**: Volume de solicitações bloqueadas por "sem alçada cadastrada" não deve ser tratado como sucesso do sistema (é lacuna de cadastro SAP) — não otimizar reduzindo rigor da checagem só para baixar esse número.

## 9. Questões Abertas

1. Cadência de recarga da base de colaboradores×CC (Senior) — diária, semanal, sob demanda? (FR-3)
2. Tenant/claims/grupos exatos do Keycloak para este módulo — reaproveitar exatamente o realm do FB_APU02 ou criar client próprio?
3. Repositório de anexos: antivírus/DLP, quarentena — só metadado+hash definidos até aqui.
4. Topologia de hospedagem, domínio, proxy, observabilidade, SLA técnico.
5. Estratégia de rollback de aplicação e banco.
6. HA, backup e restore do Postgres de produção.
7. `CD Jaboatão` sem estrutura de obra confirmada — não criar ordem automaticamente até confirmação do negócio.
8. "FL" na estrutura "Antiga" de obras — decisão de bloqueio pendente.
9. "Ordem bloqueada" em Fortaleza — semântica operacional não confirmada.
10. Quatro casos de divergência de aprovador, fallback de coordenação, balanceamento mensal (hoje por total geral, não por mês), pausa do SLA, curadoria de nomes — citados pela fonte como decisões de negócio ainda abertas, não detalhadas nesta sessão.
11. Ator do robô RPA em produção — identidade/credencial/segregação de funções.
12. Os ~877 pares filial×CC sem estratégia de aprovação no SAP seguem dependendo da janela do gerente e do degrau `GR CONTROLADORIA` — não resolvem acima de R$ 20.000 até o SAP completar o cadastro.
13. UAT autenticada da especificação original ficou em pausa, sem aceite formal de negócio registrado — este PRD parte de uma especificação de construção, não de um requisito já homologado em produção.
14. Escopo exato de quais painéis/indicadores entram na v1 (FR-17) ainda não foi detalhado com o negócio.

## 10. Índice de Suposições

- §4.2 FR-3 — cadência de recarga da base Senior assumida como manual sob demanda até confirmação (ver Questão Aberta 1).
