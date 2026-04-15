// Command validate_promql enforces orbit-engine PromQL governance.
//
// Usage:
//
//	go run ./cmd/validate_promql "orbit:tokens_saved_total:prod"         → OK
//	go run ./cmd/validate_promql "orbit_skill_tokens_saved_total"       → REJECTED
//	go run ./cmd/validate_promql --strict "orbit_unknown_metric"        → REJECTED
//
// Exit 0 = query passes governance. Exit 1 = violation detected.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/IanVDev/orbit-engine/tracking"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: validate_promql [--strict] \"<promql-query>\"")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Validates a PromQL query against orbit-engine governance rules.")
		fmt.Fprintln(os.Stderr, "  --strict  also reject unknown orbit_ metrics not in the allow-list")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Examples:")
		fmt.Fprintln(os.Stderr, "  validate_promql \"orbit:tokens_saved_total:prod\"           ✓ OK")
		fmt.Fprintln(os.Stderr, "  validate_promql \"orbit_skill_tokens_saved_total\"          ✗ REJECTED")
		fmt.Fprintln(os.Stderr, "  validate_promql --strict \"orbit_unknown_metric\"           ✗ REJECTED")
		os.Exit(1)
	}

	strict := false
	var query string

	for _, arg := range args {
		if arg == "--strict" || arg == "-strict" {
			strict = true
		} else {
			query = arg
		}
	}

	if strings.TrimSpace(query) == "" {
		fmt.Fprintln(os.Stderr, "✗ REJECTED: no query provided (fail-closed)")
		os.Exit(1)
	}

	var err error
	if strict {
		err = tracking.ValidatePromQLStrict(query)
	} else {
		err = tracking.ValidatePromQL(query)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ PASSED: %q\n", query)
}
