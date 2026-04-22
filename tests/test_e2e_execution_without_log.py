"""
Teste E2E do fluxo execution_without_log (fail-closed).

Valida a cadeia completa em execução REAL, sem mocks do core:
  orbit run → WriteExecutionLog falha (logs/ é arquivo) →
  IncrementMetric → exit 1 → observatório expõe safety CRITICAL.

Falha-se aqui significa: ou o fail-closed regrediu (contador não incrementou),
ou a ponte com o observatório quebrou (view não reflete estado). Em ambos os
casos o contrato de rastreabilidade do Orbit está comprometido.
"""

import os
import shutil
import subprocess
import sys
from pathlib import Path

import pytest

REPO = Path(__file__).resolve().parent.parent
TRACKING_DIR = REPO / "tracking"

sys.path.insert(0, str(REPO / "scripts"))
import orbit_observatory as obs  # noqa: E402


pytestmark = pytest.mark.skipif(
    shutil.which("go") is None, reason="go não instalado"
)


@pytest.fixture(scope="module")
def orbit_bin(tmp_path_factory):
    """Build do binário orbit uma vez por módulo — evita recompilar entre testes."""
    out = tmp_path_factory.mktemp("orbit_bin") / "orbit"
    r = subprocess.run(
        ["go", "build", "-o", str(out), "./cmd/orbit"],
        cwd=TRACKING_DIR,
        capture_output=True,
        text=True,
    )
    if r.returncode != 0:
        pytest.fail(f"go build falhou (fail-closed): {r.stderr}")
    return out


def _isolated_env(home: Path) -> dict:
    env = os.environ.copy()
    env["ORBIT_HOME"] = str(home)
    # Binário sem ldflags → startup-guard dispararia sem este bypass.
    env["ORBIT_SKIP_GUARD"] = "1"
    return env


def test_e2e_execution_without_log_triggers_metric_and_observatory(
    orbit_bin, tmp_path, monkeypatch
):
    """Execução real: logs/ como arquivo → fail-closed → contador → observatório."""
    home = tmp_path / "orbit_home"
    home.mkdir()
    # Injeção determinística da falha: logs/ existe como arquivo regular,
    # MkdirAll falha com "not a directory" e run.go escala para CRITICAL.
    (home / "logs").write_text("bloqueia mkdir", encoding="utf-8")

    r = subprocess.run(
        [str(orbit_bin), "run", "echo", "test"],
        env=_isolated_env(home),
        capture_output=True,
        text=True,
    )

    # ── 1. exit != 0 (fail-closed) ──────────────────────────────────────────
    assert r.returncode != 0, (
        f"esperava exit != 0 (fail-closed esperado);\n"
        f"  stdout={r.stdout!r}\n  stderr={r.stderr!r}"
    )
    assert "CRITICAL" in r.stderr, (
        f"esperava marcador CRITICAL em stderr; got: {r.stderr!r}"
    )

    # ── 2. contador == 1 ─────────────────────────────────────────────────────
    counter_path = home / "metrics" / "execution_without_log_total.count"
    assert counter_path.exists(), (
        f"métrica não persistida em {counter_path} — IncrementMetric não disparou"
    )
    raw = counter_path.read_text().strip()
    assert raw == "1", f"contador esperado=1, lido={raw!r}"

    # ── 3. observatório reflete estado ──────────────────────────────────────
    # ORBIT_HOME no processo de teste aponta para o mesmo home do orbit run,
    # para que _resolve_metrics_dir() leia o contador correto.
    monkeypatch.setenv("ORBIT_HOME", str(home))
    # parse_orbit_events.py fixa LOGS_DIR/LEDGER_PATH no import — redirecionamos
    # para o home isolado, senão o build_view() lê ~/.orbit do usuário.
    monkeypatch.setattr(obs.parser, "LOGS_DIR", str(home / "logs"))
    monkeypatch.setattr(obs.parser, "LEDGER_PATH", str(home / "client_ledger.jsonl"))

    view = obs.build_view()
    assert view["safety"]["executions_without_log_total"] == 1, (
        f"observatório não reflete contador; safety={view['safety']!r}"
    )
    assert view["safety"]["critical"] is True, (
        f"safety.critical deveria ser True quando contador>0; safety={view['safety']!r}"
    )

    # Sanity de rendering: CRITICAL aparece no dump humano.
    out = obs.render_text(view)
    assert "[CRITICAL] Execuções sem log persistido: 1" in out, (
        f"render_text não marcou CRITICAL; output:\n{out}"
    )


def test_e2e_happy_path_does_not_increment_counter(orbit_bin, tmp_path, monkeypatch):
    """Anti-regressão complementar: caminho feliz NÃO deve incrementar contador.
    Se este teste passar mas o principal falhar, a falha está na injeção.
    Se este falhar, significa que o contador subiu em execução bem-sucedida —
    métrica perdeu seu significado (fica sempre crescendo)."""
    home = tmp_path / "orbit_home"
    home.mkdir()
    (home / "logs").mkdir()  # diretório válido → sucesso

    r = subprocess.run(
        [str(orbit_bin), "run", "echo", "test"],
        env=_isolated_env(home),
        capture_output=True,
        text=True,
    )
    assert r.returncode == 0, (
        f"caminho feliz deveria retornar 0; stderr={r.stderr!r}"
    )

    counter_path = home / "metrics" / "execution_without_log_total.count"
    assert not counter_path.exists(), (
        f"contador criado indevidamente em caminho feliz: "
        f"{counter_path.read_text()!r}"
    )

    monkeypatch.setenv("ORBIT_HOME", str(home))
    monkeypatch.setattr(obs.parser, "LOGS_DIR", str(home / "logs"))
    monkeypatch.setattr(obs.parser, "LEDGER_PATH", str(home / "client_ledger.jsonl"))
    view = obs.build_view()
    assert view["safety"]["executions_without_log_total"] == 0
    assert view["safety"]["critical"] is False
