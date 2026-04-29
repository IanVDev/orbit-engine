// suggest.go — sugestão de comando mais próximo para erros de digitação.
//
// Algoritmo: prefix match primeiro (cobre digitação parcial), depois
// distância de Levenshtein ≤ 2 (cobre transposições e letras trocadas).
// Nenhuma dependência externa — stdlib apenas.
package main

import "strings"

// knownCommands é a lista autoritativa de subcomandos registrados.
// Deve ser mantida em sincronia com o switch em main.go.
var knownCommands = []string{
	"quickstart", "run", "stats", "analyze",
	"context-pack", "ctx", "doctor", "update", "logs", "history", "version", "help",
}

// suggestCommand retorna o comando conhecido mais próximo de input,
// ou "" se nenhum candidato for suficientemente próximo.
func suggestCommand(input string) string {
	if input == "" {
		return ""
	}
	low := strings.ToLower(input)

	// 1. Prefix match: "updat" → "update", "qui" → "quickstart".
	for _, cmd := range knownCommands {
		if strings.HasPrefix(cmd, low) {
			return cmd
		}
	}

	// 2. Levenshtein ≤ 2: "doctro" → "doctor", "sttas" → "stats".
	best, bestDist := "", 3
	for _, cmd := range knownCommands {
		if d := levenshtein(low, cmd); d < bestDist {
			best, bestDist = cmd, d
		}
	}
	return best
}

// levenshtein calcula a distância de edição mínima entre a e b.
// O(len(a)*len(b)) — aceitável para strings curtas de subcomandos.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}

	// Linha anterior e atual do DP.
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i, ca := range a {
		curr[0] = i + 1
		for j, cb := range b {
			cost := 1
			if ca == cb {
				cost = 0
			}
			del := prev[j+1] + 1
			ins := curr[j] + 1
			sub := prev[j] + cost
			curr[j+1] = min3(del, ins, sub)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
