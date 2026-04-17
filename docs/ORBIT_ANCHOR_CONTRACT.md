# Contrato Orbit → AURYA (`orbit anchor`)

**Status: proposto, NÃO implementado.** Este documento fixa o contrato que `orbit anchor` deve obedecer quando for construído. Nada aqui é código que executa. Existe para evitar que a implementação vire especulação — define inputs, outputs, e o que um terceiro precisa para verificar de forma independente.

---

## 1. Separação de camadas

| Camada | Papel | Mutável? | Prova para terceiros? |
|---|---|---|---|
| **Orbit — ledger local** (`~/.orbit/client_ledger.jsonl`) | registrar evidência reproduzível da sessão | sim, usuário tem write | **não** |
| **Orbit — backend HTTP** (`:9100`) | validar schema, agregar métricas, reconciliar | sim, operador tem write | **não** |
| **AURYA — ancoragem** | publicar compromisso imutável do estado Orbit num instante T | não (append-only + consenso) | **sim** |

Orbit **produz** evidência. AURYA **ancora** evidência.
Sem AURYA, Orbit é self-audit reproduzível. Com AURYA, um auditor externo verifica sem confiar no operador Orbit nem no usuário local.

`orbit explain` prova (1) e (2). `orbit anchor` + AURYA prova (3). São comandos separados por design — não misturar responsabilidades.

---

## 2. Input — o que `orbit anchor` envia

O cliente lê todas as entradas do ledger local filtradas por `session_id`, ordena por `timestamp` ascendente, e monta um compromisso único:

```json
{
  "orbit_version": "1.0",
  "session_id": "refine-1776443535",
  "client_fingerprint": "sha256:<hash do install orbit, estável por máquina>",
  "anchored_at_client": "2026-04-17T16:35:00.000Z",
  "event_count": 2,
  "ts_range": {
    "first": "2026-04-17T16:32:15.946Z",
    "last":  "2026-04-17T16:34:02.112Z"
  },
  "merkle_root": "b9a1...e4",
  "batch_hash":  "7c3f...91",
  "events": [
    {"skill_event_hash": "c6e920e08f29bca1...", "server_event_id": "cecdae4c7e00067f..."},
    {"skill_event_hash": "d1ab77...",           "server_event_id": "9f02e3..."}
  ]
}
```

### Regras de construção (normativas)

1. **`events`** contém APENAS hashes — nenhum payload sensível atravessa a fronteira. O conteúdo do evento permanece no ledger local.
2. **`merkle_root`** = SHA-256 de uma árvore binária balanceada sobre `skill_event_hash` em ordem cronológica. Folhas ímpares duplicam a última.
3. **`batch_hash`** = SHA-256 da serialização JSON canônica do objeto acima **excluindo os campos `batch_hash` e `signature`**. Canonicalização = chaves ordenadas, separadores `(",", ":")`, UTF-8 sem BOM.
4. **Assinatura (opcional mas recomendada)**: Ed25519 sobre `batch_hash` usando `ORBIT_ANCHOR_KEY`. AURYA pode rejeitar payloads não-assinados em modo estrito.

---

## 3. Endpoint AURYA (contrato do lado AURYA)

```
POST https://aurya.example/anchor/v1
Content-Type: application/json
X-Orbit-Signature: <hex ed25519 sobre batch_hash>
X-Orbit-Public-Key: <base64 chave pública Ed25519>
```

Respostas normativas:

| Código | Significado | Ação do cliente |
|---|---|---|
| `200` | `{anchor_id, tx_ref, anchored_at, aurya_height}` | grava receipt, exit 0 |
| `409` | `batch_hash` já ancorado (idempotente) — retorna receipt existente | grava receipt se ausente, exit 0 |
| `4xx` | payload inválido ou assinatura ruim | escreve falha em `~/.orbit/anchor_log.jsonl`, **fail-closed (exit != 0)** |
| `5xx` / timeout | AURYA indisponível | **fail-closed (exit != 0)** — nunca simular sucesso |

**Regra dura:** orbit NUNCA assume ancoragem sem resposta assinada pela AURYA. Fail-closed é inegociável.

---

## 4. Output — o que `orbit anchor` grava localmente

Após sucesso (200 ou 409), append em `~/.orbit/anchor_receipts.jsonl`:

```json
{
  "schema_version": 1,
  "anchored_at": "2026-04-17T16:35:00.042Z",
  "session_id": "refine-1776443535",
  "batch_hash": "7c3f...91",
  "merkle_root": "b9a1...e4",
  "event_count": 2,
  "anchor_id": "aurya-a7b2-0001-4f",
  "tx_ref": "aurya://block/142338/tx/0a19",
  "aurya_height": 142338
}
```

Append-only, uma linha por ancoragem. O ledger de eventos e o ledger de receipts são arquivos separados — um evento pode ser ancorado múltiplas vezes ao longo do tempo (janelas rotativas).

---

## 5. Verificação independente por terceiros

Qualquer auditor pode validar sem confiar no operador Orbit nem no usuário local:

```
ENTRADAS necessárias do usuário sob auditoria:
  - ~/.orbit/client_ledger.jsonl          (ledger de eventos)
  - ~/.orbit/anchor_receipts.jsonl        (receipts)
  - session_id alvo

PASSOS:
  1. Filtrar ledger por session_id; ordenar por timestamp.
  2. Para cada evento, recomputar sha256(session_id|timestamp|tokens)
     e comparar com skill_event_hash armazenado. (integridade do evento)
  3. Recomputar merkle_root dos skill_event_hash ordenados.
  4. Montar payload canônico (§2) e recomputar batch_hash.
  5. Buscar receipt com mesmo session_id no anchor_receipts.jsonl.
  6. Comparar merkle_root e batch_hash recomputados com os do receipt.
     Se divergir → ledger foi reescrito APÓS ancoragem.
  7. GET https://aurya.example/anchor/v1/<anchor_id>
     → {batch_hash, tx_ref, aurya_height}
     Se batch_hash retornado != batch_hash do receipt → receipt forjado.
  8. Validar tx_ref contra cadeia AURYA pública.
```

Cada passo é mecânico. Se (2) falha, o ledger foi adulterado. Se (6) falha, o ledger foi reescrito após ser ancorado. Se (7) falha, o receipt é inventado. Nenhum desses exige acesso privilegiado ao operador.

---

## 6. O que este contrato NÃO resolve (intencionalmente)

- **Lacuna temporal pré-âncora.** Eventos entre `last_anchor_at` e `now` não têm prova externa. `orbit explain` DEVE exibir esta janela explicitamente. A solução é aumentar frequência de ancoragem, não esconder a lacuna.
- **Reordenação antes da primeira âncora.** O encadeamento `prev_hash` no core Go ainda não entra no `skill_event_hash` — portanto uma reordenação detectável só via AURYA. Backlog conhecido.
- **Conteúdo auditável pelo terceiro.** AURYA recebe só hashes. Um auditor que queira ver o conteúdo dos eventos precisa do ledger do usuário. Isto é feature, não bug — evita vazar payloads sensíveis para um sistema externo.
- **Privacidade do `session_id`.** Se o `session_id` for informação sensível, o cliente deve derivar um `session_id_hash = sha256(session_id | salt_local)` antes de enviar. Salt nunca sai da máquina. (Padrão a decidir no Release.)

---

## 7. Roadmap de implementação (ordem sugerida, **não fazer agora**)

1. `scripts/orbit_anchor.sh --dry-run <session_id>` — lê ledger, computa `merkle_root` e `batch_hash`, imprime payload canônico em stdout. Zero rede.
2. Mock AURYA (`deploy/aurya_mock/`) — endpoint HTTP que devolve receipt determinístico. Viabiliza testes de integração.
3. Cliente real — POST + append em `anchor_receipts.jsonl`, fail-closed em qualquer erro.
4. Estender `orbit_explain.sh` com **fase 4** — se houver receipt para o `session_id`, recomputar `batch_hash` e bater contra AURYA. Se divergir → exit 2.
5. Testes de adulteração análogos a `test_orbit_explain_detects_tampering.sh`: adulterar ledger APÓS ancoragem deve ser detectado em (4).

**Gatilho para começar implementação:** AURYA expor o endpoint real com schema estável. Até lá, este documento é o contrato congelado.
