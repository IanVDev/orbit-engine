# Validation

> **⚙️ Advanced** — This document is for contributors and technical reviewers.
> If you just want to use the skill, start with [QUICK-START.md](QUICK-START.md).

How to verify that orbit-engine is working correctly.

## Test coverage

The skill includes 15 eval cases covering every rule in the engine.

### Positive cases (skill must activate)

| ID | Name | Tests |
| --- | --- | --- |
| 1 | Session long + complex task | Detection: session >15 msgs, idle MCPs, complex task. Gating: no /clear when context needed. |
| 2 | Tokens near limit | Detection: critical context. Priority: /compact first. |
| 3 | Complex migration | Detection: complex task. Strategy: Plan Mode, subtask division, model selection. |
| 4 | Idle MCPs | Detection: unused MCPs. Precision: disconnect only idle ones, keep relevant. |
| 5 | Large file pasted | Detection: large file. Strategy: @file:function reference. |
| 6 | Wrong model | Detection: Opus for routine task. Strategy: model recommendation. |
| 7 | Context above 60% | Detection: high context. Strategy: /compact with preservation. |
| 14 | Explicit request | Activation: direct question about token savings. |
| 15 | Fail-closed doubt | Activation: ambiguous signal activates (cost of not optimizing > optimizing). |

### Gating cases (skill must block specific actions)

| ID | Name | Tests |
| --- | --- | --- |
| 8 | No /clear with unsaved context | Gating: blocks /clear when decisions are not persisted. |
| 9 | No /compact on low context | Gating: blocks /compact when context is below 40%. |

### Negative cases (skill must NOT activate)

| ID | Name | Tests |
| --- | --- | --- |
| 10 | Simple question | No activation for one-line knowledge questions. |
| 11 | Trivial fix | No activation for trivial code corrections. |
| 12 | Casual conversation | No activation for social messages. |

### Stress case

| ID | Name | Tests |
| --- | --- | --- |
| 13 | Multi-signal combined | All 6 signals active simultaneously. Must prioritize by urgency, max 3 actions, risk critical. |

---

## Output format checklist

Every positive activation must produce output in this exact structure:

```text
DIAGNOSIS
- [signal 1]
- [signal 2]
- [signal 3, optional]
Risk: [low / medium / high / critical]

ACTIONS
1. [command or action]
2. [command or action]
3. [command or action, optional]

DO NOT DO NOW
- [counterproductive action in this context]
```

Verify:

- [ ] DIAGNOSIS section present with 1 to 3 bullet points
- [ ] Risk classification present (low, medium, high, or critical)
- [ ] ACTIONS section present with 1 to 3 numbered items
- [ ] Each action references a specific Claude Code command or feature
- [ ] DO NOT DO NOW section present with at least 1 item
- [ ] No estimated numbers, no tables, no long explanations
- [ ] No variation from the template structure

---

## Rule coverage matrix

| Rule | Eval IDs | Type |
| --- | --- | --- |
| Session >15 msgs without /clear | 1, 13 | Detection |
| Idle MCPs connected | 1, 4, 13 | Detection |
| Complex task without planning | 1, 3, 13, 15 | Detection |
| Context above 60% | 7, 13 | Detection |
| Large file pasted whole | 5 | Detection |
| Heavy model for routine task | 6, 13 | Detection |
| /clear blocked when context unsaved | 1, 8 | Gating |
| /compact blocked when context low | 9 | Gating |
| Multi-agent blocked when unnecessary | 13 | Gating |
| Fail-closed on doubt | 15 | Activation |
| No activation for simple questions | 10 | Negative |
| No activation for trivial tasks | 11 | Negative |
| No activation for casual conversation | 12 | Negative |

---

## How to run

1. Install the skill: drag the `skill/` folder into Claude Code
2. Open a new session
3. Test each scenario one at a time
4. Compare the response against the expected output and assertions
5. Mark each assertion as pass or fail

Total: 15 evals, 48 assertions.
