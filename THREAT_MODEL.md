# Threat Model — orbit-engine

Vetores de ameaça ativos e seu estado de mitigação.

---

## T1 — Injection de eventos no /track (MITIGADO)

**Vetor:** Atacante injeta eventos falsos no endpoint `/track` para contaminar
métricas, inflar contadores ou forçar decisões incorretas no motor de classificação.

**Mitigação implementada:**
HMAC-SHA256 via cabeçalho `X-Orbit-Signature`. Quando `ORBIT_HMAC_SECRET` está
configurado, toda requisição a `/track` e `/reconcile` exige assinatura válida.
Requisições sem assinatura ou com assinatura inválida são rejeitadas com HTTP 401
(fail-closed).

- Implementação: `security.go:ValidateHMAC`
- Ponto de aplicação: `realusage.go:151`
- Cobertura de teste: `security_init_test.go`

**Modo local (sem ORBIT_HMAC_SECRET):** autenticação desabilitada por design —
o servidor escuta em `127.0.0.1:9100` (loopback only) e não é exposto a redes.

---

## T2 — Desvio de documentação de segurança

**Vetor:** Colaborador edita este documento reintroduzindo descrições de gaps
já mitigados, causando falsa percepção de risco aberto.

**Mitigação implementada:**
`tracking/threat_model_doc_test.go` — gate anti-drift que falha no CI se frases
proibidas forem reintroduzidas ou se as referências de mitigação forem removidas.

---

## T3 — Vazamento de secrets em logs

**Vetor:** Comando executado via `orbit run` imprime tokens, senhas ou chaves no
stdout/stderr, que são persistidos em `~/.orbit/logs/`.

**Mitigação implementada:**
Redaction dupla antes de qualquer persistência:
- `tracking.RedactSecrets` (package tracking) — aplicada ao `result.Output`
- `redactOutput` (cmd/orbit) — aplicada ao log em disco

Padrões cobertos: `Authorization: Bearer`, `x-authorization:`, `password=`,
`token=`, `api_key=`, `sk-live-*`, `sk-test-*`, `AKIA...`, SSH private keys.

O SHA256 proof usa `output_bytes` do original capturado, não do texto redatado —
integridade do proof não é afetada pela redaction.

---

## Fora do escopo deste documento

- Análise de dependências transitivas de terceiros
- Ataques físicos ou de firmware
- Vetores que requerem acesso root ao sistema operacional
