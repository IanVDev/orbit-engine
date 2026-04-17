// context_pack.go — geração automática de context-pack para transição entre conversas.
//
// Captura o estado atual do projeto em ≤500 tokens estruturados prontos para
// colar em uma nova conversa sem perda de entendimento.
//
// Uso:
//
//	orbit context-pack                        → gera e imprime o pack
//	orbit context-pack --auto                 → gera silenciosamente (para hooks)
//	orbit context-pack --set-objective "..."  → define objetivo atual
//	orbit context-pack --add-decision "..."   → registra decisão tomada
//	orbit context-pack --add-risk "..."       → registra risco conhecido
//	orbit context-pack --add-next "..."       → adiciona próximo passo
//	orbit context-pack --reset                → limpa decisões/riscos/next
//
// Salva automaticamente em $ORBIT_HOME/context-pack.md.
// Trigger recomendado: 56% da janela de contexto (~100k tokens em 200k).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/IanVDev/orbit-engine/tracking"
	"github.com/prometheus/client_golang/prometheus"
)

// contextStateFile e contextPackFile são os nomes dentro de $ORBIT_HOME.
const contextStateFile = "state.json"
const contextPackFile  = "context-pack.md"

// contextState persiste o estado gerenciável do context-pack.
type contextState struct {
	Objective string    `json:"objective"`
	Decisions []string  `json:"decisions"`
	Risks     []string  `json:"risks"`
	NextSteps []string  `json:"next_steps"`
	UpdatedAt time.Time `json:"updated_at"`
}

// runContextPack é o entrypoint do subcomando context-pack.
func runContextPack(auto bool, setObj, addDecision, addRisk, addNext string, reset bool) error {
	home, err := tracking.ResolveStoreHome()
	if err != nil {
		return err
	}
	statePath := filepath.Join(home, contextStateFile)
	packPath  := filepath.Join(home, contextPackFile)

	state := loadContextState(statePath)

	// Mutations — qualquer flag de escrita atualiza o state e salva.
	mutated := false
	if setObj != "" {
		state.Objective = setObj
		mutated = true
	}
	if addDecision != "" {
		state.Decisions = append(state.Decisions, addDecision)
		mutated = true
	}
	if addRisk != "" {
		state.Risks = append(state.Risks, addRisk)
		mutated = true
	}
	if addNext != "" {
		state.NextSteps = append(state.NextSteps, addNext)
		mutated = true
	}
	if reset {
		state.Decisions = nil
		state.Risks     = nil
		state.NextSteps = nil
		mutated = true
	}
	if mutated {
		state.UpdatedAt = time.Now().UTC()
		saveContextState(statePath, state)
		if auto {
			// No modo auto, mutações isoladas não geram pack.
			return nil
		}
	}

	// Coleta estado dinâmico do sistema.
	branch := gitCurrentBranch()
	commit := gitShortCommit()
	prLine := ghOpenPR()
	stats, _ := tracking.LoadHistoryStats()

	pack := buildContextPack(state, branch, commit, prLine, stats)

	// Persiste em disco (sempre, incluindo modo auto).
	if err := os.MkdirAll(home, 0o700); err == nil {
		_ = os.WriteFile(packPath, []byte(pack+"\n"), 0o600)
	}
	contextPackGeneratedTotal.Inc()

	if auto {
		return nil // hook silencioso
	}

	fmt.Println(pack)
	fmt.Println()
	PrintTip("Salvo em: " + packPath)
	PrintTip("Cole em uma nova conversa para continuidade perfeita.")
	fmt.Println()
	return nil
}

// buildContextPack monta o texto estruturado compacto (≤500 tokens).
func buildContextPack(s contextState, branch, commit, prLine string, stats tracking.HistoryStats) string {
	var b strings.Builder
	now := time.Now().UTC().Format("2006-01-02T15:04Z")

	sep := strings.Repeat("─", 55)
	b.WriteString("━━━━━━━━━━━━━━━━━ ORBIT-CTX v1 ━━━━━━━━━━━━━━━━━━\n")

	obj := s.Objective
	if obj == "" {
		obj = "(não definido — use: orbit context-pack --set-objective)"
	}
	b.WriteString("OBJ     " + obj + "\n")
	b.WriteString(fmt.Sprintf("BRANCH  %s @ %s\n", branch, commit))
	if prLine != "" {
		b.WriteString("PR      " + prLine + "\n")
	}
	b.WriteString(fmt.Sprintf("STATS   sessions=%d | tokens=%d | streak=%dd | cost=$%.4f\n",
		stats.TotalSessions, stats.TotalTokensSaved, stats.StreakDays, stats.TotalCostSavedUSD))

	writeSection := func(title string, items []string, empty string) {
		b.WriteString(sep + "\n")
		b.WriteString(title + "\n")
		if len(items) == 0 {
			b.WriteString("  " + empty + "\n")
			return
		}
		for _, item := range items {
			b.WriteString("  · " + item + "\n")
		}
	}

	writeSection("DECISIONS", s.Decisions, "(nenhuma registrada)")
	writeSection("RISKS",     s.Risks,     "(nenhum registrado)")

	b.WriteString(sep + "\n")
	b.WriteString("NEXT\n")
	if len(s.NextSteps) == 0 {
		b.WriteString("  (nenhum registrado)\n")
	} else {
		for i, step := range s.NextSteps {
			b.WriteString(fmt.Sprintf("  %d. %s\n", i+1, step))
		}
	}

	b.WriteString(strings.Repeat("━", 52) + "\n")
	b.WriteString(fmt.Sprintf("Generated: %s | orbit context-pack\n", now))
	return b.String()
}

// ---------------------------------------------------------------------------
// State persistence
// ---------------------------------------------------------------------------

func loadContextState(path string) contextState {
	data, err := os.ReadFile(path)
	if err != nil {
		return contextState{}
	}
	var s contextState
	if err := json.Unmarshal(data, &s); err != nil {
		return contextState{}
	}
	return s
}

func saveContextState(path string, s contextState) {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

// ---------------------------------------------------------------------------
// Git + GitHub helpers — fail-soft (retornam string vazia em erro)
// ---------------------------------------------------------------------------

func gitCurrentBranch() string {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func gitShortCommit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func ghOpenPR() string {
	out, err := exec.Command("gh", "pr", "view", "--json", "number,title,state",
		"--template", "#{{.number}} {{.state}} — {{.title}}").Output()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(out))
	if len(line) > 80 {
		line = line[:77] + "..."
	}
	return line
}

// ---------------------------------------------------------------------------
// Prometheus metric
// ---------------------------------------------------------------------------

var contextPackGeneratedTotal = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "orbit_context_pack_generated_total",
	Help: "Total de context-packs gerados para transição entre conversas.",
})

// RegisterContextPackMetrics registra o contador no registerer dado.
func RegisterContextPackMetrics(reg prometheus.Registerer) {
	reg.MustRegister(contextPackGeneratedTotal)
}
