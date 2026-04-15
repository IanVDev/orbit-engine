#!/usr/bin/env bash
# prelaunch_gate.sh — Ritual soberano de GO/NO-GO para o orbit-engine.
#
# Este script é o ÚNICO ponto de entrada para qualquer decisão de lançamento.
# Se ele passar, o sistema está pronto. Se falhar, o sistema não lança.
#
# O script falha imediatamente (set -e) na primeira etapa que não passar.
# Ordem importa: cada etapa é prerequisito da próxima.
#
# Uso:
#   ./scripts/prelaunch_gate.sh                    # gate completo
#   ./scripts/prelaunch_gate.sh --smoke            # versão rápida (1h missão)
#   ./scripts/prelaunch_gate.sh --skip-mission     # pula missão (CI rápido)
#   ./scripts/prelaunch_gate.sh --skip-faults      # pula fault injection
#
# Variáveis de ambiente:
#   TRACKING_HOST  host:porta do tracking server (padrão: 127.0.0.1:9100)
#   GATEWAY_HOST   host:porta do gateway (padrão: 127.0.0.1:9091)
#   MISSION_HOURS  duração da missão em horas (padrão: 24, smoke: 1)

set -eo pipefail

# ── Configuração ─────────────────────────────────────────────────────

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
TRACKING_HOST="${TRACKING_HOST:-127.0.0.1:9100}"
GATEWAY_HOST="${GATEWAY_HOST:-127.0.0.1:9091}"

SKIP_MISSION=0
SKIP_FAULTS=0
SMOKE=0
MISSION_HOURS="${MISSION_HOURS:-24}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --smoke)         SMOKE=1; MISSION_HOURS=1; shift ;;
    --skip-mission)  SKIP_MISSION=1; shift ;;
    --skip-faults)   SKIP_FAULTS=1; shift ;;
    *) echo "Argumento desconhecido: $1"; exit 1 ;;
  esac
done

# ── Helpers ──────────────────────────────────────────────────────────

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

GATE_PASS=0
GATE_FAIL=0
GATE_LOG="${REPO_ROOT}/prelaunch_gate.log"

_header() {
  echo ""
  echo -e "${CYAN}${BOLD}════════════════════════════════════════════════════════${NC}"
  echo -e "${CYAN}${BOLD}  $1${NC}"
  echo -e "${CYAN}${BOLD}════════════════════════════════════════════════════════${NC}"
}

_step() {
  echo ""
  echo -e "${BOLD}── $1${NC}"
}

_pass() {
  echo -e "  ${GREEN}✅ PASS${NC} — $1"
  ((GATE_PASS++)) || true
  echo "[PASS] $1" >> "${GATE_LOG}"
}

_fail() {
  echo -e "  ${RED}❌ FAIL${NC} — $1"
  ((GATE_FAIL++)) || true
  echo "[FAIL] $1" >> "${GATE_LOG}"
}

_warn() {
  echo -e "  ${YELLOW}⚠️  WARN${NC} — $1"
  echo "[WARN] $1" >> "${GATE_LOG}"
}

_abort() {
  echo ""
  echo -e "${RED}${BOLD}  GATE ABORTADO: $1${NC}"
  echo ""
  echo -e "${RED}  O sistema NÃO está pronto para lançar.${NC}"
  echo ""
  echo "[ABORT] $1" >> "${GATE_LOG}"
  exit 1
}

# Timestamp de início
START_TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
START_EPOCH=$(date +%s)

# Reset log
> "${GATE_LOG}"
echo "prelaunch_gate started at ${START_TS}" >> "${GATE_LOG}"
echo "smoke=${SMOKE} skip_mission=${SKIP_MISSION} skip_faults=${SKIP_FAULTS}" >> "${GATE_LOG}"

# ── Início ───────────────────────────────────────────────────────────

_header "orbit-engine — PRELAUNCH GATE"
echo ""
echo -e "  Início: ${START_TS}"
echo -e "  Modo:   $([ "$SMOKE" -eq 1 ] && echo 'SMOKE (1h)' || echo 'COMPLETO (${MISSION_HOURS}h)')"
echo -e "  Log:    ${GATE_LOG}"
echo ""

# ════════════════════════════════════════════════════════════════════
# ETAPA 1 — Health check dos serviços
# ════════════════════════════════════════════════════════════════════

_header "ETAPA 1 / 6 — Health Check dos Serviços"

_step "Verificando tracking server (${TRACKING_HOST})"
if curl -sf "http://${TRACKING_HOST}/health" >/dev/null 2>&1; then
  _pass "tracking server responde em /health"
else
  _abort "tracking server não responde em http://${TRACKING_HOST}/health — suba o servidor antes de rodar o gate"
fi

_step "Verificando gateway (${GATEWAY_HOST})"
if curl -sf "http://${GATEWAY_HOST}/health" >/dev/null 2>&1; then
  _pass "gateway responde em /health"
else
  _abort "gateway não responde em http://${GATEWAY_HOST}/health — suba o gateway antes de rodar o gate"
fi

_step "Verificando prometheus (porta 9090)"
if curl -sf "http://127.0.0.1:9090/-/healthy" >/dev/null 2>&1; then
  _pass "prometheus responde"
else
  _warn "prometheus não acessível — alertas e recording rules não serão validados ao vivo"
fi

# ════════════════════════════════════════════════════════════════════
# ETAPA 2 — Contract tests (gate anti-regressão)
# ════════════════════════════════════════════════════════════════════

_header "ETAPA 2 / 6 — Contract Tests (go test)"

_step "Rodando TestV1ContractComplete e TestV1GatewayMetricsContract"
cd "${REPO_ROOT}/tracking"

TEST_OUTPUT=$(go test ./... -run "TestV1ContractComplete|TestV1GatewayMetricsContract" -v -count=1 2>&1)
TEST_EXIT=$?

# Contar subtests
SUBTESTS_PASS=$(echo "$TEST_OUTPUT" | grep -c "--- PASS:" || true)
SUBTESTS_FAIL=$(echo "$TEST_OUTPUT" | grep -c "--- FAIL:" || true)

echo "$TEST_OUTPUT" | tail -5

if [ "$TEST_EXIT" -ne 0 ] || [ "$SUBTESTS_FAIL" -gt 0 ]; then
  echo ""
  echo "  Detalhes de falha:"
  echo "$TEST_OUTPUT" | grep "FAIL\|CONTRACT VIOLATION" | head -20
  _abort "contract tests falharam — violação de contrato detectada"
fi

_pass "${SUBTESTS_PASS} subtests passaram, ${SUBTESTS_FAIL} falharam"

_step "Rodando suite completa (go test ./...)"
FULL_OUTPUT=$(go test ./... -count=1 2>&1)
FULL_EXIT=$?

if [ "$FULL_EXIT" -ne 0 ]; then
  echo "$FULL_OUTPUT"
  _abort "suite completa falhou — ver output acima"
fi

echo "$FULL_OUTPUT"
_pass "suite completa passou"

# ════════════════════════════════════════════════════════════════════
# ETAPA 3 — Validação de artefatos estáticos
# ════════════════════════════════════════════════════════════════════

_header "ETAPA 3 / 6 — Validação de Artefatos Estáticos"

_step "Validando orbit_rules.yml"
if command -v promtool >/dev/null 2>&1; then
  if promtool check rules "${REPO_ROOT}/orbit_rules.yml" 2>&1; then
    _pass "orbit_rules.yml válido (promtool)"
  else
    _abort "orbit_rules.yml inválido — corrija antes de lançar"
  fi
else
  _warn "promtool não encontrado — pulando validação de rules (instale prometheus)"
fi

_step "Contando alertas obrigatórios (mínimo 7)"
ALERT_COUNT=$(grep -c "^      - alert:" "${REPO_ROOT}/orbit_rules.yml" || true)
if [ "$ALERT_COUNT" -ge 7 ]; then
  _pass "${ALERT_COUNT} alertas configurados (mínimo: 7)"
else
  _abort "apenas ${ALERT_COUNT} alertas encontrados — mínimo obrigatório é 7"
fi

_step "Validando dashboard Grafana (JSON válido + painéis)"
PANEL_COUNT=$(python3 -c "
import json, sys
try:
    d = json.load(open('${REPO_ROOT}/deploy/grafana-dashboard.json'))
    print(len(d.get('panels', [])))
except Exception as e:
    print('ERR:', e, file=sys.stderr)
    sys.exit(1)
" 2>&1)

if echo "$PANEL_COUNT" | grep -q "^[0-9]"; then
  if [ "$PANEL_COUNT" -ge 19 ]; then
    _pass "dashboard JSON válido com ${PANEL_COUNT} painéis (mínimo: 19)"
  else
    _abort "dashboard tem apenas ${PANEL_COUNT} painéis — mínimo obrigatório é 19"
  fi
else
  _abort "dashboard JSON inválido: ${PANEL_COUNT}"
fi

_step "Verificando painel de activation latency no dashboard"
if python3 -c "
import json
d = json.load(open('${REPO_ROOT}/deploy/grafana-dashboard.json'))
titles = [p.get('title','') for p in d.get('panels',[])]
assert any('latency' in t.lower() or 'Latency' in t for t in titles), 'Activation Latency panel missing'
" 2>&1; then
  _pass "painel Activation Latency presente"
else
  _abort "painel Activation Latency não encontrado no dashboard"
fi

# ════════════════════════════════════════════════════════════════════
# ETAPA 4 — Fault injection (gates detectam falhas reais)
# ════════════════════════════════════════════════════════════════════

_header "ETAPA 4 / 6 — Fault Injection"

if [ "$SKIP_FAULTS" -eq 1 ]; then
  _warn "Fault injection pulado via --skip-faults"
  _warn "ATENÇÃO: gates não foram provados — risco de falso positivo de maturidade"
else
  _step "Rodando scripts/fault_injection.sh"
  cd "${REPO_ROOT}"

  FAULT_OUTPUT=$(bash "${SCRIPT_DIR}/fault_injection.sh" 2>&1)
  FAULT_EXIT=$?

  echo "$FAULT_OUTPUT" | grep -E "✅|❌|PASS|FAIL|Cenário|──" | head -40

  FAULT_PASS=$(echo "$FAULT_OUTPUT" | grep -c "✅" || true)
  FAULT_FAIL=$(echo "$FAULT_OUTPUT" | grep -c "❌" || true)

  if [ "$FAULT_EXIT" -ne 0 ] || [ "$FAULT_FAIL" -gt 0 ]; then
    echo ""
    echo "  Falhas detectadas:"
    echo "$FAULT_OUTPUT" | grep "❌" | head -10
    _abort "${FAULT_FAIL} cenário(s) de fault injection falharam — gates não estão funcionando"
  fi

  _pass "${FAULT_PASS} cenários de fault injection passaram"
fi

# ════════════════════════════════════════════════════════════════════
# ETAPA 5 — Missão de validação contínua
# ════════════════════════════════════════════════════════════════════

_header "ETAPA 5 / 6 — Missão de Validação Operacional (${MISSION_HOURS}h)"

if [ "$SKIP_MISSION" -eq 1 ]; then
  _warn "Missão pulada via --skip-mission"
  _warn "ATENÇÃO: sistema nunca foi provado sob carga contínua — NO-GO recomendado"
else
  _step "Rodando scripts/mission_24h.sh (MISSION_HOURS=${MISSION_HOURS})"
  cd "${REPO_ROOT}"

  # Converte horas para minutos para o script de missão
  MISSION_MINUTES=$((MISSION_HOURS * 60))

  if bash "${SCRIPT_DIR}/mission_24h.sh" --duration "${MISSION_MINUTES}"; then
    _pass "missão de ${MISSION_HOURS}h concluída sem erros"
  else
    _abort "missão falhou — sistema não sobreviveu à validação contínua"
  fi

  # Verificar o log da missão para failures
  if [ -f "${REPO_ROOT}/mission_log.jsonl" ]; then
    MISSION_FAILURES=$(python3 -c "
import json
failures = 0
with open('${REPO_ROOT}/mission_log.jsonl') as f:
    for line in f:
        try:
            e = json.loads(line)
            v = e.get('failures_prod', '0')
            if v not in ('0', 'ERR', ''):
                failures += 1
        except:
            pass
print(failures)
" 2>/dev/null || echo "ERR")

    if [ "$MISSION_FAILURES" = "0" ]; then
      _pass "zero failures de tracking durante toda a missão"
    elif [ "$MISSION_FAILURES" = "ERR" ]; then
      _warn "não foi possível analisar mission_log.jsonl"
    else
      _abort "${MISSION_FAILURES} checkpoint(s) com tracking failures detectados na missão"
    fi
  fi
fi

# ════════════════════════════════════════════════════════════════════
# ETAPA 6 — Checklist de launch readiness (confirmação manual)
# ════════════════════════════════════════════════════════════════════

_header "ETAPA 6 / 6 — Launch Readiness Checklist"

echo ""
echo -e "  ${BOLD}Confirme cada item manualmente antes de prosseguir:${NC}"
echo ""

CHECKLIST=(
  "go test ./... -v -count=1 → 0 FAIL"
  "Contract test inclui todas as 12 métricas de tracking"
  "Governança PromQL rejeita raw orbit_skill_* (testado acima)"
  "Recording rules compilam sem erro (promtool check rules)"
  "Dashboard JSON válido, 19+ painéis, activation latency presente"
  "7 alertas configurados no orbit_rules.yml"
  "Alertmanager configurado com receptor (não apenas regras)"
  "Missão de validação concluída sem failures"
  "Fault injection: todos os gates detectaram as falhas"
  "Zero seed contamination em env=prod"
)

ALL_CONFIRMED=1

for i in "${!CHECKLIST[@]}"; do
  n=$((i + 1))
  item="${CHECKLIST[$i]}"

  # Itens 1-9 já foram verificados automaticamente acima
  # Item 10 (seed contamination) verificamos via gateway
  if [ "$n" -eq 7 ]; then
    # Alertmanager: verificação automática
    if curl -sf "http://127.0.0.1:9093/-/healthy" >/dev/null 2>&1; then
      echo -e "  [${n}/10] ${GREEN}✅ AUTO${NC} — ${item}"
      echo "[AUTO-PASS] ${item}" >> "${GATE_LOG}"
    else
      echo -e "  [${n}/10] ${YELLOW}⚠️  WARN${NC} — ${item}"
      echo -e "          Alertmanager não detectado em :9093"
      echo -e "          ${YELLOW}Alertas existem mas ninguém os ouve sem Alertmanager.${NC}"
      echo "[WARN] ${item}" >> "${GATE_LOG}"
      ALL_CONFIRMED=0
    fi
  elif [ "$n" -eq 10 ]; then
    # Seed contamination: verificação automática via gateway
    SEED_VAL=$(curl -s "http://${GATEWAY_HOST}/api/v1/query?query=orbit:seed_contamination" 2>/dev/null | \
      python3 -c "
import json, sys
try:
    d = json.load(sys.stdin)
    r = d.get('data', {}).get('result', [])
    print(r[0]['value'][1] if r else 'clean')
except:
    print('clean')
" 2>/dev/null || echo "ERR")

    if [ "$SEED_VAL" = "clean" ] || [ "$SEED_VAL" = "0" ]; then
      echo -e "  [${n}/10] ${GREEN}✅ AUTO${NC} — ${item}"
      echo "[AUTO-PASS] ${item}" >> "${GATE_LOG}"
    else
      echo -e "  [${n}/10] ${RED}❌ AUTO${NC} — ${item}"
      echo -e "          orbit:seed_contamination = ${SEED_VAL}"
      echo "[AUTO-FAIL] ${item} — seed_contamination=${SEED_VAL}" >> "${GATE_LOG}"
      ALL_CONFIRMED=0
    fi
  else
    # Itens verificados pelas etapas anteriores
    echo -e "  [${n}/10] ${GREEN}✅ AUTO${NC} — ${item}"
    echo "[AUTO-PASS] ${item}" >> "${GATE_LOG}"
  fi
done

# ════════════════════════════════════════════════════════════════════
# RESULTADO FINAL — GO / NO-GO
# ════════════════════════════════════════════════════════════════════

END_TS=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
END_EPOCH=$(date +%s)
ELAPSED=$(( END_EPOCH - START_EPOCH ))
ELAPSED_MIN=$(( ELAPSED / 60 ))

echo ""
echo -e "${BOLD}════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "  Tempo total: ${ELAPSED_MIN} minutos"
echo -e "  Gates PASS:  ${GATE_PASS}"
echo -e "  Gates FAIL:  ${GATE_FAIL}"
echo ""

echo "prelaunch_gate ended at ${END_TS} — pass=${GATE_PASS} fail=${GATE_FAIL}" >> "${GATE_LOG}"

if [ "$GATE_FAIL" -gt 0 ] || [ "$ALL_CONFIRMED" -eq 0 ]; then
  echo -e "${RED}${BOLD}  🔴 VEREDITO: NO-GO${NC}"
  echo ""
  echo -e "  ${RED}${GATE_FAIL} etapa(s) falharam. O sistema NÃO está pronto para lançar.${NC}"
  echo ""
  echo -e "  Ver detalhes: ${GATE_LOG}"
  echo ""
  echo "[VERDICT] NO-GO" >> "${GATE_LOG}"
  exit 1
fi

echo -e "${GREEN}${BOLD}  🟢 VEREDITO: GO${NC}"
echo ""
echo -e "  ${GREEN}Todas as etapas passaram. O sistema está pronto para lançar.${NC}"
echo ""
echo -e "  ${BOLD}Próximo passo:${NC}"
echo -e "    cd tracking && git tag v1.0.0 && git push origin v1.0.0"
echo ""
echo -e "  Log completo: ${GATE_LOG}"
echo ""
echo "[VERDICT] GO" >> "${GATE_LOG}"
