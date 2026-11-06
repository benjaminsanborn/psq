package main

import (
	"sort"
	"strings"
	"unicode"
)

// fuzzyScore reports whether pattern's characters appear in s in order
// (case-insensitive), and how good the match is. Contiguous runs, matches at
// the start of words, and an exact substring all score higher, so "pr" ranks
// "prod" above "app-replica".
func fuzzyScore(pattern, s string) (score int, ok bool) {
	if pattern == "" {
		return 0, true
	}
	p := []rune(strings.ToLower(pattern))
	text := []rune(strings.ToLower(s))

	pi, prev := 0, -2
	for i, r := range text {
		if pi == len(p) {
			break
		}
		if r != p[pi] {
			continue
		}
		score++
		if prev == i-1 {
			score += 5 // contiguous
		}
		if i == 0 || isWordBoundary(text[i-1]) {
			score += 3 // start of a word
		}
		if pi == 0 {
			score -= min(i, 10) // prefer matches that start early
		}
		prev = i
		pi++
	}
	if pi < len(p) {
		return 0, false
	}
	if strings.Contains(string(text), string(p)) {
		score += 20
	}
	return score, true
}

func isWordBoundary(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

// fuzzyRank returns the indexes of items matching pattern, best first.
// Ties keep their original order.
func fuzzyRank(pattern string, n int, text func(i int) string) []int {
	type hit struct{ i, score int }
	var hits []hit
	for i := 0; i < n; i++ {
		if score, ok := fuzzyScore(pattern, text(i)); ok {
			hits = append(hits, hit{i, score})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool { return hits[a].score > hits[b].score })
	out := make([]int, len(hits))
	for k, h := range hits {
		out[k] = h.i
	}
	return out
}
