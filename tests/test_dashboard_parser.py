"""
Teste anti-regressão do parser do dashboard Orbit.
Valida comportamento com dados reais e edge cases de fail-closed.
"""

import json
import os
import sys
import tempfile
import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "scripts"))

import parse_orbit_events as p


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def make_log(
    exit_code=0,
    duration_ms=100,
    command="run",
    language="shell",
    timestamp="2026-04-01T00:00:00Z",
    version=1,
    execution_id="exec-001",
    **extra,
) -> dict:
    base = {
        "version": version,
        "timestamp": timestamp,
        "command": command,
        "language": language,
        "exit_code": exit_code,
        "duration_ms": duration_ms,
        "execution_id": execution_id,
    }
    base.update(extra)
    return base


def write_logs(tmpdir: str, logs: list[dict]) -> str:
    logs_dir = os.path.join(tmpdir, "logs")
    os.makedirs(logs_dir)
    for i, log in enumerate(logs):
        ec = log.get("exit_code", 0)
        fname = f"2026040{i}T000000Z_abc1234{i}_exit{ec}.json"
        with open(os.path.join(logs_dir, fname), "w") as f:
            json.dump(log, f)
    return logs_dir


# ---------------------------------------------------------------------------
# Dados reais (integração leve)
# ---------------------------------------------------------------------------

class TestRealData:
    def test_parse_returns_expected_count(self):
        """Parser deve retornar 262 execuções e 9 eventos âncora dos dados reais."""
        executions, anchors = p.parse_logs()
        assert len(executions) == 262, (
            f"Esperado 262 execuções, obtido {len(executions)}. "
            "Novos logs foram adicionados? Atualize este valor."
        )
        assert len(anchors) == 9, (
            f"Esperado 9 eventos âncora, obtido {len(anchors)}."
        )

    def test_all_executions_have_failure_type(self):
        executions, _ = p.parse_logs()
        missing = [e for e in executions if "failure_type" not in e]
        assert not missing, f"{len(missing)} execuções sem failure_type"

    def test_all_executions_have_timestamp(self):
        executions, _ = p.parse_logs()
        missing = [e for e in executions if not e.get("timestamp")]
        assert not missing, f"{len(missing)} execuções sem timestamp"

    def test_aggregate_totals_consistent(self):
        executions, anchors = p.parse_logs()
        ledger = p.parse_ledger()
        stats = p.aggregate(executions, anchors, ledger)
        assert stats["sucesso"] + stats["falhas"] == stats["total_execucoes"]
        assert stats["taxa_verificacao_pct"] == round(
            stats["sucesso"] / stats["total_execucoes"] * 100, 1
        )

    def test_p50_lte_p95(self):
        executions, anchors = p.parse_logs()
        ledger = p.parse_ledger()
        stats = p.aggregate(executions, anchors, ledger)
        assert stats["p50_ms"] <= stats["p95_ms"], (
            f"p50={stats['p50_ms']} > p95={stats['p95_ms']}"
        )

    def test_failure_types_sum_equals_total(self):
        executions, anchors = p.parse_logs()
        ledger = p.parse_ledger()
        stats = p.aggregate(executions, anchors, ledger)
        ft_total = sum(stats["failure_types"].values())
        assert ft_total == stats["total_execucoes"], (
            f"failure_types soma {ft_total} ≠ total {stats['total_execucoes']}"
        )


# ---------------------------------------------------------------------------
# failure_type mapping
# ---------------------------------------------------------------------------

class TestFailureType:
    @pytest.mark.parametrize("exit_code,expected", [
        (0, "none"),
        (1, "runtime_error"),
        (7, "verification_failed"),
        (127, "command_not_found"),
        (254, "system_error"),
        (None, "unknown"),
        (99, "exit_99"),
    ])
    def test_mapping(self, exit_code, expected):
        assert p._failure_type(exit_code) == expected


# ---------------------------------------------------------------------------
# Percentil
# ---------------------------------------------------------------------------

class TestPercentile:
    def test_empty(self):
        assert p._percentile([], 50) == 0.0

    def test_single(self):
        assert p._percentile([42.0], 50) == 42.0

    def test_p50_odd(self):
        assert p._percentile([1, 2, 3, 4, 5], 50) == 3.0

    def test_p50_even(self):
        assert p._percentile([1, 2, 3, 4], 50) == 2.5

    def test_p95_simple(self):
        data = list(range(1, 101))  # 1..100
        result = p._percentile(data, 95)
        assert 94.0 <= result <= 96.0

    def test_p50_lte_p95(self):
        data = [10, 20, 300, 400, 500]
        assert p._percentile(data, 50) <= p._percentile(data, 95)


# ---------------------------------------------------------------------------
# session_id derivado do nome do arquivo
# ---------------------------------------------------------------------------

class TestDeriveSessionId:
    def test_full_pattern(self):
        assert p._derive_session_id("20260402T003807Z_a6fa6323_exit0.json") == "a6fa6323"

    def test_with_nano(self):
        assert p._derive_session_id("20260402T005025Z_1775091025687877000_af86c212_exit0.json") == "af86c212"

    def test_old_format_no_hex(self):
        assert p._derive_session_id("20260402T002527Z_exit0.json") is None

    def test_exit7(self):
        assert p._derive_session_id("20260402T003807Z_85fc52d8_exit7.json") == "85fc52d8"


# ---------------------------------------------------------------------------
# Fail-closed: campos essenciais ausentes
# ---------------------------------------------------------------------------

class TestFailClosed:
    def test_missing_timestamp_raises(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            logs_dir = os.path.join(tmpdir, "logs")
            os.makedirs(logs_dir)
            bad = {"version": 1, "command": "run", "language": "shell", "exit_code": 0, "duration_ms": 10}
            with open(os.path.join(logs_dir, "bad_exit0.json"), "w") as f:
                json.dump(bad, f)
            orig = p.LOGS_DIR
            p.LOGS_DIR = logs_dir
            try:
                with pytest.raises(p.ParseError, match="timestamp"):
                    p.parse_logs()
            finally:
                p.LOGS_DIR = orig

    def test_missing_exit_code_in_versioned_log_raises(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            logs_dir = os.path.join(tmpdir, "logs")
            os.makedirs(logs_dir)
            bad = {"version": 1, "timestamp": "2026-04-01T00:00:00Z", "command": "run", "language": "shell", "duration_ms": 10}
            with open(os.path.join(logs_dir, "20260401T000000Z_aabb1122_exit0.json"), "w") as f:
                json.dump(bad, f)
            orig = p.LOGS_DIR
            p.LOGS_DIR = logs_dir
            try:
                with pytest.raises(p.ParseError, match="exit_code"):
                    p.parse_logs()
            finally:
                p.LOGS_DIR = orig

    def test_invalid_json_raises(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            logs_dir = os.path.join(tmpdir, "logs")
            os.makedirs(logs_dir)
            with open(os.path.join(logs_dir, "corrupt_exit0.json"), "w") as f:
                f.write("{not valid json")
            orig = p.LOGS_DIR
            p.LOGS_DIR = logs_dir
            try:
                with pytest.raises(p.ParseError, match="JSON inválido"):
                    p.parse_logs()
            finally:
                p.LOGS_DIR = orig

    def test_anchor_event_no_exit_code_not_raises(self):
        """Eventos âncora (sem version/exit_code) não devem causar erro."""
        with tempfile.TemporaryDirectory() as tmpdir:
            logs_dir = os.path.join(tmpdir, "logs")
            os.makedirs(logs_dir)
            anchor = {"execution_id": "abc", "anchor_status": "failed", "timestamp": "2026-04-01T00:00:00Z"}
            with open(os.path.join(logs_dir, "20260401T000000Z_anchor.json"), "w") as f:
                json.dump(anchor, f)
            orig = p.LOGS_DIR
            p.LOGS_DIR = logs_dir
            try:
                execs, anchors = p.parse_logs()
                assert len(execs) == 0
                assert len(anchors) == 1
            finally:
                p.LOGS_DIR = orig


# ---------------------------------------------------------------------------
# Aggregate com dados sintéticos
# ---------------------------------------------------------------------------

class TestAggregateUnit:
    def test_empty_logs(self):
        stats = p.aggregate([], [], [])
        assert stats["total_execucoes"] == 0
        assert stats["taxa_verificacao_pct"] == 0.0

    def test_all_success(self):
        execs = [
            {**make_log(exit_code=0, duration_ms=10), "session_id": "s1", "parent_event_id": None, "failure_type": "none"},
            {**make_log(exit_code=0, duration_ms=20), "session_id": "s2", "parent_event_id": None, "failure_type": "none"},
        ]
        stats = p.aggregate(execs, [], [])
        assert stats["taxa_verificacao_pct"] == 100.0
        assert stats["falhas"] == 0

    def test_mixed_failure_types(self):
        execs = [
            {**make_log(exit_code=0, duration_ms=10), "session_id": None, "parent_event_id": None, "failure_type": "none"},
            {**make_log(exit_code=7, duration_ms=5), "session_id": None, "parent_event_id": None, "failure_type": "verification_failed"},
            {**make_log(exit_code=1, duration_ms=50), "session_id": None, "parent_event_id": None, "failure_type": "runtime_error"},
        ]
        stats = p.aggregate(execs, [], [])
        assert stats["failure_types"]["none"] == 1
        assert stats["failure_types"]["verification_failed"] == 1
        assert stats["failure_types"]["runtime_error"] == 1
        assert stats["falhas"] == 2


# ---------------------------------------------------------------------------
# Linking temporal: todo link inferido deve ser marcado explicitamente
# ---------------------------------------------------------------------------

class TestTemporalLinking:
    def _make_exec(self, execution_id: str, timestamp: str) -> dict:
        return {
            **make_log(execution_id=execution_id, timestamp=timestamp),
            "session_id": None,
            "parent_event_id": None,
            "failure_type": "none",
        }

    def _make_ledger_entry(self, timestamp: str) -> dict:
        return {"timestamp": timestamp, "impact_estimated_tokens": 10}

    def test_linked_entry_has_method_and_confidence(self):
        """Ledger entry dentro da janela de 60s deve ter link_method e link_confidence."""
        execs = [self._make_exec("exec-001", "2026-04-01T00:00:00Z")]
        ledger = [self._make_ledger_entry("2026-04-01T00:00:30Z")]  # 30s depois
        p._link_skill_events(execs, ledger)
        assert ledger[0]["parent_event_id"] == "exec-001"
        assert ledger[0]["link_method"] == "temporal"
        assert ledger[0]["link_confidence"] == "low"

    def test_unlinked_entry_has_no_method_or_confidence(self):
        """Ledger entry fora da janela de 60s não deve receber nenhum campo de link."""
        execs = [self._make_exec("exec-001", "2026-04-01T00:00:00Z")]
        ledger = [self._make_ledger_entry("2026-04-01T00:02:01Z")]  # 121s depois
        p._link_skill_events(execs, ledger)
        assert "parent_event_id" not in ledger[0]
        assert "link_method" not in ledger[0]
        assert "link_confidence" not in ledger[0]

    def test_no_deterministic_link_without_marker(self):
        """Nenhum evento com parent_event_id pode estar sem link_method."""
        execs = [
            self._make_exec("exec-A", "2026-04-01T00:00:00Z"),
            self._make_exec("exec-B", "2026-04-01T00:01:00Z"),
        ]
        ledger = [
            self._make_ledger_entry("2026-04-01T00:00:10Z"),
            self._make_ledger_entry("2026-04-01T00:01:05Z"),
            self._make_ledger_entry("2026-04-01T00:05:00Z"),  # fora da janela
        ]
        p._link_skill_events(execs, ledger)
        for entry in ledger:
            if "parent_event_id" in entry:
                assert entry.get("link_method") == "temporal", (
                    "parent_event_id presente sem link_method='temporal'"
                )
                assert entry.get("link_confidence") == "low", (
                    "parent_event_id presente sem link_confidence='low'"
                )

    def test_real_data_all_linked_entries_marked(self):
        """Nos dados reais, todo entry com parent_event_id deve ter os marcadores."""
        executions, _ = p.parse_logs()
        ledger = p.parse_ledger()
        p._link_skill_events(executions, ledger)
        violations = [
            e for e in ledger
            if "parent_event_id" in e
            and (e.get("link_method") != "temporal" or e.get("link_confidence") != "low")
        ]
        assert not violations, (
            f"{len(violations)} entries com parent_event_id sem marcadores corretos"
        )

    def test_link_confidence_is_never_high(self):
        """link_confidence nunca pode ser 'high' — inferência temporal é sempre baixa."""
        executions, _ = p.parse_logs()
        ledger = p.parse_ledger()
        p._link_skill_events(executions, ledger)
        high_confidence = [
            e for e in ledger if e.get("link_confidence") == "high"
        ]
        assert not high_confidence, (
            f"{len(high_confidence)} entries com link_confidence='high' (proibido)"
        )

    # ── Contrato completo: todos os campos obrigatórios ──────────────────────

    def test_linked_entry_has_full_contract(self):
        """Todo entry com parent_event_id deve ter TODOS os 4 campos de link."""
        execs = [self._make_exec("exec-X", "2026-04-01T12:00:00Z")]
        ledger = [self._make_ledger_entry("2026-04-01T12:00:10Z")]  # 10s depois
        p._link_skill_events(execs, ledger)
        entry = ledger[0]
        assert entry.get("parent_event_id") == "exec-X"
        assert entry.get("link_method") == "temporal"
        assert entry.get("link_confidence") == "low"
        assert entry.get("link_semantic") == "non_causal"
        assert entry.get("link_window_seconds") == 60

    def test_link_window_seconds_is_always_60(self):
        """link_window_seconds deve ser sempre 60 — nunca outro valor."""
        execs = [
            self._make_exec("e1", "2026-04-01T10:00:00Z"),
            self._make_exec("e2", "2026-04-01T11:00:00Z"),
        ]
        ledger = [
            self._make_ledger_entry("2026-04-01T10:00:05Z"),
            self._make_ledger_entry("2026-04-01T11:00:05Z"),
        ]
        p._link_skill_events(execs, ledger)
        for entry in ledger:
            if "parent_event_id" in entry:
                assert entry["link_window_seconds"] == 60, (
                    f"link_window_seconds={entry['link_window_seconds']} (esperado 60)"
                )

    def test_link_semantic_is_always_non_causal(self):
        """link_semantic deve ser sempre 'non_causal' — nunca outro valor."""
        execs = [self._make_exec("exec-Y", "2026-04-01T08:00:00Z")]
        ledger = [self._make_ledger_entry("2026-04-01T08:00:20Z")]
        p._link_skill_events(execs, ledger)
        entry = ledger[0]
        assert entry.get("link_semantic") == "non_causal"

    def test_unlinked_entry_has_no_new_fields(self):
        """Entry sem link não deve ter link_semantic nem link_window_seconds."""
        execs = [self._make_exec("exec-Z", "2026-04-01T00:00:00Z")]
        ledger = [self._make_ledger_entry("2026-04-01T01:00:00Z")]  # 3600s depois
        p._link_skill_events(execs, ledger)
        entry = ledger[0]
        assert "link_semantic" not in entry
        assert "link_window_seconds" not in entry

    def test_real_data_full_contract(self):
        """Nos dados reais, todo entry com parent_event_id tem os 4 campos corretos."""
        REQUIRED = {
            "link_method": "temporal",
            "link_confidence": "low",
            "link_semantic": "non_causal",
            "link_window_seconds": 60,
        }
        executions, _ = p.parse_logs()
        ledger = p.parse_ledger()
        p._link_skill_events(executions, ledger)
        violations = []
        for entry in ledger:
            if "parent_event_id" not in entry:
                continue
            for field, expected in REQUIRED.items():
                if entry.get(field) != expected:
                    violations.append(
                        f"entry@{entry.get('timestamp','?')}: "
                        f"{field}={entry.get(field)!r} (esperado {expected!r})"
                    )
        assert not violations, "\n".join(violations)


if __name__ == "__main__":
    pytest.main([__file__, "-v"])
