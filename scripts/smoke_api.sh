#!/usr/bin/env bash
# Teste de fumaça da imagem da API ANTES de enviá-la ao servidor.
# Sobe o container sem rede e sem banco de verdade e confere que o servidor chega a registrar as rotas.
# Pega o que os testes unitários não pegam: o registro de rotas fica em main() e uma rota conflitante
# (ex.: "GET /api/solicitacoes/anexos/{id}" x "GET /api/solicitacoes/{id}/compartilhamentos") faz o Go entrar
# em pânico na subida — em produção isso vira API em loop de reinício.
#
# Uso: scripts/smoke_api.sh <imagem>     (ex.: orcamento-api:abc123)
set -euo pipefail

IMG="${1:?uso: smoke_api.sh <imagem>}"
NOME="orcamento-smoke-$$"
U='postgres://x:x@127.0.0.1:1/x?sslmode=disable'   # banco inexistente: o servidor tenta reconectar, não cai

cleanup() { docker rm -f "$NOME" >/dev/null 2>&1 || true; }
trap cleanup EXIT

docker run -d --name "$NOME" --network none \
  -e PORT=18099 \
  -e JWT_SECRET=smoke0123456789abcdef0123456789abcdef0123456789 \
  -e ENCRYPTION_KEY=0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef \
  -e DATABASE_URL="$U" -e DATABASE_URL_APP="$U" -e DATABASE_URL_PRIVILEGIADA="$U" \
  "$IMG" >/dev/null

# espera o servidor anunciar que subiu (ou morrer)
for _ in $(seq 1 20); do
  LOG="$(docker logs "$NOME" 2>&1 || true)"
  if grep -q "panic:" <<<"$LOG"; then
    echo "ERRO: a API entrou em pânico ao subir (provável rota conflitante):" >&2
    grep -m1 -A3 "panic:" <<<"$LOG" | cut -c1-300 >&2
    exit 1
  fi
  if grep -q "starting on port" <<<"$LOG"; then
    # dá um instante extra: o pânico de rotas acontece logo depois desse anúncio
    sleep 2
    LOG="$(docker logs "$NOME" 2>&1 || true)"
    if grep -q "panic:" <<<"$LOG"; then
      echo "ERRO: a API entrou em pânico ao subir:" >&2
      grep -m1 -A3 "panic:" <<<"$LOG" | cut -c1-300 >&2
      exit 1
    fi
    [[ "$(docker inspect -f '{{.State.Running}}' "$NOME" 2>/dev/null)" == "true" ]] \
      || { echo "ERRO: o container da API parou logo após subir:" >&2; echo "$LOG" | tail -5 >&2; exit 1; }
    echo "smoke OK: a API subiu e registrou as rotas"
    exit 0
  fi
  sleep 1
done
echo "ERRO: a API não anunciou a subida em 20 s:" >&2
docker logs "$NOME" 2>&1 | tail -8 >&2
exit 1
