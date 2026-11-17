# API SAP — Cost Center Read (centros de custo)

> Primeira documentação de API recebida do Time TI SAP (17/11/2026). Resumo para o FB_APU05; o
> documento original (com o Swagger completo, `OP_API_COSTCENTER_SRV_0001.yaml`) fica com o time do SAP.

## O que é

Serviço standard `API_COSTCENTER_SRV` (OData V2, A2X), **somente leitura** (GET e `$batch`).
Dois recursos: `A_CostCenter` (cadastro) e `A_CostCenterText` (nome/descrição por idioma), ligados
por `to_Text` / `to_CostCenter`.

| Ambiente | URL base |
| --- | --- |
| Homologação | `https://vhfcodevci.sap.ferreiracosta.com:44300/sap/opu/odata/sap/API_COSTCENTER_SRV` |
| Produção | `https://<HOST_PRD>:<PORTA>/sap/opu/odata/sap/API_COSTCENTER_SRV` (host ainda não informado) |

- **Autenticação:** Basic ou OAuth 2.0 (authorization code); a ficha técnica cita também x509. O
  usuário/cliente precisa de um **arranjo de comunicação** com autorização para o serviço (sem ele: 401/403).
- GET não precisa de token CSRF. Mandar `Accept: application/json` (senão vem XML/Atom).

## Como consultar

- Listar: `GET /A_CostCenter?$filter=...&$select=...&$top=&$skip=&$inlinecount=allpages&$expand=to_Text`
- Por chave: `/A_CostCenter(ControllingArea='A000',CostCenter='0000001000',ValidityEndDate=datetime'9999-12-31T00:00')`
- Textos: `GET /A_CostCenterText?$filter=Language eq 'PT' and ControllingArea eq 'A000'`
- Resposta: `d.results[]` (e `d.__count`). Datas voltam como `/Date(ms)/`.
- Erros no padrão OData V2 (`error.code`, `error.message.value`): 400 filtro/seleção inválidos, 401, 403, 404, 405, 500.

## Regras que afetam a nossa carga

- **Chave composta:** `ControllingArea` + `CostCenter` (10 posições, com zeros à esquerda) + `ValidityEndDate`.
- **Validade por período:** o mesmo código pode ter vários registros. Vigente = `ValidityEndDate` 9999-12-31;
  para pegar só os vigentes: `$filter=ValidityEndDate ge datetime'AAAA-MM-DDT00:00'`.
- **Idioma:** filtrar `Language eq 'PT'` para não duplicar o nome. `CostCenterName` tem 20 posições e
  `CostCenterDescription` 40.
- **Bloqueios:** campos de 1 caractere (`IsBlkd…`, `IsBlocked…`); preenchido (normalmente `X`) = ativo.
- Volumes grandes: `$select` + `$top/$skip`, ou `$batch`.

## Campos úteis para o orçamento

`CostCenter`, `ControllingArea`, `CompanyCode` (empresa), `BusinessArea` (divisão), `ProfitCenter`,
`CostCtrResponsibleUser` / `CostCtrResponsiblePersonName` (responsável), `CostCenterCategory`,
`Department`, `ValidityStartDate/EndDate`, bloqueios, `to_Text` (nome).

## Pontos a decidir antes de implementar

1. **Quem inicia a conexão.** O doc `03-INTEGRACAO-SAP-S4HANA.md` pedia que o **SAP empurrasse** os dados
   (rotas nossas com `X-API-Key`); esta API é de **leitura** — nós é que chamamos o SAP. Confirmar com o Time
   TI SAP/Duda qual modelo vale para centros de custo.
2. **Rede.** O host de homologação resolve e responde na máquina de desenvolvimento (VPN), mas **não resolve
   no servidor da AWS** (`orcamento.ferreiracosta.com.br`). Para o sistema consumir, é preciso DNS/rota do
   servidor até o SAP (Infra).
3. **Credencial** (usuário Basic ou cliente OAuth) com o arranjo de comunicação, e o **host/porta de produção**.
4. **Responsável do CC:** `CostCtrResponsibleUser` pode substituir a matrícula do colaborador para ligar usuário
   ↔ centro de custo (a matrícula deixou de ser pedida no cadastro de usuários em 17/11/2026).
5. Mapeamento `ControllingArea`/`CompanyCode`/`BusinessArea` ↔ nossas Divisões e CCs (`centros_custo`).
