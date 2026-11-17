# API SAP — G/L Account Read (contas contábeis)

> Segunda documentação de API do Time TI SAP (17/11/2026). Mesma família e mesmas regras de acesso da
> `05-API-SAP-CENTRO-DE-CUSTO.md`; aqui só o que muda.

## O que é

Serviço standard `API_GLACCOUNTINCHARTOFACCOUNTS_SRV` (OData V2, A2X), **somente leitura**.
Recursos: `A_GLAccountInChartOfAccounts` (conta no plano de contas) e `A_GLAccountText` (nomes por idioma),
ligados por `to_Text` / `to_GLAccountInChartOfAccounts`.

| Ambiente | URL base |
| --- | --- |
| Homologação | `https://vhfcodevci.sap.ferreiracosta.com:44300/sap/opu/odata/sap/API_GLACCOUNTINCHARTOFACCOUNTS_SRV` |
| Produção | `https://<HOST_PRD>:<PORTA>/sap/opu/odata/sap/API_GLACCOUNTINCHARTOFACCOUNTS_SRV` |

Autenticação, arranjo de comunicação, `Accept: application/json` e paginação: iguais à API de centros de custo.
Não há campos customizados nem `$batch` documentado.

## Regras que afetam a nossa carga

- **Chave:** `ChartOfAccounts` (4) + `GLAccount` (10, com zeros à esquerda). É a visão de **plano de contas**; dado
  por empresa não vem aqui.
- **Textos:** chave própria (`ChartOfAccounts` + `GLAccount` + `Language`); filtrar `Language eq 'PT'`. Usar
  `GLAccountLongName` (50) para o nome completo; `GLAccountName` tem só 20.
- **Indicadores booleanos** (`IsBalanceSheetAccount`, `AccountIsBlockedFor…`, `AccountIsMarkedForDeletion`,
  `IsProfitLossAccount`): no `$filter` usar `true`/`false` sem aspas. Na especificação, `IsBalanceSheetAccount` e
  `IsProfitLossAccount` têm a mesma descrição: **confirmar o comportamento real** antes de usar um deles como filtro.
- **Só contas ativas e lançáveis:** `AccountIsMarkedForDeletion eq false and AccountIsBlockedForPosting eq false`.
- **Carga incremental:** `LastChangeDateTime` (UTC, vem como `/Date(ms)/`) permite buscar só o que mudou.

## No Orçamento

O que vale para esta API e para a de centros de custo (decidido em 17/11/2026):

- O sistema **puxa** do SAP, **uma vez por dia às 06:00** (horário de Recife), não recebe push. Filiais e saldos
  continuam com a Duda (ver `03-INTEGRACAO-SAP-S4HANA.md`).
- Configuração em **Configurações** (só administrador): horário, usuário e senha do SAP (a senha é gravada
  criptografada e nunca é devolvida), e, por serviço, URL e liga/desliga. Botão "Testar conexão" por serviço.
  A rotina diária só roda para serviço ligado e com credencial. Roda uma vez por dia; se o sistema ficou fora do
  ar às 06:00, roda ao voltar.
- Código: `internal/sapodata` (cliente OData, restrito a https e a domínios `ferreiracosta.com` /
  `ferreiracosta.com.br`, sem seguir redirecionamentos) e `handlers/configuracoes.go`.
- **Hoje a leitura só confere o acesso e conta os registros.** Gravar em centros de custo e contas depende do
  mapeamento SAP ↔ cadastro (códigos com zeros à esquerda, divisão, o que fazer com bloqueados).
- Certificado de CA interna do SAP: `SAP_CA_FILE` (caminho de um PEM no container) soma a cadeia às CAs do
  sistema. A validação do certificado nunca é desligada.

O pedido completo de acesso ao SAP de produção (rede, DNS, usuário técnico, escopo e decisões) foi enviado ao
Marlos, à Infra e ao Time TI SAP em 17/11/2026.
