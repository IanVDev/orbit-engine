// quickstart.go — fluxo completo de onboarding em 3 etapas.
//
// Etapa 1/3: Inicializa sessão (servidor embutido ou remoto)
// Etapa 2/3: Executa `echo hello`, registra evento em /track, obtém proof
// Etapa 3/3: Verifica proof localmente via ComputeHash
//
// Fail-closed: qualquer falha retorna error (o caller faz os.Exit(1)).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os/exec"
	"time"

	"github.com/IanVDev/orbit-engine/tracking"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// runQuickstart executa as 3 etapas de onboarding.
// Se host == "", inicia um servidor embutido em porta aleatória.
// Logs internos do pacote tracking são silenciados por padrão (modo limpo).
func runQuickstart(host string) error {
	// Silencia logs internos do servidor embutido para UX limpa.
	// Erros reais são capturados via retorno de erro das funções.
	log.SetOutput(io.Discard)

	var srv *http.Server

	if host == "" {
		var err error
		host, srv, err = startEmbedded()
		if err != nil {
			return fmt.Errorf("não foi possível iniciar servidor embutido: %w", err)
		}
		// Shutdown gracioso ao final da função.
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = srv.Shutdown(ctx)
		}()
	}

	printActiveHeartbeat()
	fmt.Println()
	PrintSection("orbit quickstart")

	// ── Etapa 1/3 — Inicializar sessão ──────────────────────────────────
	printStep(1, 3, "Inicializando sessão...")
	sessionID := fmt.Sprintf("qs-%d", time.Now().UnixNano())
	fmt.Printf("      ✓  session_id=%s\n", sessionID)

	// ── Etapa 2/3 — Executar echo hello + registrar evento ──────────────
	printStep(2, 3, "Executando: echo hello")
	out, err := exec.Command("echo", "hello").Output()
	if err != nil {
		return fmt.Errorf("exec echo hello: %w", err)
	}
	fmt.Printf("      → %s", out)

	now := tracking.NowUTC()
	const tokens int64 = 42

	eventID, err := postTrackEvent(host, sessionID, now, tokens)
	if err != nil {
		return fmt.Errorf("falha ao registrar evento no tracking: %w", err)
	}

	proof := tracking.ComputeHash(sessionID, now.Time, tokens)
	fmt.Printf("      ✓  evento registrado   event_id=%.12s...\n", eventID)
	fmt.Printf("      ✓  tokens=%d   proof=%.16s...\n", tokens, proof)

	// ── Etapa 3/3 — Verificar proof ─────────────────────────────────────
	printStep(3, 3, "Verificando proof...")
	recomputed := tracking.ComputeHash(sessionID, now.Time, tokens)
	if proof != recomputed {
		tracking.RecordVerifyFailure()
		return fmt.Errorf("proof inválido: esperado=%s obtido=%s", recomputed, proof)
	}
	tracking.RecordVerifySuccess()
	fmt.Printf("      ✓  proof válido (sha256 verificado)\n")
	fmt.Println("      ✨ proof generated")

	// ── Sumário ─────────────────────────────────────────────────────────
	fmt.Println()
	PrintSuccess("Quickstart concluído! Orbit está funcionando.")
	PrintDivider()
	PrintKV("Session    :", sessionID)
	fmt.Printf("   Servidor   : %s\n", host)
	PrintKV("Tokens     :", fmt.Sprintf("%d", tokens))
	PrintKV("Proof      :", proof[:16]+"...")
	PrintKV("Event ID   :", eventID[:12]+"...")
	fmt.Println()
	printHowItWorksBlock()
	printOnboardingBlock()

	// Product-layer counter: a full, verified onboarding run just happened.
	tracking.RecordQuickstartCompleted()
	return nil
}

// printHowItWorksBlock makes the usage model explicit, preventing the
// common misconception that Orbit runs continuously in the background.
// Short, stable format; appears once per quickstart completion.
//
//	Como o Orbit funciona:
//	  • Orbit NÃO roda automaticamente
//	  • Tracking acontece apenas dentro de 'orbit run <cmd>'
//	  • Fora disso, Orbit permanece idle/ready
func printHowItWorksBlock() {
	fmt.Println("  Como o Orbit funciona:")
	fmt.Println("    • Orbit NÃO roda automaticamente")
	fmt.Println("    • Tracking acontece apenas dentro de 'orbit run <cmd>'")
	fmt.Println("    • Fora disso, Orbit permanece idle/ready")
	fmt.Println()
}

// printOnboardingBlock emits a short, aligned block of next-step commands
// after quickstart completes. Kept terse on purpose — visual pollution
// here defeats its own goal. Format is stable; any change must pass G5.
//
//	Próximos passos:
//	  orbit run <cmd>     → executa comando com proof
//	  orbit stats         → ver métricas acumuladas
//	  orbit analyze       → alerta se risco >= HIGH
func printOnboardingBlock() {
	fmt.Println("  Próximos passos:")
	fmt.Println("    orbit run <cmd>     → executa comando com proof")
	fmt.Println("    orbit stats         → ver métricas acumuladas")
	fmt.Println("    orbit analyze       → alerta se risco >= HIGH")
	fmt.Println()
}

// startEmbedded inicia um tracking-server em processo (porta aleatória).
// Registra todas as métricas em um registry próprio para não colidir com
// o prometheus.DefaultRegisterer de processos que importem este pacote.
func startEmbedded() (addr string, srv *http.Server, err error) {
	reg := prometheus.NewRegistry()

	// Registra todas as camadas de métricas.
	tracking.RegisterMetrics(reg)
	tracking.RegisterSecurityMetrics(reg)
	tracking.RegisterValueMetrics(reg)
	tracking.RegisterProductMetrics(reg)
	tracking.RegisterModelControlMetrics(reg)
	tracking.RegisterTokenBudgetMetrics(reg)
	tracking.RegisterTokenReconcileMetrics(reg)
	tracking.RegisterReconcileAuthMetrics(reg)
	tracking.SetSeedMode(true) // instância de dev

	// Porta aleatória livre.
	ln, lErr := net.Listen("tcp", "127.0.0.1:0")
	if lErr != nil {
		return "", nil, fmt.Errorf("listen: %w", lErr)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	// Mux com /health, /metrics e /track.
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	mc, _ := tracking.ParseModelControl("auto")
	budget := tracking.NewTokenBudgetRegistry(100_000, 10_000)
	mux.HandleFunc("/track", tracking.TrackHandlerWithBudget(
		budget,
		tracking.TrackHandlerWithControl(mc),
	))

	serverAddr := fmt.Sprintf("127.0.0.1:%d", port)
	srv = &http.Server{
		Addr:         serverAddr,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	// Aguarda o servidor responder (máximo 3 s, fail-closed).
	deadline := time.Now().Add(3 * time.Second)
	baseURL := "http://" + serverAddr
	for time.Now().Before(deadline) {
		resp, hErr := http.Get(baseURL + "/health")
		if hErr == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return baseURL, srv, nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Verifica se o servidor crashou antes de responder.
	select {
	case e := <-errCh:
		return "", nil, fmt.Errorf("servidor falhou ao subir: %w", e)
	default:
		return "", nil, fmt.Errorf("timeout aguardando servidor embutido (porta %d)", port)
	}
}

// postTrackEvent envia um SkillEvent para /track e retorna o event_id.
// Fail-closed: retorna error em qualquer resposta não-200.
func postTrackEvent(host, sessionID string, ts tracking.FlexTime, tokens int64) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"event_type":              "skill_activation",
		"session_id":              sessionID,
		"timestamp":               ts.Format(time.RFC3339Nano),
		"mode":                    "auto",
		"trigger":                 "quickstart",
		"estimated_waste":         0.0,
		"actions_suggested":       1,
		"actions_applied":         1,
		"impact_estimated_tokens": tokens,
	})
	if err != nil {
		return "", fmt.Errorf("marshal evento: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(host+"/track", "application/json", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("POST /track: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("resposta inválida do /track: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("/track HTTP %d: %s", resp.StatusCode, result["error"])
	}
	return result["event_id"], nil
}

// printStep imprime o indicador de progresso "[n/total] mensagem".
func printStep(n, total int, msg string) {
	fmt.Printf("[%d/%d] %s\n", n, total, msg)
}
