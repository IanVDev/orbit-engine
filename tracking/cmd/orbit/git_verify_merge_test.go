// git_verify_merge_test.go — testes de `orbit git verify-merge`.
//
// Abordagem: injeta gitRunner fake para controlar o output do git sem
// dependência de repositório real. Um teste adicional usa um repo temporário
// real para cobrir o caminho defaultGitRunner end-to-end.
package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// fakeRunner devolve uma linha de parents pré-definida sem chamar git.
func fakeRunner(parentsLine string) gitRunner {
	return func(_ string) (string, error) {
		return parentsLine, nil
	}
}

// errorRunner simula falha de git (repo inexistente, ref inválida, etc.).
func errorRunner(msg string) gitRunner {
	return func(_ string) (string, error) {
		return "", fmt.Errorf("%s", msg)
	}
}

// ── testes de parseParents ────────────────────────────────────────────────────

func TestParseParentsEmpty(t *testing.T) {
	got := parseParents("")
	if len(got) != 0 {
		t.Fatalf("esperado 0 parents para linha vazia, got %d", len(got))
	}
}

func TestParseParentsOne(t *testing.T) {
	got := parseParents("abc1234")
	if len(got) != 1 || got[0] != "abc1234" {
		t.Fatalf("esperado ['abc1234'], got %v", got)
	}
}

func TestParseParentsTwo(t *testing.T) {
	got := parseParents("abc1234 def5678")
	if len(got) != 2 {
		t.Fatalf("esperado 2 parents, got %d: %v", len(got), got)
	}
}

func TestParseParentsThree(t *testing.T) {
	got := parseParents("aaa bbb ccc")
	if len(got) != 3 {
		t.Fatalf("esperado 3 parents (octopus), got %d", len(got))
	}
}

// ── testes de gitVerifyMerge com runner fake ──────────────────────────────────

func TestVerifyMergeValid(t *testing.T) {
	var buf bytes.Buffer
	err := gitVerifyMerge("HEAD", fakeRunner("abc1234 def5678"), &buf)
	if err != nil {
		t.Fatalf("esperado nil, got %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "MERGE_VALID") {
		t.Fatalf("esperado MERGE_VALID no output, got:\n%s", out)
	}
	if !strings.Contains(out, "parent[0]: abc1234") {
		t.Fatalf("esperado parent[0] no output, got:\n%s", out)
	}
	if !strings.Contains(out, "parent[1]: def5678") {
		t.Fatalf("esperado parent[1] no output, got:\n%s", out)
	}
}

func TestVerifyMergeRegularCommit(t *testing.T) {
	var buf bytes.Buffer
	err := gitVerifyMerge("HEAD", fakeRunner("abc1234"), &buf)
	if err == nil {
		t.Fatal("esperado erro para commit com 1 parent")
	}
	out := buf.String()
	if !strings.Contains(out, "NOT_A_MERGE") {
		t.Fatalf("esperado NOT_A_MERGE no output, got:\n%s", out)
	}
	if !strings.Contains(out, "regular commit") {
		t.Fatalf("esperado diagnóstico 'regular commit', got:\n%s", out)
	}
	if !strings.Contains(err.Error(), "1 parent") {
		t.Fatalf("mensagem de erro deve mencionar contagem, got: %v", err)
	}
}

func TestVerifyMergeRootCommit(t *testing.T) {
	var buf bytes.Buffer
	err := gitVerifyMerge("HEAD", fakeRunner(""), &buf)
	if err == nil {
		t.Fatal("esperado erro para root commit (0 parents)")
	}
	out := buf.String()
	if !strings.Contains(out, "NOT_A_MERGE") {
		t.Fatalf("esperado NOT_A_MERGE no output, got:\n%s", out)
	}
	if !strings.Contains(out, "root commit") {
		t.Fatalf("esperado diagnóstico 'root commit', got:\n%s", out)
	}
}

func TestVerifyMergeOctopus(t *testing.T) {
	var buf bytes.Buffer
	err := gitVerifyMerge("HEAD", fakeRunner("aaa bbb ccc"), &buf)
	if err == nil {
		t.Fatal("esperado erro para octopus merge (3 parents)")
	}
	out := buf.String()
	if !strings.Contains(out, "NOT_A_MERGE") {
		t.Fatalf("esperado NOT_A_MERGE no output, got:\n%s", out)
	}
	if !strings.Contains(out, "octopus") {
		t.Fatalf("esperado diagnóstico 'octopus', got:\n%s", out)
	}
}

func TestVerifyMergeGitError(t *testing.T) {
	var buf bytes.Buffer
	err := gitVerifyMerge("bad-ref", errorRunner("exit status 128"), &buf)
	if err == nil {
		t.Fatal("esperado erro quando git falha")
	}
	if !strings.Contains(err.Error(), "verify-merge:") {
		t.Fatalf("mensagem de erro deve prefixar verify-merge:, got: %v", err)
	}
}

// ── teste com ref customizado ─────────────────────────────────────────────────

func TestVerifyMergeCustomRef(t *testing.T) {
	called := ""
	runner := func(ref string) (string, error) {
		called = ref
		return "aaa bbb", nil
	}
	var buf bytes.Buffer
	err := gitVerifyMerge("abc123", runner, &buf)
	if err != nil {
		t.Fatalf("esperado nil, got %v", err)
	}
	if called != "abc123" {
		t.Fatalf("runner deveria ter sido chamado com 'abc123', got %q", called)
	}
}

// ── teste end-to-end com repo real ────────────────────────────────────────────

func TestVerifyMergeEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git indisponível")
	}

	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v falhou: %v\n%s", args, err, out)
		}
	}

	// Setup repo com configurações mínimas (branch main explícita).
	run("init", "-q", "-b", "main")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	run("config", "commit.gpgsign", "false")

	// Cria commit inicial em main.
	run("commit", "--allow-empty", "-m", "initial")

	// Cria branch lateral com 1 commit.
	run("checkout", "-q", "-b", "feature")
	run("commit", "--allow-empty", "-m", "feature commit")

	// Volta para main e faz merge (--no-ff garante commit de merge).
	run("checkout", "-q", "main")
	run("merge", "--no-ff", "-m", "merge feature into main", "feature")

	// HEAD agora é merge commit com 2 parents — deve passar.
	realRunner := func(ref string) (string, error) {
		out, err := exec.Command("git", "show", "--no-patch", "--pretty=%P", ref).Output()
		if err != nil {
			return "", fmt.Errorf("git show falhou: %w", err)
		}
		return strings.TrimSpace(string(out)), nil
	}

	// Muda CWD para o repo temporário apenas para este teste.
	t.Chdir(repo)

	var buf bytes.Buffer

	// Merge commit deve passar.
	if err := gitVerifyMerge("HEAD", realRunner, &buf); err != nil {
		t.Fatalf("merge commit real deve passar, got: %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "MERGE_VALID") {
		t.Fatalf("esperado MERGE_VALID, got:\n%s", buf.String())
	}

	// Commit anterior ao merge (1 parent) deve falhar.
	buf.Reset()
	if err := gitVerifyMerge("HEAD~1", realRunner, &buf); err == nil {
		t.Fatalf("commit regular (HEAD~1) devia falhar em verify-merge")
	}
	if !strings.Contains(buf.String(), "NOT_A_MERGE") {
		t.Fatalf("esperado NOT_A_MERGE para HEAD~1, got:\n%s", buf.String())
	}
}
