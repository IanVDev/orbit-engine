"""
tests/test_discourse_coherence.py — §10 #5 do plano, persistido.

Alimenta a SkillRouter com:
  (a) o HERO do README.md (até o primeiro `---`)
  (b) o JSON canônico de um `orbit run` saudável

A skill DEVE ficar silenciosa (`activated=False`) em ambos. Qualquer
ativação aqui indica que sobrou linguagem-gatilho contraditória ao
discurso de evidência (verbos detect/record/diagnose/observe/prove).

Smoke reverso (`test_skill_actually_fires_on_triggers`) garante que o
teste não é no-op — texto carregado de gatilhos DEVE ativar.

Roda standalone via `python3 tests/test_discourse_coherence.py`.
"""

from __future__ import annotations

import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from orchestrator import ActivationRequest, SkillRouter


REPO_ROOT = Path(__file__).resolve().parent.parent
README = REPO_ROOT / "README.md"
ORBIT_BIN = os.environ.get("ORBIT_BIN", "/tmp/orbit-bin")


def hero_section() -> str:
    text = README.read_text(encoding="utf-8")
    return text.split("\n---\n", 1)[0]


def healthy_run_json() -> str:
    """Executa um `orbit run echo ok` isolado e devolve o JSON do resultado."""
    if not Path(ORBIT_BIN).exists():
        # Build sob demanda — mantém o teste rodável sem orquestração externa.
        subprocess.run(
            ["go", "build", "-o", ORBIT_BIN, "./cmd/orbit"],
            cwd=REPO_ROOT / "tracking",
            check=True,
        )
    with tempfile.TemporaryDirectory() as tmp:
        env = {**os.environ, "ORBIT_HOME": tmp, "ORBIT_SKIP_GUARD": "1"}
        result = subprocess.run(
            [ORBIT_BIN, "run", "--json", "--", "echo", "ok"],
            env=env, capture_output=True, text=True, check=True,
        )
        return result.stdout


class DiscourseCoherenceTest(unittest.TestCase):

    def setUp(self) -> None:
        self.router = SkillRouter()

    def test_readme_hero_does_not_trigger_skill(self) -> None:
        """HERO realinhado não deve casar com gatilhos da skill."""
        req = ActivationRequest(
            text=hero_section(), session_id="coherence-readme", turn_count=1,
        )
        d = self.router.evaluate(req)
        self.assertFalse(
            d.activated,
            f"skill ativou no HERO (score={d.score}, signals={d.signals}) — "
            f"sobrou linguagem-gatilho",
        )

    def test_orbit_run_json_does_not_trigger_skill(self) -> None:
        """Output canônico de uma execução saudável não deve ativar."""
        req = ActivationRequest(
            text=healthy_run_json(), session_id="coherence-run", turn_count=1,
        )
        d = self.router.evaluate(req)
        self.assertFalse(
            d.activated,
            f"skill ativou no JSON do orbit run (score={d.score}, "
            f"signals={d.signals})",
        )

    def test_skill_actually_fires_on_triggers(self) -> None:
        """Smoke reverso: texto cheio de gatilhos DEVE ativar.

        Sem este, os outros dois testes poderiam passar mesmo se a
        SkillRouter estivesse quebrada (sempre activated=False).
        """
        triggers = (
            "DIAGNOSIS\n- pattern\nRisk: high\n\n"
            "Análise determinística byte-idêntica linha-a-linha\n"
            "| col1 | col2 |\n|------|------|\n"
            "| auditoria exaustiva | sem suposições |\n"
        )
        req = ActivationRequest(
            text=triggers, session_id="reverse-smoke", turn_count=1,
        )
        d = self.router.evaluate(req)
        self.assertTrue(
            d.activated,
            f"smoke reverso falhou: skill deveria ativar (score={d.score}, "
            f"signals={d.signals})",
        )


if __name__ == "__main__":
    unittest.main(verbosity=2)
