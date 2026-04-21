// repo_hygiene_test.go — anti-regression guard: tracked files must not
// balloon the repository. Fails fast when a build artifact slips into
// the git index.
//
// Why live in `tracking/` package: this is where our go test ./... run
// picks it up automatically, so every CI build and every local test run
// enforces the same rule without extra wiring.
package tracking

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// maxTrackedBytes is the size ceiling for any single tracked file.
// Chosen to be comfortably above legitimate source assets (docs,
// fixtures, small binaries) while catching every known Go build
// artifact in this repo, which are all ~12MB.
const maxTrackedBytes int64 = 5 * 1024 * 1024 // 5 MB

// TestNoLargeBinariesTracked asserts that no file currently tracked in
// git exceeds maxTrackedBytes. Build artifacts (the `orbit` CLI, the
// tracking server, validators) belong in .gitignore; committing them
// bloats clones, poisons `git log --stat`, and produces merge noise on
// every rebuild.
//
// The test walks `git ls-files` (running from the repo root reached by
// ascending from the module dir) and `git cat-file -s` for each entry,
// which reports the blob size as stored in the index rather than the
// working-tree size. That distinction matters: a file can be staged-
// for-deletion locally yet still be present in HEAD; we want to catch
// that too.
func TestNoLargeBinariesTracked(t *testing.T) {
	repoRoot := findRepoRoot(t)

	lsFiles := exec.Command("git", "-C", repoRoot, "ls-files", "-z")
	out, err := lsFiles.Output()
	if err != nil {
		t.Fatalf("git ls-files failed: %v", err)
	}

	var offenders []string
	for _, path := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if path == "" {
			continue
		}
		// `git cat-file -s :<path>` yields the size (in bytes) of the
		// blob recorded in the index for that path. Cheaper and more
		// correct than stat'ing the working tree.
		sz := exec.Command("git", "-C", repoRoot, "cat-file", "-s", ":"+path)
		szOut, err := sz.Output()
		if err != nil {
			// File may be a gitlink/submodule entry — skip silently.
			continue
		}
		var bytes int64
		if _, scanErr := parseInt64(strings.TrimSpace(string(szOut)), &bytes); scanErr != nil {
			continue
		}
		if bytes > maxTrackedBytes {
			offenders = append(offenders, formatOffender(path, bytes))
		}
	}

	if len(offenders) > 0 {
		t.Fatalf("tracked files exceed %d bytes (%d MB):\n  %s\n\nFix: `git rm --cached <file>` and add to .gitignore.",
			maxTrackedBytes, maxTrackedBytes/1024/1024,
			strings.Join(offenders, "\n  "))
	}
}

// findRepoRoot walks up from the module dir to the git root. Uses
// `git rev-parse` rather than hard-coding a relative path so the test
// survives any future directory layout change.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("git rev-parse --show-toplevel failed: %v", err)
	}
	return filepath.Clean(strings.TrimSpace(string(out)))
}

func parseInt64(s string, dst *int64) (int, error) {
	var n int64
	var consumed int
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int64(r-'0')
		consumed++
	}
	*dst = n
	return consumed, nil
}

func formatOffender(path string, size int64) string {
	mb := float64(size) / 1024.0 / 1024.0
	return pad(path, 60) + "  " + formatFloat(mb) + " MB"
}

func pad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

func formatFloat(f float64) string {
	// avoid importing strconv for a trivial 1-decimal print
	whole := int64(f)
	frac := int64((f - float64(whole)) * 10)
	return itoa(whole) + "." + itoa(frac)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}
