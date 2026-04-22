# Orbit — System Contract

> **Regra de existência**: se não há código ou teste que sustente, o item
> **não pertence a este contrato**. Aspirações vão em `## Roadmap`.
>
> **Regra de manutenção**: cada invariante desta página tem um teste. O
> gate G12 (`tests/test_system_contract.sh`) trava isso — se alguém remove
> uma invariante aqui sem remover o teste (ou vice-versa), o gate falha.

---

## 1. Identidade do sistema (decisão explícita)

**Orbit v0.1.x = ferramenta que produz provas individuais.**

- Cada `orbit run` gera um proof auto-contido.
- Cada proof é re-verificável isoladamente via `orbit verify <log>`.
- Superfície pública contratada: os 7 subcomandos do CLI (`run`, `verify`,
  `diagnose`, `doctor`, `quickstart`, `stats`, `release`, `update`).

**Orbit v0.2+ = sistema de prova agregado (roadmap).**

- Cadeia JSONL (`prev_proof`) já existe em `tracking/store.go` mas **não é
  exposta pela CLI atual**. O CLI hoje escreve um arquivo JSON por execução
  em `~/.orbit/logs/`, não JSONL encadeado.
- Promoção para contrato público exige: comando `orbit verify --chain`,
  interface para inspeção longitudinal, observatório.

**Implicação**: tudo que este contrato contém é v0.1.x. Chain-over-JSONL,
observatório, billing, multi-tenant e shadow-mode estão no código mas
**não são contrato** — vivem em `tracking/store.go` + `orchestrator/` como
infraestrutura interna testada.

---

## 2. Invariantes (nunca podem acontecer)

Cada item tem: arquivo que a impõe + teste que a prova + sub-ID para G12.

| # | Invariante | Onde é imposta | Teste que prova |
|---|---|---|---|
| I1 | Orbit nunca escreve fora de `$ORBIT_HOME` | allowlist em 7 arquivos (`logstore.go`, `snapshot.go`, `context_pack.go`, `run.go`, `update.go`, `history.go`, `store.go`) | `tests/test_no_user_writes.sh` |
| I2 | Proof = `sha256(session_id + "\|" + timestamp + "\|" + output_bytes)` | `tracking/cmd/orbit/run.go:127` + `verify.go:108-116` (canonicalizado via `tracking.ComputeHash`) | `tests/smoke_e2e.sh` (cenário tampering) |
| I3 | Log persistido tem 11 campos obrigatórios (schema v1) | `tracking/cmd/orbit/run.go:34-53` (struct `RunResult`) | `tests/test_log_contract.sh` |
| I4 | `orbit verify` rejeita log adulterado (proof mismatch) | `tracking/cmd/orbit/verify.go:71-78` | `tests/smoke_e2e.sh` (smoke 4) |
| I5 | Logs são gravados com permissão `0600`, diretórios `0700` | `tracking/cmd/orbit/logstore.go:44,56` (codificado) | `tests/test_system_contract.sh` valida via grep |
| I6 | SKILL.md tem frontmatter + 9 markers invariantes | `skill/SKILL.md` (frontmatter YAML + seções) | `tests/test_skill_contract.sh` |
| I7 | Chain integrity: log legado (prev_proof vazio) inserido no MEIO da sequência → `Critical=true` + métrica dedicada | `tracking/store.go` (switch 3 casos) + `tracking.SchemaVersionStore=1` | `tracking/store_test.go:TestChainFailsOnLegacyInsertedMidSequence` |
| I8 | Nenhum arquivo tracked no git excede 5 MiB (anti-bloat) | `tracking/repo_hygiene_test.go:const maxTrackedBytes=5MB` | `tracking/repo_hygiene_test.go:TestNoLargeBinariesTracked` |
| I9 | Makefile não tem targets duplicados (nenhum `overriding recipe`) | `Makefile` (revisão manual + guard) | `tests/test_makefile_no_dup.sh` |
| I10 | Docs públicos na raiz não apontam o gate do Produto B | `docs/server-stack/` segregado + guard | `tests/test_docs_dont_claim_v1.sh` |
| I11 | `docs/CLI_RELEASE_GATE.md` contém exatamente o mesmo número de gates que `scripts/gate_cli.sh` | paridade doc ↔ script | `tests/test_gate_doc_parity.sh` |
| I12 | Secrets conhecidos (Bearer, sk-live/test, AKIA…, `password=`, SSH privates) NUNCA são persistidos em texto puro em `~/.orbit/logs/`. Aplicado em `output` E em `args`. Preserva `output_bytes` do original (I2 continua válido) | `tracking/redact.go` + `tracking/cmd/orbit/run.go` (chamado antes de `WriteExecutionLog`) | `tests/test_data_safety.sh` (cen. 1) |
| I13 | Logs em `$ORBIT_HOME/logs/` têm cap enforceado automaticamente (default 10000, configurável via `ORBIT_MAX_LOGS`). Prune síncrono após cada write remove os mais antigos por mtime | `tracking/cmd/orbit/logstore.go` (`pruneOldLogs`) | `tests/test_data_safety.sh` (cen. 2) |
| I14 | Binário sem commit-stamp (build sem `-ldflags -X main.Commit=...`) aborta `orbit run` antes de executar. Bypass explícito via `ORBIT_SKIP_GUARD=1` (documented escape hatch) | `tracking/cmd/orbit/startup_guard.go` + chamada em `main.go:44` | `tests/test_data_safety.sh` (cen. 3) |

**Leitura**: se qualquer linha da coluna "Teste que prova" for removida ou
parar de passar, o commit não é merjeável — `make gate-cli` bloqueia.

---

## 3. Garantias operacionais (sempre validadas)

Coisas que o sistema roda para garantir a primeira frente boa e a reversão
possível. Diferente das invariantes (negação), estas são positivas.

| # | Garantia | Mecanismo | Teste |
|---|---|---|---|
| O1 | `make gate-cli` roda offline em `<120s` | `GOTOOLCHAIN=local` + zero deps externas | medição ao rodar o gate |
| O2 | Rollback restaura `.bak` e valida versão pós-restore | `scripts/orbit_rollback.sh` | `tests/test_rollback.sh` |
| O3 | Install one-liner aborta se `sha256sum -c` falha | `scripts/install_remote.sh:133` | `tests/test_install_remote.sh` (cen. 4) |
| O4 | Release gate valida distribuição pública (tag + binário + sha256 + versão) | `scripts/release_gate.sh` (5 passos) | `tests/test_release_gate.sh` (6 cenários) |
| O5 | `orbit release` bloqueia push fora-de-main, tree-sujo, tag duplicada | `tracking/cmd/orbit/release.go:58-117` (6 fail-closed) | `tests/test_orbit_release.sh` (7 cenários) |
| O6 | Skill produz silêncio quando sessão é saudável | `skill/SKILL.md` Silence Rule + 18 evals | `tests/run_tests.py` |
| O7 | Log falhado em write não é persistido (fail-closed) | `tracking/store.go:AppendSessionRecord` valida antes de append | `tracking/store_test.go` seção 5 |

---

## 4. Roadmap (explicitamente FORA do contrato atual)

Estas coisas existem em algum nível no repositório mas **não estão
promovidas a contrato** — não são cobertas pelas invariantes acima. Se
quiser virar contrato, cada uma precisa: código + teste + entrada nova
em `## 2. Invariantes`.

| Item | Estado | Gatilho para virar contrato |
|---|---|---|
| `orbit verify --chain` (chain inter-logs no CLI) | Não implementado no CLI. Infra existe em `tracking/store.go`. | Ship comando + teste + nova entrada em §2 |
| Observatório de chain | `site/` é landing page, não dashboard. | Construir produto + teste de ponta-a-ponta |
| Identidade (`user_id`) + plano + billing | Explicitamente fora — `MONETIZATION.md` Fase 1 | Requer 30 dias de `resource_cost` observado antes da Fase 3 |
| Shadow mode do SkillRouter | Implementado em `orchestrator/client.py` mas só pertence ao Produto B | Promover só se server stack virar release público |
| Push automático de tag | Depende de infra externa (PAT/SSH do usuário) | Não virará contrato — é responsabilidade do ambiente |

> Promovidos para §2 nesta revisão: **I12 SECRET_SAFETY**, **I13 LOG_RETENTION**,
> **I14 ENV_INTEGRITY**. Implementação + mutation tests provados antes de virar
> contrato (conforme §5).

---

## 5. Convenção de mudança

- **Adicionar invariante**: (a) implementar em código, (b) criar teste que
  quebra sem a implementação (mutation test), (c) adicionar linha na
  tabela §2, (d) `make gate-cli` verde.
- **Remover invariante**: requer PR explícito justificando por que a
  proteção não é mais necessária. Remover sem justificativa trava G12.
- **Roadmap → Invariante**: mesmo processo de "adicionar invariante".
  Item sai de §4 e entra em §2 no mesmo commit.

Qualquer alteração deste documento **sem** alteração correspondente em
código/teste é regressão silenciosa. O gate G12 trava isso.
