# Requisitos de Negócio — Pacote "Envio TI" (FB_APU05 / Controladoria)

> Fonte: pacote de documentação de negócio em `/tmp/envio TI/envio TI` (gerado em 2026-08-19, especificação de construção — não é prova de execução em produção). Levantamento feito em 2026-10-06 para servir de insumo ao Project Brief / PRD do BMAD.

## Visão geral do domínio

Sistema de gestão de solicitações de movimentação de orçamento/investimento ("Novo JIRA — Gestão de Orçamento"), com cinco tipos de chamado: `transferencia`, `inclusao`, `inclusao_sfc` (RPA), `imobilizado`, `obras`. Fluxo de aprovação por alçadas (calculadas ou por autorizador nominal) e exportação final para o SAP via robô VBA (SAP GUI). É um hub único de requisições orçamentárias da Ferreira Costa — Obras e Imobilizado são dois dos cinco tipos, não o sistema inteiro.

Perfis: `solicitante` e `administrador` (aprovador é papel calculado, não perfil de login). Fluxo do admin: fila → assumir → atender/pendência → finalizar (incluído/erro) → exportar para robô SAP → reabertura automática por comentário pós-finalização. SLA de 48h úteis (calendário de feriados PE/federais, fuso America/Recife).

**Decisão já tomada (2026-10-06): banco de produção será Postgres**, alinhado ao padrão do FB_APU02. O DDL Oracle do pacote fica como referência histórica, mas não será a base da implementação.

## Modelo de dados

~37 tabelas (Postgres). Blocos principais:

1. **Identidade** — `usuarios` (perfil vem do SSO/Entra, nunca do payload).
2. **Cadastros mestres** — `divisoes` (44), `centros_custo` (115), `contas` (334 = 191 BIFC + 143 SFC, PK composta `(plano,codigo)` pois códigos colidem entre planos), `classes_imobilizado` (8), `rel_cc_conta` (27 pares, incompleta por desenho).
3. **Obras/estrutura** — `locais_obra` (9), `subgrupos_despesa` (203, PK não é nome), `obra_ordens` (217, grão de local+subgrupo+tipo_despesa+tipo_faturamento).
4. **Aprovação** — `alcadas` (matriz SAP, 7.683 ativas/12.581 total — única tabela >1000 linhas, exige paginação obrigatória), `papel_pessoa` (80, cargo≠pessoa), `dono_cc`, `regras_aprovacao`+`regras_aprovacao_pessoas`, `aprovadores_obra`, `autorizadores_formulario`.
5. **Solicitação** — `solicitacoes` (raiz, lock otimista por `versao`, conflito=409 sem retry, histórico append-only com `on delete restrict`), `solicitacao_lados`, `solicitacao_lancamentos`, `solicitacao_obras_linhas`, `solicitacao_aprovadores` (snapshot, não referência), `solicitacao_anexos`, `solicitacao_operacoes_lote` (idempotência por ator+chave).
6. **SAP/governança** — `exportacoes_sap`, `painel_snapshots`, `cadastro_overrides/auditoria/snapshots`.

**5 requisitos não-negociáveis** (não são detalhe de implementação):
- Aprovador recalculado no servidor e gravado como snapshot.
- Conflito de versão = HTTP 409 lógico, sem retry automático.
- Histórico append-only.
- Idempotência por (ator, chave) em operações em lote/exportação.
- Paginação obrigatória em leituras que podem passar de 1000 linhas (ex.: `alcadas`).

## Carga inicial

20 CSVs, 13.962 linhas, UTF-8 BOM, separador `;`, só cadastros mestres (zero usuários/chamados/anexos — usuários nascem do SSO). Dados sensíveis reais (nomes, matrículas, e-mails) — uso corporativo restrito.

Pendências de dados conhecidas (não são bugs do sistema, são lacunas de cadastro):
- `CD Jaboatão` sem estrutura de obra confirmada.
- 877 pares filial×CC sem estratégia de aprovação cadastrada no SAP.
- 34 "pessoas" placeholder do banco de referência que NÃO devem ser carregadas.
- `rel_cc_conta` incompleta (27 de ~21 mil combinações possíveis) — filtro conta×CC desativado até completar.

## Regras de negócio por formulário

- **Imobilizado**: aquisição de ativo fixo (classe de imobilizado, não conta contábil); sem fornecedor (cotação vai no anexo), sem competência/mês; autorizador nominal validado por CC+faixa (não usa o resolver de alçadas).
- **Inclusão SFC (RPA)**: idêntico à inclusão comum, exceto plano de contas (`CONTAS_SFC`, 143 contas que colidem em código com o BIFC — ex. `3030` = Verba Comercial no BIFC vs Programa de Alimentação do Trabalhador no SFC; nunca misturar); usa o resolver de alçadas normal.
- **Obras**: multi-linha (vários subgrupos/ordens por chamado), por local de obra + subgrupo de despesa + ordem de investimento (sem filial/CC/conta/mês); 4 classificações (inclusão, FL, transferência de saldo — dois blocos balanceados, retirada de saldo); ordem aceita literal `CRIAR` quando ainda não existe no SAP (nasce na finalização, mesma transação); autorizador de lista fixa (11-12 nomes) com teto de alçada por pessoa.

## Modelo de aprovação (núcleo mais complexo do domínio)

Dois modelos coexistem:

**(a) Resolver calculado** — usado só por `transferencia` e `inclusao_sfc`:
camada de importação (filial≥9000) → regras especiais curadas (RH/DP/Manutenção/Marketing/Utilidades, por precedência) → matriz de alçadas SAP filtrada por filial_norm+CC+valor → janela do gerente até R$20.000 (decisor vem do grupo do CC, caso colegiado para Logística — 3 gerentes, qualquer um aprova) → fallback "sobe para o GR mais próximo acima" → acima de R$20k escalona para diretoria/superintendência (fora do fluxo do sistema nesta fase).

**(b) Autorizador nominal** — usado por `inclusao` (desde 2026-08-04), `imobilizado` e `obras` (lista fixa): pessoa escolhida em combobox, validada contra CC/faixa de valor, sem cálculo de alçada.

Cobertura medida: 95,6% das 34.544 combinações filial×CC×valor resolvem automaticamente; o resto é lacuna de cadastro no SAP, não defeito do sistema.

**Auditoria**: todo aprovador resolvido é gravado como snapshot (nome, motivo legível, regra_id/versão) — nunca referência viva, para preservar a "verdade" da data do chamado.

## Contrato de API (openapi.yaml)

30 rotas. Recursos principais: `/bootstrap`, `/usuarios` (+`/me`), lookups mestres (`/divisoes`, `/centros-custo`, `/contas`, `/contas-sfc`, `/classes-imobilizado`, `/locais-obra`, `/subgrupos-despesa`, `/obra-ordens`), lookups de aprovação (`/alcadas`, `/papel-pessoa`, `/dono-cc`, `/regras-aprovacao`, `/autorizadores-formulario`), núcleo transacional (`/solicitacoes`, `/solicitacoes/importar-jira`, `/solicitacoes/{id}`, `/solicitacoes/acoes-lote`), anexos, cadastros administráveis (com histórico/restauração de versão), exportação SAP (`/sap/exportacoes`), painéis (`/paineis/{painel}`).

Regras de contrato: lista de solicitações devolve só `{items, pagina, tamanho}` (sem total/cursor); conflito de versão = 409 sem retry; lote/exportação idempotentes por (ator, chave).

## App de referência

Protótipo/mock de frontend (`Novo JIRA Referencia.html`, derivado de artefato Supabase de homologação) que define o **padrão de telas esperado** — não é entregável de produção. Usa `dataProvider.js` como camada única de acesso a dados (hoje `StaticProvider` local; trocar por `ApiProvider` para ligar ao backend real sem mudar telas). Os módulos `sapExport.js`/`obrasExport.js`/`estruturaExport.js` geram/leem arquivos `.xlsm` no navegador, preservando `vbaProject.bin` byte a byte.

## Robô SAP

Dois lotes de exportação, nunca misturados:
- **Lote - Despesa** (transferência/inclusão/SFC/imobilizado): lançamento mês a mês, registrado no backend, idempotente.
- **Lote - Obra** (ordem×valor): download client-side, sem registro no backend ainda (dívida técnica conhecida).

Robô VBA (`modRoboOrcamento.bas`) automatiza SAP GUI: simula (`S_ALR_87013620`), calcula "Novo Plano", grava via `KP06`. Importante: gerar o arquivo NÃO finaliza o chamado — finalização é ação transacional separada (`POST /solicitacoes/acoes-lote`) com lock otimista.

## Lacunas/ambiguidades a confirmar com o negócio antes de formalizar como requisito

1. HA, backup/restore do banco de produção — aberto.
2. SSO/Entra: tenant, claims, grupos — modelado em perfis, mas fluxo real não definido.
3. Repositório de anexos: antivírus/DLP, quarentena — só metadado+hash definidos.
4. 877 pares filial×CC sem estratégia de aprovação no SAP — pendência de cadastro, não do sistema.
5. `CD Jaboatão` sem estrutura de obra confirmada — não criar ordem automaticamente até confirmação.
6. FL na estrutura "Antiga" — decisão de bloqueio pendente.
7. "Ordem bloqueada" em Fortaleza — semântica operacional não confirmada.
8. Consulta online de existência/saldo/bloqueio de ordem no SAP — integração futura, não implementada agora.
9. Ator do robô RPA em produção — identidade/credencial/segregação de funções em aberto.
10. Balanceamento de transferência é por total geral, não por mês — decisão já registrada, sujeita a revisão.
11. Persistência do Lote-Obra no backend — passo futuro (hoje só client-side).
12. "Verba duplicada": gravar aviso no histórico quando ignorado; considerar exportações em GERADO como risco de duplicidade — ainda em aberto.
13. Decisões de aprovação sem resposta mesmo após curadoria (CC 6405 sem gerente, CC 6260 sem gerente geral no CD, pseudo-filial 4016) — aceitas como estão, mas vale confirmar antes do go-live.
14. UAT autenticada em pausa; aceite formal para produção pendente.

> Recomendação: preservar o racional histórico ("por que não foi feito de outro jeito") como seção de ADRs no PRD/Architecture — várias regras parecem arbitrárias sem o contexto do incidente que as motivou (ex. HTTP 409 vs 40001, paginação obrigatória em `alcadas`).
