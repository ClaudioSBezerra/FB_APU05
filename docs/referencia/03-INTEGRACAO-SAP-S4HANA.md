# Integração SAP S/4HANA — Centros, Centro de Custo e Contas Contábeis

> Escrito para: Maria Eduarda (Duda), ABAP — pedido do Marlos (21/10/2026) para o FB_APU05 (Controladoria/Ferreira Costa) passar a receber Centro, Centro de Custo e Conta Contábil via API do SAP S/4HANA, em vez de carga manual (CSV).

## Contexto rápido

O FB_APU05 é o sistema de gestão de solicitações de movimentação de orçamento/investimento da Ferreira Costa (ver `02-REQUISITOS-NEGOCIO-ENVIO-TI.md`). Hoje três cadastros mestres do sistema — Centro de Custo, Conta Contábil e (a partir de agora) Centro — são carregados manualmente via CSV por um administrador. O pedido do Marlos é substituir isso por uma integração automática com o S/4HANA.

**Importante — dois sistemas diferentes:** o FB_APU05 já tem uma integração rodando com um sistema chamado **SFC** (Oracle, servidor FCCORP) — usada hoje para Colaboradores e Fornecedores. O **SFC não é o SAP S/4HANA**: é um sistema legado separado. Esta especificação é sobre uma integração **nova**, com o S/4HANA de verdade, para três cadastros que o SFC não cobre.

**Modelo de integração: o SAP empurra, não nós puxamos.** Diferente da integração com o SFC (onde temos um conector Python nosso que lê o Oracle periodicamente), aqui o lado SAP é quem deve chamar o FB_APU05 — por exemplo, um job ABAP agendado (ou um step de uma iFlow, se vocês usarem SAP CPI) que lê os dados e faz um `POST` HTTP para cada uma das 3 rotas abaixo.

## Autenticação

Todas as rotas usam o mesmo header `X-API-Key` que já protege a integração do SFC — **uma única chave por instância do FB_APU05**, não uma por cadastro. Ela é gerada na tela **"ERP Bridge — Credenciais"** do FB_APU05 (perfil administrador → botão "Gerar nova API Key") e precisa ser combinada com quem administra o FB_APU05 (Claudio) por um canal seguro — não por e-mail/chat em texto plano.

```
X-API-Key: <chave gerada na tela ERP Bridge — Credenciais>
Content-Type: application/json
```

Sem a chave (ou com uma chave errada), toda rota devolve `401`.

## As 3 rotas

Base: `https://controladoria.fbtax.cloud` (produção).

### 1. `POST /api/erp-bridge/centros/sincronizar`

"Centro" aqui é o conceito SAP de **Plant/Werk** (filial/local físico) — um cadastro **novo** no FB_APU05 (tabela `centros`), sem nenhuma relação com "Centro de Custo" (que já existe há mais tempo como `centros_custo`). Não confundir os dois.

```json
{
  "centros": [
    { "codigo": "1001", "nome": "CD Jaboatão" },
    { "codigo": "1002", "nome": "Loja Boa Viagem" }
  ]
}
```

| Campo | Obrigatório | Observação |
|---|---|---|
| `codigo` | sim | chave de negócio (única) — sugestão: código do Plant (`WERKS`) |
| `nome` | sim | descrição do Plant |

### 2. `POST /api/erp-bridge/centros-custo/sincronizar`

```json
{
  "centros_custo": [
    { "codigo": "CC0001", "nome": "Diretoria Financeira", "divisao_codigo": "01", "filial": "0001" },
    { "codigo": "CC0002", "nome": "Controladoria", "divisao_codigo": "01", "filial": "" }
  ]
}
```

| Campo | Obrigatório | Observação |
|---|---|---|
| `codigo` | sim | chave de negócio (única) |
| `nome` | sim | descrição do centro de custo |
| `divisao_codigo` | sim | código de uma "divisão" já cadastrada no FB_APU05 — **perguntar ao Claudio** qual o equivalente SAP disso antes de mapear (pode ser Área de Controladoria, Grupo de Centro de Custo, ou outra coisa — não é um conceito padrão do SAP, é nosso) |
| `filial` | não | texto livre, pode vir vazio |

Se `divisao_codigo` não bater com nenhuma divisão já cadastrada no FB_APU05, a linha é **ignorada** (não derruba a sincronização inteira) e aparece no contador `ignorados` da resposta.

### 3. `POST /api/erp-bridge/contas/sincronizar`

```json
{
  "contas": [
    { "plano": "BIFC", "codigo": "1.01.001", "nome": "Caixa" },
    { "plano": "SFC", "codigo": "2.02.002", "nome": "Fornecedores Nacionais" }
  ]
}
```

| Campo | Obrigatório | Observação |
|---|---|---|
| `plano` | sim | só aceita exatamente `"BIFC"` ou `"SFC"` (outro valor = linha ignorada) — **perguntar ao Claudio** o que esses dois planos significam no contexto de vocês antes de decidir de onde tirar cada um |
| `codigo` | sim | código da conta — chave de negócio é o par `(plano, codigo)` |
| `nome` | sim | descrição da conta |

## Resposta (as 3 rotas, mesmo formato)

```json
{ "criados": 10, "atualizados": 3, "ignorados": 1, "total": 14 }
```

- `200` — sempre que o corpo é válido, mesmo que algumas linhas tenham sido ignoradas.
- `400` — corpo vazio ou mal formado (JSON inválido, ou chave `centros`/`centros_custo`/`contas` ausente/vazia).
- `401` — `X-API-Key` ausente ou inválida.

É upsert: `codigo` (ou `plano`+`codigo`, no caso de contas) já existente atualiza o registro; novo cria. Pode mandar sempre a base inteira a cada sincronização — não precisa calcular delta do lado SAP.

## O que falta confirmar do lado SAP

Isso é decisão/levantamento seu, não um requisito fechado — são só sugestões de ponto de partida (views/APIs padrão do S/4HANA), a confirmar contra o que está de fato liberado no ambiente da Ferreira Costa:

- **Centro (Plant)**: CDS view `I_Plant` — não há uma API pública padrão tão direta quanto as outras duas; pode precisar de uma CDS view customizada em cima dela, exposta via Communication Arrangement.
- **Centro de Custo**: CDS view `I_CostCenter` / API `API_COSTCENTER_SRV`.
- **Conta Contábil**: CDS view `I_GLAccountInChartOfAccounts` / API `API_GLACCOUNTINCHARTOFACCOUNTS_SRV`.

Qualquer dúvida sobre o lado FB_APU05 (formato, erros, teste), falar com o Claudio.
