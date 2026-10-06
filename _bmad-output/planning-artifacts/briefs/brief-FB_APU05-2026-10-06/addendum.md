---
title: Addendum — FB_APU05 Product Brief
status: final
created: 2026-10-06
updated: 2026-10-06
---

# Addendum

Contexto e detalhe técnico que não cabe no brief executivo, mas que o PRD/Arquitetura devem herdar diretamente.

## Fontes completas

- `docs/referencia/01-STACK-E-PADROES-FB_APU02.md` — stack, segurança, padrão de telas e banco de dados de referência (reaproveitado do FB_APU02).
- `docs/referencia/02-REQUISITOS-NEGOCIO-ENVIO-TI.md` — modelo de dados completo (~37 tabelas), regras de negócio por formulário, modelo de aprovação detalhado, contrato de API (openapi.yaml), e a lista completa das 14 lacunas/ambiguidades do pacote original.

## Por que a integração SAP é via arquivo pluggable, não hardcoded

O time de SAP (contato: gestor de integração SAP da Ferreira Costa) vai construir uma API própria para receber as solicitações aprovadas, mas essa API ainda não existe e não é controlada por este projeto. Decisão: a camada de exportação do FB_APU05 deve ser isolada atrás de uma interface (ex.: algo como `SAPExporter`) cuja implementação v1 gera o pacote `.xlsm` para o robô VBA existente (replicando os dois lotes documentados: "Lote - Despesa" e "Lote - Obra"), e cuja troca futura por uma implementação que chama a API SAP não deve exigir mudança no núcleo do domínio (resolução de alçada, snapshot de auditoria, lock otimista). Isso é uma restrição de arquitetura que o Architect deve tratar como requisito não-funcional desde o desenho inicial, não como refactor futuro.

## Reaproveitamento do SSO Keycloak do FB_APU02 — detalhe técnico

O FB_APU02 já tem, em produção, uma integração SSO paralela ao login próprio:
- Frontend completa OIDC/PKCE direto contra o Keycloak real da Ferreira Costa (`https://iam.fcxlabs.com/realms/ferreiracosta`).
- Recebe o access token do Keycloak, envia para um endpoint backend que valida via JWKS, extrai o e-mail (com checagem obrigatória de `email_verified` — achado de segurança documentado no FB_APU02: sem essa checagem, um usuário poderia alterar o próprio e-mail de perfil no Keycloak e logar como outra pessoa).
- Busca o usuário por e-mail e emite os tokens próprios da aplicação (JWT 30min + refresh cookie) — o token do Keycloak nunca é exposto ao resto do sistema.

Recomendação: o FB_APU05 deve replicar esse mesmo padrão (mesmo realm, mesma checagem de `email_verified`), em vez de reinventar a integração. Isso resolve diretamente a lacuna "SSO/Entra: tenant, claims, grupos — fluxo real não definido" que o pacote envio TI original deixava aberta.

## Nota sobre o prazo

Prazo "ideal" é outubro/2026, mas o patrocinador confirmou explicitamente que prefere estender o prazo a cortar qualquer um dos 5 tipos de solicitação ou das lacunas já documentadas. Isso deve ser comunicado ao PM como um trade-off já decidido (prazo é a variável flexível, escopo completo é a variável fixa) — evita a armadilha comum de um PM assumir o inverso.

## Lacunas de cadastro vs. lacunas de código

Importante distinção que surgiu na conversa: várias das 14 lacunas do pacote original (ex.: 877 pares filial×CC sem estratégia de aprovação no SAP, CD Jaboatão sem estrutura de obra confirmada) são **lacunas de dados cadastrados no SAP**, não lacunas de lógica do sistema. O compromisso de "tudo entra no lançamento" significa que o FB_APU05 deve ter a lógica pronta para tratar esses casos corretamente assim que o cadastro existir (e sinalizar de forma clara/auditável quando não existir) — não que a engenharia vai "resolver" o cadastro do SAP, que é fora do controle deste time.

## Critério de sucesso — nota de processo

O patrocinador definiu o critério de sucesso como qualitativo ("abandonarem o Jira") deliberadamente, sem meta numérica. Isso é aceitável para o brief, mas o PM deve considerar propor 1-2 métricas operacionais simples no PRD (ex.: % de solicitações processadas fora da planilha após N semanas) para dar ao time algo mensurável sem reabrir essa decisão com o patrocinador.
