# PR de novo parser — expansion protocol

> **Este template aplica-se APENAS a PRs que adicionam um novo parser ao
> `dispatchParser`.** Para bugfix, docs, refactor ou mudanças no contrato
> de `expansion_policy`, abra uma PR sem template e descreva o escopo.
>
> Template selecionável via URL: `?template=new_parser.md`

---

## Contexto do candidato

Qual comando este parser cobre?

- **Bucket (command.split()[0]):** `<preencher>`
- **Evento roteado:** `EventTestRun` / `EventBuild` / `<outro>`
- **Toolchain:** `<cargo / tsc / rustc / ...>`

---

## Pré-submissão — protocolo de expansão

Li `EXPANSION_DECISION_PROTOCOL` em `scripts/parse_orbit_events.py` e
confirmo que as quatro condições foram cumpridas. A ausência de
qualquer item abaixo é motivo de **rejeição imediata** em revisão, sem
discussão adicional.

- [ ] **T0** — timestamp ISO 8601 UTC da primeira observação de
  `expansion_candidates`.
  - `T0 = <ISO>`
  - Trecho literal do `/api/dashboard` em T0 (sanitizar paths privados
    se necessário) colado abaixo.
- [ ] **T1** — timestamp ISO 8601 UTC de uma observação
  `>= T0 + PARSER_EXPANSION_WINDOW_DAYS` (7 dias).
  - `T1 = <ISO>`
  - Trecho literal do `/api/dashboard` em T1 colado abaixo.
  - Distância T1 − T0 ≥ 7 dias confirmada.
- [ ] **Bucket idêntico** — `command.split()[0]` retorna o MESMO token
  em T0 e T1 (mesmo command bucket acima).
- [ ] **Zero mudança no contrato** — nenhuma alteração em
  `PARSER_EXPANSION_THRESHOLD`, `PARSER_EXPANSION_WINDOW_DAYS`,
  `PARSER_EXPANSION_MIN_DISTINCT_DAYS`, `expansion_policy`,
  `EXPANSION_DECISION_PROTOCOL` ou no shape de `expansion_candidates`.
  Essas alterações, se necessárias, exigem PR separada com justificativa
  própria.

---

## Mudança técnica

- [ ] `dispatchParser` continua como fonte única — apenas UMA nova
  cláusula `case` foi adicionada em `tracking/cmd/orbit/diagnose.go`.
- [ ] Nenhum regex paralelo fora de `diagnose.go`.
- [ ] Nenhuma camada nova — parser novo segue o padrão de assinatura:
  `parse<Tool>Failure(d *Diagnosis, output string)`.
- [ ] `BuildGuidance` estendido para o novo evento (se aplicável),
  reusando `firstFileLine`.

---

## Evidências (colar no corpo da PR)

### T0 — `/api/dashboard` em `<ISO>`

```json
{
  "expansion_policy": {"threshold": 5, "window_days": 7, "min_distinct_days": 2},
  "expansion_candidates": [
    {"command": "<bucket>", "silenced_count": <N>, "distinct_days": <M>}
  ]
}
```

### T1 — `/api/dashboard` em `<ISO>` (T1 − T0 ≥ 7 dias)

```json
{
  "expansion_policy": {"threshold": 5, "window_days": 7, "min_distinct_days": 2},
  "expansion_candidates": [
    {"command": "<bucket>", "silenced_count": <N>, "distinct_days": <M>}
  ]
}
```

### Output canônico que o parser CASA (high confidence)

```
<colar output literal que produz confidence=high>
```

### Output que o parser NÃO casa (fail-closed)

```
<colar output real — erro de rede, missing deps, etc. —
 em que o parser deve devolver confidence=none>
```

---

## Testes obrigatórios adicionados

Novo arquivo `tracking/cmd/orbit/diagnose_<tool>_test.go` com:

- [ ] `TestParse<Tool>Failure_HighConfidence`
- [ ] `TestParse<Tool>Failure_MediumConfidence` (se o formato suporta)
- [ ] `TestParse<Tool>Failure_NoMatch_FailsClosed` — com ≥ 2 cenários
  reais de output que NÃO devem casar
- [ ] `TestParse<Tool>Failure_EmptyOutput`
- [ ] `TestGuidanceAndDiagnose_AgreeOn<Event>` — guardião de
  convergência guidance ≡ diagnose.file:line

## Testes preservados (DEVEM continuar verdes)

- [ ] `TestDiagnosisRunSlowPathParity` — dispatcher single source of
  truth; nova linha na tabela cobre o evento
- [ ] `TestBuildDiagnosisForRun_DispatchByEvent` — nova linha confirma
  roteamento para o parser novo
- [ ] `test_expansion_candidate_contract` (10 subTests) — inalterado
- [ ] `test_decision_protocol_is_documented` — inalterado

---

## Commit message (modelo)

```
feat(diagnose): add <tool> parser for <event> event

Extends dispatchParser with a new case for <event>, routed to
parse<Tool>Failure. Follows the same contract as the go test/build
parsers: high/medium/none confidence, fail-closed on unknown format.

Evidence chain (EXPANSION_DECISION_PROTOCOL):
  T0: <ISO timestamp>  bucket=<command>  count=<N>  distinct_days=<M>
  T1: <ISO timestamp>  bucket=<command>  count=<N>  distinct_days=<M>
  Window: PARSER_EXPANSION_WINDOW_DAYS (7d) respected
  Threshold: PARSER_EXPANSION_THRESHOLD (5) met in both

Tests added:
  TestParse<Tool>Failure_HighConfidence
  TestParse<Tool>Failure_NoMatch_FailsClosed
  TestGuidanceAndDiagnose_AgreeOn<Event>

Tests preserved (all green):
  TestDiagnosisRunSlowPathParity
  TestBuildDiagnosisForRun_DispatchByEvent
  test_expansion_candidate_contract
  test_decision_protocol_is_documented
```

---

## Critérios de rejeição (leitura obrigatória antes de submeter)

A PR é **rejeitada automaticamente em revisão** — sem negociação — se
qualquer um dos itens abaixo se verificar:

1. T0 ou T1 ausentes no commit ou PR description.
2. T1 − T0 < 7 dias (`PARSER_EXPANSION_WINDOW_DAYS`).
3. Bucket divergente entre T0 e T1.
4. Regex do parser casa output arbitrário não-relacionado (fuzz smoke
   com `ls -la`, `uname -a`, etc. — se qualquer desses der
   `confidence ≠ none`, o parser é loose demais).
5. Alteração em `expansion_policy` / `EXPANSION_DECISION_PROTOCOL` /
   janela / threshold incluída junto — mistura escopos.
6. `TestDiagnosisRunSlowPathParity` falha para o novo evento.
7. Novo regex fora de `diagnose.go` — parsers têm um único domicílio.
8. Commit body sem referência a `PARSER_EXPANSION_WINDOW_DAYS` ou
   constante equivalente — sinal de que o protocolo não foi lido.

---

*Este template é versionado. Alterações exigem PR dedicada. Teste
`ParserPRTemplateTest.test_parser_pr_template_is_wellformed` trava a
integridade dos marcadores.*
