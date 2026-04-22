"""
Teste anti-regressão do body_hash (G3).

Fecha a vulnerabilidade onde o proof legado (sha256 sobre 3 campos) não cobria
o corpo do log — era possível editar output/decision/diagnosis sem quebrar
`orbit verify`. body_hash = sha256(JSON canônico excluindo body_hash) cobre
tudo. Paridade Go↔Python é validada aqui — se divergir, o scan do observatório
vira falso positivo em toda execução legítima.
"""
import json
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

pytestmark = pytest.mark.skipif(shutil.which("go") is None, reason="go não instalado")


@pytest.fixture(scope="module")
def orbit_bin(tmp_path_factory):
    out = tmp_path_factory.mktemp("orbit_bin") / "orbit"
    r = subprocess.run(
        ["go", "build", "-o", str(out), "./cmd/orbit"],
        cwd=TRACKING_DIR, capture_output=True, text=True,
    )
    if r.returncode != 0:
        pytest.fail(f"go build falhou: {r.stderr}")
    return out


def _env(home):
    e = os.environ.copy()
    e["ORBIT_HOME"] = str(home)
    e["ORBIT_SKIP_GUARD"] = "1"
    return e


def _run_and_get_log(orbit_bin, home):
    r = subprocess.run(
        [str(orbit_bin), "run", "echo", "integrity-probe"],
        env=_env(home), capture_output=True, text=True,
    )
    assert r.returncode == 0, f"orbit run falhou: {r.stderr}"
    logs = list((home / "logs").glob("*.json"))
    assert len(logs) == 1, f"esperava 1 log, achei {logs}"
    return logs[0]


def test_integrity_fails_when_log_body_tampered(orbit_bin, tmp_path):
    """Contrato do teste (requisito explícito da tarefa):
    gera log legítimo → altera campo `output` → `orbit verify` deve falhar."""
    home = tmp_path / "home"; home.mkdir()
    log_path = _run_and_get_log(orbit_bin, home)

    # ── 1. body_hash foi gravado no log legítimo ────────────────────────────
    data = json.loads(log_path.read_text())
    assert data.get("body_hash"), (
        f"body_hash ausente no log recém-criado — writer regrediu: {data.keys()}"
    )

    # ── 2. verify no log intocado passa ─────────────────────────────────────
    r = subprocess.run([str(orbit_bin), "verify", str(log_path)],
                       env=_env(home), capture_output=True, text=True)
    assert r.returncode == 0, f"verify deveria passar antes da adulteração: {r.stdout}\n{r.stderr}"

    # ── 3. Adultera campo `output` (ataque que o proof legado NÃO detectava) ─
    data["output"] = "INJETADO — payload malicioso no lugar do output real"
    log_path.write_text(json.dumps(data))

    # ── 4. verify DEVE falhar com body_hash mismatch ────────────────────────
    r = subprocess.run([str(orbit_bin), "verify", str(log_path)],
                       env=_env(home), capture_output=True, text=True)
    assert r.returncode != 0, (
        "verify passou em log adulterado — regressão crítica (G3 reaberto)"
    )
    combined = (r.stdout + r.stderr).lower()
    assert "body_hash" in combined and "mismatch" in combined, (
        f"mensagem de erro não menciona body_hash mismatch:\n{r.stdout}\n{r.stderr}"
    )


def test_observatory_marks_critical_on_tampered_log(orbit_bin, tmp_path, monkeypatch):
    """Observatório reflete adulteração detectada localmente: critical=True
    e arquivo listado em tampered_files. Se este teste passar mas o anterior
    falhar, o scan Python está ignorando adulteração real."""
    home = tmp_path / "home"; home.mkdir()
    log_path = _run_and_get_log(orbit_bin, home)

    # Baseline: log íntegro → integrity não critical.
    monkeypatch.setattr(obs.parser, "LOGS_DIR", str(home / "logs"))
    monkeypatch.setattr(obs.parser, "LEDGER_PATH", str(home / "client_ledger.jsonl"))
    view = obs.build_view()
    assert view["integrity"]["critical"] is False
    assert view["integrity"]["checked"] == 1
    assert view["integrity"]["tampered_count"] == 0

    # Adulterada: edita output → scan Python detecta e marca CRITICAL.
    data = json.loads(log_path.read_text())
    data["output"] = "TAMPERED"
    log_path.write_text(json.dumps(data))

    view = obs.build_view()
    assert view["integrity"]["critical"] is True, (
        f"observatório não detectou adulteração: {view['integrity']!r}"
    )
    assert view["integrity"]["tampered_count"] == 1
    assert log_path.name in view["integrity"]["tampered_files"]

    rendered = obs.render_text(view)
    assert "[CRITICAL] Logs adulterados: 1" in rendered


def test_backcompat_legacy_log_without_body_hash(orbit_bin, tmp_path):
    """Logs antigos sem body_hash não devem quebrar verify nem observatório.
    Requisito explícito: 'sem quebrar logs existentes'."""
    home = tmp_path / "home"; home.mkdir()
    log_path = _run_and_get_log(orbit_bin, home)

    # Remove body_hash simulando log pré-fix.
    data = json.loads(log_path.read_text())
    del data["body_hash"]
    log_path.write_text(json.dumps(data))

    # verify aceita (emite warning mas não falha).
    r = subprocess.run([str(orbit_bin), "verify", str(log_path)],
                       env=_env(home), capture_output=True, text=True)
    assert r.returncode == 0, f"verify quebrou em log legado: {r.stdout}\n{r.stderr}"
    assert "legado" in (r.stdout + r.stderr).lower()
