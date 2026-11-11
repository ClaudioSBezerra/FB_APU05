#!/usr/bin/env bash
# Deploy do FB_APU05 na Hostinger SEM Coolify (a partir do WSL).
#
# Por que assim: o código mora no GitLab interno (gitlab.fcxlabs.com resolve
# para IP privado da AWS da FC), que a Hostinger não alcança — então nem o
# Coolify nem um runner no servidor conseguem puxar dele. A máquina do dev
# alcança os dois lados, então ela empurra o código: `git archive` do commit
# já publicado no GitLab → SSH → build e `docker compose up` no servidor.
#
# Uso:
#   ./scripts/deploy_hostinger.sh           # deploy do HEAD (== origin/main)
#   ./scripts/deploy_hostinger.sh --check   # só valida pré-requisitos, não muda nada
#
# Layout no servidor (ver docs/deploy/HOSTINGER-SEM-COOLIFY.md):
#   /opt/apps/fb_apu05/.env              segredos (chmod 600, só no servidor)
#   /opt/apps/fb_apu05/releases/<sha>/   código de cada deploy (mantém os 5 últimos)
#   /opt/apps/fb_apu05/current           symlink para o release no ar
set -euo pipefail

HOST="${DEPLOY_HOST:-hostinger-fc}"
BASE=/opt/apps/fb_apu05
PROJECT=fb_apu05                      # fixa nomes de volume/rede entre releases
COMPOSE_FILE=docker-compose.prod.yml
COOLIFY_APP=d0g84w44sso0k8okwgsco4g4  # recurso antigo do Coolify — não pode rodar junto
KEEP_RELEASES=5

die()  { echo "ERRO: $*" >&2; exit 1; }
info() { echo "==> $*"; }

CHECK_ONLY=false
[[ "${1:-}" == "--check" ]] && CHECK_ONLY=true

cd "$(git rev-parse --show-toplevel)"

# 1. O que vai para produção é exatamente o que está no GitLab.
[[ -z "$(git status --porcelain --untracked-files=no)" ]] \
  || die "há alterações não commitadas — commit e push antes do deploy"
git fetch -q origin main || die "não consegui falar com o GitLab (token expirou? veja o doc)"
[[ "$(git rev-parse HEAD)" == "$(git rev-parse origin/main)" ]] \
  || die "HEAD difere de origin/main — faça 'git push origin main' (ou checkout da main) antes"
SHA="$(git rev-parse --short=12 HEAD)"
info "commit: $SHA — $(git log -1 --format=%s)"

# 2. Servidor alcançável e pronto.
ssh -o BatchMode=yes -o ConnectTimeout=10 "$HOST" true \
  || die "SSH para $HOST falhou"
ssh "$HOST" "test -f $BASE/.env" \
  || die "$BASE/.env não existe no servidor — rode a migração inicial (doc)"

# 3. Nunca subir junto com o recurso do Coolify: os dois cloudflared usam o
#    mesmo token de túnel e o tráfego seria dividido entre banco antigo e novo.
if [[ -n "$(ssh "$HOST" "docker ps -q --filter name=$COOLIFY_APP")" ]]; then
  die "o recurso FB_APU05 do Coolify ($COOLIFY_APP) ainda está rodando — pare-o antes (doc, passo 3)"
fi

if $CHECK_ONLY; then
  info "--check: pré-requisitos OK, nada foi alterado"
  exit 0
fi

# 4. Envia o código do commit.
info "enviando código para $HOST:$BASE/releases/$SHA"
git archive --format=tar HEAD \
  | ssh "$HOST" "set -e; rm -rf $BASE/releases/$SHA; mkdir -p $BASE/releases/$SHA; tar -x -C $BASE/releases/$SHA"

# 5. Build + subida. As migrations rodam sozinhas no boot do api (main.go).
info "build e subida (docker compose -p $PROJECT)"
ssh "$HOST" "set -e
  cd $BASE/releases/$SHA
  docker compose -p $PROJECT -f $COMPOSE_FILE --env-file $BASE/.env up -d --build --remove-orphans
  ln -sfn releases/$SHA $BASE/current
  echo $SHA > $BASE/DEPLOYED"

# 6. Espera o web ficar saudável (ele só sobe depois do api saudável).
info "aguardando healthcheck"
for _ in $(seq 1 60); do
  status="$(ssh "$HOST" "docker inspect --format '{{.State.Health.Status}}' fb_apu05-web-prod 2>/dev/null" || true)"
  [[ "$status" == "healthy" ]] && break
  sleep 3
done
[[ "${status:-}" == "healthy" ]] \
  || die "web não ficou saudável em 3 min (status: ${status:-?}) — veja: ssh $HOST 'docker logs fb_apu05-api-prod --tail 100'"

# 7. Limpeza: releases antigos e imagens órfãs (só dangling).
ssh "$HOST" "cd $BASE/releases && ls -1t | tail -n +$((KEEP_RELEASES + 1)) | xargs -r rm -rf; docker image prune -f >/dev/null"

info "deploy de $SHA concluído — https://controladoria.fbtax.cloud"
