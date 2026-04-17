// suggest_test.go — testes unitários de suggestCommand e levenshtein.
package main

import "testing"

func TestSuggestCommand(t *testing.T) {
	cases := []struct {
		input string
		want  string // "" significa que nenhuma sugestão é esperada
	}{
		// Prefix match
		{"updat", "update"},
		{"qui", "quickstart"},
		{"docto", "doctor"},
		{"stat", "stats"},
		{"versio", "version"},
		{"ru", "run"},
		{"ana", "analyze"},
		{"con", "context-pack"},
		{"hel", "help"},
		// Levenshtein ≤ 2
		{"doctro", "doctor"},
		{"sttas", "stats"},
		{"updaet", "update"},
		// Sem sugestão para input muito distante
		{"zzzzzzzzz", ""},
		{"xyzzy", ""},
		{"", ""},
		// Case-insensitive
		{"UPDAT", "update"},
		{"Doctor", "doctor"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := suggestCommand(tc.input)
			if got != tc.want {
				t.Errorf("suggestCommand(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"abc", "ab", 1},   // 1 deletion
		{"ab", "abc", 1},   // 1 insertion
		{"abc", "axc", 1},  // 1 substitution
		{"doctor", "doctro", 2}, // transposição de 2 passos
		{"update", "updat", 1},
		{"stats", "sttas", 2},
	}
	for _, tc := range cases {
		t.Run(tc.a+"→"+tc.b, func(t *testing.T) {
			got := levenshtein(tc.a, tc.b)
			if got != tc.want {
				t.Errorf("levenshtein(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
