# Deploy FB_APU05 no Coolify — controladoria.fbtax.cloud

Preparado para o serviço Coolify já existente
(`claudio-s-bezerra/-f-b_-a-p-u05:main-...`, app "FB_APU05 - Controladoria
Ferreira Costa", production). Replica a topologia Coolify do FB_APU02
(AD-10) adaptada à particularidade do FB_APU05: o frontend só chama
caminhos relativos (`fetch('/api/...')`), então aqui api+web ficam atrás
de um único domínio em vez de dois (ver `frontend/nginx.conf`).

## O que mudar no painel (aba Configuration)

- **Docker Compose Location**: trocar de `/docker-compose.yml` para
  `/docker-compose.prod.yml` — o arquivo atual é o de desenvolvimento
  (sobe o Vite dev server, não um build de produção).
- **Pre-deployment Command** e **Post-deployment Command**: apagar o
  `php artisan migrate` (placeholder do template genérico do Coolify,
  não existe Laravel aqui). O backend Go roda as migrations sozinho no
  boot (`main.go` → `onDBConnected`) — não precisa de comando externo.
- **Domains for web**: gerar domínio e apontar para
  `controladoria.fbtax.cloud` (também já fixado nas labels Traefik do
  compose, então o "Generate Domain" do Coolify deve bater com esse
  valor, não gerar um aleatório).
- **Domains for api**: deixar em branco — o serviço `api` não expõe
  porta pública nesse compose (`expose`, não `ports`); só `web` fala com
  a internet e faz proxy interno de `/api/*` para `api:8081`.

## Environment Variables

Preencher a partir de `.env.production.example` (na raiz do repo) —
gerar `JWT_SECRET`/`ENCRYPTION_KEY`/`DB_PASSWORD`/`DB_APP_PASSWORD`/
`DB_PRIVILEGIADA_PASSWORD` novos, nunca reaproveitar os de dev.

`SMTP_*` é opcional: só é usado por "esqueci minha senha" do login local
(`POST /api/auth/forgot-password`). Sem `SMTP_PASSWORD`, o resto do app
sobe normal — esse endpoint específico responde 500 se alguém tentar
usá-lo antes do SMTP estar configurado.

## Persistent Storage

Adicionar um volume persistente para o serviço `db` apontando para
`/var/lib/postgresql/data` (o compose já declara o volume nomeado
`postgres_data_prod`; a aba Persistent Storage do Coolify só precisa
confirmar o mount, não recriar o volume).

## Passo obrigatório pós-primeiro-deploy

A migration 010 cria as roles `fb_apu05_app`/`fb_apu05_privilegiado` com
senha estática `changeme` (não lê env var). Depois do primeiro deploy
bem-sucedido, abrir o terminal do container `db` (ou `api`) pelo Coolify
e rodar:

```sql
ALTER ROLE fb_apu05_app PASSWORD '<mesmo valor de DB_APP_PASSWORD>';
ALTER ROLE fb_apu05_privilegiado PASSWORD '<mesmo valor de DB_PRIVILEGIADA_PASSWORD>';
```

Depois reiniciar o serviço `api` para ele reconectar com a senha nova.
Sem esse passo, a aplicação sobe e funciona (a senha dev-default ainda
bate), mas fica com uma credencial fixa e pública (este repo) ativa em
produção.

## Verificação pós-deploy

- `https://controladoria.fbtax.cloud/api/health` → `{"status":"ok"}`
- Login (SSO, se `IAM_BASE_URL` já estiver configurado, ou login local)
- Abrir o painel consolidado (`/paineis/consolidado`, Story 5.1) e
  confirmar que carrega dados reais do Postgres do container `db`.

## Fora de escopo deste deploy

- Conexão com Oracle para carga de usuários por centro de custo — ainda
  não tem desenho nem story (ver memória do projeto); não faz parte
  deste compose.
- Staging/homologação separado e publicação via GHCR (AD-10 menciona,
  mas o painel atual aponta para build direto do Git — ver nota nesse
  sentido na review da arquitetura, `review-rubric.md` HIGH-1). Ajustar
  quando/se esse pipeline for de fato necessário.
