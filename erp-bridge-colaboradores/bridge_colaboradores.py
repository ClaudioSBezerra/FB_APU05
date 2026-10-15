#!/usr/bin/env python3
"""
ERP Bridge Colaboradores — conector ISOLADO que sincroniza centro de custo
do FB_APU05 a partir de SFC.SFC_VW_COLABORADORES (Oracle, servidor FCCORP).

Por que separado do erp-bridge-aws/bridge.py:
  Aquele bridge é COMPARTILHADO com o FB_APU02 (reforma tributária) e lê as
  tabelas SAP (s4i_*) do mesmo FCCORP. Para não arriscar a produção de lá,
  este conector é autônomo: abre sua própria conexão Oracle (SELECT
  read-only) e fala só com o backend do FB_APU05.

Minimização por LGPD: a query de origem só traz 4 colunas (nenhum CPF,
cargo, local de trabalho etc., mesmo a view tendo bem mais colunas que
isso), e o backend do FB_APU05 nunca persiste `nomfun` — só usa pra log
quando uma linha não casa com ninguém cadastrado.

  SELECT x.cod_empresa, x.centro_custo, x.numcad, x.nomfun
  FROM   SFC.SFC_VW_COLABORADORES x
  WHERE  x.sitafa <> '7'

Credenciais Oracle: por padrão buscadas em tempo de execução via
GET /api/erp-bridge/credentials (X-API-Key) — a mesma tela "ERP Bridge —
Credenciais" do FB_APU05 onde o administrador configura DSN/usuário/senha.
Isso evita duplicar o segredo Oracle no config.yaml deste conector; só a
`fbtax.api_key` é obrigatória aqui. Uma seção `oracle:` local no config.yaml
é opcional e só serve de fallback/dev (ex.: testar sem bater no backend).

Uso:
  python bridge_colaboradores.py                  # roda uma vez e sai
  python bridge_colaboradores.py --dry-run         # lê e conta, não envia
  python bridge_colaboradores.py --daemon          # loop diário (systemd)

Config (config.yaml no mesmo diretório, ou --config):
  fbtax:
    url: "https://controladoria.fbtax.cloud"
    api_key: "***"
  horario_diario: "03:00"   # só usado em --daemon
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

log = logging.getLogger("bridge-colaboradores")
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    handlers=[logging.StreamHandler(sys.stdout)],
)

SQL_COLABORADORES = """
    SELECT x.cod_empresa, x.centro_custo, x.numcad, x.nomfun
    FROM   SFC.SFC_VW_COLABORADORES x
    WHERE  x.sitafa <> '7'
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

    def sincronizar_colaboradores(self, colaboradores: list[dict]) -> dict:
        resp = requests.post(
            f"{self.base_url}/api/erp-bridge/colaboradores/sincronizar",
            headers={"X-API-Key": self.api_key, "Content-Type": "application/json"},
            json={"colaboradores": colaboradores},
            timeout=120,
        )
        if not resp.ok:
            raise RuntimeError(f"POST /colaboradores/sincronizar HTTP {resp.status_code}: {resp.text[:300]}")
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


def ler_colaboradores(conn: oracledb.Connection) -> list[dict]:
    cur = conn.cursor()
    cur.arraysize = 500
    cur.execute(SQL_COLABORADORES)
    linhas = []
    for cod_empresa, centro_custo, numcad, nomfun in cur:
        if cod_empresa is None or centro_custo is None or numcad is None:
            continue
        linhas.append({
            "cod_empresa": int(cod_empresa),
            "centro_custo": str(centro_custo).strip(),
            "numcad": int(numcad),
            "nomfun": (nomfun or "").strip(),
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
        colaboradores = ler_colaboradores(conn)
    finally:
        conn.close()

    log.info("Lidos %d colaboradores ativos (sitafa <> '7')", len(colaboradores))

    if dry_run:
        log.info("[dry-run] nada enviado — %d linha(s) seriam sincronizadas", len(colaboradores))
        return 0

    if not colaboradores:
        log.info("Nenhum colaborador para sincronizar — nada enviado")
        return 0

    total_atualizados = total_ignorados = 0
    for inicio in range(0, len(colaboradores), BATCH_SIZE):
        lote = colaboradores[inicio:inicio + BATCH_SIZE]
        try:
            resultado = fbtax.sincronizar_colaboradores(lote)
        except Exception as exc:  # noqa: BLE001
            log.error("Erro ao sincronizar lote [%d:%d]: %s", inicio, inicio + len(lote), exc)
            return 1
        total_atualizados += resultado.get("atualizados", 0)
        total_ignorados += resultado.get("ignorados", 0)
        log.info(
            "Lote [%d:%d] sincronizado: atualizados=%d ignorados=%d",
            inicio, inicio + len(lote), resultado.get("atualizados", 0), resultado.get("ignorados", 0),
        )

    log.info(
        "Sincronização concluída: %d atualizados, %d ignorados (total %d)",
        total_atualizados, total_ignorados, len(colaboradores),
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
    p = argparse.ArgumentParser(description="ERP Bridge Colaboradores — VW_COLABORADORES (FCCORP) -> FB_APU05")
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

    horario = cfg.get("horario_diario", "03:00")
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
