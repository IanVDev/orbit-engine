package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimePathGuardAt(t *testing.T) {
	tmp := t.TempDir()
	userPath := filepath.Join(tmp, "user", "orbit")
	sysPath := filepath.Join(tmp, "sys", "orbit")
	fakeActive := userPath

	mustCreate := func(p string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("fake"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustRemove := func(p string) { _ = os.Remove(p) }

	t.Run("neither path — no output, no error", func(t *testing.T) {
		var buf bytes.Buffer
		err := runtimePathGuardAt(&buf, userPath, sysPath, fakeActive, false)
		if err != nil {
			t.Fatalf("esperava nil, obteve: %v", err)
		}
		if buf.Len() != 0 {
			t.Errorf("esperava saída vazia, obteve: %q", buf.String())
		}
	})

	t.Run("single user path — OK message com binário ativo", func(t *testing.T) {
		mustCreate(userPath)
		defer mustRemove(userPath)
		var buf bytes.Buffer
		err := runtimePathGuardAt(&buf, userPath, sysPath, fakeActive, false)
		if err != nil {
			t.Fatalf("esperava nil, obteve: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "binário ativo") {
			t.Errorf("esperava 'binário ativo' na saída: %q", out)
		}
		if !strings.Contains(out, fakeActive) {
			t.Errorf("esperava path ativo na saída: %q", out)
		}
	})

	t.Run("single sys path — OK message com binário ativo", func(t *testing.T) {
		mustCreate(sysPath)
		defer mustRemove(sysPath)
		var buf bytes.Buffer
		err := runtimePathGuardAt(&buf, userPath, sysPath, sysPath, false)
		if err != nil {
			t.Fatalf("esperava nil, obteve: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "binário ativo") {
			t.Errorf("esperava 'binário ativo' na saída: %q", out)
		}
	})

	t.Run("dual path sem strict — WARNING, sem erro, ambos os paths no output", func(t *testing.T) {
		mustCreate(userPath)
		mustCreate(sysPath)
		defer mustRemove(userPath)
		defer mustRemove(sysPath)
		var buf bytes.Buffer
		err := runtimePathGuardAt(&buf, userPath, sysPath, fakeActive, false)
		if err != nil {
			t.Fatalf("strict=false não deve retornar erro, obteve: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "⚠️") {
			t.Errorf("esperava aviso ⚠️: %q", out)
		}
		if !strings.Contains(out, userPath) {
			t.Errorf("output deve mencionar userPath: %q", out)
		}
		if !strings.Contains(out, sysPath) {
			t.Errorf("output deve mencionar sysPath: %q", out)
		}
		if !strings.Contains(out, fakeActive) {
			t.Errorf("output deve mencionar binário ativo: %q", out)
		}
		if !strings.Contains(out, "sugestão:") {
			t.Errorf("output deve conter sugestão de fix: %q", out)
		}
	})

	t.Run("dual path com strict — error retornado, mensagem menciona ambos os paths", func(t *testing.T) {
		mustCreate(userPath)
		mustCreate(sysPath)
		defer mustRemove(userPath)
		defer mustRemove(sysPath)
		var buf bytes.Buffer
		err := runtimePathGuardAt(&buf, userPath, sysPath, fakeActive, true)
		if err == nil {
			t.Fatal("strict=true com dual-path deve retornar error")
		}
		msg := err.Error()
		if !strings.Contains(msg, userPath) {
			t.Errorf("error deve mencionar userPath: %q", msg)
		}
		if !strings.Contains(msg, sysPath) {
			t.Errorf("error deve mencionar sysPath: %q", msg)
		}
		if !strings.Contains(msg, "ORBIT_STRICT_PATH") {
			t.Errorf("error deve mencionar ORBIT_STRICT_PATH: %q", msg)
		}
		// Com strict, não deve ter escrito nada em w (o erro é o canal de saída)
		if buf.Len() != 0 {
			t.Errorf("strict com error não deve escrever em w: %q", buf.String())
		}
	})
}
