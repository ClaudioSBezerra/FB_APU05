# Deploy FB_APU05 na Hostinger: Coolify e script

> **Desde 12/11/2026:** o GitHub voltou a funcionar na rede da FC. O código vai para **os dois**
> repositórios (GitLab oficial, com o pipeline da AppSec, e GitHub). O deploy normal volta a ser
> pelo **Coolify**, que puxa do GitHub. O `scripts/deploy_hostinger.sh`, que puxa do GitLab, fica
> como **plano B** se o GitHub for bloqueado de novo. Os dois usam o **mesmo volume do banco**
> (`fb_apu05_postgres_data_prod`). Veja a seção [Coolify e script](#coolify-e-script).

Histórico: este documento substituiu [../deploy-coolify.md](../deploy-coolify.md) em 11/11/2026,
quando o GitHub foi bloqueado.

## Por que mudou

- O repositório saiu do GitHub e foi para o GitLab interno:
  `https://gitlab.fcxlabs.com/sap/FB_APU05`. Na rede da FC, o GitHub fica bloqueado
  pelo Cisco Umbrella.
- `gitlab.fcxlabs.com` resolve para IP privado da AWS da FC (`172.30.x.x`). A Hostinger
  (`hostinger-fc`, 76.13.171.196) **não alcança** o GitLab. Por isso nem o Coolify nem um
  runner no servidor conseguem puxar o código.
- A máquina do desenvolvedor (WSL) alcança os dois. Então ela empurra o código:
  `scripts/deploy_hostinger.sh` faz `git archive` do commit publicado no GitLab, envia por
  SSH, gera as imagens no servidor e sobe com `docker compose`. É o mesmo modelo de stack do
  FB_APU02 (`/opt/apps/fbapu02-hostinger`), sem Coolify.

## Deploy do dia a dia

```bash
git push origin main                 # publica no GitLab
./scripts/deploy_hostinger.sh        # leva exatamente esse commit para produção
```

O script recusa o deploy nestes casos:
- há alterações não commitadas;
- o `HEAD` é diferente de `origin/main`;
- o `.env` não existe no servidor;
- o recurso antigo do Coolify ainda está rodando.

`./scripts/deploy_hostinger.sh --check` só valida esses pontos e não altera nada.

O token do GitLab fica no cache em memória por 8 horas. Se o `git fetch` falhar por
autenticação, renove o cache:

```bash
read -s -p "Token GitLab: " GLT; echo
printf "protocol=https\nhost=gitlab.fcxlabs.com\nusername=oauth2\npassword=%s\n" "$GLT" | git credential approve
unset GLT
```

### Layout no servidor

| Caminho | Conteúdo |
| --- | --- |
| `/opt/apps/fb_apu05/.env` | segredos (`chmod 600`), existe só no servidor |
| `/opt/apps/fb_apu05/releases/<sha>/` | código de cada deploy; ficam os 5 últimos |
| `/opt/apps/fb_apu05/current` | symlink para o release no ar |
| `/opt/apps/fb_apu05/DEPLOYED` | sha do release no ar |

Stack: `docker compose -p fb_apu05 -f docker-compose.prod.yml`, com os containers
`fb_apu05-{api,web,db,cloudflared}-prod` e o volume `fb_apu05_postgres_data_prod`.

### Rollback de código

```bash
ssh hostinger-fc 'ls -1t /opt/apps/fb_apu05/releases'   # escolha o sha anterior
ssh hostinger-fc 'cd /opt/apps/fb_apu05/releases/<sha> && \
  docker compose -p fb_apu05 -f docker-compose.prod.yml --env-file ../../.env up -d --build && \
  ln -sfn releases/<sha> ../../current'
```

As migrations só avançam. Um rollback para antes de uma migration nova não desfaz o schema.

---

## Migração inicial: Coolify → stack própria (uma vez só)

Faça cada passo com **confirmação humana**: eles mexem no banco e no tráfego de produção.
Hoje o app está no recurso Coolify `d0g84w44sso0k8okwgsco4g4`, no commit `c858d92`. Só fica
fora do ar entre os passos 3 e 5, por alguns minutos.

### 1. Preparar o diretório e o `.env`

O Coolify guarda as variáveis do app em `/data/coolify/applications/<uuid>/.env`.

```bash
ssh hostinger-fc
A=d0g84w44sso0k8okwgsco4g4
cut -d= -f1 /data/coolify/applications/$A/.env | sort      # só os NOMES, conferir abaixo
mkdir -p /opt/apps/fb_apu05/releases
install -m 600 /data/coolify/applications/$A/.env /opt/apps/fb_apu05/.env
```

Variáveis que o `docker-compose.prod.yml` usa e que precisam estar no arquivo:
- **Obrigatórias:** `DB_USER`, `DB_PASSWORD`, `DB_NAME`, `DB_APP_PASSWORD`,
  `DB_PRIVILEGIADA_PASSWORD`, `JWT_SECRET`, `ENCRYPTION_KEY`, `APP_URL`,
  `CLOUDFLARE_TUNNEL_TOKEN`.
- **Opcionais:** `IAM_*` e `SMTP_*`.

Se o Coolify tiver gravado as variáveis com outros nomes ou com prefixo `SERVICE_*`, ajuste
o arquivo copiado. Nunca cole os valores em chat ou ticket.

### 2. Backup lógico do banco atual

```bash
DB=$(docker ps --format '{{.Names}}' | grep "^db-$A")
mkdir -p /root/backups
docker exec "$DB" sh -c 'pg_dumpall -U "$POSTGRES_USER"' | gzip > /root/backups/fb_apu05-pre-migracao-$(date +%Y%m%d-%H%M).sql.gz
ls -lh /root/backups/fb_apu05-pre-migracao-*
```

O `pg_dumpall` inclui as roles `fb_apu05_app` e `fb_apu05_privilegiado`, com as senhas
trocadas no pós-deploy. Um `pg_dump` simples perderia essas senhas.

### 3. Parar o recurso no Coolify (início da janela)

No painel do Coolify, abra o app **"FB_APU05 - Controladoria Ferreira Costa"** e clique em
**Stop**. Desligue também o auto-deploy e o webhook, que já não recebe nada do GitHub. Use o
painel, e não `docker stop`, senão o Coolify sobe os containers de novo.

Confirme com `docker ps --filter name=$A`. O resultado deve vir vazio.

### 4. Copiar o volume do Postgres para a stack nova

Os dois lados usam a mesma versão (`postgres:15-alpine`). A cópia de arquivos é exata e
preserva roles, senhas e dados.

```bash
OLD=$(docker volume ls --format '{{.Name}}' | grep "^$A" | grep -i postgres)
echo "$OLD"      # deve ser UM volume só
docker volume create \
  --label com.docker.compose.project=fb_apu05 \
  --label com.docker.compose.volume=postgres_data_prod \
  fb_apu05_postgres_data_prod
docker run --rm -v "$OLD":/from:ro -v fb_apu05_postgres_data_prod:/to alpine sh -c 'cp -a /from/. /to/'
```

O volume antigo fica intacto e garante o rollback.

### 5. Primeiro deploy (do WSL)

```bash
./scripts/deploy_hostinger.sh
```

O túnel Cloudflare usa o mesmo token. No painel Cloudflare, a rota pública de
`controladoria.fbtax.cloud` aponta para `http://web:80`. O nome `web` continua resolvendo
na rede da stack nova, então o tráfego volta sozinho quando o `cloudflared` sobe.

### 6. Verificar

- `https://controladoria.fbtax.cloud/api/health` deve responder `{"status":"ok"}`.
- O login local e o SSO devem funcionar.
- O painel e a fila devem mostrar os mesmos dados de antes.
- `ssh hostinger-fc 'docker ps --filter name=fb_apu05'` deve listar 4 containers, com api,
  web e db `healthy`.

### Se der errado (rollback da migração)

```bash
ssh hostinger-fc 'cd /opt/apps/fb_apu05/current && docker compose -p fb_apu05 -f docker-compose.prod.yml --env-file /opt/apps/fb_apu05/.env down'
```

Use `down` **sem `-v`**. Depois clique em **Start** no recurso do Coolify. Ele volta com o
volume antigo, que a migração não alterou.

### 7. Depois de ~1 semana estável

Apague o recurso FB_APU05 no Coolify e, por último, o volume antigo. Faça isso só depois de
conferir que o backup do passo 2 existe.

---

## Coolify e script

### Repositórios

- `origin`: busca no GitLab e envia para **GitLab e GitHub** ao mesmo tempo (dois `pushurl`).
  Um `git push origin main` atualiza os dois.
- `gitlab`: só o GitLab. Use para branches de merge request, porque as opções
  `-o merge_request.*` só existem no GitLab:
  `git push -u gitlab <branch> -o merge_request.create -o merge_request.target=main`.
- `github`: só o GitHub.

O merge request é aprovado e mesclado no GitLab. Depois, `git pull origin main && git push origin main`
leva o merge também ao GitHub, e o Coolify publica.

### Regra de ouro

**Só um dos dois pode estar rodando:** o recurso FB_APU05 do Coolify ou a stack `fb_apu05` do script.
Os dois `cloudflared` usam o mesmo token de túnel, então rodando juntos o tráfego se divide. Os dois
Postgres também montariam o mesmo volume ao mesmo tempo e corromperiam o banco.

- O script **recusa** subir se o Coolify estiver rodando.
- O Coolify **não** sabe da stack do script. Antes de dar Deploy ou Start nele, derrube a stack:

```bash
ssh hostinger-fc 'cd /opt/apps/fb_apu05/current && docker compose -p fb_apu05 -f docker-compose.prod.yml --env-file /opt/apps/fb_apu05/.env down'
```

Use `down` **sem `-v`**: o volume é externo e o compose não o apaga, mas não arrisque.

### Script → Coolify (modo normal)

1. Com o `docker-compose.prod.yml` já usando o volume externo (12/11), garanta que o GitHub está
   atualizado: `git push origin main`.
2. Derrube a stack do script (comando acima).
3. No Coolify, no app **FB_APU05**, clique em **Deploy** e depois religue o **Auto Deploy**.
4. Confira que o Postgres do Coolify montou o volume certo. A saída tem que ser `fb_apu05_postgres_data_prod`:
   ```bash
   ssh hostinger-fc 'docker inspect $(docker ps -q --filter name=db-d0g84w44sso0k8okwgsco4g4) --format "{{range .Mounts}}{{.Name}}{{end}}"'
   ```
   Se aparecer outro nome, o Coolify criou um volume novo (banco vazio). Pare o app no Coolify e
   suba a stack do script de novo; os dados reais continuam intactos no volume externo.
5. `https://controladoria.fbtax.cloud/api/health` deve responder `{"status":"ok"}`.

### Coolify → script (GitHub bloqueado)

1. No Coolify, desligue o **Auto Deploy** e clique em **Stop** no app FB_APU05.
2. Rode `./scripts/deploy_hostinger.sh` do WSL. Ele publica o `main` do GitLab no mesmo banco.
