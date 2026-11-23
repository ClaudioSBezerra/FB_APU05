# API SAP — Trava orçamentária (saldo por CC × conta × mês)

> Documentação recebida do Time TI SAP em 23/11/2026 (Maria Eduarda Sakovitz). Análise para uso no FB_APU05.

## O que é

Serviço **OData V4** (`ZSB_TRAVA_ORCAMENTARIA`, entidade `Trava_orcamentaria`), **somente GET**. Para cada `objnr` (objeto CO) +
`kstar` (conta) + `gjahr` (exercício) devolve, **mês a mês (1–12)**: `plan` (planejado), `real` (realizado), `comp`
(comprometido), `disposto` (= real + comp), `disponivel` (= plan − disposto) e `disp_acum` (disponível **acumulado** até o mês).
Fonte: tabela COSP. **Para validar um documento no mês *n*, usar `disp_acum_n`** (considera sobra/estouro dos meses anteriores).

| | QAS | PRD |
| --- | --- | --- |
| URL | `https://vhfcoqasci.sap.ferreiracosta.com:44300/sap/opu/odata4/sap/zsb_trava_orcamentaria/srvd_a2x/sap/zsd_trava_orcamentaria/0001/` | idem com `vhfcoprdci` |
| Auth | Basic (usuário SAP — **a confirmar**) | idem |

Consulta: `GET …/Trava_orcamentaria?$filter=objnr eq 'KSFC010010026040' and kstar eq '4210100001' and gjahr eq '2026'`
(resposta `{"@odata.count":1,"value":[{…}]}`; sem lançamentos = `value: []`; por chave: `Trava_orcamentaria(objnr='…',kstar='…',gjahr='…')` → 404 se não existir).
**Sempre informar os três campos**: sem filtro agrega toda a COSP do mandante e dá timeout (504).

## Mapeamento com o nosso cadastro (verificado em 23/11/2026)

| Campo da API | No FB_APU05 |
| --- | --- |
| `objnr` | `'KS' + 'FC01' + lpad(centros_custo.codigo, 10, '0')` — ex.: CC `10026040` → `KSFC010010026040` (bate com o exemplo da doc). A área de controladoria é `FC01` nos 4.771 centros do QAS |
| `kstar` | `contas.codigo` (plano 0050, 10 dígitos) |
| `gjahr` | ano de `solicitacao_lancamentos.mes` (já existe a regra de exercício único na transferência) |
| mês *n* | `extract(month from mes)` |

Os 30 centros antigos de 4 dígitos (ex.: `6000`) **não existem no SAP**: não têm saldo (a consulta nem deve ser feita).

## Onde usar

| Solicitação | Uso | Regra proposta |
| --- | --- | --- |
| **Transferência** — linhas de **origem** (retirada) | **Trava**: a soma retirada de cada (CC, conta) até o mês *n* não pode passar de `disp_acum_n` | bloquear (ou avisar — ver decisão 1) |
| Transferência — linha de **destino** | Informativo (mostra o saldo do destino) | sem trava |
| Inclusão / Inclusão SFC (aumento de verba) | Informativo para o solicitante e para o **aprovador** (quanto já está disponível/estourado no CC) | sem trava (é pedido de verba nova) |
| Imobilizado e Obras | não têm CC/conta/mês | fora |
| Tela do aprovador | reconsultar o saldo **na hora da decisão** (o saldo muda) | mostrar; reconferir ao aprovar |

Várias origens do mesmo (CC, conta) em meses diferentes: validar mês a mês de forma acumulada
(retirado até o mês *n* ≤ `disp_acum_n`), não cada linha isolada.

## Pontos de atenção técnicos

1. **Rede:** o host responde daqui (HTTP 401 sem credencial), mas o servidor da **AWS não alcança `vhfcoqasci:44300`**
   (timeout). O gateway Kong (`10.131.1.80:8000`) é alcançável e **não tem rota** para esta API (404). Falta: rota no Kong
   (preferível, mesmo padrão `apikey` das outras) **ou** liberação de firewall AWS → SAP:44300.
2. **Credencial:** Basic com usuário SAP (a confirmar) + autorização S_SERVICE no serviço. Pelo Kong seria `apikey`.
3. **V4, não V2:** envelope `value`/`@odata.count` (o cliente `internal/sapodata` é V2: `d.results`). Pede um cliente pequeno novo.
4. **Sem autorização por objeto** no SAP: qualquer credencial enxerga qualquer CC. O **nosso** servidor é quem decide quem pode
   consultar o quê (origem: CCs do solicitante; aprovador: o CC da solicitação) — nunca expor um proxy aberto.
5. **Latência/timeout:** consulta filtrada por chave; timeout curto (~5–8 s), poucas chamadas em paralelo, cache de poucos segundos.
6. **Moeda de controlling** e exercício = ano civil (mês *n* = coluna `WTGn`).

## Perguntas para o Time TI SAP (Duda)

1. Rota no **Kong** (`sap-qas-budget-read`?) com `apikey` — ou liberar AWS → `vhfcoqasci/vhfcoprdci:44300`?
2. Qual usuário/arranjo (S_SERVICE) usaremos? Mesmo `API_USER` do Kong?
3. O `plan` (`WRTTP=1`) soma **todas as versões** de planejamento da COSP (`VERSN`)? Se houver mais de uma versão, o planejado pode
   vir duplicado. Qual versão vale?
4. O planejado do SAP é o mesmo orçamento aprovado (Orçamento TI e demais áreas)? Quando uma transferência aprovada é executada
   no SAP, o `plan` já reflete na hora?
5. O `comp` (`WRTTP` 21/22) inclui requisições e pedidos ainda sem fatura? (para o "disponível" refletir o que de fato já foi pedido)
6. Há outros tipos de objeto além de CC (`KS`) que precisaremos (ordens `OR`)? Hoje só CC.

## Decisões do usuário (pendentes)

1. **Trava dura ou aviso?** Bloquear a transferência quando a origem não tem saldo, ou só avisar e deixar o aprovador decidir?
2. **SAP fora do ar:** impedir o envio, ou deixar enviar marcado "saldo não verificado" e conferir de novo na aprovação?
3. Mostrar o saldo do **destino** ao solicitante, ou só da origem (que ele já pode ver)?
