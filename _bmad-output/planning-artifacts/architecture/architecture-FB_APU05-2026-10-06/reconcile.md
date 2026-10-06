---
title: Reconciliação — ARCHITECTURE-SPINE.md vs. insumos (PRD, addendum, brief, stack FB_APU02)
status: draft
created: 2026-10-06
---

# Reconciliação do Architecture Spine — FB_APU05

Checagem do `ARCHITECTURE-SPINE.md` (2026-10-06) contra os 4 insumos que o geraram. Não lista o que já está coberto (mesmo que resumido com outras palavras) — só o que um construtor sentiria falta.

## Gaps encontrados

### GAP-1 — Paginação obrigatória não virou AD; está "escondida" numa linha de tabela

**Fonte:** PRD §5.2 — "Qualquer listagem que possa ultrapassar 1.000 linhas (ex.: `alçadas`, hoje com 7.683 faixas ativas) deve paginar **obrigatoriamente** — nunca retornar lista completa de uma vez."

**O que o spine tem:** só aparece como uma cláusula dentro da linha "Data & formatos" da tabela de Consistency Conventions ("listas retornam `{items, pagina, tamanho}` — sem `total` nem cursor").

**Por que isso é um problema:** o PRD trata isso como um NFR de segurança/performance obrigatório (com número concreto de escala — 7.683 linhas hoje, crescendo), não como uma preferência de formato de resposta. Do jeito que está no spine, um construtor implementando uma nova listagem (ex.: fila do administrador, cadastro de contas, painéis) lê a convenção como "este é o formato que a lista usa quando paginada" — não como "paginação é obrigatória mesmo que a tela não peça". Não há nenhum AD com `Prevents` que bloqueie explicitamente um endpoint `SELECT * FROM alcadas` sem paginação.

**Recomendação:** promover a um AD próprio (ou anexar a um AD existente, ex. AD-1) com `Rule` explícita: "todo endpoint de listagem pagina por padrão (mesmo sem parâmetro de página explícito do cliente); proibido endpoint que retorna coleção completa de uma tabela sem LIMIT/OFFSET implícito."

---

### GAP-2 — "Nunca inventar aprovador" (histórico de incidente real) não é invariante estrutural, só está em FR-10

**Fonte:** PRD FR-10 — "Quando não há alçada cadastrada... sistema **bloqueia com 'sem alçada cadastrada'**... nunca inventa aprovador ou escalonamento fictício." E addendum, seção "Histórico do motor de aprovação": até agosto/2026 o sistema de referência resolvia pares filial×CC sem estratégia para um **cargo que não existe na estrutura real da empresa**, e a interface nunca refletiu isso — criando divergência silenciosa entre banco e UI. A correção foi "bloquear com mensagem explícita em vez de resolver para um cargo fictício". O addendum enquadra isso como "regra, não escolha de estilo".

**O que o spine tem:** AD-2 define a porta única `Resolver` com dispatch por `tipo_solicitacao`, mas o `Rule` só fala de arquitetura de interface (`Resolve(solicitacao) → (Aprovador, error)`) — não codifica a invariante de negócio-crítica "nunca retornar um aprovador fictício; ausência de alçada é erro explícito, não fallback silencioso". Isso é exatamente o tipo de "requisito quieto" que a estrutura de ADs (focada em forma/dependência) tende a não capturar, porque é uma regra de **conteúdo** da decisão, não de **estrutura** do código — mas é precisamente a regra que já causou um incidente documentado de divergência banco↔UI em produção.

**Recomendação:** adicionar ao `Rule` do AD-2 (ou criar AD-2a) algo como: "`Resolver.Resolve` nunca retorna um `Aprovador` sintético/fictício quando não há alçada cadastrada acima da janela do gerente — retorna erro tipado `SemAlcadaCadastrada` (citando CC e filial), que a camada HTTP traduz em bloqueio explícito ao usuário. Nenhuma implementação de `Resolver` pode ter fallback silencioso." Vale também cobrir o caso "cargo sem titular" (que É válido, não deve ser bloqueado) para não confundir as duas regras.

---

### GAP-3 — Restrição de dados sensíveis (nomes reais/matrículas/alçadas nominais de diretores) não foi herdada pelo spine

**Fonte:** addendum, seção "Dado sensível na fonte original" — o pacote de origem contém nomes completos, matrículas e valores de alçada nominal de diretores reais, classificados como uso corporativo restrito. O addendum é explícito: "este PRD e o addendum do Brief tratam esses casos sempre por **cargo/papel**, nunca por nome de pessoa — **o mesmo vale para qualquer artefato futuro (arquitetura, épicos, stories) que derive deste PRD**, dado que o repositório do projeto é público."

**O que o spine tem:** nada. Nenhuma menção a essa restrição em nenhuma seção (Consistency Conventions, Deferred, ou em qualquer AD).

**Por que isso é um problema:** o spine é exatamente um dos "artefatos futuros" que o addendum avisa para não vazar nomes reais — e ele é, por sua vez, a base para os próximos artefatos (épicos, stories, specs de dados) que o BMAD vai gerar. Sem essa restrição explicitamente carregada adiante, há risco real de um construtor (ou uma próxima sessão de arquitetura/épicos) colar um nome real de diretor ao documentar um exemplo de `AutorizadorNominal`, snapshot de aprovação, ou seed de dados de teste — em um repositório que o próprio addendum classifica como público. Isso não é um detalhe de estilo; é a única instrução de confidencialidade de todo o pacote de insumos.

**Recomendação:** adicionar uma linha nas Consistency Conventions (ou um AD de documentação): "qualquer exemplo, seed de dados, fixture ou trecho ilustrativo neste e em artefatos derivados (épicos, stories, specs) referencia aprovador/autorizador sempre por cargo/papel — nunca por nome real de pessoa ou matrícula, mesmo em dados de teste inspirados na carga real."

---

### GAP-4 — Padrões de segurança HTTP do FB_APU02 (SecurityMiddleware, rate limiting, emissão/rotação de JWT próprio) não foram citados, apesar do AD-9 prometer herança de "padrões de segurança já validados"

**Fonte:** `01-STACK-E-PADROES-FB_APU02.md` §2 — `SecurityMiddleware` global (HSTS, CSP, X-Frame-Options DENY, nosniff, Referrer-Policy, Permissions-Policy); rate limiting em memória por IP (login 5/15min, registro 10/h, forgot-password 3/h); CORS com whitelist via `ALLOWED_ORIGINS`; JWT access token 30min + refresh token 7 dias em cookie HttpOnly/SameSite=Strict, rotacionado a cada uso, com blacklist no logout. PRD FR-1 também exige explicitamente: "sistema emite seus próprios tokens (JWT + refresh)" — ou seja, mesmo usando Keycloak para autenticação, o FB_APU05 tem sua própria camada de sessão/token que replica esse desenho.

**O que o spine tem:** AD-9 ("Stack herdado") diz "versões idênticas... e padrões de segurança já validados" na justificativa (`Prevents`), mas o `Rule` só fala de **versões de pacote** — não menciona o middleware de segurança HTTP, rate limiting, nem o desenho concreto de emissão/rotação/blacklist do JWT próprio. AD-6 cobre apenas o fluxo OIDC/PKCE + JWKS + `email_verified` (a troca de token com o Keycloak), mas não o que acontece depois — a emissão do token próprio do FB_APU05 que o FR-1 exige.

**Por que isso é um problema:** um construtor lendo só o spine implementaria o handshake com Keycloak (AD-6) e pararia aí, sem necessariamente replicar o `SecurityMiddleware` global, o rate limiting no endpoint de troca de token, nem o padrão de refresh token rotacionado + blacklist no logout — que são exatamente os "padrões de segurança já validados em produção" que a Brief e o AD-9 citam como mitigante de risco, mas não amarram a uma `Rule` verificável.

**Recomendação:** ou (a) estender o `Rule` do AD-9 para citar explicitamente esses mecanismos como herdados ("inclui `SecurityMiddleware` global idêntico, rate limiting por IP nos endpoints de autenticação/troca de token, CORS whitelist via env"), ou (b) estender AD-6 para cobrir o pós-Keycloak: emissão de JWT próprio (30min) + refresh (7 dias, HttpOnly/SameSite=Strict, rotacionado, blacklist no logout). Também vale uma frase sobre AES-256-GCM: não há, nos insumos, menção a segredos de terceiros armazenados no banco do FB_APU05 hoje — então o padrão pode não se aplicar ainda; mas se/quando a futura `ExportadorAPI` (SAP) precisar guardar credencial, o spine deveria antecipar que ela segue o mesmo padrão `AES-256-GCM` + `ENCRYPTION_KEY` separado do `JWT_SECRET`, em vez de deixar essa decisão para ser redescoberta then.

---

### GAP-5 — Restrições binárias conhecidas do gerador VBA (addendum) não são referenciadas pelo AD-3

**Fonte:** addendum, "Restrições técnicas do gerador de exportação SAP (FR-15)" — quatro lições de bugs reais e documentados: preservar `vbaProject.bin` byte a byte; nunca usar replacement de string tipo `$1`/`$&` sem escapar (corrompe fórmulas); remover `xl/calcChain.xml` ao reescrever linhas; aba de rascunho sempre visível, nunca `veryHidden`.

**O que o spine tem:** AD-3 define a porta `ExportadorSAP` / `ExportadorVBA` e promete que a troca futura pela API SAP não exige reescrever chamadores — mas não menciona, nem referencia, essas quatro restrições concretas de implementação, que são precisamente o tipo de conhecimento operacional frágil (bugs já corrigidos uma vez, documentados porque reapareceram) que se perde quando não é amarrado a um artefato estrutural.

**Por que isso é um problema:** essas não são regras de negócio genéricas — são "cicatrizes" de produção específicas do formato binário `.xlsm`. Se o spine não aponta para elas, a próxima camada (épicos/stories do `ExportadorVBA`) pode não puxar o addendum e reintroduzir exatamente os bugs que ele documenta (ex.: um dev usando replace ingênuo de string ao montar o lote, ou ocultando a aba de rascunho "para não poluir a UI").

**Recomendação:** no `Rule` ou nas notas do AD-3, adicionar uma referência explícita: "implementação de `ExportadorVBA` deve respeitar as 4 restrições binárias documentadas no addendum do PRD (seção 'Restrições técnicas do gerador de exportação SAP') — não reescrever `vbaProject.bin`, escapar todo conteúdo gerado antes de qualquer substituição de string, remover `calcChain.xml` ao reescrever linhas, manter aba de rascunho sempre visível."

---

## O que foi conferido e está coberto (não listado em detalhe)

- Lock otimista + HTTP 409 sem retry (AD-5) — coberto.
- Single-company / sem `company_id` (AD-7) — coberto, inclusive citando corretamente o anti-padrão do FB_APU02 (RLS não aplicado) a evitar.
- Autenticação Keycloak + `email_verified` (AD-6) — coberto no que diz respeito ao handshake OIDC; ver GAP-4 para o que falta depois do handshake.
- Upload manual da base Senior, sem job agendado (AD-8) — coberto.
- Stack de versões (AD-9) e pipeline de deploy (AD-10) — cobertos quanto a versões/infra; ver GAP-4 quanto a padrões de segurança de aplicação.
- Writer path único para `aprovador_snapshot`/`versao`/`exportacoes_sap` (AD-4) — coberto.
- Paradigma híbrido domínio isolado vs. Handler Factory (AD-1) — coberto.
- Mapeamento capability → arquitetura e Deferred (questões em aberto do PRD) — coberto de forma consistente com a seção 9 do PRD.

## Itens menores, não elevados a gap principal

- FR-2 (proteção contra rebaixar/desativar o último administrador ativo) não tem AD correspondente — é mais regra de domínio pontual do que invariante arquitetural; mencionar apenas para o caso de a próxima fase (épicos) não esquecer de testar esse caso.
- Risco de primeira ordem do PRD §5.3 (especificação de origem nunca passou por UAT/aceite formal) não aparece no spine (nem em Deferred). Não é uma decisão de arquitetura, mas é um risco que vale uma linha em Deferred ou nas notas do documento, para que mudanças de requisito não peguem a arquitetura de surpresa.
