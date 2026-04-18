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
import re
import sys
import tempfile
import unittest
from datetime import datetime
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

    # ── Discriminação test vs build via error_type ────────────────────
    #
    # Contrato de shape: a view DEVE expor `error_type` preenchido
    # quando o payload persistido trouxer (ex.: "go_build_error").
    # Sem isso, a UI não tem como distinguir visualmente uma falha de
    # build de uma falha de test — ambas ficariam com apenas file:line.

    def test_build_error_surfaces_error_type(self) -> None:
        _write_log(
            self._logs, "buildXX",
            timestamp="2026-04-18T12:00:00Z",
            event="BUILD",
            command="go",
            diagnosis={
                "version":    1,
                "error_type": "go_build_error",
                "file":       "./main.go",
                "line":       3,
                "message":    "undefined: undefinedSymbol",
                "confidence": "high",
            },
        )
        _write_log(
            self._logs, "testYY",
            timestamp="2026-04-18T12:00:01Z",
            event="TEST_RUN",
            command="go",
            diagnosis={
                "version":    1,
                "error_type": "go_test_assertion",
                "test_name":  "TestA",
                "file":       "a_test.go",
                "line":       1,
                "message":    "oops",
                "confidence": "high",
            },
        )

        rds = parser.run()["recent_diagnoses"]
        by_error = {d["error_type"]: d for d in rds}

        self.assertIn("go_build_error", by_error,
                      f"build error sumiu do surface: {rds}")
        self.assertIn("go_test_assertion", by_error)

        build = by_error["go_build_error"]
        self.assertEqual(build["event"], "BUILD")
        self.assertEqual(build["test_name"], "")  # build não tem test
        self.assertEqual(build["file"], "./main.go")


class SilencedEventsContractTest(unittest.TestCase):
    """
    Silenced = decision=TRIGGER_ANALYZE + parser não contribuiu.

    Sinal operacional: se o breakdown `silenced_by_command` mostrar
    o mesmo comando repetido, é o gatilho para adicionar um parser
    novo ao dispatcher. Nunca antes.
    """

    def setUp(self) -> None:
        self._tmp = tempfile.mkdtemp()
        self._logs = Path(self._tmp) / "logs"
        self._logs.mkdir()
        self._orig_logs_dir = parser.LOGS_DIR
        parser.LOGS_DIR = str(self._logs)
        self._orig_ledger = parser.LEDGER_PATH
        parser.LEDGER_PATH = str(Path(self._tmp) / "no_ledger.jsonl")

    def tearDown(self) -> None:
        parser.LOGS_DIR = self._orig_logs_dir
        parser.LEDGER_PATH = self._orig_ledger

    def _write_analyze_log(
        self,
        name: str,
        *,
        ts: str,
        decision: str = "TRIGGER_ANALYZE",
        command: str = "cargo",
        event: str = "TEST_RUN",
        confidence: object = _SENTINEL,  # _SENTINEL = sem campo diagnosis
    ) -> None:
        payload = {
            "version":    1,
            "timestamp":  ts,
            "command":    command,
            "exit_code":  1,
            "output":     "",
            "session_id": name,
            "language":   "rust",
            "event":      event,
            "decision":   decision,
        }
        if confidence is not _SENTINEL:
            payload["diagnosis"] = {"version": 1, "confidence": confidence}
        p = self._logs / f"{ts.replace(':','-')}_{name}_exit1.json"
        p.write_text(json.dumps(payload), encoding="utf-8")

    def test_counts_triggered_analysis_without_parser_hit(self) -> None:
        # 2 cargo silenciados (sem diagnosis, none)
        self._write_analyze_log("s1", ts="2026-04-18T09:00:00Z")
        self._write_analyze_log(
            "s2", ts="2026-04-18T09:00:01Z", confidence="none",
        )
        # 1 tsc silenciado
        self._write_analyze_log("s3", ts="2026-04-18T09:00:02Z", command="tsc")
        # 1 hit: diagnose funcionou — NÃO conta
        self._write_analyze_log(
            "h1", ts="2026-04-18T09:00:03Z", confidence="high",
        )
        # Snapshot — NÃO conta
        self._write_analyze_log(
            "n1", ts="2026-04-18T09:00:04Z",
            decision="TRIGGER_SNAPSHOT", command="git", event="CODE_CHANGE",
        )
        # Execução saudável — NÃO conta
        self._write_analyze_log(
            "ok", ts="2026-04-18T09:00:05Z",
            decision="NONE", command="echo",
        )

        result = parser.run()

        self.assertEqual(result["silenced_events"], 3)
        self.assertEqual(
            result["silenced_by_command"],
            {"cargo": 2, "tsc": 1},
            f"breakdown errado: {result['silenced_by_command']}",
        )

    def test_zero_when_no_analyze_logs(self) -> None:
        self._write_analyze_log(
            "only_snapshot", ts="2026-04-18T09:10:00Z",
            decision="TRIGGER_SNAPSHOT", command="git",
        )
        result = parser.run()
        self.assertEqual(result["silenced_events"], 0)
        self.assertEqual(result["silenced_by_command"], {})

    def test_breakdown_capped_at_5(self) -> None:
        # 6 comandos distintos, todos silenciados — só top 5 aparece
        for i, cmd in enumerate(["a", "b", "c", "d", "e", "f"]):
            self._write_analyze_log(
                f"cmd{i}", ts=f"2026-04-18T09:20:{i:02d}Z", command=cmd,
            )
        result = parser.run()
        self.assertEqual(result["silenced_events"], 6)
        self.assertLessEqual(len(result["silenced_by_command"]), 5)


class ExpansionContractTest(unittest.TestCase):
    """
    Contrato operacional da regra de expansão de parser.

    Um comando X é expansion candidate apenas se, dentro da janela de
    PARSER_EXPANSION_WINDOW_DAYS, há:
      - >= PARSER_EXPANSION_THRESHOLD silenced events
      - >= PARSER_EXPANSION_MIN_DISTINCT_DAYS dias UTC distintos

    Este único teste trava TODOS os invariantes da política em subTests,
    incluindo paridade Python↔TS (regex no route.ts).
    """

    # Relógio congelado para determinismo total.
    FIXED_NOW_ISO = "2026-04-18T12:00:00Z"

    def setUp(self) -> None:
        self._tmp = tempfile.mkdtemp()
        self._logs = Path(self._tmp) / "logs"
        self._logs.mkdir()
        self._orig_logs_dir = parser.LOGS_DIR
        parser.LOGS_DIR = str(self._logs)
        self._orig_ledger = parser.LEDGER_PATH
        parser.LEDGER_PATH = str(Path(self._tmp) / "no_ledger.jsonl")
        # Congela o "now" do parser via monkeypatch do helper puro.
        self._orig_now = parser._now_epoch
        now = datetime.fromisoformat(self.FIXED_NOW_ISO.replace("Z", "+00:00"))
        self._now_epoch = now.timestamp()
        parser._now_epoch = lambda: self._now_epoch

    def tearDown(self) -> None:
        parser.LOGS_DIR = self._orig_logs_dir
        parser.LEDGER_PATH = self._orig_ledger
        parser._now_epoch = self._orig_now

    # Helper local — variante do _write_analyze_log com controle fino de ts.
    def _log(
        self, name: str, *, ts: str, command: str = "cargo",
        confidence: object = _SENTINEL,
    ) -> None:
        payload = {
            "version":    1,
            "timestamp":  ts,
            "command":    command,
            "exit_code":  1,
            "output":     "",
            "session_id": name,
            "language":   "rust",
            "event":      "TEST_RUN",
            "decision":   "TRIGGER_ANALYZE",
        }
        if confidence is not _SENTINEL:
            payload["diagnosis"] = {"version": 1, "confidence": confidence}
        p = self._logs / f"{ts.replace(':','-')}_{name}_exit1.json"
        p.write_text(json.dumps(payload), encoding="utf-8")

    def test_expansion_candidate_contract(self) -> None:
        """10 invariantes da política em subTests independentes."""

        # ── 1. below_threshold: 4 silenced em 3 dias → NÃO candidato ──
        with self.subTest("below_threshold"):
            self._reset_logs()
            for i in range(4):
                self._log(f"b{i}", ts=f"2026-04-15T10:0{i}:00Z")
            result = parser.run()
            self.assertEqual(result["expansion_candidates"], [])

        # ── 2. single_day_burst: 6 no mesmo dia → NÃO candidato ───────
        with self.subTest("single_day_burst"):
            self._reset_logs()
            for i in range(6):
                self._log(f"s{i}", ts=f"2026-04-17T10:0{i}:00Z")
            result = parser.run()
            self.assertEqual(
                result["expansion_candidates"], [],
                "burst num único dia NÃO é sustentado",
            )

        # ── 3. multi_day_at_threshold: 5 em 2 dias → candidato ─────────
        with self.subTest("multi_day_at_threshold"):
            self._reset_logs()
            for i in range(3):
                self._log(f"m{i}", ts=f"2026-04-17T10:0{i}:00Z")
            for i in range(2):
                self._log(f"n{i}", ts=f"2026-04-18T10:0{i}:00Z")
            result = parser.run()
            self.assertEqual(
                result["expansion_candidates"],
                [{"command": "cargo", "silenced_count": 5, "distinct_days": 2}],
            )

        # ── 4. outside_window: 10 silenced 10 dias atrás → não conta ──
        with self.subTest("outside_window"):
            self._reset_logs()
            for i in range(10):
                # 2026-04-08 = 10 dias antes de 04-18
                self._log(f"o{i}", ts=f"2026-04-08T10:0{i // 2}:{i % 2}0Z")
            result = parser.run()
            self.assertEqual(result["expansion_candidates"], [])

        # ── 5. command_bucket: `cargo build --release` ≡ `cargo build` ─
        with self.subTest("command_bucket"):
            self._reset_logs()
            for i in range(3):
                self._log(f"a{i}", ts=f"2026-04-17T10:0{i}:00Z",
                          command="cargo build")
            for i in range(3):
                self._log(f"b{i}", ts=f"2026-04-18T10:0{i}:00Z",
                          command="cargo build --release")
            result = parser.run()
            self.assertEqual(
                result["expansion_candidates"],
                [{"command": "cargo", "silenced_count": 6, "distinct_days": 2}],
                "flags diferentes devem colapsar no mesmo binário",
            )

        # ── 6. utc_tz_consistency: timestamp na borda da meia-noite ──
        with self.subTest("utc_tz_consistency"):
            self._reset_logs()
            # 23:59Z e 00:01Z → dois dias UTC distintos
            for i in range(3):
                self._log(f"u{i}", ts=f"2026-04-16T23:59:0{i}Z")
            for i in range(2):
                self._log(f"v{i}", ts=f"2026-04-17T00:01:0{i}Z")
            result = parser.run()
            cands = result["expansion_candidates"]
            self.assertEqual(len(cands), 1)
            self.assertEqual(cands[0]["distinct_days"], 2,
                             "dias UTC devem cruzar meia-noite corretamente")

        # ── 7. deterministic_ordering: empate de count → alfabético ───
        with self.subTest("deterministic_ordering"):
            self._reset_logs()
            for i in range(3):
                self._log(f"z{i}", ts=f"2026-04-17T10:0{i}:00Z", command="zulu")
            for i in range(2):
                self._log(f"zz{i}", ts=f"2026-04-18T10:0{i}:00Z", command="zulu")
            for i in range(3):
                self._log(f"a{i}", ts=f"2026-04-17T11:0{i}:00Z", command="alpha")
            for i in range(2):
                self._log(f"aa{i}", ts=f"2026-04-18T11:0{i}:00Z", command="alpha")
            result = parser.run()
            cmds = [c["command"] for c in result["expansion_candidates"]]
            self.assertEqual(cmds, ["alpha", "zulu"],
                             "empate de count ordena alfabeticamente asc")

        # ── 8. intent_floor_lock: trava do sentido mínimo da política ─
        with self.subTest("intent_floor_lock"):
            self.assertGreaterEqual(
                parser.PARSER_EXPANSION_THRESHOLD, 3,
                "Threshold < 3 vira ruído — recomendação especulativa",
            )
            self.assertGreaterEqual(
                parser.PARSER_EXPANSION_MIN_DISTINCT_DAYS, 2,
                "< 2 dias distintos NÃO é 'sustentado'",
            )
            self.assertGreaterEqual(
                parser.PARSER_EXPANSION_WINDOW_DAYS, 3,
                "janela < 3 dias amplifica ruído de burst",
            )

        # ── 9. cross_language_parity: TS espelha Python exatamente ────
        with self.subTest("cross_language_parity"):
            ts_path = Path(__file__).resolve().parent.parent / "site/src/app/api/dashboard/route.ts"
            ts_text = ts_path.read_text(encoding="utf-8")
            for name, py_val in [
                ("PARSER_EXPANSION_THRESHOLD",         parser.PARSER_EXPANSION_THRESHOLD),
                ("PARSER_EXPANSION_WINDOW_DAYS",       parser.PARSER_EXPANSION_WINDOW_DAYS),
                ("PARSER_EXPANSION_MIN_DISTINCT_DAYS", parser.PARSER_EXPANSION_MIN_DISTINCT_DAYS),
            ]:
                m = re.search(rf"const\s+{name}\s*=\s*(\d+)\s*;", ts_text)
                self.assertIsNotNone(m, f"constante {name} não encontrada em route.ts")
                self.assertEqual(
                    int(m.group(1)), py_val,
                    f"drift Python↔TS em {name}: py={py_val} ts={m.group(1)}",
                )

        # ── 10. policy_embedded_in_output: política viaja no JSON ──────
        with self.subTest("policy_embedded_in_output"):
            self._reset_logs()
            result = parser.run()
            self.assertEqual(result["expansion_policy"], {
                "threshold":         parser.PARSER_EXPANSION_THRESHOLD,
                "window_days":       parser.PARSER_EXPANSION_WINDOW_DAYS,
                "min_distinct_days": parser.PARSER_EXPANSION_MIN_DISTINCT_DAYS,
            })

    # Utility — limpa a pasta de logs entre subTests.
    def _reset_logs(self) -> None:
        for f in self._logs.glob("*.json"):
            f.unlink()


if __name__ == "__main__":
    unittest.main(verbosity=2)
