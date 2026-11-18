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

## Como o Orçamento usa

Leitura **uma vez por dia, às 06:00**, configurada em Configurações (ver `06-API-SAP-CONTAS-CONTABEIS.md`, seção
"No Orçamento", que vale para as duas APIs). A API de contas contábeis está na mesma fila e no mesmo agendamento.

## Pontos a decidir antes de gravar nos cadastros

1. ~~Quem inicia a conexão.~~ **Decidido:** o Orçamento puxa, 1x/dia às 06:00 (o doc `03` pedia push; vale só
   para filiais e saldos, com a Duda).
2. **Rede.** O host de homologação resolve e responde na máquina de desenvolvimento (VPN), mas **não resolve
   no servidor da AWS** (`orcamento.ferreiracosta.com.br`). É preciso DNS/rota do servidor até o SAP (Infra).
3. **Credencial** (usuário Basic) com o arranjo de comunicação, e o **host/porta de produção**.
4. **Responsável do CC:** `CostCtrResponsibleUser` pode substituir a matrícula do colaborador para ligar usuário
   ↔ centro de custo (a matrícula deixou de ser pedida no cadastro de usuários em 17/11/2026).
5. Mapeamento `ControllingArea`/`CompanyCode`/`BusinessArea` ↔ nossas Divisões e CCs (`centros_custo`).

## Gateway Kong e carga diária (implementado em 18/11/2026)

O time de SAP publicou a API num gateway interno (Kong) — **GET somente**, autenticação por header `apikey`
(usuário técnico `API_USER` do lado do gateway), sem Basic e sem o certificado autoassinado do SAP.

| | QAS (testado) |
| --- | --- |
| Serviço | `sap-qas-cost-center-read` |
| URL interna (usada pelo Orçamento) | `http://10.131.1.80:8000/sap/qas/cost-center-read/` |
| URL externa | `http://sap-api-gateway.ferreiracosta.com:8000/sap/qas/cost-center-read/` |
| Parâmetro obrigatório | `?sap-client=300` (não é fixado no Kong; o código manda) |

**Como liga:** informe a **apikey** e o **mandante** em **Configurações → Gateway SAP** (criptografados no banco) — ou, alternativa, defina a variável de ambiente **`SAP_API_KEY`** no servidor (Coolify/compose);
`SAP_CLIENT` traz o mandante. Com `SAP_API_KEY` definida, o Orçamento usa `apikey` no lugar de usuário/senha
(a credencial Basic da tela deixa de ser exigida) e a URL em Configurações pode ser `http://` de **IP privado** ou
de domínio da Ferreira Costa (IP público e hosts de fora são recusados). Sem `SAP_API_KEY`, nada muda: segue o
modo direto (HTTPS + Basic) de antes. A chave **nunca** vai para o git nem para a tela.

**O que roda às 06:00 (horário de Recife, ajustável em Configurações):** `RotinaSAPDiaria` chama
`sapodata.LerCentrosCusto` (paginado, 500 por página, `$expand=to_Text`, só vigentes hoje) e grava em
`centros_custo` (`handlers/centros_custo_sap_carga.go`), numa transação só:

- **Código:** o código SAP completo é **filial + CC de 4 dígitos** (`10016000` = filial `1001` + CC `6000`). Cada
  código completo vira um centro de custo em `centros_custo`.
- **Divisão = filial:** o código sem os 4 últimos dígitos (`1001`, `1002`, `81`…). A divisão é **criada na carga** se
  não existir (nome: o da tabela de parâmetros do Controle TI, ex. `1001` GARANHUNS; senão o próprio código, para
  ajustar depois em Cadastros). A coluna `filial` do centro recebe o mesmo valor (só se estiver vazia).
- **Nome:** descrição em PT (cai para o nome curto, depois para outro idioma).
- **Fase 1 (esta):** os 106 centros antigos de 4 dígitos (CC "puro", ligados a 507 usuários, 2 solicitações e 2
  exceções) **não são tocados**. Validação em 18/11/2026: 98 deles existem no SAP como final de código; 8 não
  (`6091`, `6245`, `6246`, `6271`, `6471`, `6564`, `CC-TESTE-01`, `CC-TESTE-02`).
- **Fases seguintes:** (2) remapear o CC próprio dos usuários para o código completo usando empresa + filial da view
  de colaboradores (a filial ainda não é gravada no sistema; falta o mapeamento empresa/filial → prefixo SAP);
  (3) remover os de 4 dígitos que ninguém mais usa. **Atenção:** as alçadas casam por filial + código do CC —
  conferir como a matriz do SAP escreve essa chave antes de a solicitação passar a usar o código completo.
- **Não tocados:** campos do orçamento além de nome, divisão e filial. **Nunca apagados:** centro que sumiu do SAP
  continua no cadastro.
- **Segurança da carga:** se o SAP devolver zero centros ou der erro, nada é gravado e o resultado vai como falha.
- Resultado e horário aparecem em Configurações (mesmo quadro da sonda).

QAS medido: 4.716 centros vigentes (4.771 no total), ~6 s para ler tudo, 66 `BusinessArea` distintas.

### Virada para PRD — checklist

1. **Time SAP/Kong:** pedir o serviço de **produção** do mesmo gateway (rota tipo `/sap/prd/cost-center-read/`,
   host/porta) e uma **`apikey` nova** (não reaproveitar a do QAS, que circulou fora do cofre).
2. **Mandante (`sap-client`)** de PRD — o 300 é do QAS. Ajustar `SAP_CLIENT`.
3. **Coolify (produção):** definir `SAP_API_KEY` e `SAP_CLIENT` nas variáveis de ambiente do backend e redeployar
   (o compose já repassa as duas; vazias = modo direto antigo).
4. **Configurações → SAP:** trocar a URL de "Centros de custo" para a rota de PRD, conferir o horário (06:00),
   clicar em **Testar** (confere chave, rede e conta os registros) e só então **ativar**.
5. **Rede:** o container do Orçamento tem de alcançar o IP/host do gateway (o app roda dentro da rede Ferreira
   Costa; usuários só acessam conectados).
6. **Divisões:** a carga cria as filiais sozinha (no QAS são 66 prefixos). Depois da 1ª carga, ajustar em Cadastros os
   nomes que ficaram iguais ao código.
7. Primeira carga em PRD acompanhada: ver o resultado em Configurações (criados, atualizados, divisões novas, ignorados).
8. **Rotacionar** a `apikey` do QAS depois da virada.
