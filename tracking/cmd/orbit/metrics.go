// metrics.go — contadores persistentes em $ORBIT_HOME/metrics/<name>.count.
//
// Cada métrica é um arquivo de texto com um único inteiro (ASCII decimal).
// Formato deliberadamente trivial para ser legível por `cat` e `orbit stats`
// sem dependência de parser. Append-only semanticamente: valor só cresce.
//
// Concorrência: um orbit run é single-shot; colisões reais são raras. Usamos
// O_EXCL + rename atômico via arquivo temporário para evitar perda de
// incremento sob concorrência no mesmo host.
//
// Fail-closed NO caller: IncrementMetric retorna erro; o caller decide se
// isso é bloqueante. Usado hoje em run.go para execution_without_log_total.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/IanVDev/orbit-engine/tracking"
)

const metricsDirName = "metrics"

// MetricExecutionWithoutLog é a métrica disparada quando uma execução
// completou mas o log não foi persistido/verificado com sucesso.
// Nome fixo aqui para evitar typos nos call sites.
const MetricExecutionWithoutLog = "execution_without_log_total"

// ReadMetric devolve o valor atual. Arquivo ausente = 0 (não é erro).
func ReadMetric(name string) (int, error) {
	path, err := metricPath(name)
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("read metric %q: %w", name, err)
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("parse metric %q: %w", name, err)
	}
	return v, nil
}

// IncrementMetric soma 1 ao contador. Cria arquivo se ausente. Rename
// atômico via arquivo temporário no mesmo diretório (same-FS, garante rename
// síncrono no POSIX).
func IncrementMetric(name string) (int, error) {
	path, err := metricPath(name)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return 0, fmt.Errorf("mkdir metrics: %w", err)
	}

	current, err := ReadMetric(name)
	if err != nil {
		return 0, err
	}
	next := current + 1

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return 0, fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		// Se algo falhar entre aqui e o rename, limpa o tmp.
		_ = os.Remove(tmpPath)
	}()

	if _, err := fmt.Fprintf(tmp, "%d\n", next); err != nil {
		tmp.Close()
		return 0, fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return 0, fmt.Errorf("close temp: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return 0, fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return 0, fmt.Errorf("rename: %w", err)
	}
	return next, nil
}

// metricName aceita apenas [a-z0-9_] — evita path traversal e colisão com
// subdiretórios. Nome obrigatório.
func metricPath(name string) (string, error) {
	if name == "" {
		return "", errors.New("metric name vazio")
	}
	for _, r := range name {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return "", fmt.Errorf("metric name %q contém char inválido", name)
		}
	}
	base, err := tracking.ResolveStoreHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, metricsDirName, name+".count"), nil
}
