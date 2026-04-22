package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadMetric_AbsentIsZero(t *testing.T) {
	t.Setenv("ORBIT_HOME", t.TempDir())
	v, err := ReadMetric("missing_counter")
	if err != nil {
		t.Fatalf("ReadMetric: %v", err)
	}
	if v != 0 {
		t.Fatalf("counter ausente deveria ler 0, got %d", v)
	}
}

func TestIncrementMetric_StartsAtOne(t *testing.T) {
	t.Setenv("ORBIT_HOME", t.TempDir())
	v, err := IncrementMetric(MetricExecutionWithoutLog)
	if err != nil {
		t.Fatalf("IncrementMetric: %v", err)
	}
	if v != 1 {
		t.Fatalf("primeiro incremento deveria ser 1, got %d", v)
	}
}

func TestIncrementMetric_AccumulatesAndPersists(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ORBIT_HOME", tmp)

	for want := 1; want <= 5; want++ {
		got, err := IncrementMetric(MetricExecutionWithoutLog)
		if err != nil {
			t.Fatalf("IncrementMetric %d: %v", want, err)
		}
		if got != want {
			t.Fatalf("esperado %d, got %d", want, got)
		}
	}

	// Reabre: o valor deve persistir (nada in-memory).
	got, err := ReadMetric(MetricExecutionWithoutLog)
	if err != nil {
		t.Fatalf("ReadMetric: %v", err)
	}
	if got != 5 {
		t.Fatalf("esperado 5 após 5 incrementos, got %d", got)
	}

	// Arquivo está no caminho esperado (cat-able).
	path := filepath.Join(tmp, "metrics", MetricExecutionWithoutLog+".count")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read metric file: %v", err)
	}
	if string(data) != "5\n" {
		t.Fatalf("conteúdo do arquivo: got %q, want %q", string(data), "5\n")
	}
}

func TestIncrementMetric_RejectsInvalidName(t *testing.T) {
	t.Setenv("ORBIT_HOME", t.TempDir())
	for _, name := range []string{"", "Bad", "with space", "slash/inside", "dash-hyphen"} {
		if _, err := IncrementMetric(name); err == nil {
			t.Errorf("esperava erro para nome inválido %q", name)
		}
	}
}

func TestReadMetric_RejectsCorruptFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ORBIT_HOME", tmp)

	// Grava lixo no arquivo de métrica.
	dir := filepath.Join(tmp, "metrics")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, MetricExecutionWithoutLog+".count")
	if err := os.WriteFile(path, []byte("not-a-number"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := ReadMetric(MetricExecutionWithoutLog); err == nil {
		t.Fatal("esperava erro ao ler arquivo corrompido")
	}
}
