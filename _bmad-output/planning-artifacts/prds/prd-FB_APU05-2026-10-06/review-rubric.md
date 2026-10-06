# PRD Quality Review — FB_APU05 — Módulo Controladoria

## Overall verdict

Este PRD é incomum pela especificidade honesta nas partes que o time já trabalhou a fundo: motor de aprovação, fila do administrador e exportação SAP têm regras testáveis, trade-offs nomeados e um glossário que resiste a uso cruzado. Mas a seção que o próprio PRD chama de entrega central da v1 — os cinco tipos de solicitação (FR-5 a FR-9) — não recebeu o mesmo tratamento: quatro deles não têm bloco de "Consequences (testable)", e um recurso nomeado (FR-17, Painéis) fica fora das duas listas de escopo do MVP sem que isso seja assinalado como pendência. Combinado com o fato de que a UAT da especificação original nunca foi homologada pelo negócio (Questão Aberta 13) estar enterrada em meio a uma lista plana de 14 itens — em vez de aparecer em §5.3 Riscos —, o documento é mais arriscado para quem for escrever stories a partir dele do que sua qualidade de prosa sugere.

## Decision-readiness — adequate

A maior parte do documento nomeia trade-offs com clareza: §5.3 declara explicitamente que "prazo é a variável flexível, escopo completo é a variável fixa", e FR-10 tem um **Out of Scope** que diz o que foi abandonado ("escalonamento para diretoria/superintendência acima de R$ 20.000 fica fora do fluxo deste sistema nesta fase") em vez de empurrar a decisão para "considerações". As Questões Abertas (§9) são, em sua maioria, genuinamente abertas — não perguntas retóricas com resposta na frase seguinte.

O problema é de peso relativo, não de ausência. A Questão Aberta 13 diz: *"UAT autenticada da especificação original ficou em pausa, sem aceite formal de negócio registrado — este PRD parte de uma especificação de construção, não de um requisito já homologado em produção."* Esse é provavelmente o fato mais relevante para calibrar a confiança em todo o resto do PRD — e ele aparece como item 13 de uma lista plana de 14, nunca referenciado em §5.3 (Riscos e Mitigações), que é exatamente onde um leitor esperaria encontrá-lo ao lado do risco do motor de aprovação. Um decisor que leia só Visão + Riscos não vai saber que a base de requisitos nunca foi validada pelo negócio.

### Findings
- **high** UAT não homologada está subenterrada, não ausente (§9 Questão Aberta 13; cf. §5.3) — O fato de que a especificação de origem nunca passou por aceite formal de negócio deveria estar em §5.3 Riscos e Mitigações, não apenas como item 13/14 de uma lista de questões abertas sem diferenciação de peso. *Fix:* adicionar uma frase em §5.3 referenciando a Questão Aberta 13 como risco de primeira ordem, distinto dos riscos de esforço técnico já listados.

## Substance over theater — strong

Quatro personas (Renata, solicitante comum do Comercial, administrador, autorizador nominal), cada uma amarrada a FRs específicos (UJ-1→FR-4, UJ-2→FR-10, UJ-3→FR-12–14, UJ-4→FR-8/FR-11) — no limite do que o rubric considera aceitável, sem nenhuma UJ flutuante. Não há seção de diferenciação/inovação forçada. Os NFRs são concretos e específicos ao produto, não boilerplate: §5.2 cita o número exato de linhas ativas hoje ("7.683 faixas ativas" em alçadas) e define o contrato de paginação (`{items, pagina, tamanho}`, sem `total` nem cursor); §4.3 cita "27 de ~21 mil combinações possíveis" para `rel_cc_conta`. A Visão (§1) nomeia o sistema legado real ("Novo JIRA — Gestão de Orçamento", robô VBA) e a estratégia de ponte até a API SAP — não poderia ser trocada para outro PRD sem reescrita.

## Strategic coherence — strong

A tese é clara e específica: substituir planilha + robô por um hub único, mantendo o robô VBA como ponte deliberada até a API SAP existir, com o núcleo de domínio desenhado para sobreviver à troca da camada de integração (§1). A decisão de lançar os 5 tipos completos de uma vez, sem piloto faseado, decorre dessa tese (eliminar o processo antigo por completo, não parcialmente) e é justificada explicitamente pelo patrocinador em §5.3. SM-1 mede abandono prático do processo antigo — valida a tese diretamente, não é métrica de atividade (não há DAU/MAU). SM-C1 é uma contra-métrica bem pensada, amarrada a um vetor de gaming concreto (afrouxar a checagem de alçada só para reduzir bloqueios). Nenhuma seção lê como item de backlog isolado sem conexão com a tese — com a exceção parcial de FR-17 (Painéis), que serve "acompanhamento gerencial" e não a substituição do fluxo de solicitação em si; isso não derruba a coerência geral, mas é o ponto mais fraco do encadeamento tese→features.

## Done-ness clarity — thin

Este é o ponto mais sério do PRD, porque afeta exatamente os cinco tipos de solicitação que a Visão chama de entrega completa da v1.

FR-1 a FR-4 e FR-10 a FR-15 têm blocos **"Consequences (testable)"** bem formados — condições verificáveis, não adjetivos. FR-15 em particular é um dos melhores FRs do documento: idempotência, recálculo server-side de elegibilidade, aviso de verba duplicada com regra precisa de quando ele conta como risco (status `GERADO` ainda não finalizado). Mas **FR-5, FR-6, FR-7, FR-8 e FR-9 — os cinco tipos de solicitação, a entrega central do produto — não têm bloco de Consequences**. FR-6 inteiro é uma frase: *"Solicitante pode abrir uma inclusão de orçamento, com autorizador nominal (desde 2026-08-04 — antes usava resolver calculado)."* Não há lista de campos, regra de validação, nem comportamento de erro. Um engenheiro não consegue derivar critério de aceite dessa frase sozinha — precisaria voltar à especificação de origem (fora deste PRD) para saber o que realmente constitui "abrir uma inclusão".

FR-16 (Histórico e restauração de cadastro) trata ~7 tipos de cadastro mencionados em §4.7 (divisões, CCs, contas, alçadas, papel×pessoa, exceções de CC, feriados de SLA) como um único requisito genérico, sem limite de profundidade de restauração, sem regra de permissão, sem diferenciação por tipo de cadastro.

FR-17 (Painéis) tem uma única consequência testável ("Painéis leem de snapshots pré-calculados") e o próprio `[NOTE FOR PM]` admite que o escopo não foi fechado com o negócio — isso não é um FR implementável como está.

### Findings
- **critical** FR-5 a FR-9 sem critério de "pronto" (§4.3, FR-5 a FR-9) — Os cinco tipos de solicitação, descritos na Visão como "os cinco tipos completos" da v1, não têm bloco de Consequences testáveis; a maioria é uma ou duas frases de descrição. *Fix:* adicionar Consequences (testable) a cada FR-5–FR-9 com campos obrigatórios, regras de validação e comportamento de erro — mesmo que isso signifique puxar conteúdo da especificação de origem para dentro do PRD.
- **high** FR-17 não tem escopo definido o suficiente para ser implementado (§4.8) — O `[NOTE FOR PM]` admite que "escopo exato de quais painéis e indicadores entram na v1... ainda não foi detalhado com o negócio", mas FR-17 está redigido como um requisito numerado e commitado. *Fix:* ou mover FR-17 para uma lista de pendências de descoberta (fora do corpo de FRs numerados) até o negócio definir escopo, ou reduzir o FR a um placeholder explícito de descoberta.
- **medium** FR-16 trata cadastros heterogêneos como um requisito único (§4.7, FR-16) — Sete tipos de cadastro mencionados na descrição da feature, mas o FR não diferencia regras de permissão, profundidade de histórico, ou validação por tipo. *Fix:* ao menos nomear se há diferenças de comportamento entre os cadastros, ou declarar explicitamente que o comportamento é uniforme por design.

## Scope honesty — thin

A seção de Non-Goals (§6) é direta e específica — quatro "este produto não..." com justificativa de cada omissão (dependência externa real vs. decisão de escopo). O único `[ASSUMPTION]` do documento (§4.2 FR-3) está corretamente indexado em §10, sem excesso de suposições silenciosas — a maior parte dos desconhecidos vira Questão Aberta explícita em vez de suposição não marcada, o que é o comportamento mais honesto.

Mas duas lacunas de escopo específicas não são assinaladas como tal:

Primeiro, **FR-17 não aparece em nenhuma das duas listas de Escopo do MVP** (§7, subseções "6.1 Dentro do Escopo" e "6.2 Fora do Escopo do MVP") — apesar de ser uma feature nomeada com FR próprio em §4.8. Não há "ver Questão Aberta" nem nota explicando a omissão; o leitor precisa notar sozinho que FR-17 não está em nenhuma lista.

Segundo, **§7/6.1 afirma "Os 5 tipos de solicitação completos (FR-5 a FR-9)"**, mas as Questões Abertas 7, 8, 9 e 10 documentam especificamente lacunas de regra de negócio não resolvidas em Obras (um dos cinco tipos): `CD Jaboatão` sem estrutura confirmada, status "FL" com bloqueio pendente, semântica de "Ordem bloqueada" não confirmada, e "quatro casos de divergência de aprovador" citados como decisão de negócio ainda aberta. A palavra "completos" e essas quatro pendências específicas do mesmo tipo de solicitação não são reconciliadas em lugar nenhum do documento.

A densidade de itens em aberto (14 Questões Abertas + 1 Assumption + 3 `[NOTE FOR PM]`) seria aceitável para um PRD de baixo risco, mas este é explicitamente "para o time que vai desenhar a arquitetura e quebrar o trabalho em épicos/stories" (§0) — ou seja, greenlight para construir — e várias dessas pendências (Obras, UAT, painéis) tocam o núcleo funcional, não só infraestrutura.

### Findings
- **high** FR-17 ausente de ambas as listas de Escopo do MVP (§7, "6.1 Dentro do Escopo" / "6.2 Fora do Escopo do MVP") — Feature nomeada com FR próprio em §4.8, mas sua inclusão na v1 não é afirmada nem negada em nenhuma lista — fica para o leitor inferir. *Fix:* adicionar FR-17 a uma das duas listas, ou criar uma terceira categoria explícita ("escopo a definir com o negócio") que o nomeie.
- **medium** "5 tipos completos" entra em tensão com 4 questões abertas específicas de Obras (§7/6.1 vs. §9 Questões 7–10) — A alegação de completude não reconhece as pendências de regra de negócio do próprio tipo que está sendo declarado completo. *Fix:* qualificar a frase em §7/6.1 ("completos, exceto pelas pendências de Obras listadas em Q7–Q10") ou resolver essas questões antes de declarar completude.

## Downstream usability — adequate

Glossário, numeração de FR/UJ/SM e cross-references resolvem bem: FR-1 a FR-17 contíguos sem lacunas nem duplicatas, UJ-1 a UJ-4 idem, e referências cruzadas (FR-4→FR-16, FR-9→Questão Aberta 7, §5.1→FR-16/Questão 10, §10→§4.2/Questão 1) todas apontam para algo que existe. A maioria das seções usa termos do Glossário diretamente em vez de "ver acima".

O ponto fraco: das quatro UJs, **só a UJ-1 tem protagonista nomeado (Renata)**. UJ-2 é "colaborador do Comercial", UJ-3 é "administrador", UJ-4 é "autorizador" — papéis genéricos, não pessoas carregando contexto. O rubric trata isso como requisito de UJ bem formada, e é especialmente relevante aqui porque o produto tem múltiplos stakeholders com papéis distintos (solicitante, solicitante-com-exceção, aprovador calculado, autorizador nominal, administrador) — exatamente o cenário em que nomear o protagonista ajuda quem for escrever stories a manter o contexto da jornada.

### Findings
- **medium** Três de quatro UJs sem protagonista nomeado (§2.2, UJ-2/UJ-3/UJ-4) — Apenas UJ-1 (Renata) nomeia a pessoa; as demais usam papel genérico ("colaborador do Comercial", "administrador", "autorizador"). *Fix:* dar nome a pelo menos um protagonista por UJ, mesmo fictício, para manter consistência com UJ-1.

## Shape fit — adequate

O formato escolhido — UJs com protagonista + FRs numerados + glossário — é adequado para uma ferramenta interna multi-stakeholder (solicitante, solicitante-com-exceção, aprovador calculado, autorizador nominal, administrador), não superformalizado nem subformalizado no nível macro. Não há sinal de UJ forçada em papel de operador único, nem ausência de UJ em produto que precisaria delas.

A execução, porém, não sustenta totalmente o formato escolhido: a lacuna de nomeação de protagonistas (ver Downstream usability) pesa mais aqui precisamente porque o rubric considera UJs com protagonista nomeado "load-bearing" para esse tipo de produto multi-stakeholder — não é um detalhe cosmético neste caso específico, é o mecanismo que o PRD escolheu para carregar contexto entre papéis.

## Mechanical notes

- **Numeração de seção:** §7 "Escopo do MVP" contém subseções rotuladas "6.1"/"6.2" em vez de "7.1"/"7.2" — resquício da numeração de §6 (Non-Goals).
- **Deriva de glossário:** §3 define "Lote-Despesa"/"Lote-Obra" (com hífen), mas o corpo do texto (FR-15, §4.6, §7/6.1) usa "lote Despesa"/"lote Obra" (sem hífen) consistentemente. Cosmético, mas pode atrapalhar busca textual downstream.
- **Terminologia "usuário" vs. "colaborador":** FR-1 e FR-17 usam "usuário"; FR-3, FR-4 e o Glossário usam "colaborador". Parecem a mesma entidade, mas o Glossário não equaciona os dois termos explicitamente.
- **Termos sem entrada no Glossário:** `painel_snapshots` e rotas `/paineis/{painel}` (§4.8) são citados sem definição formal em §3.
- **Índice de Suposições:** roundtrip limpo — o único `[ASSUMPTION]` inline (§4.2 FR-3) está corretamente indexado em §10, sem entradas órfãs.
- **Continuidade de ID:** FR-1–FR-17, UJ-1–UJ-4, SM-1/SM-C1 contíguos, únicos, sem lacunas nem duplicatas. Cross-references verificadas resolvem corretamente.
