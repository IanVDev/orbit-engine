# orbit-engine CLI — Release Gate (Prod Gate v1)

> **Escopo**: binário `orbit` (CLI local) — o artefato **efetivamente publicado**
> em GitHub Releases (v0.1.x). Este é o contrato de release da CLI.
>
> **Não confundir com** `docs/server-stack/` — aqueles documentos descrevem o
> Produto B (tracking-server + gateway + Prometheus/Grafana), um milestone
> interno nunca tagueado.

---

## TL;DR

```bash
make gate-cli
```

Se retorna 🟢 **PASS**, a tag pode ser criada (`make tag-release VERSION=vX.Y.Z`).
Se retorna 🔴 **FAIL**, a release está bloqueada. Sem subjetividade.

`make gate-cli` gera `gate_report.json` com o resultado de cada gate. Esse arquivo é um artefato local de diagnóstico — **não commitar** (coberto por `.gitignore`).

**Requer apenas**: `go`, `python3`, `bash`. **Não requer**: rede, Prometheus,
Grafana, Docker, Alertmanager. Roda offline em **< 120 s** em ambiente limpo.

---

## Os 17 gates (todos devem PASS)

| # | Gate | O que valida | Script |
|---|------|-------------|--------|
| G1 | `G1_go_test` | Testes Go da árvore `tracking/...` | `go test ./...` |
| G2 | `G2_no_user_writes` | Invariante *"Orbit não escreve fora de `$ORBIT_HOME`"* | `tests/test_no_user_writes.sh` |
| G3 | `G3_readme_claims` | Léxico do README hero (detect/record/diagnose/observe/prove) | `tests/test_readme_claims.sh` |
| G4 | `G4_python_evals` | 18 evals do skill (ativação/silêncio) | `tests/run_tests.py` |
| G5 | `G5_smoke_e2e` | Binário real: `version`, `run` (ok + fail), `verify` (ok + tamper), `doctor --json` | `tests/smoke_e2e.sh` |
| G6 | `G6_log_contract` | Schema v1 do log persistido + paridade com `run --json` | `tests/test_log_contract.sh` |
| G7 | `G7_rollback` | `scripts/orbit_rollback.sh` restaura `.bak` após update quebrado | `tests/test_rollback.sh` |
| G8 | `G8_no_mk_dup` | Makefile sem *"overriding recipe"* | `tests/test_makefile_no_dup.sh` |
| G9 | `G9_docs_scope` | Docs públicos não apontam gate do Produto B | `tests/test_docs_dont_claim_v1.sh` |
| G10 | `G10_skill_contract` | Frontmatter + seções invariantes em `skill/SKILL.md` | `tests/test_skill_contract.sh` |
| G11 | `G11_gate_doc_parity` | Contagem de gates neste doc bate com `scripts/gate_cli.sh` | `tests/test_gate_doc_parity.sh` |
| G12 | `G12_system_contract` | Cada invariante de `docs/SYSTEM_CONTRACT.md` aponta código+teste existentes | `tests/test_system_contract.sh` |
| G13 | `G13_integrity` | I17 body_hash (1 byte → verify FAIL) + I18 chain break + I19 merkle determinístico | `tests/test_integrity.sh` |
| G14 | `G14_anchor_verification` | I20: signature Ed25519 + monotonic anti-replay + full-match + leaf_count | `tests/test_anchor_verification.sh` |
| G15 | `G15_trusted_signer` | I21: AppPub == trustedAuryaPubKey (pub key pinned) | `tests/test_trusted_signer.sh` |
| G16 | `G16_skill_version` | Consistência SKILL.md (SSOT) ↔ marcador README ↔ tag `prompt-skill-v*` | `tests/test_skill_version_consistency.sh` |
| G17 | `G17_slash_command_bridge` | Skill orbit-prompt tem bridge `/orbit-prompt` em `.claude/commands/` | `scripts/check_claude_slash_command_bridge.sh` |

Cada Gi emite `{gate, status, duration_ms, tail}` em `gate_report.json`.

---

## Contrato do log estruturado v1 (travado por G6)

Campos obrigatórios em todo `~/.orbit/logs/*.json` e em `orbit run --json`:

| Campo | Tipo | Origem |
|---|---|---|
| `version` | int (== 1) | constante `LogSchemaVersion` |
| `command` | string | argv[1] |
| `exit_code` | int | exit do subprocesso |
| `output` | string | stdout+stderr combinados |
| `proof` | string | sha256(session_id + timestamp + output_bytes) |
| `session_id` | string | `run-<unix_nano>` |
| `timestamp` | string | RFC3339Nano |
| `duration_ms` | int | wall-clock da execução |
| `output_bytes` | int | `len(output)` |
| `event` | string | resultado do classifier |
| `decision` | string | resultado do decision engine |

Bump de schema (`version: 2`) é **breaking**.

---

## Proof / Tampering (travado por G5)

`orbit verify` recalcula `sha256(session_id + timestamp + output_bytes)`
(`tracking/cmd/orbit/verify.go:15-17`). Adulterar qualquer um dos 3 campos do
escopo → `verify` retorna exit 1. Adulterar `output` sem alterar `output_bytes`
**não é detectado** — esse é o contrato atual, deliberado (o proof é um
*commit-marker*, não um blob-hash).

---

## Rollback (travado por G7)

`scripts/update_orbit.sh` grava `<dest>.bak` antes de substituir o binário.
`scripts/orbit_rollback.sh` restaura esse `.bak` com validação fail-closed:

1. Backup existe?
2. Backup executa `version`?
3. `mv` atômico (com fallback `sudo`)?
4. Binário restaurado executa `version`?

Qualquer FAIL → script aborta com mensagem acionável. Testado em ciclo
completo (old→new→rollback→old) por `tests/test_rollback.sh`.

---

## Tag de release

```bash
make gate-cli                         # exit 0 obrigatório
make tag-release VERSION=v0.1.2       # executa gate-cli novamente por segurança
git push origin v0.1.2                # dispara .github/workflows/release.yml
```

O workflow `release.yml` compila para 4 plataformas (`linux/amd64`,
`linux/arm64`, `darwin/amd64`, `darwin/arm64`), embute `Version/Commit/BuildTime`
via `-ldflags`, valida o binário linux/amd64 com `orbit version`, e publica
assets + `.sha256` no GitHub Release.

---

## O que está fora de escopo deste gate

Tudo que é Produto B (server stack):

- `prelaunch_gate.sh`, `mission_24h.sh`, `fault_injection.sh`
- Métricas Prometheus, recording rules, governança PromQL
- Painéis Grafana, Alertmanager
- `tracking-server`, `gateway`, `seed-server`

Esses artefatos continuam existindo no repo e são validados por `make gate-server`
(antigamente `gate-release`), mas **não bloqueiam** a tag da CLI.

---

## Evidência atual

Última execução:

```
[PASS] G1_go_test                   (~14s)
[PASS] G2_no_user_writes            (<1s)
[PASS] G3_readme_claims             (<1s)
[PASS] G4_python_evals              (<1s)
[PASS] G5_smoke_e2e                 (~2s)
[PASS] G6_log_contract              (~1s)
[PASS] G7_rollback                  (~2s)
[PASS] G8_no_mk_dup                 (<1s)
[PASS] G9_docs_scope                (<1s)
[PASS] G10_skill_contract           (<1s)
[PASS] G11_gate_doc_parity          (<1s)
[PASS] G12_system_contract          (<1s)
[PASS] G13_integrity                (~3s)
[PASS] G14_anchor_verification      (~5s)
[PASS] G15_trusted_signer           (~3s)

🟢 PROD GATE v1: PASS — 15 gates OK
```

Se você vê qualquer gate diferente de PASS, **não tague**.
