// bind.go — Helper for fail-closed network bind resolution.
//
// Threat model: binding on 0.0.0.0 exposes the process to every host that
// can reach the machine's interfaces. In a developer workstation on a
// corporate LAN, this leaks /metrics and (for /track) accepts forged
// events from any peer. Default must be loopback; opening up requires an
// explicit operator opt-in.
package tracking

import (
	"os"
	"strings"
)

// BindAllEnv is the environment variable that, when set to "1", allows
// the process to bind on all interfaces. Any other value (including
// unset) forces the loopback host 127.0.0.1.
const BindAllEnv = "ORBIT_BIND_ALL"

// ResolveListenAddr normalizes a listen address against the fail-closed
// default. If the requested address has no host component (e.g. ":9100"),
// 127.0.0.1 is prepended unless ORBIT_BIND_ALL=1 is set, in which case the
// address is returned unchanged (listen on all interfaces).
//
// If the caller passes an explicit host (e.g. "10.0.0.5:9100" or
// "127.0.0.1:9100"), it is returned unchanged — the caller has made a
// deliberate choice and we do not second-guess it.
func ResolveListenAddr(requested string) string {
	if !strings.HasPrefix(requested, ":") {
		return requested
	}
	if os.Getenv(BindAllEnv) == "1" {
		return requested
	}
	return "127.0.0.1" + requested
}
