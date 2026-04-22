#!/usr/bin/env bash
# Testa o shell-init gerado por scripts/orbit_invisible.sh.
#
# Cobertura:
#   1. orbit_wrap propaga exit code do comando
#   2. ORBIT_INVISIBLE_OFF=1 ativa passthrough
#   3. Ausência do binário orbit → passthrough (fail-open)
#   4. Install → status detecta; uninstall limpa shell-init e rc
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
TARGET="$SCRIPT_DIR/scripts/orbit_invisible.sh"

fail() { printf 'FAIL: %s\n' "$1" >&2; exit 1; }
pass() { printf 'PASS: %s\n' "$1"; }

# ──────────────────────────────────────────────────────────────────────────
# Sandbox isolado: HOME temporário para não tocar no shell real do usuário.
# ──────────────────────────────────────────────────────────────────────────
SANDBOX="$(mktemp -d)"
trap 'rm -rf "$SANDBOX"' EXIT

export HOME="$SANDBOX"
export ORBIT_HOME="$SANDBOX/.orbit"
export SHELL="/bin/bash"

# ──────────────────────────────────────────────────────────────────────────
# Teste 4: install cria shell-init + injeta no rc; uninstall limpa.
# ──────────────────────────────────────────────────────────────────────────
"$TARGET" install >/dev/null
[ -f "$ORBIT_HOME/shell-init.sh" ] || fail "install não criou shell-init.sh"
grep -qF "orbit-invisible begin" "$HOME/.bashrc" || fail "install não injetou marcador no .bashrc"
pass "install gera shell-init e injeta rc"

"$TARGET" install >/dev/null
# segundo install deve ser idempotente (não duplicar bloco)
count=$(grep -cF "orbit-invisible begin" "$HOME/.bashrc")
[ "$count" = "1" ] || fail "install duplicou bloco (count=$count)"
pass "install é idempotente"

"$TARGET" uninstall >/dev/null
[ ! -f "$ORBIT_HOME/shell-init.sh" ] || fail "uninstall não removeu shell-init"
grep -qF "orbit-invisible begin" "$HOME/.bashrc" && fail "uninstall não removeu marcador" || true
pass "uninstall limpa shell-init e rc"

# ──────────────────────────────────────────────────────────────────────────
# Prepara shell-init standalone para testar orbit_wrap.
# ──────────────────────────────────────────────────────────────────────────
INIT="$SANDBOX/init.sh"
"$TARGET" print > "$INIT"

# Teste 1: exit code é propagado.
rc=0
bash -c "source '$INIT'; orbit_wrap false" && rc=0 || rc=$?
[ "$rc" = "1" ] || fail "orbit_wrap não propagou exit 1 (rc=$rc)"
pass "orbit_wrap propaga exit code"

# Teste 2: ORBIT_INVISIBLE_OFF=1 → passthrough mesmo com orbit no PATH.
FAKE_PATH="$SANDBOX/fakebin"
mkdir -p "$FAKE_PATH"
cat > "$FAKE_PATH/orbit" <<'EOF'
#!/usr/bin/env bash
# orbit fake que SEMPRE falha — se for chamado, o teste quebra.
echo "ORBIT_FAKE_CALLED" >&2
exit 99
EOF
chmod +x "$FAKE_PATH/orbit"

out=$(PATH="$FAKE_PATH:$PATH" ORBIT_INVISIBLE_OFF=1 bash -c "source '$INIT'; orbit_wrap echo hello" 2>&1)
[ "$out" = "hello" ] || fail "ORBIT_INVISIBLE_OFF não ativou passthrough (out='$out')"
pass "ORBIT_INVISIBLE_OFF ativa passthrough"

# Teste 3: sem orbit no PATH → passthrough.
EMPTY_PATH="/usr/bin:/bin"  # sem diretórios com orbit
out=$(PATH="$EMPTY_PATH" bash -c "source '$INIT'; orbit_wrap echo world" 2>&1)
[ "$out" = "world" ] || fail "ausência de orbit não fez passthrough (out='$out')"
pass "fail-open quando orbit ausente"

# Teste 5: com orbit presente, orbit_wrap delega (e propaga exit do orbit).
rc=0
PATH="$FAKE_PATH:$PATH" bash -c "source '$INIT'; orbit_wrap echo x" >/dev/null 2>&1 && rc=0 || rc=$?
[ "$rc" = "99" ] || fail "orbit_wrap não delegou ao binário orbit (rc=$rc, esperado 99)"
pass "orbit_wrap delega para orbit run quando presente"

printf '\nAll tests passed.\n'
