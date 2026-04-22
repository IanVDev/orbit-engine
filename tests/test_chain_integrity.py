"""
Teste anti-regressão da chain prev_proof.

Fecha o gap complementar ao body_hash: mesmo com cada log individualmente
íntegro, remover ou reordenar logs passava despercebido. prev_proof =
body_hash do log anterior fecha isso: qualquer hole na sequência quebra o
match em `orbit verify --chain`. Paridade Go↔Python validada aqui.
"""
import json
import os
import shutil
import subprocess
import sys
import time
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


def _gen_logs(orbit_bin, home, n):
    home.mkdir(parents=True, exist_ok=True)
    for i in range(n):
        r = subprocess.run(
            [str(orbit_bin), "run", "echo", f"log-{i}"],
            env=_env(home), capture_output=True, text=True,
        )
        assert r.returncode == 0, f"run {i} falhou: {r.stderr}"
        # Timestamp no nome do arquivo tem nanossegundos; não é necessário
        # sleep, mas 1ms evita colisão em FS de baixa granularidade.
        time.sleep(0.01)
    logs = sorted((home / "logs").glob("*.json"))
    assert len(logs) == n, f"esperava {n} logs, achei {len(logs)}"
    return logs


def test_chain_fails_when_log_removed_or_reordered(orbit_bin, tmp_path):
    """Contrato explícito: 3 logs → remove o do meio → --chain deve falhar."""
    home = tmp_path / "home"
    logs = _gen_logs(orbit_bin, home, 3)

    # ── 1. cada log aponta para o body_hash do anterior ─────────────────────
    data = [json.loads(p.read_text()) for p in logs]
    assert data[0].get("prev_proof", "") == "", "genesis deve ter prev_proof vazio"
    assert data[1]["prev_proof"] == data[0]["body_hash"], "elo 0→1 quebrado"
    assert data[2]["prev_proof"] == data[1]["body_hash"], "elo 1→2 quebrado"

    # ── 2. --chain aceita a sequência intacta ───────────────────────────────
    r = subprocess.run([str(orbit_bin), "verify", "--chain"],
                       env=_env(home), capture_output=True, text=True)
    assert r.returncode == 0, f"--chain falsamente reprovou chain íntegra:\n{r.stdout}\n{r.stderr}"
    assert "chain íntegra" in r.stdout

    # ── 3. Remove o log do meio ─────────────────────────────────────────────
    logs[1].unlink()

    # ── 4. --chain detecta o gap e falha ────────────────────────────────────
    r = subprocess.run([str(orbit_bin), "verify", "--chain"],
                       env=_env(home), capture_output=True, text=True)
    assert r.returncode != 0, (
        "--chain passou com log removido — regressão crítica (chain não detecta gap)"
    )
    combined = (r.stdout + r.stderr).lower()
    assert "chain break" in combined, f"mensagem não menciona chain break:\n{r.stdout}\n{r.stderr}"


def test_chain_fails_when_logs_reordered(orbit_bin, tmp_path):
    """Reorder = trocar body_hash entre dois arquivos (sem alterar nome).
    prev_proof passa a apontar para um body_hash que não está na posição
    esperada → detectado pelo mesmo mecanismo."""
    home = tmp_path / "home"
    logs = _gen_logs(orbit_bin, home, 3)

    # Troca body_hash + prev_proof entre log[1] e log[2] — conteúdo misturado.
    d1 = json.loads(logs[1].read_text())
    d2 = json.loads(logs[2].read_text())
    d1["body_hash"], d2["body_hash"] = d2["body_hash"], d1["body_hash"]
    d1["prev_proof"], d2["prev_proof"] = d2["prev_proof"], d1["prev_proof"]
    logs[1].write_text(json.dumps(d1))
    logs[2].write_text(json.dumps(d2))

    r = subprocess.run([str(orbit_bin), "verify", "--chain"],
                       env=_env(home), capture_output=True, text=True)
    assert r.returncode != 0, "chain não detectou reorder"


def test_observatory_reflects_chain_break(orbit_bin, tmp_path, monkeypatch):
    """Observatório Python detecta break com paridade exata ao Go."""
    home = tmp_path / "home"
    logs = _gen_logs(orbit_bin, home, 3)

    monkeypatch.setattr(obs.parser, "LOGS_DIR", str(home / "logs"))
    monkeypatch.setattr(obs.parser, "LEDGER_PATH", str(home / "client_ledger.jsonl"))

    # Baseline: chain ok.
    view = obs.build_view()
    assert view["chain"]["critical"] is False
    assert view["chain"]["checked"] == 3

    # Remove meio: Python detecta exatamente no arquivo onde Go detectaria.
    logs[1].unlink()
    view = obs.build_view()
    assert view["chain"]["critical"] is True, f"observatório não viu break: {view['chain']}"
    assert view["chain"]["broken_at"] == logs[2].name

    rendered = obs.render_text(view)
    assert "[CRITICAL] Chain quebrada" in rendered


def test_chain_fails_on_legacy_inserted_mid_sequence(orbit_bin, tmp_path, monkeypatch):
    """Ataque: strip de body_hash+prev_proof num log do MEIO quebra chain de
    forma silenciosa no código legado (reset de âncora). Novo contrato: só
    o início da sequência pode omitir body_hash — legacy_gap no meio é
    CRITICAL em Go e reportado como 'Legacy reset detectado' em Python."""
    home = tmp_path / "home"
    logs = _gen_logs(orbit_bin, home, 3)

    # Corrompe log[1]: remove body_hash e prev_proof, simulando legado no meio.
    d = json.loads(logs[1].read_text())
    d.pop("body_hash", None)
    d.pop("prev_proof", None)
    logs[1].write_text(json.dumps(d))

    r = subprocess.run([str(orbit_bin), "verify", "--chain"],
                       env=_env(home), capture_output=True, text=True)
    assert r.returncode != 0, (
        "--chain passou com legado inserido no meio — regressão crítica "
        f"(reset silencioso):\n{r.stdout}\n{r.stderr}"
    )
    combined = (r.stdout + r.stderr).lower()
    assert "legacy_gap" in combined or "legacy reset" in combined, (
        f"mensagem não sinaliza legacy_gap:\n{r.stdout}\n{r.stderr}"
    )

    # Observatório Python deve concordar: critical=true, legacy_gap=true.
    monkeypatch.setattr(obs.parser, "LOGS_DIR", str(home / "logs"))
    monkeypatch.setattr(obs.parser, "LEDGER_PATH", str(home / "client_ledger.jsonl"))
    view = obs.build_view()
    assert view["chain"]["critical"] is True
    assert view["chain"]["legacy_gap"] is True
    assert view["chain"]["broken_at"] == logs[1].name
    assert "[CRITICAL] Legacy reset detectado" in obs.render_text(view)


def test_backcompat_legacy_log_without_prev_proof(orbit_bin, tmp_path):
    """Log legado (sem body_hash) reseta âncora; chain não quebra por causa
    dele. Requisito: 'compatível com logs antigos'."""
    home = tmp_path / "home"
    logs = _gen_logs(orbit_bin, home, 3)

    # Simula log legado: remove body_hash e prev_proof do primeiro.
    d = json.loads(logs[0].read_text())
    d.pop("body_hash", None)
    d.pop("prev_proof", None)
    logs[0].write_text(json.dumps(d))

    r = subprocess.run([str(orbit_bin), "verify", "--chain"],
                       env=_env(home), capture_output=True, text=True)
    assert r.returncode == 0, f"chain falsamente reprovou com log legado:\n{r.stdout}\n{r.stderr}"
    assert "âncora" in r.stdout.lower() or "ancora" in r.stdout.lower()
