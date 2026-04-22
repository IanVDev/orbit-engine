#!/usr/bin/env bash
# tests/test_no_user_writes.sh — invariante #1 do Orbit (seção 7 do plano):
#
#   "Orbit não altera código do usuário."
#
# Estratégia: AllowList explícita de arquivos Go que podem invocar APIs
# de escrita em disco. Qualquer arquivo NOVO que invoque os.WriteFile,
# ioutil.WriteFile, os.Create ou os.OpenFile falha o teste — forçando
# revisão deliberada do destino antes de virar regressão silenciosa.
#
# Os arquivos da AllowList já foram auditados: todos escrevem
# exclusivamente sob $ORBIT_HOME (logs, snapshots, context-pack,
# marker de primeira execução) ou sob o caminho do binário orbit
# durante um update opt-in.
#
# Adicionar um arquivo à AllowList exige PR explícita justificando o
# destino; nunca silenciosamente.
#
# Uso:
#   bash tests/test_no_user_writes.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

# AllowList de arquivos auditados que podem chamar APIs de escrita.
# Caminhos relativos ao REPO_ROOT.
ALLOWLIST=(
  "tracking/cmd/orbit/logstore.go"      # escreve $ORBIT_HOME/logs/<ts>_<sid>_exit<N>.json
  "tracking/cmd/orbit/snapshot.go"      # escreve $ORBIT_HOME/snapshots/<sid>.json
  "tracking/cmd/orbit/context_pack.go"  # escreve $ORBIT_HOME/context-pack e state
  "tracking/cmd/orbit/run.go"           # escreve marker de primeira execução em $ORBIT_HOME
  "tracking/cmd/orbit/update.go"        # baixa novo binário (opt-in via `orbit update`)
  "tracking/anchor.go"                  # I15: escreve <ORBIT_HOME>.anchor (path irmão, fora de ORBIT_HOME por design para detectar wipe)
  "tracking/history.go"                 # tracking-server: append em $ORBIT_HOME
  "tracking/store.go"                   # tracking-server: append em $ORBIT_HOME
)

# Padrão de detecção: APIs Go que efetivamente criam/escrevem arquivos.
# Inclui os.WriteFile, ioutil.WriteFile (legado), os.Create (sem CreateTemp,
# que vai para os.TempDir() — fora do projeto do usuário) e os.OpenFile
# com flags de escrita.
WRITE_PATTERN='os\.WriteFile|ioutil\.WriteFile|\bos\.Create\(|os\.OpenFile'

# Coleta todos os arquivos Go não-teste com chamadas de escrita.
mapfile -t OFFENDERS < <(
  grep -rlE "$WRITE_PATTERN" \
       --include='*.go' --exclude='*_test.go' \
       tracking/ 2>/dev/null | sort -u
)

# Compara contra a AllowList.
in_allowlist() {
  local f="$1"
  local entry
  for entry in "${ALLOWLIST[@]}"; do
    [[ "$f" == "$entry" ]] && return 0
  done
  return 1
}

VIOLATIONS=0
for file in "${OFFENDERS[@]}"; do
  if ! in_allowlist "$file"; then
    echo "FAIL: $file usa API de escrita mas NÃO está na AllowList." >&2
    echo "      Linhas suspeitas:" >&2
    grep -nE "$WRITE_PATTERN" "$file" | sed 's/^/        /' >&2
    VIOLATIONS=$((VIOLATIONS + 1))
  fi
done

if [[ $VIOLATIONS -gt 0 ]]; then
  echo "" >&2
  echo "Total de violações: $VIOLATIONS" >&2
  echo "" >&2
  echo "Se a escrita é legítima (sob \$ORBIT_HOME), adicione o arquivo à" >&2
  echo "AllowList em tests/test_no_user_writes.sh com comentário do destino." >&2
  echo "Caso contrário, refatore para não escrever fora de \$ORBIT_HOME." >&2
  exit 1
fi

echo "PASS: nenhum arquivo Go escreve fora da AllowList auditada (${#ALLOWLIST[@]} arquivos permitidos)."
