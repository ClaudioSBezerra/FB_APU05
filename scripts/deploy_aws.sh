#!/usr/bin/env bash
# Deploy do FB_APU05 no servidor AWS da FC (aws-fc) — orcamento.ferreiracosta.com.br.
#
# O servidor é COMPARTILHADO com outros sistemas (FB_APU02, StockFlow, FB_APU04...).
# Esta stack é isolada: projeto compose "orcamento", containers orcamento-*, banco
# próprio sem porta publicada (docker-compose.aws.yml). O script só toca nesses nomes.
#
# As imagens são construídas AQUI (WSL) e enviadas por `docker save | ssh | docker load`:
# o disco do servidor é curto e ele não precisa alcançar GitHub/Docker Hub.
#
# Uso:
#   ./scripts/deploy_aws.sh --check   # só valida pré-requisitos, não muda nada
#   ./scripts/deploy_aws.sh --init    # 1ª vez: cria /opt/apps/fbtax/orcamento/.env com segredos novos
#   ./scripts/deploy_aws.sh           # build, envio e subida do HEAD (== origin/main)
#
# Layout no servidor:
#   /opt/apps/fbtax/orcamento/.env                 segredos (chmod 600, gerados no servidor, nunca no git)
#   /opt/apps/fbtax/orcamento/docker-compose.yml   cópia do docker-compose.aws.yml do commit no ar
#   /opt/apps/fbtax/orcamento/DEPLOYED             sha no ar
set -euo pipefail

HOST="${DEPLOY_HOST:-aws-fc}"
BASE=/opt/apps/fbtax/orcamento
PROJECT=orcamento
DOMINIO=orcamento.ferreiracosta.com.br

die()  { echo "ERRO: $*" >&2; exit 1; }
info() { echo "==> $*"; }

MODE=deploy
case "${1:-}" in
  --check) MODE=check ;;
  --init)  MODE=init ;;
  "")      ;;
  *) die "argumento desconhecido: $1" ;;
esac

cd "$(git rev-parse --show-toplevel)"

ssh -o BatchMode=yes -o ConnectTimeout=10 "$HOST" true || die "SSH para $HOST falhou"

# --- init: cria o .env uma única vez (nunca sobrescreve) ---------------------------
if [[ "$MODE" == init ]]; then
  ssh "$HOST" "set -e
    mkdir -p $BASE && chmod 700 $BASE
    if [ -e $BASE/.env ]; then echo 'já existe $BASE/.env — nada feito'; exit 0; fi
    umask 177
    cat > $BASE/.env <<EOF
APP_URL=https://$DOMINIO
DB_USER=orcamento_admin
DB_NAME=fb_apu05
DB_PASSWORD=\$(openssl rand -hex 24)
DB_APP_PASSWORD=\$(openssl rand -hex 24)
DB_PRIVILEGIADA_PASSWORD=\$(openssl rand -hex 24)
JWT_SECRET=\$(openssl rand -hex 32)
ENCRYPTION_KEY=\$(openssl rand -hex 32)
EOF
    echo 'criado $BASE/.env (chmod 600)'"
  exit 0
fi

# --- pré-requisitos ------------------------------------------------------------------
ssh "$HOST" "test -f $BASE/.env" || die "$BASE/.env não existe no servidor — rode --init"

# A stack só pode usar o que é dela: se algum nome nosso já existir de outra forma, para.
ssh "$HOST" "docker ps -a --format '{{.Names}}' | grep -E '^orcamento-' >/dev/null && \
  docker inspect orcamento-db --format '{{index .Config.Labels \"com.docker.compose.project\"}}' | grep -qx $PROJECT" \
  >/dev/null 2>&1 || true   # primeira vez: ainda não existe, ok

if [[ "$MODE" == check ]]; then
  info "--check: pré-requisitos OK, nada foi alterado"
  exit 0
fi

# --- o que vai para produção é o que está no GitLab -----------------------------------
[[ -z "$(git status --porcelain --untracked-files=no)" ]] \
  || die "há alterações não commitadas — commit e push antes do deploy"
git fetch -q origin main || die "não consegui falar com o GitLab (token expirou?)"
[[ "$(git rev-parse HEAD)" == "$(git rev-parse origin/main)" ]] \
  || die "HEAD difere de origin/main — faça 'git push origin main' antes"
SHA="$(git rev-parse --short=12 HEAD)"
info "commit: $SHA — $(git log -1 --format=%s)"

# --- build local (código exato do commit) ---------------------------------------------
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
git archive --format=tar HEAD | tar -x -C "$TMP"
info "build das imagens (linux/amd64)"
docker build -q -f "$TMP/backend/Dockerfile" -t "orcamento-api:$SHA" "$TMP" >/dev/null
docker build -q -t "orcamento-web:$SHA" "$TMP/frontend" >/dev/null

info "enviando imagens para $HOST"
docker save "orcamento-api:$SHA" "orcamento-web:$SHA" | gzip | ssh "$HOST" "gunzip | docker load" >/dev/null
scp -q "$TMP/docker-compose.aws.yml" "$HOST:$BASE/docker-compose.yml"

# --- subida ---------------------------------------------------------------------------
info "subindo a stack (docker compose -p $PROJECT)"
ssh "$HOST" "set -e
  cd $BASE
  TAG=$SHA docker compose -p $PROJECT --env-file .env -f docker-compose.yml up -d --remove-orphans
  echo $SHA > DEPLOYED"

info "aguardando healthcheck"
for _ in $(seq 1 60); do
  status="$(ssh "$HOST" "docker inspect --format '{{.State.Health.Status}}' orcamento-web 2>/dev/null" || true)"
  [[ "$status" == "healthy" ]] && break
  sleep 3
done
[[ "$status" == "healthy" ]] || die "o web não ficou saudável — veja: ssh $HOST docker logs orcamento-api"

ssh "$HOST" "curl -fsS http://172.17.0.1:3012/api/health" && echo
info "OK: $SHA no ar em http://172.17.0.1:3012 (nginx_proxy -> $DOMINIO depende do bloco no nginx.conf)"

# imagens antigas desta stack (só orcamento-*): mantém a atual e a anterior
ssh "$HOST" "docker images --format '{{.Repository}}:{{.Tag}}' | grep -E '^orcamento-(api|web):' | grep -v ':$SHA\$' | tail -n +3 | xargs -r docker rmi >/dev/null 2>&1 || true"
