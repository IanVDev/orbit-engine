#!/bin/bash
#
# import-grafana-dashboards.sh
#
# Importa os dashboards JSON do orbit-engine para uma instância Grafana
# via Grafana HTTP API.
#
# Requisitos:
#   - curl
#   - jq (para processar JSON)
#   - GRAFANA_URL e GRAFANA_TOKEN definidas via env ou argumentos
#
# Uso:
#   GRAFANA_URL=http://localhost:3000 GRAFANA_TOKEN=<token> bash scripts/import-grafana-dashboards.sh
#   ou
#   bash scripts/import-grafana-dashboards.sh http://localhost:3000 <token>
#
# Environment:
#   GRAFANA_URL      — URL base (padrão: http://localhost:3000)
#   GRAFANA_TOKEN    — API token com permissão admin (obrigatório)
#   VERBOSE          — se definido, mostra logs detalhados

set -euo pipefail

# ──────────────────────────────────────────────────────────────────────────────
# Parse arguments
# ──────────────────────────────────────────────────────────────────────────────

GRAFANA_URL="${1:-${GRAFANA_URL:-http://localhost:3000}}"
GRAFANA_TOKEN="${2:-${GRAFANA_TOKEN}}"
VERBOSE="${VERBOSE:-}"

if [[ -z "$GRAFANA_TOKEN" ]]; then
    echo "❌ GRAFANA_TOKEN não definido"
    echo ""
    echo "Uso:"
    echo "  GRAFANA_URL=http://localhost:3000 GRAFANA_TOKEN=<token> bash scripts/import-grafana-dashboards.sh"
    echo ""
    echo "Para gerar um token no Grafana:"
    echo "  Configuration (gear icon) → API Tokens → Create token (role: Admin)"
    exit 1
fi

if ! command -v curl &> /dev/null; then
    echo "❌ curl não encontrado"
    exit 1
fi

if ! command -v jq &> /dev/null; then
    echo "⚠️  jq não instalado — tentando continuar..."
fi

# ──────────────────────────────────────────────────────────────────────────────
# Functions
# ──────────────────────────────────────────────────────────────────────────────

log() {
    echo "[$(date +'%H:%M:%S')] $*"
}

vlog() {
    [[ -n "$VERBOSE" ]] && log "$@"
}

error() {
    echo "❌ $*" >&2
}

success() {
    echo "✅ $*"
}

# ──────────────────────────────────────────────────────────────────────────────
# Verifica conectividade e auth com Grafana
# ──────────────────────────────────────────────────────────────────────────────

check_grafana() {
    log "Verificando conectividade com Grafana em $GRAFANA_URL..."
    
    response=$(curl -s -w "%{http_code}" -o /tmp/grafana-health.json \
        -H "Authorization: Bearer $GRAFANA_TOKEN" \
        "$GRAFANA_URL/api/health" 2>/dev/null || echo "000")
    
    http_code="${response: -3}"
    
    if [[ "$http_code" == "200" ]]; then
        success "Conectado ao Grafana v$(jq -r '.version' /tmp/grafana-health.json 2>/dev/null || echo '?')"
        return 0
    elif [[ "$http_code" == "401" ]]; then
        error "Autenticação falhou — GRAFANA_TOKEN inválido"
        return 1
    elif [[ "$http_code" == "000" ]]; then
        error "Não foi possível conectar a $GRAFANA_URL — verifique se Grafana está rodando"
        return 1
    else
        error "Resposta inesperada do Grafana (HTTP $http_code)"
        return 1
    fi
}

# ──────────────────────────────────────────────────────────────────────────────
# Importa um dashboard JSON
# ──────────────────────────────────────────────────────────────────────────────

import_dashboard() {
    local dashboard_file="$1"
    
    if [[ ! -f "$dashboard_file" ]]; then
        error "Arquivo não encontrado: $dashboard_file"
        return 1
    fi
    
    local dashboard_name=$(jq -r '.title' "$dashboard_file" 2>/dev/null || echo "Unknown")
    local dashboard_uid=$(jq -r '.uid' "$dashboard_file" 2>/dev/null || echo "unknown-uid")
    
    log "Importando dashboard: $dashboard_name (uid=$dashboard_uid)"
    
    # Envelope requerido pela API de import do Grafana
    local payload=$(jq -n \
        --argjson dashboard "$(cat "$dashboard_file")" \
        '{dashboard: $dashboard, overwrite: true, message: "Imported by import-grafana-dashboards.sh"}')
    
    vlog "Payload:"
    vlog "$(echo "$payload" | jq -c '.' | cut -c1-100)..."
    
    response=$(curl -s -w "%{http_code}" -o /tmp/import-response.json \
        -X POST \
        -H "Authorization: Bearer $GRAFANA_TOKEN" \
        -H "Content-Type: application/json" \
        -d "$payload" \
        "$GRAFANA_URL/api/dashboards/db" 2>/dev/null)
    
    http_code="${response: -3}"
    
    if [[ "$http_code" == "200" ]]; then
        local dashboard_id=$(jq -r '.id' /tmp/import-response.json 2>/dev/null)
        local dashboard_slug=$(jq -r '.slug' /tmp/import-response.json 2>/dev/null)
        success "Dashboard importado: ID=$dashboard_id ($dashboard_slug)"
        vlog "  URL: $GRAFANA_URL/d/$dashboard_slug"
        return 0
    else
        error "Falha ao importar (HTTP $http_code)"
        vlog "Resposta: $(cat /tmp/import-response.json | jq '.' 2>/dev/null || cat /tmp/import-response.json)"
        return 1
    fi
}

# ──────────────────────────────────────────────────────────────────────────────
# Main
# ──────────────────────────────────────────────────────────────────────────────

main() {
    log "Importador de Dashboards Grafana — orbit-engine"
    echo ""
    
    # Verifica conectividade
    if ! check_grafana; then
        exit 1
    fi
    
    echo ""
    
    # Lista de dashboards a importar
    local dashboards=(
        "deploy/grafana-dashboard-security.json"
        "deploy/grafana-dashboard.json"
    )
    
    local imported=0
    local failed=0
    
    for dashboard in "${dashboards[@]}"; do
        if [[ -f "$dashboard" ]]; then
            if import_dashboard "$dashboard"; then
                ((imported++))
            else
                ((failed++))
            fi
            echo ""
        else
            vlog "Pulando: $dashboard (não encontrado)"
        fi
    done
    
    # Resumo
    echo "────────────────────────────────────────────────────────────────────────"
    log "Resumo: $imported importados, $failed falharam"
    
    if [[ $failed -eq 0 ]]; then
        success "Todos os dashboards foram importados com sucesso!"
        echo ""
        echo "Acesse em: $GRAFANA_URL/dashboards"
        exit 0
    else
        error "$failed dashboard(s) falharam"
        exit 1
    fi
}

main "$@"
