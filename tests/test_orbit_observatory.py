"""
Testes do observatório Orbit — foco na exposição de métricas persistentes
(contadores em $ORBIT_HOME/metrics/*.count) geradas pelo fail-closed do
orbit run. O observatório é read-only; aqui simulamos o arquivo que o writer
Go produz e validamos que o dump textual e o JSON carregam a métrica.
"""

import json
import os
import sys

import pytest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "scripts"))

import orbit_observatory as obs  # noqa: E402


@pytest.fixture
def orbit_home(tmp_path, monkeypatch):
    """ORBIT_HOME isolado para não tocar no ~/.orbit do usuário."""
    home = tmp_path / "orbit_home"
    (home / "metrics").mkdir(parents=True)
    (home / "logs").mkdir(parents=True)
    monkeypatch.setenv("ORBIT_HOME", str(home))
    return home


def _write_metric(home, name, value):
    (home / "metrics" / f"{name}.count").write_text(f"{value}\n", encoding="utf-8")


def test_metric_absent_returns_zero(orbit_home):
    """Arquivo ausente deve ser tratado como 0, não como erro."""
    safety = obs._collect_safety()
    assert safety == {"executions_without_log_total": 0, "critical": False}


def test_metric_present_is_reported(orbit_home):
    _write_metric(orbit_home, obs.METRIC_EXECUTION_WITHOUT_LOG, 7)
    safety = obs._collect_safety()
    assert safety["executions_without_log_total"] == 7
    assert safety["critical"] is True


def test_metric_zero_is_not_critical(orbit_home):
    _write_metric(orbit_home, obs.METRIC_EXECUTION_WITHOUT_LOG, 0)
    safety = obs._collect_safety()
    assert safety["executions_without_log_total"] == 0
    assert safety["critical"] is False


def test_metric_corrupted_falls_back_to_zero(orbit_home):
    """Observatório é read-only: métrica corrompida não deve derrubar a view."""
    (orbit_home / "metrics" / f"{obs.METRIC_EXECUTION_WITHOUT_LOG}.count").write_text(
        "lixo\n", encoding="utf-8"
    )
    safety = obs._collect_safety()
    assert safety == {"executions_without_log_total": 0, "critical": False}


def test_render_text_shows_critical_when_positive(orbit_home):
    _write_metric(orbit_home, obs.METRIC_EXECUTION_WITHOUT_LOG, 3)
    view = {
        "generated_at": "2026-04-21T00:00:00+00:00",
        "execution": {
            "total_execucoes": 0, "sucesso": 0, "falhas": 0,
            "taxa_verificacao_pct": 0.0, "comandos": {}, "p50_ms": 0, "p95_ms": 0,
            "ultimo_evento": None, "failure_types": {}, "linguagens": {},
            "session_count": 0, "skill_events": 0, "anchor_events": 0,
        },
        "security": {
            "secrets_redacted_total": 0, "executions_with_secret": 0,
            "executions_with_secret_pct": 0.0, "last_secret_event": None,
        },
        "safety": obs._collect_safety(),
        "integrity": {
            "checked": 0, "missing_body_hash": 0, "tampered_count": 0,
            "tampered_files": [], "critical": False,
        },
        "chain": {
            "checked": 0, "legacy_anchors": 0, "broken_at": None, "critical": False,
        },
        "storage": {
            "logs_dir": str(orbit_home / "logs"), "file_count": 0, "size_bytes": 0,
            "size_human": "0 B", "oldest_mtime": None, "newest_mtime": None,
            "growth_last_7d_files": 0, "growth_last_7d_bytes": 0,
        },
    }
    out = obs.render_text(view)
    assert "[CRITICAL] Execuções sem log persistido: 3" in out


def test_render_text_no_critical_when_zero(orbit_home):
    view = {
        "generated_at": "2026-04-21T00:00:00+00:00",
        "execution": {
            "total_execucoes": 0, "sucesso": 0, "falhas": 0,
            "taxa_verificacao_pct": 0.0, "comandos": {}, "p50_ms": 0, "p95_ms": 0,
            "ultimo_evento": None, "failure_types": {}, "linguagens": {},
            "session_count": 0, "skill_events": 0, "anchor_events": 0,
        },
        "security": {
            "secrets_redacted_total": 0, "executions_with_secret": 0,
            "executions_with_secret_pct": 0.0, "last_secret_event": None,
        },
        "safety": {"executions_without_log_total": 0, "critical": False},
        "integrity": {
            "checked": 0, "missing_body_hash": 0, "tampered_count": 0,
            "tampered_files": [], "critical": False,
        },
        "chain": {
            "checked": 0, "legacy_anchors": 0, "broken_at": None, "critical": False,
        },
        "storage": {
            "logs_dir": str(orbit_home / "logs"), "file_count": 0, "size_bytes": 0,
            "size_human": "0 B", "oldest_mtime": None, "newest_mtime": None,
            "growth_last_7d_files": 0, "growth_last_7d_bytes": 0,
        },
    }
    out = obs.render_text(view)
    assert "[CRITICAL]" not in out
    assert "Execuções sem log persistido: 0" in out


def test_build_view_includes_safety(orbit_home, monkeypatch):
    _write_metric(orbit_home, obs.METRIC_EXECUTION_WITHOUT_LOG, 2)
    # Isola o parser dos logs reais do usuário — aponta para o tmp_home.
    monkeypatch.setattr(obs.parser, "LOGS_DIR", str(orbit_home / "logs"))
    monkeypatch.setattr(obs.parser, "LEDGER_PATH", str(orbit_home / "client_ledger.jsonl"))
    view = obs.build_view()
    assert "safety" in view
    assert view["safety"]["executions_without_log_total"] == 2
    assert view["safety"]["critical"] is True
    # JSON-serializável (garante uso em pipeline).
    json.dumps(view)
