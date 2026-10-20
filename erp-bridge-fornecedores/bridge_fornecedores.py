#!/usr/bin/env python3
"""
ERP Bridge Fornecedores — conector ISOLADO que sincroniza o cadastro de
fornecedores do FB_APU05 a partir de FORN (Oracle, servidor FCCORP).

Por que separado do erp-bridge-aws/bridge.py e do erp-bridge-colaboradores/:
  Mesmo princípio dos demais conectores do FB_APU05 — autônomo, conexão
  Oracle própria (SELECT read-only), fala só com o backend do FB_APU05. Não
  compartilha nada com o conector do FB_APU02 nem com o de colaboradores.

Query de origem (acordada com o negócio):

  SELECT f.cgc, f.razsoc, f.fantas, f.uf
  FROM   FORN f

Diferente do bridge_colaboradores.py (que só faz UPDATE num usuário já
existente): o backend do FB_APU05 trata `fornecedores` como espelho
read-only do FORN — um cgc novo vira um registro novo, um cgc já conhecido
tem seus dados atualizados.

Credenciais Oracle: por padrão buscadas em tempo de execução via
GET /api/erp-bridge/credentials (X-API-Key) — mesma tela "ERP Bridge —
Credenciais" do FB_APU05, mesma API key já usada pelo conector de
colaboradores (um único X-API-Key por instância do FB_APU05, não um por
conector). Uma seção `oracle:` local no config.yaml é opcional e só serve
de fallback/dev.

Uso:
  python bridge_fornecedores.py                  # roda uma vez e sai
  python bridge_fornecedores.py --dry-run         # lê e conta, não envia
  python bridge_fornecedores.py --daemon          # loop diário (systemd)

Config (config.yaml no mesmo diretório, ou --config):
  fbtax:
    url: "https://controladoria.fbtax.cloud"
    api_key: "***"
  horario_diario: "03:30"   # só usado em --daemon
"""
from __future__ import annotations

import argparse
import logging
import os
import sys
import time
from datetime import datetime, timedelta
from pathlib import Path

import oracledb
import requests
import yaml

BASE_DIR = Path(__file__).resolve().parent

log = logging.getLogger("bridge-fornecedores")
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    handlers=[logging.StreamHandler(sys.stdout)],
)

SQL_FORNECEDORES = """
    SELECT f.cgc, f.razsoc, f.fantas, f.uf
    FROM   FORN f
"""

MAX_CONN_RETRIES = 3
CONN_RETRY_DELAY = 5
BATCH_SIZE = 500


class FBTax:
    def __init__(self, cfg: dict):
        self.base_url = cfg["url"].rstrip("/")
        self.api_key = cfg["api_key"]

    def buscar_credenciais_oracle(self) -> dict:
        """GET /api/erp-bridge/credentials — credenciais remotas, configuradas
        na tela ERP Bridge do FB_APU05 (sobrescrevem qualquer `oracle:` local
        no config.yaml deste conector)."""
        resp = requests.get(
            f"{self.base_url}/api/erp-bridge/credentials",
            headers={"X-API-Key": self.api_key},
            timeout=30,
        )
        if not resp.ok:
            raise RuntimeError(f"GET /credentials HTTP {resp.status_code}: {resp.text[:300]}")
        return resp.json()

    def sincronizar_fornecedores(self, fornecedores: list[dict]) -> dict:
        resp = requests.post(
            f"{self.base_url}/api/erp-bridge/fornecedores/sincronizar",
            headers={"X-API-Key": self.api_key, "Content-Type": "application/json"},
            json={"fornecedores": fornecedores},
            timeout=120,
        )
        if not resp.ok:
            raise RuntimeError(f"POST /fornecedores/sincronizar HTTP {resp.status_code}: {resp.text[:300]}")
        return resp.json()


def conectar_oracle(dsn: str, usuario: str, senha: str) -> oracledb.Connection:
    last = None
    for tentativa in range(1, MAX_CONN_RETRIES + 1):
        try:
            conn = oracledb.connect(user=usuario, password=senha, dsn=dsn, expire_time=2)
            log.info("Conectado ao FCCORP %s (thin mode)", dsn)
            return conn
        except Exception as exc:  # noqa: BLE001
            last = exc
            log.warning("Falha conexão (tentativa %d/%d): %s", tentativa, MAX_CONN_RETRIES, exc)
            time.sleep(CONN_RETRY_DELAY * tentativa)
    raise last  # type: ignore[misc]


def ler_fornecedores(conn: oracledb.Connection) -> list[dict]:
    cur = conn.cursor()
    cur.arraysize = 500
    cur.execute(SQL_FORNECEDORES)
    linhas = []
    for cgc, razsoc, fantas, uf in cur:
        if cgc is None or razsoc is None:
            continue
        linhas.append({
            "cgc": str(cgc).strip(),
            "razsoc": str(razsoc).strip(),
            "fantas": (fantas or "").strip(),
            "uf": (uf or "").strip(),
        })
    cur.close()
    return linhas


def executar_sincronizacao(cfg: dict, dry_run: bool) -> int:
    fbtax = FBTax(cfg["fbtax"])

    oracle_cfg = dict(cfg.get("oracle") or {})
    try:
        remotas = fbtax.buscar_credenciais_oracle()
        if remotas.get("oracle_dsn"):
            oracle_cfg["dsn"] = remotas["oracle_dsn"]
        if remotas.get("oracle_usuario"):
            oracle_cfg["usuario"] = remotas["oracle_usuario"]
        if remotas.get("oracle_senha"):
            oracle_cfg["senha"] = remotas["oracle_senha"]
        log.info("Credenciais Oracle obtidas via GET /api/erp-bridge/credentials")
    except Exception as exc:  # noqa: BLE001
        if not oracle_cfg:
            log.error("Não foi possível buscar credenciais remotas e não há seção 'oracle:' local: %s", exc)
            return 1
        log.warning("Falha ao buscar credenciais remotas, usando config.yaml local: %s", exc)

    for campo in ("dsn", "usuario", "senha"):
        if not oracle_cfg.get(campo):
            log.error("Configuração Oracle incompleta: falta '%s'", campo)
            return 1

    conn = conectar_oracle(oracle_cfg["dsn"], oracle_cfg["usuario"], oracle_cfg["senha"])
    try:
        fornecedores = ler_fornecedores(conn)
    finally:
        conn.close()

    log.info("Lidos %d fornecedores", len(fornecedores))

    if dry_run:
        log.info("[dry-run] nada enviado — %d linha(s) seriam sincronizadas", len(fornecedores))
        return 0

    if not fornecedores:
        log.info("Nenhum fornecedor para sincronizar — nada enviado")
        return 0

    total_criados = total_atualizados = total_ignorados = 0
    for inicio in range(0, len(fornecedores), BATCH_SIZE):
        lote = fornecedores[inicio:inicio + BATCH_SIZE]
        try:
            resultado = fbtax.sincronizar_fornecedores(lote)
        except Exception as exc:  # noqa: BLE001
            log.error("Erro ao sincronizar lote [%d:%d]: %s", inicio, inicio + len(lote), exc)
            return 1
        total_criados += resultado.get("criados", 0)
        total_atualizados += resultado.get("atualizados", 0)
        total_ignorados += resultado.get("ignorados", 0)
        log.info(
            "Lote [%d:%d] sincronizado: criados=%d atualizados=%d ignorados=%d",
            inicio, inicio + len(lote),
            resultado.get("criados", 0), resultado.get("atualizados", 0), resultado.get("ignorados", 0),
        )

    log.info(
        "Sincronização concluída: %d criados, %d atualizados, %d ignorados (total %d)",
        total_criados, total_atualizados, total_ignorados, len(fornecedores),
    )
    return 0


def segundos_ate_proximo_horario(horario_str: str) -> float:
    agora = datetime.now()
    hora, minuto = (int(p) for p in horario_str.split(":"))
    proximo = agora.replace(hour=hora, minute=minuto, second=0, microsecond=0)
    if proximo <= agora:
        proximo += timedelta(days=1)
    return (proximo - agora).total_seconds()


def main() -> int:
    p = argparse.ArgumentParser(description="ERP Bridge Fornecedores — FORN (FCCORP) -> FB_APU05")
    p.add_argument("--config", default=str(BASE_DIR / "config.yaml"))
    p.add_argument("--dry-run", action="store_true", help="lê e conta, sem enviar nada ao FB_APU05")
    p.add_argument("--daemon", action="store_true", help="loop contínuo: roda 1x/dia no horário configurado")
    args = p.parse_args()

    if not os.path.exists(args.config):
        log.error("config não encontrado: %s", args.config)
        return 1
    with open(args.config) as f:
        cfg = yaml.safe_load(f)

    if not args.daemon:
        return executar_sincronizacao(cfg, args.dry_run)

    horario = cfg.get("horario_diario", "03:30")
    log.info("Modo daemon — sincronização diária às %s", horario)
    while True:
        espera = segundos_ate_proximo_horario(horario)
        log.info("Próxima execução em %.1f minutos", espera / 60)
        time.sleep(espera)
        try:
            executar_sincronizacao(cfg, args.dry_run)
        except Exception as exc:  # noqa: BLE001
            log.error("Execução diária falhou: %s", exc)


if __name__ == "__main__":
    sys.exit(main())
