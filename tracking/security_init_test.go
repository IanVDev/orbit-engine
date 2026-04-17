// security_init_test.go — verifies the init()-time fail-closed gate for
// ORBIT_HMAC_SECRET in production. Runs the test binary as a subprocess
// so we can observe the log.Fatalf exit without tearing down the parent.
package tracking

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestSecurity_FailClosedOnMissingHMACInProduction asserts that importing
// package tracking with ORBIT_ENV=production and no ORBIT_HMAC_SECRET
// terminates the process with a non-zero exit and a recognizable message.
//
// Rationale: init() runs on import, so we cannot exercise this path in the
// same process. Re-exec the test binary with the hostile env and inspect
// the exit status + stderr.
func TestSecurity_FailClosedOnMissingHMACInProduction(t *testing.T) {
	if os.Getenv("ORBIT_TEST_SUBPROCESS") == "1" {
		// Unreachable in practice: init() already called log.Fatalf.
		// Guard here so if someone ever refactors the gate to a no-op,
		// the subprocess exits 0 and the parent test flags the regression.
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestSecurity_FailClosedOnMissingHMACInProduction")
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"ORBIT_TEST_SUBPROCESS=1",
		"ORBIT_ENV=production",
		// ORBIT_HMAC_SECRET intentionally absent.
	}

	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit when ORBIT_HMAC_SECRET is empty in production; got success.\noutput:\n%s", out)
	}
	if !strings.Contains(string(out), "ORBIT_HMAC_SECRET is required") {
		t.Fatalf("expected fail-closed message in output, got:\n%s", out)
	}
}
