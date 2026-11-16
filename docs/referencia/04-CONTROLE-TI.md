# Controle TI — módulo de despesas de TI

Substitui a planilha **FC FY27 Forecast** + painel HTML de 2026 (Welton, Administrativo FCx Labs).
Mostra **orçado × realizado × a lançar × projeção** por empresa, filial, centro de custo, tipo de
despesa e conta. Regras de negócio completas: o documento "Despesas TI — como funciona em 2026 e como
será em 2027" (Welton, 16/11/2026).

## Acesso

Menu **Controle TI** na barra lateral, **só para quem tem o interruptor ligado** em
*Gestão de usuários → coluna Controle TI* (mesmo molde do interruptor Aprovador). Hoje: Welton e
Wanessa. Sem o interruptor, o menu não aparece e `/api/ti/*` responde 403. A permissão é lida do banco
a cada chamada: ligar ou desligar vale na hora, sem sair e entrar de novo.

## O que existe

| Aba | O que faz |
| --- | --- |
| **Painel** | Os gráficos e análises do painel HTML de 2026, completos: **7 telas** (Visão geral, Realizado, Fornecedores, Lançamentos e as bases Orçado, A lançar e Projeção), ~35 visualizações, filtros, "Pontos de atenção" automáticos, modais e tema claro/escuro |
| Lançamentos | Lista nota a nota do sistema, com busca e filtros (vai virar a tela de digitação na fase 2) |
| Importar planilha | Carga do `.xlsx` (abas Parâmetros, Orçamento Forecast, Lançamentos Mensais) |

### Como o painel foi levado para o sistema

O painel de 2026 tem ~100 KB de lógica (cálculo, gráficos SVG, análises de atenção, modais). Reescrevê-lo
em React arriscava mudar visual e números. Ele foi **levado como está**: `frontend/public/painel-ti/`
(`index.html`, `painel.css`, `painel.js`), alimentado pela API (`GET /api/ti/modelo`). O que mudou em
relação ao arquivo original, e só isso:

- os dados deixaram de ser embutidos: chegam por `postMessage` (o painel avisa `fcx-pronto` e recebe
  `fcx-modelo`); o botão "Carregar planilha" virou "Importar planilha" e abre a aba de importação;
- sem leitor de Excel dentro do painel (a importação é feita pelo servidor) nem arrastar-e-soltar;
- fonte Manrope embutida no CSS (o original usava o Google Fonts, bloqueado na rede da FC).

**Segurança.** O painel roda num `<iframe sandbox="allow-scripts">` (origem opaca: não enxerga o
token do sistema nem chama a API) e o nginx aplica ao caminho `/painel-ti/` uma CSP sem rede
(`connect-src 'none'`, scripts só do próprio caminho). Essa é a **única exceção** ao
`X-Frame-Options: DENY` do servidor (`SAMEORIGIN`, só nesse caminho). Há motivo para isso: os nomes de
fornecedor e as descrições vêm de planilha/SAP e o código do painel monta HTML com eles.

**Alterar o painel:** editar os 3 arquivos de `frontend/public/painel-ti/` (são a fonte da verdade; o
`temp/Dashboard_FY27_Forecast_2.html` era só a origem). Os campos que o painel lê do modelo são os de
`internal/controleti/modelo.go` — não renomeie sem ajustar o `painel.js`.

**Limitações conhecidas.** A preferência de tema não é lembrada (o sandbox não tem `localStorage`); o
painel não tem exportação nem impressão (o original também não tinha); os filtros do painel não são
compartilhados com as outras abas.

## Regras implementadas (iguais às do painel de 2026)

- **Realizado** = lançamentos com classificação "Realizado" (status → classificação pela aba Parâmetros);
  **a lançar** = os demais.
- **Projeção**: mês fechado = realizado; mês aberto = maior valor entre o orçado e (realizado + a lançar),
  por divisão, CC e conta.
- **Sobra** = orçado − projeção. **Saldo disponível** = orçado − realizado − a lançar.
- **Divisão e CC saem do CC SAP**: divisão = tudo menos os 4 últimos dígitos, CC = os 4 últimos; `IMOB` e
  `N/D` tratados à parte. Descrições de CC, conta, divisão e tipo vêm **só** das tabelas de parâmetros.
- **Empresa** (grupo): coluna Empresa da tabela de divisões; sem ela, pela faixa da divisão
  (91–1009 = Ferreira Costa, 4016 = RetailX, "IMPORTADORA…" = Importadoras). `IMOB` fica em Ferreira Costa.
- **CC 6600 (Lei do Bem)** fica fora do painel.
- **Mês fechado automático**: do dia 6 em diante o mês anterior está fechado (o SAP aceita lançamento
  retroativo até o dia 5). Um ajuste manual em `ti_config` (`ultimo_mes_fechado`, AAAA-MM) vence a regra.

## Como foi validado

O parser original do painel (JavaScript) serve de gabarito. `internal/controleti/oraculo_test.go` compara
o modelo montado em Go com o dele, **campo a campo** (694 linhas de orçamento, 1.444 lançamentos, 162
linhas de projeção, 48 meses) — e um teste com o gabarito adulterado prova que o teste de fato reprova.
Como o painel é o mesmo código do original, os números dele só dependem do modelo, que é o que o teste
acima valida. Janeiro a setembro/2026 batem **ao centavo**; outubro difere
porque a planilha enviada não traz os lançamentos de outubro que o painel HTML recebeu por fora.

Para repetir (precisa da planilha real, que **não** fica no repositório):

```bash
TI_PLANILHA=".../FC FY27 Forecast - ajustado.xlsx" TI_ORACULO=.../oraculo.json \
TEST_DATABASE_URL='postgres://postgres:x@localhost:55499/postgres?sslmode=disable' \
  go test ./internal/controleti -v
```

## Dados e fontes

Tabelas `ti_*` (migration 025). Cada linha de orçamento e de lançamento tem uma **fonte**:

| Fonte | Origem | Reimportar a planilha |
| --- | --- | --- |
| `planilha` / `forecast_xlsx` | importação do `.xlsx` | substitui (numa transação; se falhar, nada muda) |
| `manual` | digitado no sistema (fase 2) | não toca |
| `sap_api` | recebido do SAP por API | não toca |

As tabelas de parâmetros (`ti_param_*`) vêm sempre da planilha e são trocadas por inteiro.
Os campos derivados (divisão, CC, descrições, tipo, classificação) **não são gravados**: são calculados a
cada leitura a partir do CC SAP, da conta, do status e dos parâmetros — como as fórmulas da planilha.

`ti_param_*` é independente do cadastro mestre (`divisoes`/`centros_custo`/`contas`): aqui "divisão" é o
código de filial do SAP (1002, 4016…) e o CC tem 4 dígitos. Ligar os dois fica para depois da carga SAP
(Duda).

## Fase 3 — KSB1/KSB2 e Conferência SAP (migration 026)

**Importação.** A aba *Importar planilha* aceita até 6 arquivos de uma vez (Forecast + KSB1 + KSB2). O tipo
é detectado pelo conteúdo (Forecast tem a aba "Parâmetros"; KSB1 tem "Documento de compras" + "Data de
lançamento"; KSB2 tem "Nº doc.de referência" + "Quantidade total"). Ordem de processamento: Forecast →
KSB1 → KSB2; cada arquivo tem a sua transação e o seu resultado. A planilha Forecast deve ser a completa
(jan–out); a KSB1 deve ser a acumulada do ano.

- **KSB1** (realizado): só linhas com pedido entram; substitui apenas os meses de lançamento que cobre.
- **KSB2** (saldo em aberto): só linhas de detalhe (as de total, sem classe de custo, são descartadas);
  cada carga substitui tudo. O saldo aparece como coluna "Saldo SAP" em Lançamentos.

**Conferência.** Chave = pedido + CC SAP + mês. Tolerância de R$ 0,015. Base = linhas da planilha com
classificação "Realizado" (LANÇADO e ESTORNADO) nos meses cobertos pela KSB1. As 9 situações: Conferido,
Lançado em outro mês, Valor diferente, Rateio diferente, Pendente de lançamento, Pedido não encontrado no
SAP, Lançado só no SAP, Estorno/soma negativa e Verificar. "Verificar" fica fora da base do percentual.
Endpoint: `GET /api/ti/conferencia` (exige o interruptor Controle TI).

**Regra de 2027 (SAP como fonte oficial do realizado).** A partir de `ti_config.sap_oficial_desde`
(padrão 2027-01), nos meses fechados cobertos pela KSB1, as linhas Realizado da planilha **com pedido**
são substituídas pela soma da KSB1 (só pedidos nossos); linhas sem pedido, IMOB e A LANÇAR ficam. Mês
fechado sem KSB1 nessa época aparece como **provisório** (aviso na tela). Os números de 2026 não mudam: o
SAP só confere. Ainda não foi exercitada com dados reais de 2027.

**Validação.** Com os arquivos reais de jan–set/2026: 71,1% conferido sobre 1.179 itens (838). O
documento do Welton traz 74% sobre 1.158; a diferença vem da versão da planilha (aqui 1.102 grupos), a
ordem de grandeza é a mesma.

## Próximas fases

2. **Lançamentos no sistema** (digitar nota, status CRIAR PEDIDO → LANÇADO, anexo da NF), com atualização
   por chave de conciliação (Excel/API), e telas de Realizado, Fornecedores e Bases de apoio.
4. **Notas e saldos por API** do SAP (Duda), no lugar do upload manual.

## Pontos em aberto

- Orçamento TI 2027 ainda não chegou: entra como outra fonte de orçamento (`orcamento_2027`).
- Tabela de fornecedores (uma grafia por fornecedor) vem da aba Parâmetros; a planilha de 2026 não tem
  essa tabela (0 fornecedores importados).
- Outubro/2026: faltam os lançamentos de outubro no arquivo para os números fecharem com o painel HTML.
