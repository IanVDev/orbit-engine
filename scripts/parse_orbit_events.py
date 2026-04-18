#!/usr/bin/env python3
"""Parser de eventos locais do Orbit. Lê ~/.orbit/logs/*.json e ~/.orbit/client_ledger.jsonl."""

import json
import os
import glob
import re
import sys
from datetime import datetime, timezone
from typing import Any, Optional

ORBIT_DIR = os.path.expanduser("~/.orbit")
LOGS_DIR = os.path.join(ORBIT_DIR, "logs")
LEDGER_PATH = os.path.join(ORBIT_DIR, "client_ledger.jsonl")

# Campos obrigatórios para eventos de execução (version >= 1).
# Ausência de qualquer um destes em um log versionado causa ParseError (fail-closed).
EXECUTION_ESSENTIAL_FIELDS = ("timestamp", "exit_code", "command", "language")

FAILURE_TYPES: dict[int, str] = {
    0: "none",
    1: "runtime_error",
    7: "verification_failed",
    127: "command_not_found",
    254: "system_error",
}

# Padrão de nome de arquivo: {ts}_{nano?}_{hex}_exit{code}.json
_FILENAME_SESSION_RE = re.compile(r"_([0-9a-f]{8})_exit\d+\.json$")


class ParseError(Exception):
    pass


def _failure_type(exit_code: Optional[int]) -> str:
    if exit_code is None:
        return "unknown"
    return FAILURE_TYPES.get(exit_code, f"exit_{exit_code}")


def _derive_session_id(filename: str) -> Optional[str]:
    """Extrai o session_key do nome do arquivo (hex de 8 chars antes de _exit)."""
    m = _FILENAME_SESSION_RE.search(filename)
    return m.group(1) if m else None


def _percentile(data: list[float], pct: float) -> float:
    """Percentil simples por interpolação linear (sem dependências externas)."""
    if not data:
        return 0.0
    s = sorted(data)
    n = len(s)
    idx = (pct / 100) * (n - 1)
    lo = int(idx)
    hi = lo + 1
    if hi >= n:
        return round(s[-1], 1)
    frac = idx - lo
    return round(s[lo] + frac * (s[hi] - s[lo]), 1)


def _is_execution_log(data: dict) -> bool:
    """Retorna True se o log é uma execução (tem exit_code ou version)."""
    return "exit_code" in data or "version" in data


def _validate_execution(data: dict, path: str) -> None:
    """Falha com ParseError se um campo essencial estiver ausente em um log versionado."""
    if data.get("version") is not None:
        for field in EXECUTION_ESSENTIAL_FIELDS:
            if field not in data:
                raise ParseError(
                    f"Campo essencial '{field}' ausente em {os.path.basename(path)}"
                )


def parse_logs() -> tuple[list[dict], list[dict]]:
    """Retorna (execution_logs, anchor_logs) separados por tipo."""
    pattern = os.path.join(LOGS_DIR, "*.json")
    files = sorted(glob.glob(pattern))
    if not files:
        return [], []

    executions: list[dict] = []
    anchors: list[dict] = []

    for path in files:
        try:
            with open(path, encoding="utf-8") as f:
                data = json.load(f)
        except json.JSONDecodeError as e:
            raise ParseError(f"JSON inválido em {os.path.basename(path)}: {e}") from e

        if not isinstance(data, dict):
            raise ParseError(f"Evento inválido em {os.path.basename(path)}: esperado dict")

        if not data.get("timestamp"):
            raise ParseError(
                f"Campo essencial 'timestamp' ausente em {os.path.basename(path)}"
            )

        if _is_execution_log(data):
            _validate_execution(data, path)
            fname = os.path.basename(path)
            data.setdefault("session_id", _derive_session_id(fname))
            data.setdefault("parent_event_id", None)
            data["failure_type"] = _failure_type(data.get("exit_code"))
            executions.append(data)
        else:
            anchors.append(data)

    return executions, anchors


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


def _link_skill_events(executions: list[dict], ledger: list[dict]) -> None:
    """
    Relaciona skill events com execuções por proximidade temporal (±60s).
    Preenche parent_event_id no ledger entry quando há correspondência única.
    Todo link gerado aqui é marcado como inferido: link_method="temporal",
    link_confidence="low". Nunca deve ser tratado como determinístico.
    """
    if not executions or not ledger:
        return

    def ts_epoch(iso: str) -> float:
        try:
            return datetime.fromisoformat(iso.replace("Z", "+00:00")).timestamp()
        except ValueError:
            return 0.0

    exec_times = [(e.get("execution_id"), ts_epoch(e["timestamp"])) for e in executions]

    for entry in ledger:
        entry_ts = ts_epoch(entry.get("timestamp", ""))
        if not entry_ts:
            continue
        closest_id = None
        closest_delta = float("inf")
        for exec_id, exec_ts in exec_times:
            delta = abs(entry_ts - exec_ts)
            if delta < closest_delta and delta <= 60:
                closest_delta = delta
                closest_id = exec_id
        if closest_id:
            entry["parent_event_id"] = closest_id
            entry["link_method"] = "temporal"
            entry["link_confidence"] = "low"
            entry["link_semantic"] = "non_causal"
            entry["link_window_seconds"] = 60


# ---------------------------------------------------------------------------
# Diagnosis payload (contract surfaced to dashboard)
# ---------------------------------------------------------------------------
#
# O log pode trazer um campo opcional `diagnosis` já preenchido pelo Go no
# momento do `orbit run` (ver run.go → DiagnosisPayload). Contrato:
#
#   diagnosis: {
#     "version":     int,
#     "error_type":  str?,
#     "test_name":   str?,
#     "file":        str?,
#     "line":        int?,
#     "message":     str?,
#     "confidence":  "high" | "medium" | "none"
#   }
#
# Parser é FONTE SECUNDÁRIA: só lê o payload persistido. Não tenta inferir
# de `output` se `diagnosis` está ausente ou malformado. Fail-closed:
#   - ausente          → não aparece em recent_diagnoses (retrocompat)
#   - não-dict         → ignorado
#   - sem confidence   → ignorado
#   - confidence=none  → ignorado (parser já disse "não sei")
_VALID_CONFIDENCE = frozenset({"high", "medium"})
_RECENT_DIAGNOSES_LIMIT = 10


def _extract_diagnosis_view(exec_log: dict) -> Optional[dict]:
    """Fail-closed: devolve dict pronto para surfacear, ou None."""
    d = exec_log.get("diagnosis")
    if not isinstance(d, dict):
        return None
    confidence = d.get("confidence")
    if confidence not in _VALID_CONFIDENCE:
        return None
    return {
        "timestamp":   exec_log.get("timestamp"),
        "command":     exec_log.get("command"),
        "event":       exec_log.get("event"),
        "exit_code":   exec_log.get("exit_code"),
        "error_type":  d.get("error_type", ""),
        "test_name":   d.get("test_name", ""),
        "file":        d.get("file", ""),
        "line":        d.get("line", 0),
        "message":     d.get("message", ""),
        "confidence":  confidence,
    }


def _collect_recent_diagnoses(executions: list[dict]) -> list[dict]:
    """Top-N mais recentes (timestamp desc) com confidence high/medium."""
    views = [v for v in (_extract_diagnosis_view(e) for e in executions) if v]
    views.sort(key=lambda v: v.get("timestamp") or "", reverse=True)
    return views[:_RECENT_DIAGNOSES_LIMIT]


def aggregate(
    executions: list[dict], anchors: list[dict], ledger: list[dict]
) -> dict[str, Any]:
    total = len(executions)

    if total == 0:
        return {
            "total_execucoes": 0,
            "sucesso": 0,
            "falhas": 0,
            "taxa_verificacao_pct": 0.0,
            "tempo_medio_ms": 0.0,
            "p50_ms": 0.0,
            "p95_ms": 0.0,
            "failure_types": {},
            "comandos": {},
            "linguagens": {},
            "session_count": 0,
            "anchor_events": len(anchors),
            "skill_events": len(ledger),
            "tokens_estimados": 0,
            "ultimo_evento": None,
            "recent_diagnoses": [],
            "atualizado_em": datetime.now(timezone.utc).isoformat(),
        }

    sucesso = sum(1 for e in executions if e.get("exit_code") == 0)
    falhas = total - sucesso

    durations = [
        e["duration_ms"]
        for e in executions
        if isinstance(e.get("duration_ms"), (int, float))
    ]
    tempo_medio = sum(durations) / len(durations) if durations else 0.0
    p50 = _percentile(durations, 50)
    p95 = _percentile(durations, 95)

    comandos: dict[str, int] = {}
    linguagens: dict[str, int] = {}
    failure_types: dict[str, int] = {}
    session_ids: set[str] = set()
    timestamps: list[str] = []

    for e in executions:
        cmd = e.get("command") or "unknown"
        comandos[cmd] = comandos.get(cmd, 0) + 1

        lang = e.get("language") or "unknown"
        linguagens[lang] = linguagens.get(lang, 0) + 1

        ft = e.get("failure_type", "unknown")
        failure_types[ft] = failure_types.get(ft, 0) + 1

        sid = e.get("session_id")
        if sid:
            session_ids.add(sid)

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
        "p50_ms": p50,
        "p95_ms": p95,
        "failure_types": dict(sorted(failure_types.items(), key=lambda x: -x[1])),
        "comandos": dict(sorted(comandos.items(), key=lambda x: -x[1])),
        "linguagens": dict(sorted(linguagens.items(), key=lambda x: -x[1])),
        "session_count": len(session_ids),
        "anchor_events": len(anchors),
        "skill_events": len(ledger),
        "tokens_estimados": tokens_estimados,
        "ultimo_evento": ultimo_evento,
        "recent_diagnoses": _collect_recent_diagnoses(executions),
        "atualizado_em": datetime.now(timezone.utc).isoformat(),
    }


def run() -> dict[str, Any]:
    executions, anchors = parse_logs()
    ledger = parse_ledger()
    _link_skill_events(executions, ledger)
    return aggregate(executions, anchors, ledger)


if __name__ == "__main__":
    try:
        result = run()
        print(json.dumps(result, indent=2, ensure_ascii=False))
    except ParseError as e:
        print(json.dumps({"error": str(e), "fail_closed": True}), file=sys.stderr)
        sys.exit(1)
