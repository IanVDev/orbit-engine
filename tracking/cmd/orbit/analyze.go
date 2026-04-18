// analyze.go — alias deprecated de `orbit doctor --alert-only`.
//
// Mantido apenas para compatibilidade com hooks/scripts existentes.
// Toda a lógica vive logicamente em doctor.go (modo --alert-only).
//
// Comportamento:
//   - Imprime aviso de deprecation em stderr.
//   - Executa as mesmas heurísticas, emitindo somente CRITICAL no formato
//     canônico (header / Risk / Context / Action).
//   - Silêncio quando ambiente saudável.
//
// Helpers (collectAnalysis, emitHighRisk, contextFor) seguem aqui porque
// também são reutilizados por `orbit doctor --alert-only` em doctor.go.
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

// highRiskLevel é o rótulo externo exibido ao usuário.
const highRiskLevel = "HIGH"

// analyzeDeprecationMsg é impresso em stderr toda vez que `orbit analyze`
// é invocado. Mantém compatibilidade sem esconder a migração.
const analyzeDeprecationMsg = "⚠️  `orbit analyze` está deprecated — use `orbit doctor --alert-only`. Será removido em uma versão futura.\n"

// runAnalyze é o entrypoint do subcomando deprecated. Imprime aviso e
// delega para a mesma rotina usada por `orbit doctor --alert-only`.
func runAnalyze() error {
	fmt.Fprint(os.Stderr, analyzeDeprecationMsg)
	return analyzeTo(os.Stdout)
}

// analyzeTo é a forma testável: escreve em w em vez de os.Stdout.
func analyzeTo(w io.Writer) error {
	res := collectAnalysis()
	emitHighRisk(w, res)
	return nil
}

// collectAnalysis executa o mesmo conjunto de checks que doctor usa para
// avaliar risco de ambiente. Não toca nos checks de PATH puro (ordering,
// posição), que são cosméticos para este comando.
func collectAnalysis() *doctorResult {
	res := &doctorResult{orbitBinPos: -1, localBinPos: -1}
	collectBinaryInfoSilent(res)
	collectPathInfo(res)

	checkUniqueOrbit(res)
	checkActiveBinary(res)
	checkExecutable(res)
	checkExpectedInstallPath(res)
	checkCommitStamp(res)
	checkHMACSecret(res)
	checkTrackingConnectivity(res)
	return res
}

// collectBinaryInfoSilent espelha collectBinaryInfo sem imprimir nada.
// Mantemos a duplicação mínima para não tocar em doctor.go.
func collectBinaryInfoSilent(res *doctorResult) {
	if self, err := os.Executable(); err == nil {
		res.selfPath = self
	}
	if out, err := exec.LookPath("orbit"); err == nil {
		res.currentBinary = out
	}
}

// emitHighRisk imprime em w apenas os checks CRITICAL, no formato canônico
// de 4 linhas (header, Risk, Context, Action) seguido de linha em branco.
func emitHighRisk(w io.Writer, res *doctorResult) {
	for _, c := range res.checks {
		if c.severity != sevCritical {
			continue
		}
		fmt.Fprintf(w, "⚠️ Pattern detected: %s\n", c.name)
		fmt.Fprintf(w, "Risk: %s — act now\n", highRiskLevel)
		fmt.Fprintf(w, "Context: %s\n", firstLine(contextFor(c)))
		fmt.Fprintf(w, "Action: %s\n", firstLine(fallback(c.fixHint, c.detail)))
		fmt.Fprintln(w)
	}
}

// contextFor devolve uma linha curta explicando por que o padrão foi
// detectado agora e qual o impacto imediato se nada for feito. O mapeamento
// é estático e local — não persiste nada, não chama backend e é
// independente das heurísticas (não as altera).
func contextFor(c check) string {
	switch c.name {
	case "Commit stamp (ldflags)":
		return "Binário em uso não é rastreável — proofs geradas agora perdem auditabilidade."
	case "ORBIT_HMAC_SECRET":
		return "Tracking aceitando eventos não assinados — qualquer caller pode forjar /track."
	case "Tracking-server /health":
		return "Eventos emitidos agora são descartados silenciosamente — governança off-line."
	case "Permissão de execução":
		return "Binário presente mas inexecutável — próximo `orbit` falhará no shell."
	case "Binário em /usr/local/bin/orbit",
		"Binário ativo == /usr/local/bin/orbit":
		return "Duas cópias divergentes em disco — comandos dependem de qual o PATH resolve primeiro."
	case "Binários orbit no PATH":
		return "Nenhum binário instalado — o CLI está quebrado para este shell."
	}
	// Fallback genérico: mantém o contrato de 4 linhas mesmo para checks novos.
	return "Condição crítica detectada no ambiente atual — resolva antes da próxima execução."
}

func fallback(primary, secondary string) string {
	if primary != "" {
		return primary
	}
	return secondary
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
