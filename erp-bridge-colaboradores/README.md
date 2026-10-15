# ERP Bridge Colaboradores

Conector **isolado** que sincroniza o centro de custo de cada usuário do
FB_APU05 a partir de `SFC.SFC_VW_COLABORADORES` (Oracle, servidor FCCORP).

## Por que separado do `erp-bridge-aws/`

O `erp-bridge-aws/bridge.py` é **compartilhado** com o FB_APU02 (reforma
tributária) e lê as tabelas SAP (`s4i_*`) do mesmo FCCORP. Para **não
arriscar a produção do APU02**, este conector é autônomo: abre sua própria
conexão Oracle (somente leitura) e fala só com o backend do FB_APU05 —
mesmo princípio do `erp-bridge-simulador/` feito para o FB_APU04.

## Como funciona

```
SFC.SFC_VW_COLABORADORES (cod_empresa, centro_custo, numcad, nomfun)
  → bridge_colaboradores.py (lê 1x, lotes de 500)
  → POST /api/erp-bridge/colaboradores/sincronizar  (X-API-Key)
  → backend Go: casa por (cod_empresa, numcad) contra usuarios,
    atualiza SOMENTE usuarios.cc_proprio_id
```

Query de origem (acordada com o negócio — `sitafa <> '7'` filtra afastados):

```sql
SELECT x.cod_empresa, x.centro_custo, x.numcad, x.nomfun
FROM   SFC.SFC_VW_COLABORADORES x
WHERE  x.sitafa <> '7'
```

**Minimização por LGPD**: só essas 4 colunas saem do Oracle — a view tem
bem mais colunas que isso (CPF, cargo, local de trabalho, etc.), nenhuma
delas é lida. `nomfun` nunca é persistido no FB_APU05; serve só para
aparecer no log do backend quando uma linha não casa com ninguém
cadastrado, para o administrador saber quem falta configurar.

**Credenciais Oracle**: por padrão, buscadas em tempo de execução via
`GET /api/erp-bridge/credentials` (mesma API key), que lê o que o
administrador configurou na tela **ERP Bridge — Credenciais** do FB_APU05.
Isso evita duplicar a senha do Oracle no `config.yaml` deste conector —
só a `fbtax.api_key` é obrigatória aqui.

**Pré-requisito**: cada usuário do FB_APU05 precisa ter `cod_empresa` e
`numcad` preenchidos (tela "Gestão de Usuários") — a view não tem e-mail,
então é assim que o sync sabe quem é quem.

## Uso

```bash
cp config.example.yaml config.yaml   # preencha fbtax.api_key

# roda uma vez e sai
python bridge_colaboradores.py

# só lê e conta, não envia nada
python bridge_colaboradores.py --dry-run

# loop contínuo: roda 1x/dia no horário configurado (horario_diario)
python bridge_colaboradores.py --daemon
```

## Docker

```bash
docker build -t erp-bridge-colaboradores .

# execução única (ex.: cron do sistema operacional)
docker run --rm -v "$PWD/config.yaml:/app/config.yaml" erp-bridge-colaboradores

# modo daemon (sincroniza sozinho todo dia)
docker run -d --name erp-bridge-colaboradores --restart unless-stopped \
  -v "$PWD/config.yaml:/app/config.yaml" erp-bridge-colaboradores
```

(o `CMD` padrão da imagem já é `--daemon`; para rodar uma vez só, sobrescreva
o comando: `docker run --rm -v "$PWD/config.yaml:/app/config.yaml" erp-bridge-colaboradores python bridge_colaboradores.py`.)

## systemd (alternativa ao Docker)

```ini
# /etc/systemd/system/erp-bridge-colaboradores.service
[Unit]
Description=FBTax ERP Bridge Colaboradores — Oracle VW_COLABORADORES -> FB_APU05
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=claudio
WorkingDirectory=/opt/apps/fbtax/erp-bridge-colaboradores
ExecStart=/opt/apps/fbtax/erp-bridge-colaboradores/venv/bin/python bridge_colaboradores.py --daemon
Restart=on-failure
RestartSec=30
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

```bash
systemctl start erp-bridge-colaboradores
journalctl -u erp-bridge-colaboradores -f
```
