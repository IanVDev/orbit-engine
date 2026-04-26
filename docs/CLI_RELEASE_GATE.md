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

## Os 7 gates ativos (todos devem PASS)

| # | Gate | O que valida | Script |
|---|------|-------------|--------|
| G1 | `G1_go_test` | Testes Go da árvore `tracking/...` | `go test ./...` |
| G6 | `G6_log_contract` | Schema v1 do log persistido + paridade com `run --json` | `tests/test_log_contract.sh` |
| G8 | `G8_no_mk_dup` | Makefile sem *"overriding recipe"* | `tests/test_makefile_no_dup.sh` |
| G11 | `G11_gate_doc_parity` | Contagem de gates neste doc bate com `scripts/gate_cli.sh` | `tests/test_gate_doc_parity.sh` |
| G13 | `G13_integrity` | I17 body_hash (1 byte → verify FAIL) + I18 chain break + I19 merkle determinístico | `tests/test_integrity.sh` |
| G16 | `G16_skill_version` | Consistência SKILL.md (SSOT) ↔ marcador README ↔ tag `prompt-skill-v*` | `tests/test_skill_version_consistency.sh` |
| G17 | `G17_slash_command_bridge` | Skill orbit-prompt tem bridge `/orbit-prompt` em `.claude/commands/` | `scripts/check_claude_slash_command_bridge.sh` |

Cada Gi emite `{gate, status, duration_ms, tail}` em `gate_report.json`.

Gates G2, G3, G4, G5, G7, G9, G10, G12, G14, G15 foram removidos: scripts referenciados não existem no repositório. Esses vetores de validação são candidatos a backlog — nenhum gate fake é incluído.

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

## Proof / Tampering

`orbit verify` recalcula `sha256(session_id + timestamp + output_bytes)`
(`tracking/cmd/orbit/verify.go:15-17`). Adulterar qualquer um dos 3 campos do
escopo → `verify` retorna exit 1. Adulterar `output` sem alterar `output_bytes`
**não é detectado** — esse é o contrato atual, deliberado (o proof é um
*commit-marker*, não um blob-hash).

O G13 (`test_integrity.sh`) cobre body_hash, chain e merkle — mas não cobre o
smoke E2E completo do binário real. Smoke E2E (antigo G5) está em backlog.

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

## Evidência

Execute `make gate-cli` e verifique que todos os 7 gates retornam PASS.
O relatório é salvo em `gate_report.json` (artefato local, não commitar).

Se qualquer gate retornar FAIL, **não tague**.
