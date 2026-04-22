"""
Teste anti-regressão do anchor externo (AURYA).

Gap G4: com body_hash + chain + legacy_gap, a integridade é local —
quem tem acesso a ~/.orbit/logs ainda pode apagar toda a sequência.
anchor publica merkle_root em AURYA; o receipt persistido é comparado
contra os logs atuais em verify --chain, e qualquer deleção divergente
quebra o match. Stub HTTP emula AURYA para não depender do serviço real.
"""
import json
import os
import shutil
import socket
import subprocess
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path

import pytest

REPO = Path(__file__).resolve().parent.parent
TRACKING_DIR = REPO / "tracking"

pytestmark = pytest.mark.skipif(shutil.which("go") is None, reason="go não instalado")


class AuryaStub(BaseHTTPRequestHandler):
    """Emula POST /proofstream/submit: aceita, devolve ok+hash+timestamp+sig."""

    def log_message(self, *a, **k):  # silencia ruído no pytest
        return

    def do_POST(self):
        if self.path != "/proofstream/submit":
            self.send_response(404)
            self.end_headers()
            return
        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length)
        try:
            body = json.loads(raw)
        except json.JSONDecodeError:
            self.send_response(400)
            self.end_headers()
            return
        # Sanity checks do contrato: todos os campos obrigatórios presentes.
        for k in ("app_id", "app_pub", "signature", "nonce", "timestamp", "payload"):
            if k not in body:
                self.send_response(400)
                self.end_headers()
                self.wfile.write(f"missing {k}".encode())
                return
        resp = {
            "ok": True,
            "hash": "stub-" + body["nonce"],
            "node_timestamp": "2026-04-22T12:00:00.000000000Z",
            "node_signature": "stub-sig-" + body["nonce"],
        }
        data = json.dumps(resp).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)


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


@pytest.fixture
def aurya_stub():
    # Porta 0 → SO escolhe livre; evita colisão entre execuções paralelas.
    sock = socket.socket()
    sock.bind(("127.0.0.1", 0))
    port = sock.getsockname()[1]
    sock.close()
    server = HTTPServer(("127.0.0.1", port), AuryaStub)
    t = threading.Thread(target=server.serve_forever, daemon=True)
    t.start()
    yield f"http://127.0.0.1:{port}"
    server.shutdown()
    server.server_close()


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
        time.sleep(0.01)
    logs = sorted((home / "logs").glob("*.json"))
    assert len(logs) == n
    return logs


def test_anchor_detects_local_log_deletion(orbit_bin, tmp_path, aurya_stub):
    """Fluxo central: gera logs → orbit anchor → deleta log → verify falha."""
    home = tmp_path / "home"
    logs = _gen_logs(orbit_bin, home, 3)

    r = subprocess.run(
        [str(orbit_bin), "anchor", "--host", aurya_stub],
        env=_env(home), capture_output=True, text=True,
    )
    assert r.returncode == 0, f"anchor falhou:\n{r.stdout}\n{r.stderr}"
    assert "anchor criado" in r.stdout

    receipts = sorted((home / "anchors").glob("*.json"))
    assert len(receipts) == 1, "receipt não foi persistido"
    rec = json.loads(receipts[0].read_text())
    assert rec["leaf_count"] == 3
    assert len(rec["merkle_root"]) == 64
    assert rec["node_signature"].startswith("stub-sig-")

    # Pré-deleção: verify OK.
    r = subprocess.run([str(orbit_bin), "verify", "--chain"],
                       env=_env(home), capture_output=True, text=True)
    assert r.returncode == 0, f"verify pré-deleção falhou:\n{r.stdout}\n{r.stderr}"
    assert "anchor ok" in r.stdout

    # Ataque: apaga o ÚLTIMO log. Escolha intencional — a chain sozinha
    # não detecta truncamento do fim (prefixo continua íntegro), mas o
    # anchor registrou N folhas e agora só existem N-1. Esse é o caso
    # que justifica anchor externo em cima de prev_proof.
    logs[-1].unlink()

    r = subprocess.run([str(orbit_bin), "verify", "--chain"],
                       env=_env(home), capture_output=True, text=True)
    assert r.returncode != 0, (
        "verify passou com log apagado após anchor — regressão crítica\n"
        f"{r.stdout}\n{r.stderr}"
    )
    combined = (r.stdout + r.stderr).lower()
    assert "anchor mismatch" in combined, (
        f"mensagem não sinaliza anchor mismatch:\n{r.stdout}\n{r.stderr}"
    )

    # Cenário complementar: deleção do meio também deve falhar, por outra
    # via (chain break). Cobre o caso de ataque que a chain já pega, para
    # garantir que anchor NÃO mascara detecção prévia.


def test_anchor_detects_log_deletion_from_middle(orbit_bin, tmp_path, aurya_stub):
    """Deleção do meio: chain detecta antes mesmo de chegar no anchor check.
    Confirma que as duas camadas não se anulam."""
    home = tmp_path / "home"
    logs = _gen_logs(orbit_bin, home, 3)
    r = subprocess.run(
        [str(orbit_bin), "anchor", "--host", aurya_stub],
        env=_env(home), capture_output=True, text=True,
    )
    assert r.returncode == 0
    logs[1].unlink()
    r = subprocess.run([str(orbit_bin), "verify", "--chain"],
                       env=_env(home), capture_output=True, text=True)
    assert r.returncode != 0
    combined = (r.stdout + r.stderr).lower()
    assert "chain break" in combined or "anchor mismatch" in combined


def test_anchor_fails_without_aurya(orbit_bin, tmp_path):
    """Fail-closed: AURYA offline → anchor retorna erro, não grava receipt."""
    home = tmp_path / "home"
    _gen_logs(orbit_bin, home, 2)
    # Porta fechada — 127.0.0.1:1 não aceita conexão.
    r = subprocess.run(
        [str(orbit_bin), "anchor", "--host", "http://127.0.0.1:1"],
        env=_env(home), capture_output=True, text=True,
    )
    assert r.returncode != 0, "anchor passou mesmo com AURYA inacessível"
    assert not (home / "anchors").exists() or not list((home / "anchors").glob("*.json")), (
        "receipt foi gravado apesar da falha — viola fail-closed"
    )


def test_merkle_root_deterministic_across_runs(orbit_bin, tmp_path, aurya_stub):
    """Dois anchors consecutivos sobre mesmo conjunto → mesmo merkle_root."""
    home = tmp_path / "home"
    _gen_logs(orbit_bin, home, 4)
    roots = []
    for _ in range(2):
        r = subprocess.run(
            [str(orbit_bin), "anchor", "--host", aurya_stub],
            env=_env(home), capture_output=True, text=True,
        )
        assert r.returncode == 0
        time.sleep(0.01)  # garante filename distinto do receipt
    receipts = sorted((home / "anchors").glob("*.json"))
    assert len(receipts) == 2
    for p in receipts:
        roots.append(json.loads(p.read_text())["merkle_root"])
    assert roots[0] == roots[1], "merkle_root não é determinístico"
