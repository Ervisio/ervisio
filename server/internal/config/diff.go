package config

import "strings"

// Diff returns a unified-style line diff ("-"/"+"/" " prefixes, no hunk
// headers) between two texts. It is meant for small files such as the config.
func Diff(a, b string) string {
	x := splitLines(a)
	y := splitLines(b)
	// Longest common subsequence table; config files are tiny.
	n, m := len(x), len(y)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var sb strings.Builder
	i, j := 0, 0
	for i < n || j < m {
		switch {
		case i < n && j < m && x[i] == y[j]:
			sb.WriteString(" " + x[i] + "\n")
			i++
			j++
		case i < n && (j == m || lcs[i+1][j] >= lcs[i][j+1]):
			sb.WriteString("-" + x[i] + "\n")
			i++
		default:
			sb.WriteString("+" + y[j] + "\n")
			j++
		}
	}
	return sb.String()
}

func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
