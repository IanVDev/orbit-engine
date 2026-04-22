// logs.go — subcomando `orbit logs` para gestão dos logs persistidos
// em $ORBIT_HOME/logs/.
//
// Uso:
//
//	orbit logs prune --older-than 30d   remove arquivos cujo modTime é
//	                                    anterior ao cutoff informado.
//
// Aceita sufixo 'd' (dias) além dos formatos suportados por
// time.ParseDuration (ex: "72h", "45m").
//
// Fail-closed: qualquer erro de filesystem (ReadDir, Remove, Stat) aborta
// e retorna o erro — o caller faz os.Exit(1).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/IanVDev/orbit-engine/tracking"
)

func runLogs(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("uso: orbit logs prune --older-than <duração> (ex: 30d, 72h)")
	}
	switch args[0] {
	case "prune":
		fs := flag.NewFlagSet("logs prune", flag.ExitOnError)
		olderThan := fs.String("older-than", "30d", "remove logs mais antigos que esta duração (ex: 30d, 72h)")
		_ = fs.Parse(args[1:])
		dur, err := parseRetention(*olderThan)
		if err != nil {
			return fmt.Errorf("logs prune: --older-than inválido: %w", err)
		}
		return runLogsPrune(dur)
	default:
		return fmt.Errorf("logs: subcomando desconhecido %q (use: prune)", args[0])
	}
}

// parseRetention aceita sufixo 'd' (dias) além dos formatos padrão de
// time.ParseDuration. "30d" → 30*24h.
func parseRetention(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("duração vazia")
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || n < 0 {
			return 0, fmt.Errorf("duração inválida %q", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// runLogsPrune remove arquivos em $ORBIT_HOME/logs/ cujo modTime é mais
// antigo que `older`. Fail-closed no primeiro erro de I/O.
func runLogsPrune(older time.Duration) error {
	base, err := tracking.ResolveStoreHome()
	if err != nil {
		return fmt.Errorf("logs prune: resolve home: %w", err)
	}
	dir := filepath.Join(base, logsDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("orbit logs prune: nenhum log em %s\n", dir)
			return nil
		}
		return fmt.Errorf("logs prune: read %q: %w", dir, err)
	}
	cutoff := time.Now().Add(-older)
	removed := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// Hardening: ignora symlinks — prune opera apenas em arquivos
		// regulares que o próprio orbit gravou. Não seguir links evita
		// remover targets fora do diretório de logs se um link apontar
		// para fora (ex: ~/.ssh/id_rsa). Type() usa lstat, não dereferencia.
		if e.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return fmt.Errorf("logs prune: stat %q: %w", e.Name(), err)
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if err := os.Remove(p); err != nil {
			return fmt.Errorf("logs prune: remove %q: %w", p, err)
		}
		removed++
	}
	fmt.Printf("orbit logs prune: %d arquivo(s) removido(s) (older-than=%s)\n", removed, older)
	return nil
}
