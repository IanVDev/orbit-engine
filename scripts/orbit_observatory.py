#!/usr/bin/env python3
"""
Observatório Orbit — leitura pura dos artefatos existentes.

Consolida quatro camadas mentais em um único dump textual:

  1. Execução — o que o Orbit está fazendo
  2. Segurança — o que está sendo protegido (redactor)
  3. Uso real — como o produto está sendo usado
  4. Sistema  — saúde do armazenamento

Não modifica nada. Reaproveita `parse_orbit_events.py` como fonte primária
de agregação e só adiciona as camadas ausentes (segurança + sistema),
ambas derivadas de leitura direta de ~/.orbit/logs/*.json.

Uso:
    python3 scripts/orbit_observatory.py              # texto humano
    python3 scripts/orbit_observatory.py --json       # JSON para pipeline

Saída JSON combina o agregado do parser + blocos `security` e `storage`,
prontos para alimentar uma API /dashboard sem código novo no core.
"""
from __future__ import annotations

import argparse
import glob
import hashlib
import json
import os
import sys
from datetime import datetime, timezone

# Importa o parser existente — ele é a fonte autoritativa de execução.
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import parse_orbit_events as parser  # noqa: E402

REDACTED_MARKER = "[REDACTED]"

# Nome do arquivo que espelha MetricExecutionWithoutLog em tracking/cmd/orbit/metrics.go.
# Mantemos a string hard-coded: divergência aqui seria uma quebra silenciosa do contrato.
METRIC_EXECUTION_WITHOUT_LOG = "execution_without_log_total"


def _resolve_metrics_dir() -> str:
    """Espelha tracking.ResolveStoreHome: ORBIT_HOME > ~/.orbit, subdir `metrics`."""
    base = os.environ.get("ORBIT_HOME", "").strip() or os.path.expanduser("~/.orbit")
    return os.path.join(base, "metrics")


def _read_counter(name: str) -> int:
    """Lê contador persistente. Arquivo ausente = 0 (contrato do writer Go).
    Parse inválido também devolve 0 — leitura é read-only e não propaga erro
    para não derrubar o observatório por uma métrica corrompida."""
    path = os.path.join(_resolve_metrics_dir(), f"{name}.count")
    try:
        with open(path, "r", encoding="utf-8") as f:
            return int(f.read().strip() or 0)
    except FileNotFoundError:
        return 0
    except (OSError, ValueError):
        return 0


def _canonical_body_hash(log: dict) -> str:
    """Paridade exata com tracking/cmd/orbit/integrity.go:CanonicalHash.
    - remove body_hash (nunca entra na própria assinatura)
    - keys ordenadas, compact, ensure_ascii=False (bate com SetEscapeHTML(false))
    Divergência aqui derruba o scan silenciosamente: se o Python produzir
    bytes diferentes do Go, todo log legítimo vira falso positivo."""
    copy = {k: v for k, v in log.items() if k != "body_hash"}
    canon = json.dumps(copy, sort_keys=True, separators=(",", ":"), ensure_ascii=False)
    return hashlib.sha256(canon.encode("utf-8")).hexdigest()


def _collect_integrity() -> dict:
    """Scan dos logs: para cada um com body_hash, recompute e compare.
    Logs legados (sem body_hash) são contados à parte — não são falha."""
    tampered: list[str] = []
    checked = 0
    missing = 0
    for path in sorted(glob.glob(os.path.join(parser.LOGS_DIR, "*.json"))):
        try:
            with open(path, encoding="utf-8") as f:
                data = json.load(f)
        except (OSError, json.JSONDecodeError):
            continue
        if not isinstance(data, dict):
            continue
        stored = data.get("body_hash")
        if not stored:
            missing += 1
            continue
        checked += 1
        if _canonical_body_hash(data) != stored:
            tampered.append(os.path.basename(path))
    return {
        "checked": checked,
        "missing_body_hash": missing,
        "tampered_count": len(tampered),
        "tampered_files": tampered[:5],
        "critical": len(tampered) > 0,
    }


def _collect_chain() -> dict:
    """Percorre logs ordenados e valida prev_proof == body_hash anterior.
    Espelha runVerifyChain em Go: legado só é tolerado ANTES da primeira
    âncora — legado após chain iniciada = reset suspeito (legacy_gap).
    Primeiro break pára o scan — fail-closed. Paridade com Go é crítica:
    divergência silenciosa produz falso positivo no dashboard."""
    paths = sorted(glob.glob(os.path.join(parser.LOGS_DIR, "*.json")))
    prev_body = ""
    checked = 0
    resets = 0
    break_at: str | None = None
    legacy_gap = False
    chain_started = False
    for path in paths:
        try:
            with open(path, encoding="utf-8") as f:
                data = json.load(f)
        except (OSError, json.JSONDecodeError):
            continue
        if not isinstance(data, dict):
            continue
        body = data.get("body_hash") or ""
        if not body:
            if chain_started:
                break_at = os.path.basename(path)
                legacy_gap = True
                break
            prev_body = ""
            resets += 1
            continue
        pp = data.get("prev_proof") or ""
        if prev_body != "" and pp != prev_body:
            break_at = os.path.basename(path)
            break
        prev_body = body
        chain_started = True
        checked += 1
    return {
        "checked": checked,
        "legacy_anchors": resets,
        "broken_at": break_at,
        "legacy_gap": legacy_gap,
        "critical": break_at is not None,
    }


def _collect_safety() -> dict:
    """Métricas de falha de proteção: contadores persistentes que o orbit run
    incrementa em situações fail-closed (ex.: execução sem log persistido)."""
    executions_without_log = _read_counter(METRIC_EXECUTION_WITHOUT_LOG)
    return {
        "executions_without_log_total": executions_without_log,
        "critical": executions_without_log > 0,
    }


def _collect_security(executions: list[dict]) -> dict:
    """Segurança vem do output persistido: conta ocorrências de [REDACTED]
    tanto em `output` (schema novo) quanto em `stdout`/`stderr` (schema
    antigo). Tolerante a ausência dos campos."""
    total_redacted = 0
    exec_with_secret = 0
    last_event_ts: str | None = None
    for e in executions:
        haystack_parts = []
        for field in ("output", "stdout", "stderr"):
            v = e.get(field)
            if isinstance(v, str):
                haystack_parts.append(v)
        # Args pode conter secrets redigidos se vieram como flags.
        args_v = e.get("args")
        if isinstance(args_v, list):
            haystack_parts.extend([a for a in args_v if isinstance(a, str)])
        haystack = "\n".join(haystack_parts)
        if not haystack:
            continue
        hits = haystack.count(REDACTED_MARKER)
        if hits > 0:
            total_redacted += hits
            exec_with_secret += 1
            ts = e.get("timestamp")
            if ts and (last_event_ts is None or ts > last_event_ts):
                last_event_ts = ts
    total = len(executions) or 1
    return {
        "secrets_redacted_total": total_redacted,
        "executions_with_secret": exec_with_secret,
        "executions_with_secret_pct": round(exec_with_secret / total * 100, 1),
        "last_secret_event": last_event_ts,
    }


def _collect_storage() -> dict:
    """Sistema: inventário bruto de ~/.orbit/logs/."""
    logs_dir = parser.LOGS_DIR
    files = sorted(glob.glob(os.path.join(logs_dir, "*.json")))
    if not files:
        return {
            "logs_dir": logs_dir,
            "file_count": 0,
            "size_bytes": 0,
            "size_human": "0 B",
            "oldest_mtime": None,
            "newest_mtime": None,
            "growth_last_7d_files": 0,
            "growth_last_7d_bytes": 0,
        }
    total_bytes = 0
    mtimes: list[float] = []
    now = datetime.now(timezone.utc).timestamp()
    growth_files = 0
    growth_bytes = 0
    cutoff_7d = now - 7 * 86400
    for p in files:
        try:
            st = os.stat(p)
        except OSError:
            continue
        total_bytes += st.st_size
        mtimes.append(st.st_mtime)
        if st.st_mtime >= cutoff_7d:
            growth_files += 1
            growth_bytes += st.st_size
    return {
        "logs_dir": logs_dir,
        "file_count": len(files),
        "size_bytes": total_bytes,
        "size_human": _humanize_bytes(total_bytes),
        "oldest_mtime": _iso(min(mtimes)) if mtimes else None,
        "newest_mtime": _iso(max(mtimes)) if mtimes else None,
        "growth_last_7d_files": growth_files,
        "growth_last_7d_bytes": growth_bytes,
    }


def _humanize_bytes(n: int) -> str:
    for unit in ("B", "KB", "MB", "GB"):
        if n < 1024 or unit == "GB":
            return f"{n:.1f} {unit}" if unit != "B" else f"{n} B"
        n /= 1024  # type: ignore[assignment]
    return f"{n} B"


def _iso(ts: float) -> str:
    return datetime.fromtimestamp(ts, tz=timezone.utc).isoformat(timespec="seconds")


def build_view() -> dict:
    """Monta o payload único consumido pelo dashboard (ou stdout humano)."""
    executions, anchors = parser.parse_logs()
    ledger = parser.parse_ledger()
    parser._link_skill_events(executions, ledger)
    aggregate = parser.aggregate(executions, anchors, ledger)
    return {
        "generated_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "execution": aggregate,
        "security": _collect_security(executions),
        "safety": _collect_safety(),
        "integrity": _collect_integrity(),
        "chain": _collect_chain(),
        "storage": _collect_storage(),
    }


def _fmt_delta(iso_ts: str | None) -> str:
    if not iso_ts:
        return "n/d"
    try:
        t = datetime.fromisoformat(iso_ts.replace("Z", "+00:00"))
    except ValueError:
        return iso_ts
    delta = datetime.now(timezone.utc) - t
    if delta.days > 0:
        return f"{iso_ts} ({delta.days}d atrás)"
    hours = delta.seconds // 3600
    if hours > 0:
        return f"{iso_ts} ({hours}h atrás)"
    mins = (delta.seconds % 3600) // 60
    return f"{iso_ts} ({mins}min atrás)"


def render_text(view: dict) -> str:
    exe = view["execution"]
    sec = view["security"]
    saf = view["safety"]
    integ = view["integrity"]
    chain = view["chain"]
    sto = view["storage"]
    lines = []
    lines.append(f"ORBIT OBSERVATORY — {view['generated_at']}")
    lines.append("")
    lines.append("── 1. Execução ─────────────────────────────────")
    lines.append(
        f"  Total: {exe['total_execucoes']}   "
        f"Sucesso: {exe['sucesso']} ({exe['taxa_verificacao_pct']}%)   "
        f"Falhas: {exe['falhas']}"
    )
    top_cmds = ", ".join(f"{k}:{v}" for k, v in list(exe["comandos"].items())[:4])
    lines.append(f"  Comandos: {top_cmds or 'n/d'}")
    lines.append(f"  Duração p50: {exe['p50_ms']}ms   p95: {exe['p95_ms']}ms")
    lines.append(f"  Último evento: {_fmt_delta(exe['ultimo_evento'])}")
    if exe["failure_types"]:
        ft = ", ".join(f"{k}:{v}" for k, v in exe["failure_types"].items() if k != "none")
        if ft:
            lines.append(f"  Falhas por tipo: {ft}")
    lines.append("")
    lines.append("── 2. Segurança ────────────────────────────────")
    lines.append(
        f"  Secrets redacted: {sec['secrets_redacted_total']}   "
        f"Exec com secret: {sec['executions_with_secret']} "
        f"({sec['executions_with_secret_pct']}%)"
    )
    lines.append(f"  Último evento sensível: {_fmt_delta(sec['last_secret_event'])}")
    ewl = saf["executions_without_log_total"]
    if saf["critical"]:
        lines.append(f"  [CRITICAL] Execuções sem log persistido: {ewl}")
    else:
        lines.append(f"  Execuções sem log persistido: {ewl}")
    if integ["critical"]:
        sample = ", ".join(integ["tampered_files"])
        lines.append(
            f"  [CRITICAL] Logs adulterados: {integ['tampered_count']} "
            f"de {integ['checked']} verificados (ex: {sample})"
        )
    else:
        lines.append(
            f"  Integridade: {integ['checked']} body_hash válidos, "
            f"{integ['missing_body_hash']} legados"
        )
    if chain["critical"]:
        if chain.get("legacy_gap"):
            lines.append(
                f"  [CRITICAL] Legacy reset detectado em {chain['broken_at']} "
                f"(log sem body_hash inserido após âncora)"
            )
        else:
            lines.append(
                f"  [CRITICAL] Chain quebrada em {chain['broken_at']} "
                f"(log removido, reordenado ou adulterado)"
            )
    else:
        lines.append(
            f"  Chain: {chain['checked']} logs encadeados, "
            f"{chain['legacy_anchors']} âncoras legadas"
        )
    lines.append("")
    lines.append("── 3. Uso real ─────────────────────────────────")
    top_lang = ", ".join(f"{k}:{v}" for k, v in list(exe["linguagens"].items())[:4])
    lines.append(f"  Linguagens: {top_lang or 'n/d'}")
    lines.append(f"  Sessões distintas: {exe['session_count']}")
    lines.append(f"  Skill events: {exe['skill_events']}   Anchors: {exe['anchor_events']}")
    lines.append("")
    lines.append("── 4. Sistema ──────────────────────────────────")
    lines.append(
        f"  Logs: {sto['file_count']} arquivos, {sto['size_human']}   "
        f"(dir: {sto['logs_dir']})"
    )
    lines.append(
        f"  Crescimento 7d: +{sto['growth_last_7d_files']} arquivos, "
        f"+{_humanize_bytes(sto['growth_last_7d_bytes'])}"
    )
    lines.append(
        f"  Janela: {sto['oldest_mtime']} → {sto['newest_mtime']}"
    )
    lines.append("")
    return "\n".join(lines)


def main() -> int:
    ap = argparse.ArgumentParser(description="Observatório Orbit (leitura pura)")
    ap.add_argument("--json", action="store_true", help="emite JSON estruturado")
    args = ap.parse_args()
    try:
        view = build_view()
    except parser.ParseError as e:
        print(json.dumps({"error": str(e), "fail_closed": True}), file=sys.stderr)
        return 1
    if args.json:
        print(json.dumps(view, indent=2, ensure_ascii=False))
    else:
        print(render_text(view))
    return 0


if __name__ == "__main__":
    sys.exit(main())
