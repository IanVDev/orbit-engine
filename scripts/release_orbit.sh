#!/usr/bin/env bash
# scripts/release_orbit.sh — Ritual soberano de release do orbit-engine.
#
# Este script é o ÚNICO ponto de entrada autorizado para publicar uma release.
# Encadeia todos os gates em ordem fixa e aborta na primeira falha. Nada
# neste script é opcional em produção — as únicas flags que desligam etapas
# são para reexecução defensiva (ex: gate já rodado em outro terminal).
#
# Uso:
#   ./scripts/release_orbit.sh v1.0.0                    # release real
#   ./scripts/release_orbit.sh --dry-run v1.0.0          # simula (zero push)
#   ./scripts/release_orbit.sh --smoke-gate v1.0.0       # gate smoke (1h)
#   ./scripts/release_orbit.sh --skip-gate v1.0.0        # pula gate (já rodado)
#   ./scripts/release_orbit.sh --force-smoke v1.0.0      # smoke do binário linux via Docker
#   ./scripts/release_orbit.sh --yes v1.0.0              # sem prompt interativo
#
# Etapas (ordem obrigatória):
#   1. Validar argumento VERSION (regex v<M>.<m>.<p>)
#   2. Validar working tree limpo
#   3. Validar branch = main e sincronizado com origin
#   4. Validar que a tag ainda não existe (local + remote)
#   5. Rodar prelaunch_gate.sh (ou --smoke-gate)
#   6. Rodar go test ./... -race -count=1 (tracking + cmd/orbit)
#   7. Scrapar /metrics e validar contrato de 4 product-counters
#   8. Confirmação interativa (a menos que --yes)
#   9. git tag -a <VERSION> + git push origin <VERSION>   [MUTAÇÃO]
#  10. gh run watch (aguarda release.yml concluir)
#  11. Validar 8 assets no GitHub Release
#  12. Baixar binário linux-amd64 + sha256 + `orbit version`
#  13. Veredito final: 🟢 RELEASE OK  ou  🔴 RELEASE ABORTED em etapa N
#
# Fail-closed: set -euo pipefail + trap ERR.
#
# Requisitos:
#   - bash 4+, git, curl, go (1.21+), gh CLI autenticado
#   - tracking server rodando em TRACKING_HOST (default 127.0.0.1:9100)
#   - acesso de push ao remote origin
#
# Variáveis de ambiente (honradas):
#   TRACKING_HOST       host:porta do tracking server (default 127.0.0.1:9100)
#   GITHUB_REPO         owner/repo (default IanVDev/orbit-engine)
#   DRY_RUN             1 para simular (equivalente a --dry-run)
#   ASSUME_YES          1 para pular confirmação (equivalente a --yes)

set -euo pipefail

# ── Globais ─────────────────────────────────────────────────────────────────

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

TRACKING_HOST="${TRACKING_HOST:-127.0.0.1:9100}"
GITHUB_REPO="${GITHUB_REPO:-IanVDev/orbit-engine}"
DRY_RUN="${DRY_RUN:-0}"
ASSUME_YES="${ASSUME_YES:-0}"
SKIP_GATE=0
SMOKE_GATE=0
FORCE_SMOKE=0
VERSION=""

LOG_FILE="${REPO_ROOT}/release_orbit.log"
CURRENT_STEP=0
CURRENT_STEP_NAME=""

GREEN=$'\033[0;32m'
RED=$'\033[0;31m'
YELLOW=$'\033[1;33m'
CYAN=$'\033[0;36m'
BOLD=$'\033[1m'
NC=$'\033[0m'

# ── Helpers ─────────────────────────────────────────────────────────────────

log() {
  local level="$1"; shift
  local msg="$*"
  local ts; ts="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
  printf '[%s] [%s] %s\n' "${ts}" "${level}" "${msg}" >> "${LOG_FILE}"
}

step() {
  CURRENT_STEP=$((CURRENT_STEP + 1))
  CURRENT_STEP_NAME="$1"
  echo
  printf '%s── STEP %d — %s%s\n' "${BOLD}" "${CURRENT_STEP}" "${CURRENT_STEP_NAME}" "${NC}"
  log STEP "${CURRENT_STEP} ${CURRENT_STEP_NAME}"
}

ok() {
  printf '  %s✅ OK%s — %s\n' "${GREEN}" "${NC}" "$1"
  log OK "${CURRENT_STEP_NAME}: $1"
}

warn() {
  printf '  %s⚠️  WARN%s — %s\n' "${YELLOW}" "${NC}" "$1"
  log WARN "${CURRENT_STEP_NAME}: $1"
}

fail() {
  printf '\n  %s❌ FAIL%s — %s\n' "${RED}" "${NC}" "$1"
  log FAIL "${CURRENT_STEP_NAME}: $1"
  printf '\n%s🔴 RELEASE ABORTED em STEP %d (%s)%s\n' "${RED}${BOLD}" "${CURRENT_STEP}" "${CURRENT_STEP_NAME}" "${NC}"
  printf '   Log: %s\n\n' "${LOG_FILE}"
  exit 1
}

dryrun_skip() {
  printf '  %s⏩ DRY-RUN%s — pulando: %s\n' "${CYAN}" "${NC}" "$1"
  log DRY_SKIP "${CURRENT_STEP_NAME}: $1"
}

run_step() {
  # run_step "description" cmd [args...]
  local desc="$1"; shift
  if [[ "${DRY_RUN}" == "1" ]]; then
    dryrun_skip "${desc} (cmd: $*)"
    return 0
  fi
  log CMD "${desc}: $*"
  if "$@"; then
    ok "${desc}"
  else
    fail "${desc} — comando retornou exit ≠ 0"
  fi
}

assert_contains() {
  # assert_contains "haystack" "needle" "msg"
  local haystack="$1"
  local needle="$2"
  local msg="${3:-assert_contains}"
  if [[ "${haystack}" == *"${needle}"* ]]; then
    ok "${msg}"
  else
    fail "${msg} — string não contém: ${needle}"
  fi
}

confirm() {
  local prompt="$1"
  if [[ "${ASSUME_YES}" == "1" || "${DRY_RUN}" == "1" ]]; then
    log CONFIRM "auto-yes (ASSUME_YES=${ASSUME_YES} DRY_RUN=${DRY_RUN})"
    return 0
  fi
  local reply=""
  printf '\n  %s%s%s [y/N]: ' "${BOLD}" "${prompt}" "${NC}"
  read -r reply
  if [[ "${reply}" != "y" && "${reply}" != "Y" ]]; then
    log CONFIRM "user declined"
    fail "usuário não confirmou — release abortado por escolha"
  fi
  log CONFIRM "user confirmed"
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "comando obrigatório não encontrado: $1"
}

# trap qualquer erro não tratado
on_err() {
  local code=$?
  printf '\n%s🔴 RELEASE ABORTED — trap ERR em STEP %d (%s), exit=%d%s\n' \
    "${RED}${BOLD}" "${CURRENT_STEP}" "${CURRENT_STEP_NAME}" "${code}" "${NC}"
  log TRAP "exit=${code} step=${CURRENT_STEP} name=${CURRENT_STEP_NAME}"
  exit "${code}"
}
trap on_err ERR

# ── Parse de argumentos ─────────────────────────────────────────────────────

usage() {
  cat <<EOF
uso: $0 [FLAGS] VERSION

FLAGS:
  --dry-run         simula release sem criar tag ou push
  --smoke-gate      roda prelaunch_gate.sh --smoke (1h) em vez do completo
  --skip-gate       pula prelaunch_gate (já rodado manualmente antes)
  --force-smoke     após download, executa 'orbit quickstart' no container Docker linux/amd64
  --yes             sem confirmação interativa
  -h, --help        ajuda

VERSION: v<major>.<minor>.<patch>  (ex: v1.0.0)
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)    DRY_RUN=1; shift ;;
    --smoke-gate)  SMOKE_GATE=1; shift ;;
    --skip-gate)   SKIP_GATE=1; shift ;;
    --force-smoke) FORCE_SMOKE=1; shift ;;
    --yes)         ASSUME_YES=1; shift ;;
    -h|--help)    usage; exit 0 ;;
    v*)           VERSION="$1"; shift ;;
    *)            usage; echo "argumento desconhecido: $1"; exit 2 ;;
  esac
done

# reset log
: > "${LOG_FILE}"
log START "release_orbit.sh version=${VERSION} dry_run=${DRY_RUN} smoke_gate=${SMOKE_GATE} skip_gate=${SKIP_GATE} force_smoke=${FORCE_SMOKE}"

printf '%s%s════════════════════════════════════════════════════════%s\n' "${CYAN}" "${BOLD}" "${NC}"
printf '%s%s  orbit-engine — RELEASE RITUAL%s\n' "${CYAN}" "${BOLD}" "${NC}"
printf '%s%s════════════════════════════════════════════════════════%s\n' "${CYAN}" "${BOLD}" "${NC}"
printf '  VERSION     : %s\n' "${VERSION:-<faltando>}"
printf '  DRY_RUN     : %s\n' "${DRY_RUN}"
printf '  SMOKE_GATE  : %s\n' "${SMOKE_GATE}"
printf '  SKIP_GATE   : %s\n' "${SKIP_GATE}"
printf '  FORCE_SMOKE : %s\n' "${FORCE_SMOKE}"
printf '  REPO        : %s\n' "${GITHUB_REPO}"
printf '  LOG         : %s\n' "${LOG_FILE}"

# ── STEP 1 — Validar VERSION ────────────────────────────────────────────────

step "Validar argumento VERSION"
[[ -n "${VERSION}" ]] || fail "VERSION não informada. Uso: $0 [flags] v<M>.<m>.<p>"
if [[ "${VERSION}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[a-z0-9]+)?$ ]]; then
  ok "formato válido: ${VERSION}"
else
  fail "formato inválido: ${VERSION} (esperado: v<M>.<m>.<p> ou v<M>.<m>.<p>-<rc>)"
fi

# ── STEP 2 — Dependências ───────────────────────────────────────────────────

step "Validar dependências no PATH"
for cmd in git curl go gh python3; do require_cmd "${cmd}"; done
[[ "${FORCE_SMOKE}" != "1" ]] || require_cmd docker
ok "git, curl, go, gh, python3 presentes"

# ── STEP 3 — Working tree limpo ─────────────────────────────────────────────

step "Validar working tree limpo"
cd "${REPO_ROOT}"
if [[ -n "$(git status --porcelain)" ]]; then
  git status --short
  fail "working tree sujo — commit ou stash antes de lançar"
fi
ok "working tree limpo"

# ── STEP 4 — Branch = main + sync com origin ────────────────────────────────

step "Validar branch = main e sincronia com origin"
branch="$(git rev-parse --abbrev-ref HEAD)"
[[ "${branch}" == "main" ]] || fail "branch atual é '${branch}', deve ser 'main'"
ok "branch = main"

run_step "git fetch origin (tags + refs)" git fetch --tags origin

local_sha="$(git rev-parse HEAD)"
remote_sha="$(git rev-parse origin/main)"
if [[ "${local_sha}" != "${remote_sha}" ]]; then
  fail "HEAD local (${local_sha:0:7}) != origin/main (${remote_sha:0:7}) — faça pull/push antes"
fi
ok "sincronizado com origin/main em ${local_sha:0:7}"

# ── STEP 5 — Tag ainda não existe ───────────────────────────────────────────

step "Validar que a tag ${VERSION} não existe"
if git rev-parse "refs/tags/${VERSION}" >/dev/null 2>&1; then
  fail "tag ${VERSION} já existe localmente"
fi
if git ls-remote --tags origin "refs/tags/${VERSION}" | grep -q "${VERSION}"; then
  fail "tag ${VERSION} já existe no remote origin"
fi
ok "tag ${VERSION} disponível"

# ── STEP 6 — prelaunch_gate.sh ──────────────────────────────────────────────

step "Executar prelaunch_gate.sh"
GATE_SCRIPT="${SCRIPT_DIR}/prelaunch_gate.sh"
[[ -x "${GATE_SCRIPT}" ]] || fail "prelaunch_gate.sh ausente ou sem +x: ${GATE_SCRIPT}"

if [[ "${SKIP_GATE}" == "1" ]]; then
  warn "--skip-gate ativado: assumindo que o gate foi rodado em outro terminal"
  if [[ ! -f "${REPO_ROOT}/prelaunch_gate.log" ]]; then
    fail "--skip-gate exige prelaunch_gate.log existente como evidência"
  fi
  if ! grep -q '\[VERDICT\] GO' "${REPO_ROOT}/prelaunch_gate.log"; then
    fail "prelaunch_gate.log não contém [VERDICT] GO — gate não passou"
  fi
  ok "gate validado via log prévio ([VERDICT] GO encontrado)"
else
  gate_args=()
  [[ "${SMOKE_GATE}" == "1" ]] && gate_args+=("--smoke")
  run_step "prelaunch_gate.sh ${gate_args[*]:-completo}" "${GATE_SCRIPT}" "${gate_args[@]}"
fi

# ── STEP 7 — go test -race ──────────────────────────────────────────────────

step "go test ./... -race -count=1 em tracking/"
if [[ "${DRY_RUN}" == "1" ]]; then
  dryrun_skip "go test -race"
else
  cd "${REPO_ROOT}/tracking"
  if ! go test ./... -race -count=1 >"${REPO_ROOT}/release_orbit.gotest.log" 2>&1; then
    tail -40 "${REPO_ROOT}/release_orbit.gotest.log"
    fail "go test -race falhou — ver release_orbit.gotest.log"
  fi
  ok "go test -race passou"
  cd "${REPO_ROOT}"
fi

# ── STEP 8 — Contrato /metrics (4 product counters) ─────────────────────────

step "Validar contrato /metrics (4 product counters)"
if [[ "${DRY_RUN}" == "1" ]]; then
  dryrun_skip "curl ${TRACKING_HOST}/metrics"
else
  metrics_body="$(curl -sf --max-time 5 "http://${TRACKING_HOST}/metrics" 2>/dev/null)" \
    || fail "tracking server não responde em http://${TRACKING_HOST}/metrics"
  for m in \
    orbit_proofs_generated_total \
    orbit_quickstart_completed_total \
    orbit_verify_success_total \
    orbit_verify_failure_total ; do
    # Extrai valor da linha "metric_name <float>" no formato Prometheus text.
    val="$(echo "${metrics_body}" | awk -v m="${m}" 'NF==2 && $1==m {print $2}')"
    [[ -n "${val}" ]] || fail "métrica ${m} não encontrada em /metrics (servidor sem atividade?)"
    if ! python3 -c "import sys; sys.exit(0 if float('${val}') > 0 else 1)" 2>/dev/null; then
      fail "métrica ${m} = ${val} (esperado > 0 — rode 'orbit quickstart' antes do release)"
    fi
    ok "métrica ${m} = ${val}"
  done
fi

# ── STEP 9 — Confirmação interativa ─────────────────────────────────────────

step "Confirmação humana antes de publicar"
confirm "Publicar release ${VERSION} no GitHub (${GITHUB_REPO})?"
ok "confirmação recebida"

# ── STEP 10 — Criar tag e push ──────────────────────────────────────────────

step "Criar tag ${VERSION} e push para origin [MUTAÇÃO]"
if [[ "${DRY_RUN}" == "1" ]]; then
  dryrun_skip "git tag -a ${VERSION} && git push origin ${VERSION}"
else
  run_step "git tag -a ${VERSION}" git tag -a "${VERSION}" -m "${VERSION}"
  run_step "git push origin ${VERSION}" git push origin "${VERSION}"
fi

# ── STEP 11 — Aguardar workflow release.yml ─────────────────────────────────

step "Aguardar workflow release.yml concluir"
if [[ "${DRY_RUN}" == "1" ]]; then
  dryrun_skip "gh run watch --repo ${GITHUB_REPO}"
else
  # Pegar o run_id mais recente disparado pela tag
  sleep 5  # tempo para o GitHub registrar o trigger
  run_id="$(gh run list --workflow=release.yml --repo "${GITHUB_REPO}" \
              --limit 1 --json databaseId -q '.[0].databaseId' 2>/dev/null || echo "")"
  [[ -n "${run_id}" ]] || fail "não achei run recente de release.yml"
  ok "acompanhando run_id=${run_id}"
  if ! timeout 300 gh run watch "${run_id}" --repo "${GITHUB_REPO}" --exit-status; then
    fail "workflow release.yml falhou ou excedeu timeout 300s (run ${run_id})"
  fi
  ok "workflow concluído com sucesso"
fi

# ── STEP 12 — Validar 8 assets no release ───────────────────────────────────

step "Validar 8 assets no GitHub Release"
if [[ "${DRY_RUN}" == "1" ]]; then
  dryrun_skip "gh release view ${VERSION}"
else
  expected_assets=(
    "orbit-${VERSION}-linux-amd64"    "orbit-${VERSION}-linux-amd64.sha256"
    "orbit-${VERSION}-linux-arm64"    "orbit-${VERSION}-linux-arm64.sha256"
    "orbit-${VERSION}-darwin-amd64"   "orbit-${VERSION}-darwin-amd64.sha256"
    "orbit-${VERSION}-darwin-arm64"   "orbit-${VERSION}-darwin-arm64.sha256"
  )
  release_assets="$(gh release view "${VERSION}" --repo "${GITHUB_REPO}" \
                      --json assets -q '.assets[].name' 2>/dev/null)" \
    || fail "gh release view ${VERSION} falhou"
  missing=0
  for a in "${expected_assets[@]}"; do
    if echo "${release_assets}" | grep -qx "${a}"; then
      ok "asset presente: ${a}"
    else
      warn "asset ausente: ${a}"
      missing=$((missing + 1))
    fi
  done
  [[ "${missing}" -eq 0 ]] || fail "${missing} asset(s) ausente(s) no release"
fi

# ── STEP 13 — Smoke do binário publicado (linux-amd64) ──────────────────────

step "Smoke do binário publicado (linux-amd64)"
if [[ "${DRY_RUN}" == "1" ]]; then
  dryrun_skip "download + sha256 + orbit version"
else
  tmpdir="$(mktemp -d)"
  trap 'rm -rf "${tmpdir}"' EXIT
  bin_name="orbit-${VERSION}-linux-amd64"
  cd "${tmpdir}"
  run_step "download ${bin_name}" gh release download "${VERSION}" --repo "${GITHUB_REPO}" \
    --pattern "${bin_name}" --pattern "${bin_name}.sha256"

  # SHA256: varia entre macOS (shasum) e linux (sha256sum)
  if command -v sha256sum >/dev/null 2>&1; then
    run_step "verificar sha256 (sha256sum)" sha256sum -c "${bin_name}.sha256"
  elif command -v shasum >/dev/null 2>&1; then
    expected_hash="$(awk '{print $1}' "${bin_name}.sha256")"
    actual_hash="$(shasum -a 256 "${bin_name}" | awk '{print $1}')"
    if [[ "${expected_hash}" == "${actual_hash}" ]]; then
      ok "sha256 OK (${actual_hash:0:16}...)"
    else
      fail "sha256 mismatch: esperado=${expected_hash:0:16}... obtido=${actual_hash:0:16}..."
    fi
  else
    fail "nem sha256sum nem shasum disponíveis"
  fi

  # Só executamos o binário se a plataforma bater (linux-amd64)
  host_os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  host_arch="$(uname -m)"
  [[ "${host_arch}" == "x86_64" ]] && host_arch="amd64"
  chmod +x "${bin_name}"
  if [[ "${host_os}" == "linux" && "${host_arch}" == "amd64" ]]; then
    version_out="$(ORBIT_SKIP_GUARD=1 "./${bin_name}" version 2>&1)" \
      || fail "binário não executou: ${version_out}"
    assert_contains "${version_out}" "${VERSION}" "binário reporta versão ${VERSION}"
    assert_contains "${version_out}" "commit=" "binário reporta commit"
  else
    warn "host ${host_os}/${host_arch} ≠ linux/amd64 — smoke nativo pulado (sha256 já validado)"
  fi

  if [[ "${FORCE_SMOKE}" == "1" ]]; then
    ok "iniciando --force-smoke: orbit quickstart no container Docker linux/amd64"
    smoke_out="$(docker run --rm --platform linux/amd64 \
      -v "$(pwd)/${bin_name}:/orbit:ro" \
      ubuntu:22.04 \
      /orbit quickstart 2>&1)" \
      || fail "--force-smoke: orbit quickstart falhou no container (exit ≠ 0)\nOutput:\n${smoke_out}"
    assert_contains "${smoke_out}" "Quickstart concluído" \
      "--force-smoke: container emitiu 'Quickstart concluído'"
    ok "--force-smoke: orbit quickstart no container Docker OK"
  fi

  cd "${REPO_ROOT}"
fi

# ── Veredito final ──────────────────────────────────────────────────────────

echo
printf '%s%s════════════════════════════════════════════════════════%s\n' "${GREEN}" "${BOLD}" "${NC}"
if [[ "${DRY_RUN}" == "1" ]]; then
  printf '%s%s  🟡 DRY-RUN OK — release %s validado sem efeito%s\n' "${YELLOW}" "${BOLD}" "${VERSION}" "${NC}"
else
  printf '%s%s  🟢 RELEASE %s PUBLICADO%s\n' "${GREEN}" "${BOLD}" "${VERSION}" "${NC}"
fi
printf '%s%s════════════════════════════════════════════════════════%s\n' "${GREEN}" "${BOLD}" "${NC}"
echo
printf '  Log: %s\n' "${LOG_FILE}"
echo

log END "success version=${VERSION} dry_run=${DRY_RUN}"
exit 0
