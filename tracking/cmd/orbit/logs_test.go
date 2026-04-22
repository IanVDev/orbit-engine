package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPrune_RemovesOldFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ORBIT_HOME", tmp)

	dir := filepath.Join(tmp, logsDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	old := filepath.Join(dir, "old.json")
	fresh := filepath.Join(dir, "fresh.json")
	for _, p := range []string{old, fresh} {
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}

	past := time.Now().Add(-40 * 24 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if err := runLogsPrune(30 * 24 * time.Hour); err != nil {
		t.Fatalf("runLogsPrune: %v", err)
	}

	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("esperava old.json removido, stat err=%v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh.json não deveria ter sido removido: %v", err)
	}
}

func TestPrune_NoLogsDirIsNoOp(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ORBIT_HOME", tmp)
	if err := runLogsPrune(30 * 24 * time.Hour); err != nil {
		t.Errorf("diretório ausente deveria ser no-op, got err=%v", err)
	}
}

// TestPrune_IgnoresSymlinks garante que o prune nunca segue nem remove
// symlinks dentro de $ORBIT_HOME/logs/. Proteção contra apagar arquivos
// fora do diretório via link apontando para caminho externo.
func TestPrune_IgnoresSymlinks(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ORBIT_HOME", tmp)

	dir := filepath.Join(tmp, logsDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Target vive FORA do dir de logs — simula o cenário de ataque onde
	// um symlink plantado em logs/ aponta para um arquivo sensível.
	outside := filepath.Join(tmp, "sensitive_outside.json")
	if err := os.WriteFile(outside, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink não suportado neste FS: %v", err)
	}

	// Garante que cutoff < modTime de tudo que acabamos de criar, para
	// que o branch de remoção dispare mesmo na ausência de backdate.
	time.Sleep(10 * time.Millisecond)
	if err := runLogsPrune(5 * time.Millisecond); err != nil {
		t.Fatalf("runLogsPrune: %v", err)
	}

	if _, err := os.Lstat(link); err != nil {
		t.Errorf("symlink em logs/ foi removido pelo prune: %v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("target externo do symlink foi afetado indevidamente: %v", err)
	}
}

func TestParseRetention(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"30d", 30 * 24 * time.Hour},
		{"1d", 24 * time.Hour},
		{"72h", 72 * time.Hour},
		{"45m", 45 * time.Minute},
	}
	for _, c := range cases {
		got, err := parseRetention(c.in)
		if err != nil {
			t.Fatalf("parse %q: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("parse %q = %v, want %v", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"", "bogus", "-1d", "30x"} {
		if _, err := parseRetention(bad); err == nil {
			t.Errorf("esperava erro para %q", bad)
		}
	}
}
