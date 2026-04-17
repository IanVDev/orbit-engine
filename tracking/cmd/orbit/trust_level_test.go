package main

import "testing"

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
