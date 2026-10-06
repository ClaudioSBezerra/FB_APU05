---
title: FB_APU05 — Módulo Controladoria (Gestão de Solicitações Orçamentárias)
status: final
created: 2026-10-06
updated: 2026-10-06
---

# Product Brief: FB_APU05 — Módulo Controladoria

## Resumo Executivo

FB_APU05 substitui o processo manual de gestão de solicitações orçamentárias/de investimento da Ferreira Costa — hoje operado via planilha ("Novo JIRA — Gestão de Orçamento") e robô VBA — por um sistema web com fluxo de aprovação automatizado por alçadas, trilha de auditoria e exportação governada para o SAP. Cobre cinco tipos de solicitação (transferência, inclusão, inclusão SFC, imobilizado, obras), resolvendo automaticamente ~95,6% das combinações filial×CC×valor sem intervenção humana no cálculo do aprovador.

A v1 mantém a exportação atual (arquivo para o robô VBA) como ponte, com a camada de integração desenhada para ser substituída por uma API SAP que outro time está construindo em paralelo — sem precisar reescrever o núcleo do domínio quando ela chegar.

## Risco e Mitigação

Prazo-alvo é outubro/2026 (forte, mas não corte de escopo — o usuário aceita ultrapassar para entregar os 5 tipos completos). Isso é um prazo apertado para um domínio com motor de aprovação em duas camadas, ~37 tabelas e regras especiais curadas.

**Mitigantes já em mãos:**
- Modelo de dados já vem especificado (dicionário de dados + DDL prontos para Postgres), reduzindo uma das maiores fontes de atraso em projetos novos.
- Autenticação (o maior gap que o pacote de requisitos original deixava aberto) já tem padrão validado em produção: reaproveitar a integração Keycloak do FB_APU02 praticamente elimina esse risco.
- Padrões de tela, segurança e estrutura de código já existem e foram validados em produção no FB_APU02 — não é arquitetura do zero.

**Risco remanescente:** a complexidade do motor de aprovação (regras especiais curadas por precedência, casos colegiados, fallback de escalonamento) é o maior fator de incerteza de esforço. Recomenda-se que o PM dimensione isso com cuidado no PRD antes de comprometer publicamente a data de outubro.

## O Problema

Hoje a Controladoria processa solicitações orçamentárias em planilha: o aprovador é calculado manualmente ou por conhecimento informal de quem está na área, não há trilha de auditoria formal de "quem aprovou e por quê", edições simultâneas não são tratadas, e a exportação para o SAP depende de um robô VBA com histórico documentado de bugs recorrentes (aba oculta, radio button incompleto, delimitador persistente, entre outros, todos corrigidos pontualmente mas sintomáticos de um processo frágil).

Não há um incidente único motivando este projeto — é o reconhecimento de que o processo atual não escala nem audita como deveria, e precisa sair de planilha para sistema.

## A Solução

Um hub único de solicitações orçamentárias com dois motores de aprovação coexistentes:
- **Resolver calculado** (transferência, inclusão SFC): cadeia de regras especiais curadas → matriz de alçadas SAP → janela do gerente do CC → escalonamento.
- **Autorizador nominal** (inclusão, imobilizado, obras): pessoa validada por CC/faixa de valor, sem cálculo de alçada.

Cada aprovação resolvida é gravada como snapshot de auditoria (nunca referência viva). Lock otimista protege contra edição concorrente. A camada de exportação SAP é abstraída: v1 gera o pacote para o robô VBA existente; a arquitetura já isola essa fronteira para plugar a API SAP quando o outro time entregar.

Autenticação reaproveita o padrão já validado em produção no projeto irmão FB_APU02: SSO via Keycloak corporativo da Ferreira Costa (OIDC/PKCE), sem cadastro de usuário separado — o perfil nasce do SSO.

## Quem Isso Atende

~500 usuários internos da Ferreira Costa:
- **Solicitantes** — qualquer área que precise movimentar orçamento/investimento.
- **Administradores (Controladoria)** — processam a fila: assumir → atender/pendência → finalizar → exportar para o robô SAP.

Aprovador é um papel calculado pelo sistema, não um perfil de login à parte.

## Critério de Sucesso

O sistema é considerado bem-sucedido quando o processo atual (planilha + robô VBA, apelidado internamente de "Jira", percebido como burocrático) é **abandonado na prática pelos usuários** — não por decreto, mas porque o fluxo novo resolve melhor. Não há meta numérica formal (ex.: tempo de aprovação, taxa de erro) definida nesta fase; fica como refinamento natural do PRD junto à Controladoria.

## Escopo

**Dentro do lançamento** (mesmo que isso estenda o prazo além de outubro/2026 — ver Risco):
- Os 5 tipos de solicitação completos, com suas regras específicas, lançados de uma vez para todo o público (sem piloto faseado).
- Os dois motores de aprovação (resolver calculado + autorizador nominal) com snapshot de auditoria.
- Fila do administrador completa (assumir → atender → finalizar → reabrir por comentário).
- Exportação para o formato atual do robô VBA (lotes Despesa e Obra), incluindo persistência do Lote-Obra no backend (hoje só client-side no protótipo).
- Cadastros mestres via carga inicial (CSVs já especificados) + tela administrável com histórico/restauração de versão.
- Tratamento explícito das lacunas de cadastro já conhecidas (ex.: ~877 pares filial×CC sem estratégia de aprovação no SAP, CD Jaboatão sem estrutura de obra — lista completa no addendum): o sistema **suporta o fluxo assim que o cadastro existir no SAP** e **sinaliza com clareza quando não existir**; completar o cadastro em si é trabalho do negócio/SAP, não de engenharia.

**Fora do lançamento** (dependência externa real, não escolha de escopo):
- Integração via API SAP — a API ainda não existe; é construída por outro time (contato: gestor de integração SAP da Ferreira Costa). Este projeto consome quando ela estiver pronta.
- Consulta online de existência/saldo/bloqueio de ordem no SAP — depende da mesma API acima.

## Visão

Em 2-3 anos, FB_APU05 se torna o hub único de solicitações orçamentárias da Ferreira Costa, com a exportação via robô VBA completamente aposentada em favor da API SAP, e serve de base/padrão para outros módulos de Controladoria que hoje ainda rodam em planilha.
