# orbit-prompt — Changelog

Registro de versões publicadas. Uma entrada = uma tag git `prompt-skill-v<X.Y.Z>`.

SSOT da versão: `orbit-prompt.skill → SKILL.md → frontmatter version:`.
Enforcement: gate `G16_skill_version` em `tests/test_skill_version_consistency.sh`.

---

## 1.1.2 — 2026-04-23 (genesis of enforcement)

Primeiro marco com consistência verificável por gate. Tudo antes desta entrada
é **histórico não-autoritativo** — versões que apareceram em commits ou README
mas nunca foram materializadas em artefato com garantia.

**Reconciliação:**
- Frontmatter `version: 1.1.0` + README `v1.1.1` convergidos em `1.1.2`.
- Versão `1.1.1` (commit `b62ac75` / rename `/prompt` → `/orbit-prompt`) era
  fantasma: mencionada em commit message mas nunca bumped no frontmatter.
  Descontinuada. Quem referenciar `1.1.1` está referenciando um não-artefato.

**Alterações observáveis:**
- Simplificação da interface: `/orbit-prompt "<task>"` como entrada primária
  recomendada, documentada no topo do SKILL.md.
- Triggers de linguagem natural rebaixados para seção `Advanced triggers
  (optional)` — presentes, funcionais, não recomendados como primário.
- README externo reescrito com separação explícita das duas superfícies
  (diagnóstico automático silencioso vs comando manual).

**Enforcement introduzido:**
- Gate `G16_skill_version` em `scripts/gate_cli.sh`: valida consistência
  entre frontmatter (SSOT), marcador HTML no README
  (`<!-- SKILL_VERSION: X.Y.Z -->`), e tag git se presente em HEAD.
- Fail-closed: divergência em qualquer par → exit 1.

**Contrato preservado byte-para-byte:**
- 8 waste patterns (bloco literal).
- Output formats DIAGNOSIS/ACTIONS/DO NOT DO NOW.
- Output formats ORIGINAL/ANALYSIS/IMPROVED PROMPT/KEY IMPROVEMENTS/READY TO SEND.
- Comando `/orbit-prompt`.
- Todos os 3 triggers naturais (`analyze cost`, `is this optimal?`,
  `Before answering, apply orbit-engine`).

Consumidor de versão anterior não percebe diferença no output — por isso
PATCH, não MINOR.
