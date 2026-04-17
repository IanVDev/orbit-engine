# ORBIT-ENGINE — DONE 100% Checklist

> **Data**: 2026-04-17  
> **HEAD no momento da avaliação**: `6b35dfb`  
> **Veredito atual**: `NOT_READY` (5 gaps críticos)  
> **Regra**: Se um item não passar no teste, Orbit NÃO está pronto. Sem discussão.

---

## Pré-requisitos

Rode na raiz do repo (`/Users/ian/Documents/orbit-engine`). Go instalado.

```bash
cd /Users/ian/Documents/orbit-engine
go version   # deve ser 1.21+
```

---

## Portão 0 — Baseline verde (bloqueador absoluto)

Nada avança sem isso.

```bash
cd tracking && go test ./... -count=1 -race
cd .. && bash orbit-hygiene/test.sh
./scripts/build_orbit.sh
```

**Saída esperada**
- `go test`: `ok` em todos os pacotes, zero FAIL, zero race warning.
- `orbit-hygiene/test.sh`: sai com `PASS` (exit 0).
- `build_orbit.sh`: instala binário, `orbit version` imprime `orbit version ... (commit=6b35dfb build=<timestamp>)`.

**Falha aqui** → NOT_READY. Pare tudo e conserte o teste/build antes de seguir.

---

## G1 — Documentação de segurança alinhada ao código

**Fato**: HMAC está wired e testado. Doc `THREAT_MODEL.md` T1 ainda diz "P3 backlog".

**Critério verificável**
```bash
grep -n "P3 backlog\|Sem autenticação" THREAT_MODEL.md
```

**Saída esperada para PASS**: _vazio_ (nenhuma linha retornada).  
**Ação para fechar**: editar T1 — trocar "⚠️ Gap" por "✅ MITIGADA", apontar código (`security.go:ValidateHMAC`, `realusage.go:151`) e teste (`security_init_test.go:TestSecurity_FailClosedOnMissingHMACInProduction`).

**Teste anti-regresso (adicionar)**
```bash
cat > tracking/threat_model_doc_test.go <<'EOF'
package tracking

import (
    "os"
    "strings"
    "testing"
)

func TestThreatModelDocReflectsHMACReality(t *testing.T) {
    b, err := os.ReadFile("../THREAT_MODEL.md")
    if err != nil { t.Fatal(err) }
    s := string(b)
    if strings.Contains(s, "P3 backlog") || strings.Contains(s, "Sem autenticação no /track") {
        t.Fatal("THREAT_MODEL.md T1 está stale: código já implementou HMAC com fail-closed prod.")
    }
}
EOF
cd tracking && go test -run TestThreatModelDocReflectsHMACReality -count=1
```

---

## G2 — Métricas de valor (não só técnicas)

**Fato**: counters técnicos existem (`orbit_skill_activations_total`, `orbit_skill_tokens_saved_total`). **Faltam** counters de valor do produto.

**Critério verificável**
```bash
cd tracking && grep -E "orbit_proofs_generated_total|orbit_hygiene_installations_total|orbit_quickstart_completed_total|orbit_verify_success_total" *.go | grep -v _test.go
```

**Saída esperada para PASS**: 4 linhas (uma por métrica).  
**Hoje**: _vazio_ → gap aberto.

**Ação para fechar** (ordem, cada um com 1 teste):

1. `orbit_proofs_generated_total` — incrementado em `ComputeHash` ou no handler `/track` quando `tokens_used > 0`. Teste: request com tokens → métrica +1.
2. `orbit_hygiene_installations_total` — incrementado no `hygiene install` com `result` label (`installed`/`already_present`). Teste: comando executado 2x → contador 1x installed, 1x already_present.
3. `orbit_quickstart_completed_total` — incrementado ao final de `quickstart.go` após verify OK. Teste: subprocess de `orbit quickstart` → métrica +1.
4. `orbit_verify_success_total` / `orbit_verify_failure_total` — já existe lógica em `ComputeHash`; expor como métrica. Teste: proof inválido → failure +1.

**Comando de aceite final**
```bash
curl -s http://127.0.0.1:9100/metrics | grep -E "orbit_(proofs_generated|hygiene_installations|quickstart_completed|verify_(success|failure))_total"
# Esperado: 4+ linhas, não vazio
```

---

## G3 — Quickstart determinístico (teste E2E)

**Fato**: `quickstart.go` existe, imprime `[1/3]`, `[2/3]`, `[3/3]` e valida proof. **Não existe** teste Go que exercite o comando inteiro e valide output linha-a-linha.

**Critério verificável**
```bash
grep -rn "TestQuickstart\|TestCmdQuickstart" tracking/cmd/orbit/*_test.go
```

**Saída esperada para PASS**: pelo menos 1 resultado.  
**Hoje**: _vazio_ → gap aberto.

**Ação para fechar** — adicionar `tracking/cmd/orbit/quickstart_test.go`:

```go
package main

import (
    "os/exec"
    "regexp"
    "strings"
    "testing"
)

// TestQuickstartDeterministic roda `orbit quickstart` 2x e valida que
// a estrutura de saída é idêntica (session_id muda, mas steps/proof são consistentes).
func TestQuickstartDeterministic(t *testing.T) {
    bin := buildOrbitForTest(t) // helper: compila em tmp
    run := func() string {
        out, err := exec.Command(bin, "quickstart").CombinedOutput()
        if err != nil { t.Fatalf("quickstart: %v\n%s", err, out) }
        return string(out)
    }
    a, b := run(), run()

    stepRe := regexp.MustCompile(`\[(\d)/3\]`)
    if len(stepRe.FindAllString(a, -1)) != 3 { t.Fatal("faltam steps em run A") }
    if len(stepRe.FindAllString(b, -1)) != 3 { t.Fatal("faltam steps em run B") }

    for _, want := range []string{"proof válido", "sha256 verificado"} {
        if !strings.Contains(a, want) || !strings.Contains(b, want) {
            t.Fatalf("saída não contém %q", want)
        }
    }
}
```

**Aceite**
```bash
cd tracking && go test ./cmd/orbit -run TestQuickstartDeterministic -count=3
# -count=3: se for flaky, pega em ≥1 tentativa. Precisa passar 3x.
```

---

## G4 — Distribuição pública (release + binário versionado)

**Fato**: `build_orbit.sh` builda de source; `orbit version` imprime version+commit+buildTime; `install.sh` instala local. **Faltam**: GitHub release, binário pré-built, one-liner de install.

**Critério verificável**
```bash
ls .github/workflows/ | grep -E "release|publish"
gh release list 2>/dev/null | head -3
```

**Saída esperada para PASS**:
- Pelo menos 1 workflow `release*.yml`.
- Pelo menos 1 release publicado (`v1.0.0`).

**Hoje**: só `regression-guards.yml` e `test.yml` → gap aberto.

**Ação para fechar** (mínimo viável, sem overengineering):

1. Criar `.github/workflows/release.yml`:
   - Trigger: tag `v*`.
   - Build para `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`.
   - Upload assets ao release (binários + SHA256).
   - LDFLAGS: `-X main.Version=$GITHUB_REF_NAME -X main.Commit=$GITHUB_SHA`.

2. Documentar one-liner em `README.md`:
   ```bash
   curl -sSL https://github.com/<user>/orbit-engine/releases/latest/download/install.sh | bash
   ```

3. Primeira tag: `git tag v1.0.0 && git push --tags`.

**Aceite**
```bash
curl -sSL -o /tmp/orbit https://github.com/<user>/orbit-engine/releases/latest/download/orbit-$(uname -s | tr A-Z a-z)-$(uname -m)
chmod +x /tmp/orbit
/tmp/orbit version
# Esperado: orbit version v1.0.0 (commit=<sha> build=<ts>)
```

**Teste anti-regresso no CI**: após publicar, `release.yml` deve rodar `./orbit version` pós-upload e falhar se output não contiver `v1.0.0`.

---

## G5 — UX consistente cross-command (audit)

**Fato**: `hygiene` usa INSTALLED/NOT_INSTALLED/ALREADY_PRESENT; `doctor` usa OK/WARNING/CRITICAL; `quickstart` usa `[n/3]` com `✓`. Nunca foi auditado se todos os comandos seguem o mesmo padrão.

**Critério verificável** — toda saída de comando deve se encaixar em um dos 3 padrões:
- **Status explícito** (INSTALLED/NOT_INSTALLED/WARNING/CRITICAL/OK)
- **Step progress** (`[n/total] mensagem` + `✓`/`✗`)
- **KV report** (`Label     : valor`)

```bash
# Lista comandos expostos
grep -E 'case "' tracking/cmd/orbit/main.go | awk -F'"' '{print $2}'

# Para cada, execute e capture saída. Verifique visualmente contra os 3 padrões.
for cmd in doctor hygiene analyze quickstart context-pack version run; do
  echo "=== orbit $cmd ==="
  orbit "$cmd" --help 2>&1 | head -20
done
```

**Saída esperada para PASS**: cada comando imprime em um dos 3 formatos. Zero saída "livre" estilo `fmt.Println("alguma coisa")` sem prefixo de status/step/KV.

**Ação para fechar** (só se gap real for encontrado):
- Refatorar saída divergente para usar `PrintSection`/`PrintKV`/`printStep`.
- Adicionar teste de contrato: `TestAllCommandsUseStandardOutput` que roda cada comando com `--help` e valida regex do padrão.

**Aceite**
```bash
cd tracking && go test ./cmd/orbit -run TestAllCommandsUseStandardOutput -count=1
```

---

## Portão Final — Declaração DONE

Orbit está `READY` quando:

```bash
# 1. Baseline verde
cd tracking && go test ./... -count=1 -race
bash ../orbit-hygiene/test.sh

# 2. Portões G1–G5 passam
cd tracking && go test -run "TestThreatModelDoc|TestQuickstartDeterministic|TestAllCommandsUseStandardOutput" -count=1

# 3. Métricas de valor expostas
curl -s http://127.0.0.1:9100/metrics | grep -cE "orbit_(proofs|hygiene_installations|quickstart_completed|verify_success)" | grep -q "^[4-9]"

# 4. Release publicado + binário pré-built funciona
curl -sSL -o /tmp/orbit https://github.com/<user>/orbit-engine/releases/latest/download/orbit-$(uname -s | tr A-Z a-z)-amd64
chmod +x /tmp/orbit && /tmp/orbit version | grep -q "v1."

# 5. Doctor deep limpo em prod-like
ORBIT_ENV=production ORBIT_HMAC_SECRET=test orbit doctor --deep --strict
```

**Todos os 5 passos saem com exit 0** → `READY`.  
**Qualquer passo falha** → `NOT_READY`, com o G correspondente identificado no output.

---

## Mapa gaps → épicos Jira (para quando você mandar criar)

| Gap | Épico KAN sugerido | Tipo | Tasks |
|---|---|---|---|
| G1 | `DOC DRIFT: THREAT_MODEL alinhado ao código` | Tarefa única | 1 edit + 1 teste |
| G2 | `VALUE METRICS: counters de produto` | Epic | 4 tasks (uma por métrica) |
| G3 | `QUICKSTART E2E TEST` | Tarefa única | 1 teste Go |
| G4 | `DISTRIBUIÇÃO v1.0.0` | Epic | 3 tasks (workflow + tag + docs) |
| G5 | `UX AUDIT cross-command` | Epic | 1 teste + N refatorações |

---

## Threat model deste checklist

- **Abuso provável**: marcar G como DONE sem rodar o comando de aceite → falso READY.  
  **Mitigação**: cada G exige um teste Go que roda no CI. Se teste não existe, G continua aberto.
- **Abuso provável**: release publicado mas binário quebrado → usuário externo não consegue rodar.  
  **Mitigação**: G4 exige `orbit version` pós-upload no próprio workflow.
- **Drift docs ↔ código**: já aconteceu (G1). Mitigação: `TestThreatModelDocReflectsHMACReality` impede reintrodução.

---

## ESSÊNCIA CHECK: ok
Este checklist não adiciona features. Ele apenas fecha os gaps já existentes, cada um com critério verificável em código/teste/comando. Zero inovação, zero overengineering.
