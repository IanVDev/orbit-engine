// release.go — implementa `orbit release <version>`.
//
// Automatiza a criação de release público: valida estado do repo, cria tag
// anotada, faz push, e (opcionalmente) dispara release_gate.sh para validar
// a distribuição pública. Elimina erro humano na última milha.
//
// Fluxo fail-closed:
//   [1/6] Validar formato da versão (vX.Y.Z[-suffix])
//   [2/6] Validar estado do repo: na main, clean, sincronizado com origin/main
//   [3/6] Validar tag não existe (nem local nem remoto)
//   [4/6] (opcional, default on) Rodar `make gate-cli` antes de taguear
//   [5/6] Criar tag anotada + push; detecta HTTP 403 e loga CRITICAL
//   [6/6] (opcional, --wait-ci) Aguardar release.yml finalizar, rodar release_gate.sh
//
// Qualquer falha → exit 1. Logs determinísticos com marcadores [N/6].
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// versionPattern trava o formato de VERSION no contrato do release.yml:
// "v" + semver (vX.Y.Z) com sufixo pre-release opcional (-rc1, -beta, etc.).
var versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.]+)?$`)

// ReleaseOptions controla o comportamento do subcomando.
type ReleaseOptions struct {
	Version    string
	SkipGate   bool
	WaitCI     bool
	WaitCITime time.Duration
	Repo       string // default: IanVDev/orbit-engine
}

// runRelease é o entrypoint de `orbit release`.
func runRelease(opts ReleaseOptions, w io.Writer) error {
	if opts.Repo == "" {
		opts.Repo = "IanVDev/orbit-engine"
	}

	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "🚀  orbit release %s\n", opts.Version)
	fmt.Fprintln(w, "")

	// ── [1/6] Formato da versão ───────────────────────────────────────────
	fmt.Fprintln(w, "[1/6] validando formato da versão...")
	if !versionPattern.MatchString(opts.Version) {
		return fmt.Errorf("VERSION inválida %q — esperado vX.Y.Z[-suffix] (ex: v0.1.1, v1.0.0-rc1)", opts.Version)
	}
	fmt.Fprintf(w, "      ✓  %s\n", opts.Version)

	// ── [2/6] Estado do repo ──────────────────────────────────────────────
	fmt.Fprintln(w, "[2/6] validando estado do repo...")
	repoRoot, err := gitTopLevel()
	if err != nil {
		return fmt.Errorf("não estou num repo git: %w", err)
	}

	branch, err := runGitIn(repoRoot, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return fmt.Errorf("git rev-parse falhou: %w", err)
	}
	if branch != "main" {
		return fmt.Errorf("release só pode ser feito a partir de main (atual: %s)", branch)
	}

	status, err := runGitIn(repoRoot, "status", "--porcelain")
	if err != nil {
		return err
	}
	if status != "" {
		return fmt.Errorf("working tree sujo — commit ou stash antes de release:\n%s", status)
	}

	// origin/main deve existir e HEAD deve estar sincronizado.
	if _, err := runGitIn(repoRoot, "fetch", "origin", "main"); err != nil {
		return fmt.Errorf("git fetch origin main falhou: %w", err)
	}
	local, _ := runGitIn(repoRoot, "rev-parse", "HEAD")
	remote, _ := runGitIn(repoRoot, "rev-parse", "origin/main")
	if local != remote {
		return fmt.Errorf("HEAD local (%s) diverge de origin/main (%s) — faça pull/push antes", shortSHA(local), shortSHA(remote))
	}
	fmt.Fprintf(w, "      ✓  branch=main, clean, sync com origin/main (%s)\n", shortSHA(local))

	// ── [3/6] Tag não existe ──────────────────────────────────────────────
	fmt.Fprintln(w, "[3/6] verificando que tag não existe...")
	if _, err := runGitIn(repoRoot, "rev-parse", "-q", "--verify", "refs/tags/"+opts.Version); err == nil {
		return fmt.Errorf("tag %s já existe localmente — delete com `git tag -d %s` ou use outra versão", opts.Version, opts.Version)
	}
	remoteTags, err := runGitIn(repoRoot, "ls-remote", "--tags", "origin")
	if err != nil {
		return fmt.Errorf("git ls-remote falhou (sem rede?): %w", err)
	}
	if strings.Contains(remoteTags, "refs/tags/"+opts.Version+"\n") ||
		strings.HasSuffix(remoteTags, "refs/tags/"+opts.Version) {
		return fmt.Errorf("tag %s já existe em origin — nunca re-use uma versão tagueada", opts.Version)
	}
	fmt.Fprintf(w, "      ✓  %s livre (local e remoto)\n", opts.Version)

	// ── [4/6] gate-cli (opcional) ─────────────────────────────────────────
	if !opts.SkipGate {
		fmt.Fprintln(w, "[4/6] rodando make gate-cli...")
		cmd := exec.Command("make", "gate-cli")
		cmd.Dir = repoRoot
		cmd.Stdout = w
		cmd.Stderr = w
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("gate-cli falhou — release bloqueado: %w", err)
		}
		fmt.Fprintln(w, "      ✓  gate-cli PASS")
	} else {
		fmt.Fprintln(w, "[4/6] gate-cli pulado (--skip-gate)")
	}

	// ── [5/6] Criar tag + push ────────────────────────────────────────────
	fmt.Fprintln(w, "[5/6] criando tag + push...")
	tagMsg := fmt.Sprintf("orbit-engine %s — validated release", opts.Version)
	if _, err := runGitIn(repoRoot, "tag", "-a", opts.Version, "-m", tagMsg); err != nil {
		return fmt.Errorf("git tag falhou: %w", err)
	}

	pushOut, pushErr := runGitInRaw(repoRoot, "push", "origin", opts.Version)
	if pushErr != nil {
		// Detecta HTTP 403 explicitamente — é o modo de falha clássico
		// (sem permissão, PAT expirado, proxy bloqueando).
		lower := strings.ToLower(pushOut)
		if strings.Contains(lower, "403") || strings.Contains(lower, "forbidden") || strings.Contains(lower, "permission denied") {
			fmt.Fprintf(w, "\nCRITICAL: environment has no write permission to push tags to %s\n", opts.Repo)
			fmt.Fprintln(w, "         fix: garanta PAT/SSH com escopo write no remote, ou rode de outro ambiente.")
		}
		// Rollback da tag local — não deixamos estado incoerente.
		_, _ = runGitIn(repoRoot, "tag", "-d", opts.Version)
		return fmt.Errorf("git push da tag falhou: %w\n%s", pushErr, pushOut)
	}
	fmt.Fprintf(w, "      ✓  tag %s criada e publicada em origin\n", opts.Version)

	// ── [6/6] Validação opcional pós-push ─────────────────────────────────
	if opts.WaitCI {
		fmt.Fprintf(w, "[6/6] aguardando CI + validando release_gate (timeout=%s)...\n", opts.WaitCITime)
		if err := waitAndValidateRelease(repoRoot, opts, w); err != nil {
			return fmt.Errorf("release_gate pós-CI falhou: %w", err)
		}
		fmt.Fprintln(w, "      ✓  release público validado ponta a ponta")
	} else {
		fmt.Fprintln(w, "[6/6] (skip) --wait-ci não passado — rode `make release-gate VERSION="+opts.Version+"` quando CI finalizar")
	}

	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "🟢 RELEASE: %s publicado em origin. release.yml foi disparado.\n", opts.Version)
	fmt.Fprintln(w, "")
	return nil
}

// waitAndValidateRelease espera o release ficar disponível e roda
// release_gate.sh. Polling simples — não depende de token GH, só do HTTP
// público dos assets.
func waitAndValidateRelease(repoRoot string, opts ReleaseOptions, w io.Writer) error {
	script := filepath.Join(repoRoot, "scripts", "release_gate.sh")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("scripts/release_gate.sh não encontrado")
	}
	deadline := time.Now().Add(opts.WaitCITime)
	interval := 20 * time.Second
	for {
		cmd := exec.Command("bash", script, "--version", opts.Version)
		cmd.Dir = repoRoot
		var out strings.Builder
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err == nil {
			fmt.Fprint(w, out.String())
			return nil
		}
		if time.Now().After(deadline) {
			fmt.Fprint(w, out.String())
			return fmt.Errorf("timeout após %s — CI provavelmente ainda não finalizou ou release está quebrado", opts.WaitCITime)
		}
		fmt.Fprintf(w, "      ... release_gate ainda FAIL, retry em %s\n", interval)
		time.Sleep(interval)
	}
}

// runGitIn roda git em um diretório e retorna stdout trimmed. Falha se exit != 0.
// Nome distinto de `runGit` em snapshot.go (assinaturas diferentes — co-existência
// explícita para preservar o contrato daquele helper).
func runGitIn(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// runGitInRaw retorna combined output (para parsing de stderr do push).
func runGitInRaw(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func shortSHA(sha string) string {
	if len(sha) >= 7 {
		return sha[:7]
	}
	return sha
}
