#!/usr/bin/env python3
"""Parser de eventos locais do Orbit. Lê ~/.orbit/logs/*.json e ~/.orbit/client_ledger.jsonl."""

import json
import os
import glob
import sys
from datetime import datetime, timezone
from typing import Any

ORBIT_DIR = os.path.expanduser("~/.orbit")
LOGS_DIR = os.path.join(ORBIT_DIR, "logs")
LEDGER_PATH = os.path.join(ORBIT_DIR, "client_ledger.jsonl")


class ParseError(Exception):
    pass


def parse_logs() -> list[dict]:
    pattern = os.path.join(LOGS_DIR, "*.json")
    files = sorted(glob.glob(pattern))
    if not files:
        return []

    events = []
    for path in files:
        try:
            with open(path, encoding="utf-8") as f:
                data = json.load(f)
            if not isinstance(data, dict):
                raise ParseError(f"Evento inválido em {path}: esperado dict")
            events.append(data)
        except json.JSONDecodeError as e:
            raise ParseError(f"JSON inválido em {path}: {e}") from e

    return events


def parse_ledger() -> list[dict]:
    if not os.path.exists(LEDGER_PATH):
        return []

    events = []
    with open(LEDGER_PATH, encoding="utf-8") as f:
        for i, line in enumerate(f, 1):
            line = line.strip()
            if not line:
                continue
            try:
                data = json.loads(line)
                if not isinstance(data, dict):
                    raise ParseError(f"Linha {i} do ledger inválida: esperado dict")
                events.append(data)
            except json.JSONDecodeError as e:
                raise ParseError(f"JSON inválido na linha {i} do ledger: {e}") from e

    return events


def aggregate(logs: list[dict], ledger: list[dict]) -> dict[str, Any]:
    total = len(logs)
    if total == 0:
        return {
            "total_execucoes": 0,
            "sucesso": 0,
            "falhas": 0,
            "taxa_verificacao_pct": 0.0,
            "tempo_medio_ms": 0.0,
            "comandos": {},
            "linguagens": {},
            "skill_events": len(ledger),
            "tokens_estimados": 0,
            "ultimo_evento": None,
            "atualizado_em": datetime.now(timezone.utc).isoformat(),
        }

    sucesso = sum(1 for e in logs if e.get("exit_code") == 0)
    falhas = total - sucesso

    durations = [e["duration_ms"] for e in logs if isinstance(e.get("duration_ms"), (int, float))]
    tempo_medio = sum(durations) / len(durations) if durations else 0.0

    comandos: dict[str, int] = {}
    linguagens: dict[str, int] = {}
    timestamps = []

    for e in logs:
        cmd = e.get("command") or "unknown"
        comandos[cmd] = comandos.get(cmd, 0) + 1

        lang = e.get("language") or "unknown"
        linguagens[lang] = linguagens.get(lang, 0) + 1

        ts = e.get("timestamp")
        if ts:
            timestamps.append(ts)

    ultimo_evento = sorted(timestamps)[-1] if timestamps else None

    tokens_estimados = sum(
        e.get("impact_estimated_tokens", 0) or 0 for e in ledger
    )

    return {
        "total_execucoes": total,
        "sucesso": sucesso,
        "falhas": falhas,
        "taxa_verificacao_pct": round(sucesso / total * 100, 1),
        "tempo_medio_ms": round(tempo_medio, 1),
        "comandos": dict(sorted(comandos.items(), key=lambda x: -x[1])),
        "linguagens": dict(sorted(linguagens.items(), key=lambda x: -x[1])),
        "skill_events": len(ledger),
        "tokens_estimados": tokens_estimados,
        "ultimo_evento": ultimo_evento,
        "atualizado_em": datetime.now(timezone.utc).isoformat(),
    }


def run() -> dict[str, Any]:
    logs = parse_logs()
    ledger = parse_ledger()
    return aggregate(logs, ledger)


if __name__ == "__main__":
    try:
        result = run()
        print(json.dumps(result, indent=2, ensure_ascii=False))
    except ParseError as e:
        print(json.dumps({"error": str(e), "fail_closed": True}), file=sys.stderr)
        sys.exit(1)
