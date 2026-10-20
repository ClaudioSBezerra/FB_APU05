# ERP Bridge Fornecedores

Conector **isolado** que sincroniza o cadastro de fornecedores do FB_APU05 a
partir de `FORN` (Oracle, servidor FCCORP).

## Por que separado dos demais conectores

Mesmo princípio do `erp-bridge-colaboradores/`: autônomo, conexão Oracle
própria (somente leitura), fala só com o backend do FB_APU05 — nenhum
compartilhamento com o conector do FB_APU02 nem com o de colaboradores.

## Como funciona

```
FORN (cgc, razsoc, fantas, uf)
  → bridge_fornecedores.py (lê 1x, lotes de 500)
  → POST /api/erp-bridge/fornecedores/sincronizar  (X-API-Key)
  → backend Go: upsert por cgc em `fornecedores`
    (cgc novo = criado; cgc já conhecido = atualizado)
```

Query de origem (acordada com o negócio):

```sql
SELECT f.cgc, f.razsoc, f.fantas, f.uf
FROM   FORN f
```

Diferente do `erp-bridge-colaboradores/` (que só faz UPDATE num usuário já
existente): aqui `fornecedores` é uma tabela nova no FB_APU05, espelho
read-only do FORN — nunca editada manualmente no FB_APU05, só
resincronizada.

**Credenciais Oracle**: por padrão, buscadas em tempo de execução via
`GET /api/erp-bridge/credentials` — **a mesma API key** já usada pelo
`erp-bridge-colaboradores/` (uma API key por instância do FB_APU05, não uma
por conector). Isso evita duplicar a senha do Oracle no `config.yaml` deste
conector.

## Uso

```bash
cp config.example.yaml config.yaml   # preencha fbtax.api_key

# roda uma vez e sai
python bridge_fornecedores.py

# só lê e conta, não envia nada
python bridge_fornecedores.py --dry-run

# loop contínuo: roda 1x/dia no horário configurado (horario_diario)
python bridge_fornecedores.py --daemon
```

## Docker

```bash
docker build -t erp-bridge-fornecedores .

# execução única (ex.: cron do sistema operacional)
docker run --rm -v "$PWD/config.yaml:/app/config.yaml" erp-bridge-fornecedores

# modo daemon (sincroniza sozinho todo dia)
docker run -d --name erp-bridge-fornecedores --restart unless-stopped \
  -v "$PWD/config.yaml:/app/config.yaml" erp-bridge-fornecedores
```

(o `CMD` padrão da imagem já é `--daemon`; para rodar uma vez só, sobrescreva
o comando: `docker run --rm -v "$PWD/config.yaml:/app/config.yaml" erp-bridge-fornecedores python bridge_fornecedores.py`.)

## systemd (alternativa ao Docker)

```ini
# /etc/systemd/system/erp-bridge-fornecedores.service
[Unit]
Description=FBTax ERP Bridge Fornecedores — Oracle FORN -> FB_APU05
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=claudio
WorkingDirectory=/opt/apps/fbtax/erp-bridge-fornecedores
ExecStart=/opt/apps/fbtax/erp-bridge-fornecedores/venv/bin/python bridge_fornecedores.py --daemon
Restart=on-failure
RestartSec=30
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

```bash
systemctl start erp-bridge-fornecedores
journalctl -u erp-bridge-fornecedores -f
```
