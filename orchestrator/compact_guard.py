"""
orchestrator/compact_guard.py — facade Python para scripts/orbit_compact_guard.sh.

Mantém o controle determinístico de compact acessível do runtime Python sem
duplicar a lógica de snapshot/detect/rehydrate (o script bash é a fonte única
da verdade). Todas as falhas propagam CompactGuardError — fail-closed.

Uso típico (ciclo automático):

    from orchestrator import compact_guard

    compact_guard.snapshot(
        session_id="sess-1",
        current_task="Executar skill X",
        objective="Responder consulta do usuário",
    )

    # ... chamada ao modelo retorna output_text ...

    if compact_guard.detect(output_text):
        ctx = compact_guard.rehydrate(session_id="sess-1")
        # ctx contém current_task, objective, constraints, last_output
"""

from __future__ import annotations

import os
import subprocess
from pathlib import Path
from typing import Optional

_REPO_ROOT   = Path(__file__).resolve().parent.parent
_GUARD_PATH  = _REPO_ROOT / "scripts" / "orbit_compact_guard.sh"
_TIMEOUT_SEC = 5.0


class CompactGuardError(Exception):
    """Raised on any fail-closed failure from the guard script."""


def _run(args: list[str]) -> subprocess.CompletedProcess:
    if not _GUARD_PATH.exists():
        raise CompactGuardError(f"guard script ausente: {_GUARD_PATH}")
    try:
        return subprocess.run(
            ["bash", str(_GUARD_PATH), *args],
            capture_output=True,
            text=True,
            timeout=_TIMEOUT_SEC,
        )
    except subprocess.TimeoutExpired as e:
        raise CompactGuardError(f"guard timeout: {args[0] if args else '?'}") from e


def snapshot(
    session_id: str,
    current_task: str,
    objective: str,
    constraints: str = "",
    last_output: str = "",
) -> None:
    """Persist snapshot. Raises CompactGuardError on failure."""
    if not session_id:
        raise CompactGuardError("session_id é obrigatório")
    proc = _run([
        "snapshot",
        "--session-id",   session_id,
        "--current-task", current_task,
        "--objective",    objective,
        "--constraints",  constraints,
        "--last-output",  last_output,
    ])
    if proc.returncode != 0:
        raise CompactGuardError(f"snapshot falhou: {proc.stderr.strip()}")


def detect(text: str) -> bool:
    """Return True if text contains the 'Compacted' marker."""
    if not text:
        return False
    proc = _run(["detect", text])
    if proc.returncode == 0:
        return True
    if proc.returncode == 1:
        return False
    raise CompactGuardError(f"detect erro: {proc.stderr.strip()}")


def rehydrate(session_id: Optional[str] = None) -> dict:
    """
    Return rehydrated context dict. Fail-closed: raises CompactGuardError
    if snapshot is missing, corrupt, or session_id mismatches.
    """
    args = ["rehydrate"]
    if session_id:
        args += ["--expect-session-id", session_id]
    proc = _run(args)
    if proc.returncode != 0:
        raise CompactGuardError(
            f"rehydrate fail-closed: {proc.stderr.strip() or proc.stdout.strip()}"
        )

    result: dict = {}
    for line in proc.stdout.splitlines():
        if ":" not in line or line.startswith("==="):
            continue
        key, _, value = line.partition(":")
        result[key.strip()] = value.strip()
    return result
