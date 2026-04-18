"""
tests/test_dashboard_diagnosis.py — anti-regressão do contrato
`diagnosis` no parser do dashboard.

Protege 5 propriedades do shape consumido pelo dashboard:

  1. Fonte primária: payload persistido é lido direto, sem re-parse.
  2. Ausente: logs sem `diagnosis` são retrocompatíveis (não aparecem
     em recent_diagnoses, não quebram a agregação).
  3. Confidence=none: parser já disse "não sei"; não surfaceia.
  4. Shape malformado (não-dict, confidence inválido): silenciosamente
     ignorado — NUNCA derruba o dashboard.
  5. Ordem: recent_diagnoses vem por timestamp decrescente, capped em 10.

Roda standalone: python3 tests/test_dashboard_diagnosis.py
"""

from __future__ import annotations

import json
import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from scripts import parse_orbit_events as parser


def _write_log(
    dirpath: Path,
    name: str,
    *,
    timestamp: str,
    exit_code: int = 1,
    event: str = "TEST_RUN",
    command: str = "go",
    diagnosis: object = None,  # usamos `object` porque testamos shapes inválidos
) -> Path:
    payload = {
        "version":    1,
        "timestamp":  timestamp,
        "command":    command,
        "args":       ["test", "./..."],
        "exit_code":  exit_code,
        "output":     "",
        "proof":      "deadbeef",
        "session_id": name,
        "language":   "go",
        "event":      event,
        "decision":   "TRIGGER_ANALYZE",
    }
    # Só inclui diagnosis se o parametro foi passado. Permite testar "ausente".
    if diagnosis is not _SENTINEL:
        payload["diagnosis"] = diagnosis

    path = dirpath / f"{timestamp.replace(':','-')}_{name}_exit{exit_code}.json"
    path.write_text(json.dumps(payload), encoding="utf-8")
    return path


# Sentinela para distinguir "não passou nada" de "passou None".
_SENTINEL = object()


class RecentDiagnosesContractTest(unittest.TestCase):

    def setUp(self) -> None:
        self._tmp = tempfile.mkdtemp()
        self._logs = Path(self._tmp) / "logs"
        self._logs.mkdir()
        # Aponta o parser para nosso tempdir.
        self._orig_logs_dir = parser.LOGS_DIR
        parser.LOGS_DIR = str(self._logs)
        self._orig_ledger = parser.LEDGER_PATH
        parser.LEDGER_PATH = str(Path(self._tmp) / "absent_ledger.jsonl")

    def tearDown(self) -> None:
        parser.LOGS_DIR = self._orig_logs_dir
        parser.LEDGER_PATH = self._orig_ledger

    # ── #1 + #2 + #3 ─────────────────────────────────────────────────

    def test_payload_surfaces_and_filters(self) -> None:
        """
        - Log com confidence=high aparece (fonte primária, sem re-parse).
        - Log com confidence=none NÃO aparece.
        - Log sem campo diagnosis NÃO aparece.
        - Log com sucesso (exit=0) sem diagnosis também não polui a lista.
        """
        _write_log(
            self._logs, "high001",
            timestamp="2026-04-18T10:00:00Z",
            diagnosis={
                "version":     1,
                "error_type":  "go_test_assertion",
                "test_name":   "TestHigh",
                "file":        "a_test.go",
                "line":        7,
                "message":     "boom",
                "confidence":  "high",
            },
        )
        _write_log(
            self._logs, "medium02",
            timestamp="2026-04-18T10:00:01Z",
            diagnosis={
                "version":    1,
                "error_type": "file_line_only",
                "file":       "b.go",
                "line":       42,
                "message":    "nil",
                "confidence": "medium",
            },
        )
        _write_log(
            self._logs, "noneNNN",
            timestamp="2026-04-18T10:00:02Z",
            diagnosis={"version": 1, "confidence": "none"},
        )
        _write_log(
            self._logs, "absentX",
            timestamp="2026-04-18T10:00:03Z",
            diagnosis=_SENTINEL,  # campo ausente
        )
        _write_log(
            self._logs, "successY",
            timestamp="2026-04-18T10:00:04Z",
            exit_code=0,
            diagnosis=_SENTINEL,
        )

        result = parser.run()
        rds = result["recent_diagnoses"]

        self.assertEqual(len(rds), 2, f"esperava 2 diagnoses, veio: {rds}")
        tests = {d["test_name"] or d["file"] for d in rds}
        self.assertIn("TestHigh", tests)
        self.assertIn("b.go", tests)

        high = next(d for d in rds if d["test_name"] == "TestHigh")
        self.assertEqual(high["confidence"], "high")
        self.assertEqual(high["file"], "a_test.go")
        self.assertEqual(high["line"], 7)
        self.assertEqual(high["error_type"], "go_test_assertion")
        self.assertEqual(high["command"], "go")
        self.assertEqual(high["event"], "TEST_RUN")

    # ── #4 — Fail-closed: shapes inválidos nunca quebram o dashboard ─

    def test_malformed_diagnosis_is_silently_ignored(self) -> None:
        cases = [
            ("not_a_dict",       "a string instead of dict"),
            ("list_not_dict",    ["arr"]),
            ("missing_conf",     {"file": "x.go", "line": 1}),
            ("invalid_conf",     {"confidence": "super"}),
            ("null_conf",        {"confidence": None}),
        ]
        for i, (name, bad) in enumerate(cases):
            _write_log(
                self._logs, name,
                timestamp=f"2026-04-18T10:01:{i:02d}Z",
                diagnosis=bad,
            )

        # Não deve lançar. Deve devolver 0 recentes.
        result = parser.run()
        self.assertEqual(result["recent_diagnoses"], [],
                         "shapes inválidos vazaram para recent_diagnoses")

    # ── #5 — Ordem + limite ───────────────────────────────────────────

    def test_ordering_desc_and_limited_to_10(self) -> None:
        for i in range(15):
            _write_log(
                self._logs, f"seq{i:02d}",
                timestamp=f"2026-04-18T11:{i:02d}:00Z",
                diagnosis={
                    "confidence": "medium",
                    "file": f"f{i}.go",
                    "line": i + 1,
                },
            )

        result = parser.run()
        rds = result["recent_diagnoses"]

        self.assertEqual(len(rds), 10, "limite de 10 violado")
        # Mais recentes primeiro — o "i" maior tem timestamp maior.
        self.assertEqual(rds[0]["file"], "f14.go")
        self.assertEqual(rds[-1]["file"], "f5.go")
        tss = [d["timestamp"] for d in rds]
        self.assertEqual(tss, sorted(tss, reverse=True), "ordem não é desc")

    # ── Compatibilidade: ausência global ──────────────────────────────

    def test_no_logs_at_all_returns_empty_list(self) -> None:
        result = parser.run()
        self.assertIn("recent_diagnoses", result)
        self.assertEqual(result["recent_diagnoses"], [])


if __name__ == "__main__":
    unittest.main(verbosity=2)
