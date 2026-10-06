---
name: 'Review adversarial — ARCHITECTURE-SPINE FB_APU05'
type: architecture-review
subtype: adversarial
target: _bmad-output/planning-artifacts/architecture/architecture-FB_APU05-2026-10-06/ARCHITECTURE-SPINE.md
created: 2026-10-06
status: final
---

# Review Adversarial — Architecture Spine FB_APU05

## Método

Para cada par de unidades (épicos/áreas do backlog provisório — Solicitações, Motor de Aprovação, Fila do
Administrador, Exportação SAP, Identidade/CC, Cadastros Administráveis, Painéis, Autenticação, Infraestrutura),
tentei construir dois times hipotéticos que leem **apenas** o spine (AD-1..AD-10 + Consistency Conventions +
Capability Map), obedecem cada regra ao pé da letra, e ainda assim produzem artefatos incompatíveis entre si —
formato de dado, dono de escrita, contrato de erro, ou caminho de mutação de estado.

Cada subseção abaixo é um par construído concretamente: "Time A faz X (compatível com o AD), Time B faz Y
(também compatível com o AD), X e Y não interoperam." Isso é o critério de "buraco": a regra existente não
decide entre X e Y.

**Resultado: 10 buracos confirmados** (3 deles correspondem aos pontos de atenção pedidos — Aprovador,
corpo do 409, assinaturas de ExportadorSAP — e são tratados com mais profundidade). Nenhum par ficou livre de
pelo menos um ponto de divergência plausível; os 10 abaixo são os que produzem incompatibilidade de *runtime*
real, não apenas estilo.

---

## Achado #1 (CRÍTICO) — Formato do `Aprovador` retornado por `Resolver.Resolve`

**Par:** Motor de Aprovação × Fila do Administrador (e, por extensão, qualquer handler dos 5 tipos de
Solicitação que consome o resultado).

AD-2 fixa a assinatura `Resolver.Resolve(solicitacao) → (Aprovador, error)` e fixa que existem duas
implementações, `ResolverCalculado` (alçada por centro de custo / hierarquia) e `AutorizadorNominal`
(aprovador nomeado fixo). A convenção de nomenclatura diz apenas que o *nome* do tipo deve ser `Aprovador`
(termo do Glossário) — nada no spine define seus campos.

**Time A (constrói `aprovacao`/`ResolverCalculado` primeiro):** modela

```go
type Aprovador struct {
    ColaboradorID uuid.UUID // FK para COLABORADOR, sempre um gestor de CC existente
    Perfil        string    // "gestor_cc" | "nominal"
}
```

Isso funciona perfeitamente para `ResolverCalculado`, que sempre resolve para um `COLABORADOR` real (o gestor
do centro de custo, conforme o ER `COLABORADOR ||--|| CENTRO_CUSTO`). O snapshot (`APROVADOR_SNAPSHOT`) grava
uma FK.

**Time B (constrói `AutorizadorNominal` em paralelo, ou depois, lendo só o spine):** percebe que um aprovador
nominal fixo (ex.: "Diretor Financeiro aprova toda Obra acima de X") pode não ter necessariamente um
`ColaboradorID` estável de longo prazo na base importada da Senior (PRD trata CC/colaborador como carga
externa, AD-8) — e nada no spine proíbe um aprovador nominal ser uma pessoa/cargo fora da tabela
`COLABORADOR` (ex.: um cargo vago, um comitê, ou um e-mail de aprovação cadastrado só no FB_APU05). Modela

```go
type Aprovador struct {
    Nome  string
    Email string
}
```

**Por que não interoperam:** `APROVADOR_SNAPSHOT` é uma única tabela (`SOLICITACAO ||--|| APROVADOR_SNAPSHOT`
no ER, cardinalidade 1:1) que precisa de um esquema de colunas único para os dois casos. Se o Time A grava
`colaborador_id uuid NOT NULL` e o Time B precisa gravar um aprovador sem `ColaboradorID`, o INSERT do Time B
quebra a constraint do Time A — ou força um `colaborador_id` nulo que o Time A não previu (e cujo consumidor,
a Fila do Administrador exibindo "quem aprovou", não sabe se deve fazer `JOIN COLABORADOR` ou renderizar um
campo de texto livre). Pior: um terceiro time construindo o handler HTTP que expõe `GET
/solicitacoes/:id/aprovador` para o frontend não sabe, só pelo spine, se o JSON de resposta é
`{"colaborador_id": "...", "nome": "..."}` (resolvido via join) ou `{"nome": "...", "email": "..."}` plano —
são dois contratos de API incompatíveis, ambos "corretos" em relação ao AD-2 literal.

**AD faltante sugerido:** fixar `Aprovador` como struct única e **total** (cobre os dois casos de alçada), por
exemplo:
```go
type Aprovador struct {
    Tipo          string     // "gestor_cc" | "nominal" — enum fechado, igual ao Glossário
    ColaboradorID *uuid.UUID // presente quando Tipo == "gestor_cc"; nil quando nominal e não houver cadastro
    NomeExibicao  string     // sempre presente — fonte única de verdade para exibição na Fila do Administrador
}
```
e declarar explicitamente se `AutorizadorNominal` **sempre** resolve para um `COLABORADOR` cadastrado (caso em
que `AutorizadorNominal` vira só uma política de seleção diferente, mas o retorno ainda é sempre um
`ColaboradorID`) ou se admite nominal "fora de cadastro". Essa é a decisão de negócio que falta — o spine
delega a dois times a chance de responder diferente.

---

## Achado #2 (CRÍTICO) — Corpo da resposta HTTP 409 do lock otimista

**Par:** Fila do Administrador × Solicitações (qualquer handler que escreve em `solicitacoes` via
AD-5 — ex.: "aprovar/reprovar" na Fila vs. "editar antes de aprovar" em Solicitações).

AD-5 e a Consistency Convention fixam: `WHERE versao = :versao_lida`; zero linhas afetadas ⇒ HTTP 409; "sem
retry automático no servidor". Isso fixa **o código de status**, não o corpo.

**Time A (Fila do Administrador):** quer que o frontend, ao receber 409, recarregue a solicitação
automaticamente e mostre "este registro foi alterado por outra pessoa, veja a versão atual". Constrói:
```json
{ "erro": "conflito_versao", "versao_atual": 7 }
```
— precisa fazer um `SELECT versao FROM solicitacoes WHERE id = :id` extra após o UPDATE de 0 linhas para
devolver `versao_atual`.

**Time B (Solicitações, constrói o endpoint de edição em paralelo sem coordenar):** não vê necessidade de
outro SELECT (consideram caro/desnecessário) e devolvem o padrão de erro genérico do Handler Factory herdado
do FB_APU02 — que (por inspeção do sistema irmão, não documentada neste spine) é algo como:
```json
{ "message": "Conflito de versão", "code": "VERSION_CONFLICT" }
```
sem `versao_atual`.

**Por que não interoperam:** o frontend React (único, compartilhado entre os dois fluxos — mesma tela de
Fila consumindo ambos os tipos de mutação) precisa de **um** parser de erro 409. Se os dois formatos
coexistem, o componente de tratamento de conflito ou (a) é escrito duas vezes, duplicando a regra de UX de
"mostrar versão atual e oferecer recarregar", ou (b) silenciosamente não consegue oferecer o reload automático
em um dos dois fluxos porque o campo esperado não existe. Isso é exatamente o tipo de inconsistência que AD-5
diz querer evitar ("sem retry automático" implica que a UX de conflito é *manual e visível* — mas o spine não
garante que ela seja *visível da mesma forma* nos dois fluxos).

Observação adicional: o spine também não diz se o corpo de erro 409 é um caso específico ou uma instância do
contrato de erro genérico do Handler Factory (que não está documentado neste spine — ele é "herdado" do
FB_APU02 por referência implícita em AD-9, mas AD-9 só fala de *versões de stack*, não de *contratos de
payload*). Ou seja: o contrato de erro HTTP como um todo (não só o 409) é uma dependência tácita em um
documento externo ao repositório FB_APU05, não verificável a partir do spine.

**AD faltante sugerido:** um AD (ou extensão de AD-5) fixando o envelope de erro 409:
```json
{ "erro": "conflito_versao", "versao_atual": <int>, "versao_enviada": <int> }
```
e declarando que esse é um caso do envelope de erro genérico (que precisaria, por sua vez, estar citado
explicitamente — hoje está fora do spine).

---

## Achado #3 (ALTO) — Assinaturas de `GerarLoteDespesa` / `GerarLoteObra` subespecificadas

**Par:** Exportação SAP × Fila do Administrador (que dispara a exportação e exibe status) — e,
secundariamente, Exportação SAP × Motor de Aprovação (quem bate a versão/lock da solicitação exportada).

AD-3 fixa só os *nomes* dos métodos da interface `ExportadorSAP`. Nenhum parâmetro, tipo de retorno, nem
granularidade de lote é especificado.

**Time A (Exportação SAP, pensa em termos de "lote" — o próprio nome do método):** modela por agregação —
um lote cobre N solicitações aprovadas no período:
```go
GerarLoteDespesa(solicitacoes []SolicitacaoAprovada) (loteID uuid.UUID, error)
```
O método grava internamente N linhas em `exportacoes_sap` (uma por solicitação) referenciando o mesmo
`loteID`, e devolve só o identificador do lote. O arquivo `.xlsm` fica em disco/objeto, referenciado por path
dentro do registro de lote.

**Time B (Fila do Administrador, constrói o botão "Exportar" por linha da fila, pensa por solicitação):**
assume granularidade 1:1 porque é o que a tela pede ("exportar esta solicitação agora"):
```go
GerarLoteDespesa(solicitacaoID uuid.UUID) ([]byte, error)
```
devolvendo os bytes do arquivo para o handler fazer *stream* direto como download, sem tocar em
`exportacoes_sap` (deixando essa escrita para uma chamada separada que o Time B assume que *outro* código
fará).

**Por que não interoperam:** nem a assinatura (lote vs. unitário, retorno de ID vs. bytes) nem **quem grava
`exportacoes_sap`** está decidido pelo spine. AD-4 diz que só os módulos `aprovacao`/`exportacao` podem
escrever nesses campos — mas não diz **em qual dos dois métodos da interface** a escrita acontece, nem se o
handler precisa fazer uma chamada adicional (`RegistrarExportacao`) depois de receber o retorno. Se o Time B
constrói achando que `GerarLoteDespesa` só gera bytes e outra função grava o registro, e essa outra função
nunca é especificada em nenhum AD, a escrita em `exportacoes_sap` simplesmente não acontece — violação
silenciosa de AD-4 por omissão, não por um UPDATE direto proibido.

Isso é precisamente o que AD-3 diz querer evitar ("Lote-Despesa e Lote-Obra divergirem em como ... gravam
snapshot") — mas a regra escrita só amarra os *nomes* dos métodos, não o contrato que preveniria a divergência
que o próprio "Prevents" descreve. Há uma lacuna entre a intenção declarada do AD e a regra operacional.

**AD faltante sugerido:** especificar a assinatura completa, incluindo granularidade de lote e efeito
colateral obrigatório:
```go
type ExportadorSAP interface {
    // ids: 1..N solicitações aprovadas e elegíveis; retorna o lote criado.
    // Efeito colateral obrigatório: grava 1 linha em exportacoes_sap por solicitação,
    // status inicial "gerado", dentro da mesma transação da geração do arquivo.
    GerarLoteDespesa(ctx context.Context, ids []uuid.UUID) (LoteExportacao, error)
    GerarLoteObra(ctx context.Context, ids []uuid.UUID) (LoteExportacao, error)
}

type LoteExportacao struct {
    ID         uuid.UUID
    ArquivoRef string // path/URI do .xlsm — nunca bytes crus fora da interface
    Itens      []ExportacaoSAPStatus
}
```
e declarar que o handler HTTP nunca recebe bytes diretamente da interface de domínio — só referências —
fechando a divergência "retorna bytes" vs. "retorna ID".

---

## Demais pares incompatíveis encontrados

### #4 — Onde mora o dispatch `tipo_solicitacao → Resolver`?

**Par:** Solicitações (5 handlers, um por tipo) × Motor de Aprovação.

AD-2 diz "dispatch por `tipo_solicitacao`", mas não diz se o dispatch (a tabela de mapeamento tipo→estratégia)
mora dentro do pacote `aprovacao` (um `switch` central em `Resolver.Resolve` ou em uma fábrica
`NovoResolver(tipo)`) ou se cada um dos 5 handlers de solicitação decide, individualmente, qual implementação
chamar. Nada em AD-1 ("handlers nunca calculam aprovador... sempre delegam") proíbe um handler de decidir
*qual* `Resolver` delegar — só proíbe calcular o aprovador em si.

Dois times construindo handlers de tipos diferentes (ex.: "transferência" e "obras") poderiam cada um
hardcodar seu próprio `if tipo == "obras" { autorizadorNominal.Resolve(...) }`. Isso funciona para cada handler
isoladamente e não viola a letra de AD-1/AD-2 — mas duplica a regra de negócio "qual tipo usa qual estratégia"
em 5 lugares, exatamente a fragmentação que AD-2 diz existir para prevenir ("Prevents: cada um dos 5 tipos... implementar sua própria variação da regra de alçada" — aqui a variação não é no cálculo, mas na seleção
da estratégia, que o AD não cobre).

**Risco concreto:** quando a regra de negócio mudar (ex.: "obras" passa a usar `ResolverCalculado` acima de
certo valor), um time que só mexe no pacote `aprovacao` não vai encontrar as 5 ocorrências espalhadas nos
handlers.

### #5 — `CC_EXCECAO`: dois donos para a mesma entidade

**Par:** Identidade de Solicitante e CC (4.2, upload CSV, AD-8) × Cadastros Administráveis (4.7, Handler
Factory).

O ER mostra `COLABORADOR ||--o{ CC_EXCECAO : autoriza` — uma exceção de autorização de centro de custo (ex.:
alguém autorizado a solicitar fora do seu CC próprio). O Capability Map não cita `CC_EXCECAO` em nenhuma
linha; ela cabe tanto em "4.2 Identidade de Solicitante e CC" (mesma área temática, poderia vir junto no CSV
da Senior) quanto em "4.7 Cadastros Administráveis" (é, por definição, um cadastro mantido por um
administrador, via Handler Factory). AD-8 só fala de carga da base de colaborador/CC via upload — não diz se
exceções também vêm do CSV da Senior ou se são mantidas manualmente no FB_APU05.

**Time A (Identidade/CC):** assume que `CC_EXCECAO` é parte do arquivo CSV da Senior (mesma carga de AD-8) —
sobrescrita completa a cada upload manual.

**Time B (Cadastros Administráveis):** constrói um CRUD completo (`POST/PUT/DELETE
/admin/cc-excecoes`) seguindo o padrão Handler Factory, achando (razoavelmente, dado o nome "cadastro
administrável") que é dado mantido pela aplicação.

**Por que não interoperam:** se os dois existirem, o próximo upload CSV do Time A apaga/sobrescreve exceções
que administradores cadastraram manualmente pelo CRUD do Time B (ou vice-versa, dependendo da ordem de
implantação) — perda silenciosa de dados, sem que nenhum dos dois AD-8 ou o regime "Handler Factory" genérico
tenha sido violado.

### #6 — Claim/grupo do Keycloak que carrega o perfil

**Par:** Autenticação (4.1, AD-6/AD-7) × Identidade/CC (4.2 — quem provisiona usuários/perfis no Keycloak).

AD-7 diz "Middleware de perfil deriva exclusivamente da claim de sessão" — mas o próprio spine lista, em
Deferred, "Tenant/claims/grupos exatos do Keycloak para este módulo... (Questão Aberta 2)" como não resolvido.
Isso significa que, hoje, dois times podem literalmente adotar claims diferentes:

**Time A (middleware/Autenticação):** replica o FB_APU02 ao pé da letra (AD-6 manda "replicar o fluxo já
validado") e lê `realm_access.roles` procurando `"administrador"`.

**Time B (Identidade/CC ou quem provisiona usuários):** assume (porque o FB_APU05 é um módulo novo, "single
company", AD-7) que o jeito certo é um grupo dedicado `/FB_APU05/administradores`, já que roles de realm são
globais e compartilhadas com FB_APU02 — poderia inadvertidamente promover/rebaixar usuários do sistema irmão.

**Por que não interoperam:** usuários cadastrados pelo Time B com grupo `/FB_APU05/administradores` nunca
terão a role de realm que o Time A verifica — todo mundo cai como "solicitante" por padrão, uma falha de
autorização silenciosa (não um crash), exatamente o tipo de erro que AD-6 cita como precedente já corrigido no
FB_APU02 ("esquecer a checagem de `email_verified`") — aqui é uma nova variante da mesma classe de risco, e o
próprio spine já sinaliza (em Deferred) que a decisão não foi tomada.

### #7 — Paginação sem total: Painéis quebra a convenção por necessidade própria

**Par:** Painéis e Indicadores (4.8) × qualquer épico de listagem (Fila do Administrador, Cadastros
Administráveis).

A Consistency Convention fixa `{items, pagina, tamanho}` sem `total` nem cursor "(PRD §5.2)" para listas. O
Capability Map marca 4.8 como "— (escopo a definir, PRD §7.3)", ou seja, **nenhum AD governa Painéis**.

**Time A (Fila/Cadastros):** segue a convenção à risca — toda lista paginada devolve `{items, pagina,
tamanho}`.

**Time B (Painéis):** precisa de contagens agregadas para os indicadores (ex.: "32 solicitações pendentes
este mês") — sem `total` disponível nos endpoints de lista existentes, e sem AD que diga como Painéis deve
buscar dado agregado, o time razoavelmente decide que seu próprio endpoint de indicadores **é** um caso
especial e devolve `{total, series: [...]}`, quebrando a convenção porque, estritamente, não é uma "lista" no
sentido do PRD §5.2 — é uma agregação. Nenhuma regra escrita impede essa leitura, mas o resultado é dois
estilos de contrato de resposta na mesma API (paginado-sem-total vs. agregado-com-total) sem um AD que
reconheça a distinção.

### #8 — Nome dos parâmetros de requisição de paginação

**Par:** Fila do Administrador × Cadastros Administráveis.

A convenção fixa o **formato da resposta** (`{items, pagina, tamanho}`) mas não o nome dos **parâmetros de
query** da requisição. Dois times, cada um olhando só a resposta, poderiam escolher `?pagina=1&tamanho=20`
(espelhando os nomes da resposta, em português, mais consistente) ou `?page=1&page_size=20` (seguindo convenção
HTTP/Go mais comum, já que a camada HTTP "segue convenção Go padrão" conforme a linha de Naming). Um cliente
frontend único que lista tanto a Fila quanto os Cadastros precisaria de dois adaptadores de query string para
a mesma "forma" de paginação.

### #9 — Ordem de escrita entre exportação e lock otimista (quem bate `versao` por último)

**Par:** Exportação SAP × Motor de Aprovação / Fila do Administrador.

AD-4 isola as escritas de `aprovador_snapshot`, `versao` e status de `exportacoes_sap` nos módulos de domínio,
mas trata os três como alvos equivalentes de uma mesma regra. AD-5 amarra lock otimista a escritas em
`solicitacoes` via `versao`. Não fica definido se **gerar uma exportação SAP também incrementa `versao`** da
solicitação (mudando seu estado de "aprovada" para "exportada", por exemplo) nem, se incrementa, se o
`exportacao` precisa primeiro ler a `versao` atual e aplicar o mesmo `WHERE versao = :versao_lida` que AD-5
exige para qualquer escrita em `solicitacoes`.

**Time A (Motor de Aprovação):** ao aprovar, grava `aprovador_snapshot` e incrementa `versao` respeitando o
lock otimista (porque aprovar é, na leitura do time, uma escrita em `solicitacoes` coberta por AD-5).

**Time B (Exportação SAP):** ao gerar o lote, entende que só está gravando em `exportacoes_sap` (uma tabela
*child*, não `solicitacoes` diretamente) — então não lê nem verifica `versao` antes de marcar a solicitação
como "exportada" em algum campo de status dentro de `solicitacoes`, escrevendo por UPDATE incondicional (ainda
"exclusivo do módulo exportacao", portanto não viola AD-4 literalmente, mas ignora AD-5 porque, para esse
time, AD-5 é "regra da Fila do Administrador").

**Por que não interoperam:** uma corrida entre um admin editando a solicitação (passando pelo lock) e o
job/chamada de exportação (ignorando o lock) pode fazer a exportação sobrescrever uma edição concorrente sem
detectar conflito — o cenário exato que AD-5 existe para prevenir, mas appliable apenas a um subconjunto de
escritores que o spine não delimita explicitamente.

### #10 — Health-check: contrato não especificado entre apps e pipeline

**Par:** Infraestrutura (AD-10) × qualquer serviço implantado (backend Go, frontend estático).

AD-10 menciona "deploy... com health-check" mas não especifica path (`/health` vs `/healthz`), método, nem
formato de corpo/condição de sucesso (200 vazio vs. `{"status":"ok"}` vs. checagem de conectividade com
Postgres/Keycloak). Isso é de menor gravidade porque hoje há um único backend monolítico, mas, se o
Time de Infraestrutura (que escreve o workflow do GitHub Actions e a config do Coolify/Traefik) assume um path
herdado do FB_APU02 sem confirmar que o Time de backend do FB_APU05 implementou exatamente esse path e
semântica (ex.: FB_APU02 pode checar dependências externas e falhar o healthcheck se o Keycloak estiver fora,
levando a uma política diferente de rollout), o deploy pode ficar "verde" estruturalmente falso ou
"vermelho" por falha de dependência não essencial — diverge silenciosamente do comportamento esperado.

---

## Resumo de severidade

| # | Par | Severidade | Tipo de buraco |
| --- | --- | --- | --- |
| 1 | Motor de Aprovação × Fila do Administrador | Crítico | Formato de dado compartilhado (`Aprovador`) |
| 2 | Fila do Administrador × Solicitações | Crítico | Contrato de erro divergente (409) |
| 3 | Exportação SAP × Fila do Administrador | Alto | Assinatura/efeito colateral de interface subespecificado |
| 4 | Solicitações × Motor de Aprovação | Médio | Duplicação de regra de dispatch |
| 5 | Identidade/CC × Cadastros Administráveis | Alto | Dois donos de escrita da mesma entidade (`CC_EXCECAO`) |
| 6 | Autenticação × Identidade/CC | Crítico | Claim/grupo de perfil não decidido (falha de autorização silenciosa) |
| 7 | Painéis × épicos de listagem | Médio | Convenção de paginação quebrada por necessidade de agregação |
| 8 | Fila do Administrador × Cadastros Administráveis | Baixo | Nome de parâmetros de query não fixado |
| 9 | Exportação SAP × Motor de Aprovação/Fila | Alto | Caminho de mutação concorrente sem lock consistente |
| 10 | Infraestrutura × serviços | Baixo | Contrato de health-check não especificado |

**Total: 10 pares incompatíveis identificados**, sendo 3 classificados como Crítico (#1, #2, #6), capazes de
produzir falhas silenciosas de dado ou de autorização em produção, não apenas inconsistência estética.

## Recomendação consolidada

Os buracos #1, #2, #3, #5, #6 e #9 justificam ADs novos ou extensões formais ao spine antes do início da
implementação dos épicos correspondentes (Motor de Aprovação, Exportação SAP, Fila do Administrador,
Identidade/CC, Autenticação) — nessa ordem de prioridade, já que #1/#2/#6 têm potencial de falha silenciosa
em produção (dado perdido ou autorização incorreta), não apenas divergência de estilo. Os buracos #4, #7, #8 e
#10 podem ser resolvidos por convenção adicional de menor peso (uma linha na tabela "Consistency Conventions"
já resolveria #7 e #8) sem necessidade de um AD dedicado.
