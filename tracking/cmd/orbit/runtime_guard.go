// runtime_guard.go — verificação leve de dual install path no início dos
// comandos runtime (run, stats).
//
// Comportamento por severidade:
//
//	sevOK       — único caminho instalado → exibe "binário ativo" em stderr (TTY)
//	sevWarning  — dual-path sem strict    → aviso em stderr (TTY), sem abortar
//	sevCritical — dual-path + strict=1   → retorna error (caller: exit 1)
//
// ORBIT_STRICT_PATH=1 converte o aviso de dual-path em falha imediata.
// Comportamento padrão (strict=false) continua sendo apenas WARNING.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// enforceRuntimePathIntegrity é o ponto de entrada chamado pelos comandos
// runtime. Encapsula leitura de env/filesystem e TTY check; delega a lógica
// para runtimePathGuardAt (testável sem I/O real).
func enforceRuntimePathIntegrity() error {
	home, _ := os.UserHomeDir()
	userPath := filepath.Join(home, ".orbit", "bin", "orbit")
	active, _ := exec.LookPath("orbit")
	strict := os.Getenv("ORBIT_STRICT_PATH") == "1"

	w := io.Writer(io.Discard)
	if stderrIsTTY() {
		w = os.Stderr
	}
	return runtimePathGuardAt(w, userPath, expectedInstallPath, active, strict)
}

// runtimePathGuardAt é a forma pura/testável. Escreve mensagens em w;
// o caller decide se w é stderr real ou io.Discard.
//
//	userPath — caminho canônico (~/.orbit/bin/orbit)
//	sysPath  — caminho alternativo (/usr/local/bin/orbit)
//	active   — binário resolvido pelo PATH (exec.LookPath)
//	strict   — ORBIT_STRICT_PATH=1 → CRITICAL em vez de WARNING
func runtimePathGuardAt(w io.Writer, userPath, sysPath, active string, strict bool) error {
	res := &doctorResult{currentBinary: active}
	checkDualInstallPathsAt(res, userPath, sysPath, active, strict)

	if len(res.checks) == 0 {
		return nil // nenhum path instalado — coberto por orbit doctor
	}
	c := res.checks[0]
	switch c.severity {
	case sevOK:
		fmt.Fprintf(w, "orbit: binário ativo — %s\n", active)
	case sevWarning:
		fmt.Fprintf(w, "⚠️  orbit: %s\n", c.detail)
		fmt.Fprintf(w, "   sugestão: %s\n", c.fixHint)
	case sevCritical:
		return fmt.Errorf(
			"dual install path detectado (ORBIT_STRICT_PATH=1):\n   %s\n   fix: %s",
			c.detail, c.fixHint,
		)
	}
	return nil
}
