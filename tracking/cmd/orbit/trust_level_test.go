package main

import (
	"testing"
	"time"
)

// TestTrustLevel_ThreeStates é o anti-regressão do contrato de três
// rótulos. Exerce a função pura com a matriz mínima que cobre cada
// estado, incluindo interação entre skip-env e bypass-command.
func TestTrustLevel_ThreeStates(t *testing.T) {
	cases := []struct {
		name                      string
		verdictOK, skip, bypassed bool
		want                      TrustLevel
	}{
		{"clean env → TRUSTED", true, false, false, TrustTrusted},

		// ── DEGRADED: alguma forma de bypass tolerável.
		{"skip env only → DEGRADED", true, true, false, TrustDegraded},
		{"bypass command only → DEGRADED", true, false, true, TrustDegraded},
		{"inconsistency + bypass cmd → DEGRADED", false, false, true, TrustDegraded},
		{"inconsistency + skip env → DEGRADED", false, true, false, TrustDegraded},

		// ── BLOCKED: veredito ruim E nenhum bypass ativo.
		{"inconsistency + no bypass → BLOCKED", false, false, false, TrustBlocked},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeTrustLevel(tc.verdictOK, tc.skip, tc.bypassed)
			if got != tc.want {
				t.Errorf("got %s, want %s", got, tc.want)
			}
		})
	}

	// Propriedade bônus: String() devolve rótulos estáveis (contrato
	// externo — se alguém renomear quebra telemetria/UX).
	if TrustTrusted.String() != "TRUSTED" ||
		TrustDegraded.String() != "DEGRADED" ||
		TrustBlocked.String() != "BLOCKED" {
		t.Error("rótulos externos de TrustLevel mudaram — quebra contrato")
	}
}

// TestCurrentTrustLevel_NoSubprocessForVersion: anti-regressão do bug onde
// `orbit doctor --deep` reportava CRITICAL "timeout após 3s" porque
// currentTrustLevel("version") chamava queryVersionCommit, que spawnava
// `orbit version` em subprocess, que re-entrava em currentTrustLevel("version"),
// que spawnava outro subprocess... recursão exponencial.
//
// Fix: fast-return TrustTrusted para "version"/"--version"/"-v" no início
// de currentTrustLevel, antes de qualquer I/O.
//
// O teste prova a propriedade comportamental por proxy: tempo. Se o
// fast-path for removido, `currentTrustLevel("version")` invocará
// findAllOrbitsInPath + firstInPath + queryVersionCommit (exec.Command com
// timeout 3s). Mesmo no melhor caso (sem orbit no PATH), a varredura de
// PATH custa muito mais que os 50ms abaixo. Com fast-path: <1ms.
func TestCurrentTrustLevel_NoSubprocessForVersion(t *testing.T) {
	for _, sub := range []string{"version", "--version", "-v"} {
		t.Run(sub, func(t *testing.T) {
			start := time.Now()
			got := currentTrustLevel(sub)
			elapsed := time.Since(start)

			if got != TrustTrusted {
				t.Errorf("currentTrustLevel(%q) = %s; want TRUSTED (fast-path)", sub, got)
			}
			if elapsed > 50*time.Millisecond {
				t.Errorf("currentTrustLevel(%q) levou %v (fast-path quebrado — provavelmente está chamando queryVersionCommit)", sub, elapsed)
			}
		})
	}
}
