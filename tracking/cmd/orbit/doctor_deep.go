// doctor_deep.go — checks adicionais ativados por `orbit doctor --deep`.
//
// Foco: detectar inconsistências de ambiente que se manifestam como "bug
// fantasma" — binários múltiplos, symlinks apontando para versões antigas,
// wrappers shell intermediando a execução, e divergência entre o `Commit`
// baked no binário atual e o `orbit version` que o shell resolve.
//
// Restrições:
//   - fail-closed: múltiplos binários e commit mismatch são sempre CRITICAL
//   - isolado: não altera a lógica do doctor "raso"; apenas adiciona checks
package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// narrativeProbes é a lista de strings suspeitas que podem indicar a
// origem de outputs em prosa que usuários reportam. Extensível sem tocar
// em heurísticas de risco.
var narrativeProbes = []string{
	"operacional",
	"Sessões rastreadas",
	"budget folgado",
	"Tokens economizados",
	"Mission Log",
}

// upgradeDuplicatesToCritical eleva o check de "binários orbit únicos" de
// WARNING → CRITICAL quando --deep está ativo. A regra vem explícita do
// produto: múltiplos binários = sistema inconsistente.
func upgradeDuplicatesToCritical(res *doctorResult) {
	for i := range res.checks {
		if res.checks[i].name == "Binários orbit únicos" &&
			res.checks[i].severity == sevWarning {
			res.checks[i].severity = sevCritical
		}
	}
}

// checkSymlinkChain resolve o binário ativo até o alvo final, mostrando
// a cadeia. Se o alvo final diverge do path original, registra OK com o
// mapeamento (informativo — a criticidade já é coberta por checkExpectedInstallPath).
func checkSymlinkChain(res *doctorResult) {
	if res.currentBinary == "" {
		return
	}
	chain, final, err := resolveSymlinkChain(res.currentBinary)
	if err != nil {
		res.add("Symlink chain", sevWarning,
			fmt.Sprintf("falha ao resolver: %v", err), "")
		return
	}
	if len(chain) > 1 {
		res.add("Symlink chain", sevOK,
			strings.Join(chain, " → "), "")
	} else {
		res.add("Symlink chain", sevOK,
			fmt.Sprintf("sem indireção (%s)", final), "")
	}
}

// resolveSymlinkChain caminha pelos symlinks um-a-um (até 8 saltos) para
// produzir a cadeia legível. EvalSymlinks só dá o destino final.
func resolveSymlinkChain(path string) ([]string, string, error) {
	chain := []string{path}
	current := path
	for i := 0; i < 8; i++ {
		info, err := os.Lstat(current)
		if err != nil {
			return chain, current, err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return chain, current, nil
		}
		next, err := os.Readlink(current)
		if err != nil {
			return chain, current, err
		}
		if !filepath.IsAbs(next) {
			next = filepath.Join(filepath.Dir(current), next)
		}
		chain = append(chain, next)
		current = next
	}
	return chain, current, fmt.Errorf("cadeia de symlinks > 8 saltos (loop?)")
}

// checkWrapperScript detecta se o binário ativo é um script shell
// (shebang) em vez de binário nativo. Wrappers silenciosos são uma
// fonte comum de outputs "misteriosos".
func checkWrapperScript(res *doctorResult) {
	if res.currentBinary == "" {
		return
	}
	target, err := filepath.EvalSymlinks(res.currentBinary)
	if err != nil {
		target = res.currentBinary
	}
	f, err := os.Open(target)
	if err != nil {
		return
	}
	defer f.Close()
	head := make([]byte, 4)
	n, _ := f.Read(head)
	if n >= 2 && head[0] == '#' && head[1] == '!' {
		res.add("Wrapper script (shebang)", sevCritical,
			fmt.Sprintf("%s é script, não binário Go", target),
			"reinstale o binário nativo: scripts/build_orbit.sh")
		return
	}
	res.add("Wrapper script (shebang)", sevOK, "binário nativo", "")
}

// checkCommitMismatch compara Commit baked neste binário com o Commit
// reportado por `orbit version` via PATH. Divergência = CRITICAL.
//
// Quando doctor roda via `go run`, Commit==unknown e o check é pulado
// (já coberto por checkCommitStamp).
func checkCommitMismatch(res *doctorResult) {
	if Commit == "" || Commit == "unknown" {
		return
	}
	if res.currentBinary == "" {
		return
	}
	out, err := runOrbitVersion(res.currentBinary)
	if err != nil {
		res.add("Commit mismatch (self vs PATH)", sevCritical,
			fmt.Sprintf("falha ao executar '%s version': %v", res.currentBinary, err),
			"")
		return
	}
	pathCommit := extractCommit(out)
	if pathCommit == "" {
		res.add("Commit mismatch (self vs PATH)", sevWarning,
			"não foi possível extrair commit do output", "")
		return
	}
	if pathCommit != Commit {
		res.add("Commit mismatch (self vs PATH)", sevCritical,
			fmt.Sprintf("self=%s  PATH=%s", Commit, pathCommit),
			"reinstale a versão correta em "+expectedInstallPath)
		return
	}
	res.add("Commit mismatch (self vs PATH)", sevOK,
		fmt.Sprintf("ambos em %s", Commit), "")
}

// runOrbitVersion executa `<path> version` com timeout curto.
func runOrbitVersion(path string) (string, error) {
	cmd := exec.Command(path, "version")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	select {
	case err := <-done:
		return buf.String(), err
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		return buf.String(), fmt.Errorf("timeout após 3s")
	}
}

// extractCommit busca `commit=<valor>` no output de `orbit version`.
// Exportado-em-espírito para poder ser exercido pelo teste.
func extractCommit(s string) string {
	const key = "commit="
	i := strings.Index(s, key)
	if i < 0 {
		return ""
	}
	rest := s[i+len(key):]
	end := strings.IndexAny(rest, " )\n\t")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(rest[:end])
}

// checkNarrativeOrigin faz grep leve no cwd por strings que costumam
// aparecer em outputs narrativos não-identificados. Ajuda o usuário a
// localizar qual componente está produzindo mensagens desconhecidas.
// Skippa silenciosamente se o cwd não parece ser o repo.
func checkNarrativeOrigin(res *doctorResult) {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	// Heurística barata: só procura se cwd contém tracking/ ou é o repo.
	if _, statErr := os.Stat(filepath.Join(cwd, "tracking")); statErr != nil {
		if _, statErr2 := os.Stat(filepath.Join(cwd, "go.mod")); statErr2 != nil {
			return
		}
	}
	hits := grepProbes(cwd, narrativeProbes, 8)
	if len(hits) == 0 {
		res.add("Origem de narrativa (grep)", sevOK,
			"nenhuma das strings suspeitas encontrada no cwd", "")
		return
	}
	res.add("Origem de narrativa (grep)", sevWarning,
		fmt.Sprintf("%d arquivo(s) contêm strings suspeitas (ex.: %s)",
			len(hits), hits[0]),
		"revise esses arquivos para identificar a camada que gera o output")
}

// grepProbes procura recursivamente (com limite) por qualquer probe.
// Retorna lista de "arquivo:linha:probe" até maxHits.
func grepProbes(root string, probes []string, maxHits int) []string {
	var hits []string
	skipDirs := map[string]bool{
		".git": true, "node_modules": true, ".venv": true,
		"vendor": true, "dist": true, "build": true,
	}
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || len(hits) >= maxHits {
			return nil
		}
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		// Só arquivos de texto de tamanho razoável.
		if info.Size() > 512*1024 {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		switch ext {
		case ".go", ".py", ".sh", ".md", ".yml", ".yaml", ".json", ".txt", "":
		default:
			return nil
		}
		// Evita falso positivo: o próprio arquivo que declara as probes.
		base := filepath.Base(p)
		if strings.HasPrefix(base, "doctor_deep") {
			return nil
		}
		data, rErr := os.ReadFile(p)
		if rErr != nil {
			return nil
		}
		for _, probe := range probes {
			if bytes.Contains(data, []byte(probe)) {
				rel, _ := filepath.Rel(root, p)
				hits = append(hits, fmt.Sprintf("%s (%s)", rel, probe))
				if len(hits) >= maxHits {
					return filepath.SkipAll
				}
				break
			}
		}
		return nil
	})
	return hits
}
