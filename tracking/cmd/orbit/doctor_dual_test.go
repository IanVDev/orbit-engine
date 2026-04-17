package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckDualInstallPathsAt(t *testing.T) {
	tmp := t.TempDir()
	userPath := filepath.Join(tmp, "user", "orbit")
	sysPath := filepath.Join(tmp, "sys", "orbit")
	fakeActive := filepath.Join(tmp, "user", "orbit")

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

	t.Run("only user path — OK with path in detail", func(t *testing.T) {
		mustCreate(userPath)
		defer mustRemove(userPath)
		res := &doctorResult{}
		checkDualInstallPathsAt(res, userPath, sysPath, fakeActive, false)
		if len(res.checks) != 1 || res.checks[0].severity != sevOK {
			t.Fatalf("esperava 1 check OK, obteve: %+v", res.checks)
		}
		if !strings.Contains(res.checks[0].detail, userPath) {
			t.Errorf("detail deveria mencionar path ativo: %q", res.checks[0].detail)
		}
	})

	t.Run("only sys path — OK with path in detail", func(t *testing.T) {
		mustCreate(sysPath)
		defer mustRemove(sysPath)
		res := &doctorResult{}
		checkDualInstallPathsAt(res, userPath, sysPath, sysPath, false)
		if len(res.checks) != 1 || res.checks[0].severity != sevOK {
			t.Fatalf("esperava 1 check OK, obteve: %+v", res.checks)
		}
		if !strings.Contains(res.checks[0].detail, sysPath) {
			t.Errorf("detail deveria mencionar path ativo: %q", res.checks[0].detail)
		}
	})

	t.Run("both paths without strict — WARNING", func(t *testing.T) {
		mustCreate(userPath)
		mustCreate(sysPath)
		defer mustRemove(userPath)
		defer mustRemove(sysPath)
		res := &doctorResult{}
		checkDualInstallPathsAt(res, userPath, sysPath, fakeActive, false)
		if len(res.checks) != 1 {
			t.Fatalf("esperava exatamente 1 check, obteve %d", len(res.checks))
		}
		c := res.checks[0]
		if c.severity != sevWarning {
			t.Errorf("severidade esperada WARNING, obteve %s", c.severity.tag())
		}
		if !strings.Contains(c.detail, userPath) {
			t.Errorf("detail não menciona userPath: %q", c.detail)
		}
		if !strings.Contains(c.detail, sysPath) {
			t.Errorf("detail não menciona sysPath: %q", c.detail)
		}
		if !strings.Contains(c.detail, fakeActive) {
			t.Errorf("detail não menciona binário ativo: %q", c.detail)
		}
		if !strings.Contains(c.fixHint, sysPath) {
			t.Errorf("fixHint deveria sugerir remoção do sysPath: %q", c.fixHint)
		}
	})

	t.Run("both paths with ORBIT_STRICT_PATH — CRITICAL", func(t *testing.T) {
		mustCreate(userPath)
		mustCreate(sysPath)
		defer mustRemove(userPath)
		defer mustRemove(sysPath)
		res := &doctorResult{}
		checkDualInstallPathsAt(res, userPath, sysPath, fakeActive, true)
		if len(res.checks) != 1 {
			t.Fatalf("esperava exatamente 1 check, obteve %d", len(res.checks))
		}
		c := res.checks[0]
		if c.severity != sevCritical {
			t.Errorf("strict=true deveria emitir CRITICAL, obteve %s", c.severity.tag())
		}
		if !strings.Contains(c.detail, fakeActive) {
			t.Errorf("detail deveria mencionar binário ativo: %q", c.detail)
		}
	})

	t.Run("neither path — no check emitted", func(t *testing.T) {
		res := &doctorResult{}
		checkDualInstallPathsAt(res, userPath, sysPath, "", false)
		if len(res.checks) != 0 {
			t.Fatalf("esperava 0 checks, obteve: %+v", res.checks)
		}
	})

	t.Run("unknown active binary shown as desconhecido", func(t *testing.T) {
		mustCreate(userPath)
		mustCreate(sysPath)
		defer mustRemove(userPath)
		defer mustRemove(sysPath)
		res := &doctorResult{}
		checkDualInstallPathsAt(res, userPath, sysPath, "", false)
		if !strings.Contains(res.checks[0].detail, "desconhecido") {
			t.Errorf("activeBinary vazio deveria mostrar 'desconhecido': %q", res.checks[0].detail)
		}
	})
}
