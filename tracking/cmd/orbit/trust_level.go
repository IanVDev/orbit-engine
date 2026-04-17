// trust_level.go — sinalização leve de confiabilidade do ambiente.
//
// Deriva do mesmo estado usado por startup_guard.go (não duplica lógica,
// não altera comportamento). Apenas rotula o momento em 3 níveis:
//
//	TRUSTED   — veredito OK, sem bypasses: tudo verificável e consistente.
//	DEGRADED  — estamos rodando, mas sem garantia (bypass de subcomando
//	            diagnóstico, ORBIT_SKIP_GUARD=1, ou guarda inativa).
//	BLOCKED   — veredito inconsistente e a guarda fail-closed é aplicável;
//	            este estado só é observável em camadas externas: ao alcançar
//	            a impressão natural do banner, o log.Fatal já teria abortado.
//
// Integração: print opcional em stderr, e somente quando DEGRADED — para
// não poluir o fluxo limpo do usuário feliz (TRUSTED fica silencioso por
// design, consistente com a política de "silêncio é sinal" do analyze).
package main

import (
	"fmt"
	"os"
)

// TrustLevel é o rótulo externo exibido ao usuário.
type TrustLevel int

const (
	TrustTrusted TrustLevel = iota
	TrustDegraded
	TrustBlocked
)

func (l TrustLevel) String() string {
	switch l {
	case TrustTrusted:
		return "TRUSTED"
	case TrustDegraded:
		return "DEGRADED"
	case TrustBlocked:
		return "BLOCKED"
	}
	return "UNKNOWN"
}

// computeTrustLevel é a forma pura/testável. Nenhum I/O.
//
//	verdictOK        — resultado de evaluateStartupIntegrity().OK
//	skipGuardEnv     — ORBIT_SKIP_GUARD=1 (bypass explícito)
//	bypassedCommand  — subcomando está na lista de bypass (doctor, version, ...)
func computeTrustLevel(verdictOK, skipGuardEnv, bypassedCommand bool) TrustLevel {
	// Inconsistência detectada E a guarda *iria* abortar (nenhum bypass
	// ativo) → BLOCKED. Em execução real, esse estado só é retornado se
	// alguém chamar o nível antes de enforceStartupIntegrity(); útil para
	// camadas externas que queiram inspecionar sem abortar.
	if !verdictOK && !skipGuardEnv && !bypassedCommand {
		return TrustBlocked
	}
	// Qualquer forma de bypass ou veredito não-OK tolerado → DEGRADED.
	if !verdictOK || skipGuardEnv || bypassedCommand {
		return TrustDegraded
	}
	return TrustTrusted
}

// currentTrustLevel é a versão com I/O para uso no main(). Reutiliza os
// coletores de startup_guard.go para manter acoplamento baixo.
func currentTrustLevel(subcommand string) TrustLevel {
	_, bypassed := guardBypassCommands[subcommand]
	skipEnv := os.Getenv("ORBIT_SKIP_GUARD") == "1"

	// Em build efêmero (go run) não há como validar PATH-commit; trate
	// como DEGRADED-por-design para deixar explícito ao usuário.
	if isEphemeralBuild() {
		if skipEnv || bypassed {
			return TrustDegraded
		}
		return TrustDegraded
	}

	self, _ := os.Executable()
	found := findAllOrbitsInPath()
	active := firstInPath()
	pathCommit := ""
	if active != "" {
		pathCommit = queryVersionCommit(active)
	}
	v := evaluateStartupIntegrity(self, Commit, found, active, pathCommit)
	return computeTrustLevel(v.OK, skipEnv, bypassed)
}

// printTrustBanner emite uma única linha em stderr apenas quando
// DEGRADED — TRUSTED é silencioso, BLOCKED nunca chega aqui em fluxo
// normal (o log.Fatal já rodou).
func printTrustBanner(level TrustLevel) {
	if level != TrustDegraded {
		return
	}
	fmt.Fprintf(os.Stderr, "orbit: trust=%s — rode `orbit doctor --deep` para diagnóstico\n", level)
}
