# Validation

> **⚙️ Advanced** — This document is for contributors and technical reviewers.
> If you just want to use the skill, start with [QUICK-START.md](QUICK-START.md).

How to verify that orbit-engine is working correctly.

## Test coverage

The skill includes 13 eval cases covering every observable pattern and rule.

### Positive cases (skill must activate)

| ID | Name | Tests |
| --- | --- | --- |
| 1 | Correction chain | Detection: 3+ short follow-ups correcting output. Diagnosis includes prompt quality. |
| 2 | Repeated edits to same file | Detection: same file edited 3+ times. Risk high or critical. |
| 3 | Complex migration without constraints | Detection: weak prompt on large task. Recommends Plan Mode + constraints. |
| 4 | Unsolicited long response | Detection: response far exceeded what prompt implied. |
| 5 | Exploratory reading without plan | Detection: 5+ files read in one turn without specific goal. |
| 6 | Large file pasted | Detection: large content pasted instead of @file reference. |
| 7 | Explicit request | Activation: direct question about efficiency. Always fires. |
| 8 | Ambiguous — fail closed | Activation: unclear signal, skill activates (cost of missing > false positive). |

### Gating cases (skill must block specific actions)

| ID | Name | Tests |
| --- | --- | --- |
| 9 | No /clear with unsaved context | Gating: blocks /clear when decisions are not persisted. Recommends /compact instead. |
| 10 | No /compact on short session | Gating: blocks /compact when conversation is short and context is light. |

### Negative cases (skill must NOT activate)

| ID | Name | Tests |
| --- | --- | --- |
| 11 | Simple question | No activation for one-line knowledge questions. |
| 12 | Trivial fix | No activation for trivial code corrections. |
| 13 | Casual conversation | No activation for social messages. |

---

## Output format checklist

Every positive activation must produce output matching its risk level:

### Risk: low

- [ ] DIAGNOSIS present with 1 bullet point
- [ ] Risk classification present: "low"
- [ ] One-line note: "No action required. Something to keep in mind."
- [ ] NO ACTIONS section
- [ ] NO DO NOT DO NOW section

### Risk: medium

- [ ] DIAGNOSIS present with 1–2 bullet points
- [ ] Risk classification present: "medium"
- [ ] ACTIONS present with 1–2 numbered items
- [ ] DO NOT DO NOW present with at least 1 item
- [ ] Actions include mix of commands AND prompt/structure advice

### Risk: high

- [ ] DIAGNOSIS present with 2–3 bullet points
- [ ] Risk classification present: "high — address before continuing"
- [ ] ACTIONS present with 2–3 numbered items
- [ ] DO NOT DO NOW present with at least 1 item
- [ ] Skill waits for user acknowledgment before proceeding

### Risk: critical

- [ ] DIAGNOSIS present with 2–3 bullet points
- [ ] Risk classification present: "critical"
- [ ] ACTIONS present, first item prefixed with ⚠️
- [ ] DO NOT DO NOW present with at least 1 item
- [ ] Skill waits for user to act on ⚠️ action before proceeding

### All risk levels

- [ ] Each diagnosis item describes something observable in the conversation
- [ ] No fabricated numbers, dollar amounts, or percentages
- [ ] No variation from the template structure
- [ ] Actions reference real Claude Code commands, prompt improvements, or task structure changes

---

## Pattern coverage matrix

| Observable Pattern | Eval IDs | Type |
| --- | --- | --- |
| Unsolicited long response | 4 | Detection |
| Correction chain (3+ follow-ups) | 1 | Detection |
| Repeated edits to same target | 2 | Detection |
| Exploratory reading without plan | 5 | Detection |
| Weak prompt (missing constraints) | 1, 3 | Detection |
| Large content pasted | 6 | Detection |
| /clear blocked when context unsaved | 9 | Gating |
| /compact blocked when context light | 10 | Gating |
| Rewrite prompt blocked when prompt is clear | 10 | Gating |
| Fail-closed on doubt | 8 | Activation |
| No activation for simple questions | 11 | Negative |
| No activation for trivial tasks | 12 | Negative |
| No activation for casual conversation | 13 | Negative |

---

## How to run

1. Install the skill: drag the `skill/` folder into Claude Code
2. Open a new session
3. Test each scenario one at a time
4. Compare the response against the expected output and risk-level checklist
5. Mark each assertion as pass or fail

Total: 13 evals.
