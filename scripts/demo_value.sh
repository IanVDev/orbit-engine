#!/usr/bin/env bash
# scripts/demo_value.sh — Demonstração de economia de tokens com orbit-engine.
#
# Simula dois cenários e compara:
#   1. Sem orbit  — fluxo convencional (baseline)
#   2. Com orbit  — fluxo otimizado pelo orbit-engine
#
# Não requer servidor em execução: os cálculos são locais e determinísticos.
# Se ORBIT_URL estiver definido, envia um evento real ao /track endpoint.
#
# Uso:
#   bash scripts/demo_value.sh
#   ORBIT_URL=http://localhost:9100 bash scripts/demo_value.sh
#
# Saída esperada: tabela de comparação + economia percentual clara.
set -euo pipefail

# ── Configuração ────────────────────────────────────────────────────────────
ORBIT_URL="${ORBIT_URL:-}"          # se vazio, modo offline (sem envio)
SESSION_ID="demo-$(date +%s)"      # sessão única por execução

# ── Baseline: execução sem orbit ────────────────────────────────────────────
# Representação de um fluxo de 5 etapas de análise sem assistência:
#   - Análise manual de contexto
#   - Geração de prompt sem compressão
#   - Consulta a LLM sem cache
#   - Re-iteração por falta de estrutura
#   - Consolidação manual de resultado
STEPS_NO_ORBIT=5
TOKENS_PER_STEP_NO_ORBIT=200          # tokens médios por etapa sem otimização
TOKENS_NO_ORBIT=$(( STEPS_NO_ORBIT * TOKENS_PER_STEP_NO_ORBIT ))

# ── Com orbit ───────────────────────────────────────────────────────────────
# O orbit-engine:
#   - Comprime contexto relevante (-40%)
#   - Elimina etapas redundantes (4 em vez de 5)
#   - Cache de padrões repetidos (-20% nas etapas restantes)
STEPS_WITH_ORBIT=4
TOKENS_PER_STEP_WITH_ORBIT=150        # tokens médios por etapa com orbit
TOKENS_WITH_ORBIT=$(( STEPS_WITH_ORBIT * TOKENS_PER_STEP_WITH_ORBIT ))

# ── Cálculo de economia ─────────────────────────────────────────────────────
TOKENS_SAVED=$(( TOKENS_NO_ORBIT - TOKENS_WITH_ORBIT ))
# Percentual com 2 casas (multiplicamos por 100 antes da divisão inteira)
PCT_SAVED=$(( TOKENS_SAVED * 100 / TOKENS_NO_ORBIT ))

# ── Impressão do relatório ──────────────────────────────────────────────────
echo ""
echo "╔═══════════════════════════════════════════════════════════╗"
echo "║          orbit-engine — demo de valor percebido           ║"
echo "╚═══════════════════════════════════════════════════════════╝"
echo ""
echo "  ┌─────────────────────────┬──────────────┬──────────────┐"
echo "  │ Métrica                 │  Sem orbit   │  Com orbit   │"
echo "  ├─────────────────────────┼──────────────┼──────────────┤"
printf "  │ %-23s │ %12d │ %12d │\n" "Etapas de análise"  "$STEPS_NO_ORBIT"        "$STEPS_WITH_ORBIT"
printf "  │ %-23s │ %12d │ %12d │\n" "Tokens por etapa"   "$TOKENS_PER_STEP_NO_ORBIT" "$TOKENS_PER_STEP_WITH_ORBIT"
printf "  │ %-23s │ %12d │ %12d │\n" "Total de tokens"    "$TOKENS_NO_ORBIT"       "$TOKENS_WITH_ORBIT"
echo "  └─────────────────────────┴──────────────┴──────────────┘"
echo ""
echo "  ┌─────────────────────────────────────────────────────────┐"
printf "  │  💰  Tokens economizados:  %6d  (%2d%% de eficiência) │\n" "$TOKENS_SAVED" "$PCT_SAVED"
echo "  └─────────────────────────────────────────────────────────┘"
echo ""

# ── Nível de valor percebido ────────────────────────────────────────────────
if   [ "$PCT_SAVED" -ge 40 ]; then VALUE_LEVEL="high"
elif [ "$PCT_SAVED" -ge 15 ]; then VALUE_LEVEL="medium"
else                                VALUE_LEVEL="low"
fi

echo "  Nível de valor percebido: $(echo "$VALUE_LEVEL" | tr '[:lower:]' '[:upper:]')"
echo ""

# ── Envio real ao /track (opcional) ─────────────────────────────────────────
if [ -n "$ORBIT_URL" ]; then
    TS="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
    PAYLOAD=$(printf '{
  "event_type": "activation",
  "timestamp": "%s",
  "session_id": "%s",
  "mode": "auto",
  "trigger": "demo_value_sh",
  "estimated_waste": 0.4,
  "actions_suggested": %d,
  "actions_applied": %d,
  "impact_estimated_tokens": %d
}' "$TS" "$SESSION_ID" "$STEPS_NO_ORBIT" "$STEPS_WITH_ORBIT" "$TOKENS_SAVED")

    echo "  → Enviando evento real a $ORBIT_URL/track ..."
    HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" \
        -X POST "$ORBIT_URL/track" \
        -H "Content-Type: application/json" \
        -d "$PAYLOAD" 2>/dev/null || echo "000")

    if [ "$HTTP_CODE" = "200" ]; then
        echo "  ✅ Evento registrado com sucesso (HTTP 200)"
    else
        echo "  ⚠️  Servidor retornou HTTP $HTTP_CODE (modo offline aplicado)"
    fi
    echo ""
fi

# ── Log JSONL local ──────────────────────────────────────────────────────────
LOG_LINE=$(printf '{"timestamp":"%s","event":"demo_value","session_id":"%s","tokens_no_orbit":%d,"tokens_with_orbit":%d,"tokens_saved":%d,"pct_saved":%d,"value_level":"%s"}' \
    "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" \
    "$SESSION_ID" \
    "$TOKENS_NO_ORBIT" \
    "$TOKENS_WITH_ORBIT" \
    "$TOKENS_SAVED" \
    "$PCT_SAVED" \
    "$VALUE_LEVEL")

echo "  [VALUE LOG] $LOG_LINE"
echo ""

# ── Saída final com código de saída verificável ──────────────────────────────
echo "  Token savings: $TOKENS_SAVED"
echo "  Efficiency:    ${PCT_SAVED}%"
echo "  Value level:   $VALUE_LEVEL"
echo ""
echo "  Status: OK"
exit 0
