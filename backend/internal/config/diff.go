package config

import "strings"

// maxDiffLines bounds the O(n·m) table; managed files have a few dozen lines.
const maxDiffLines = 400

// DiffLine is one line of a line diff: Op is " " (both), "-" (only in a) or
// "+" (only in b).
type DiffLine struct {
	Op   string `json:"op"`
	Text string `json:"text"`
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// Lines is a longest-common-subsequence line diff turning a into b. Inputs
// longer than maxDiffLines return tooLarge.
func Lines(a, b string) (out []DiffLine, tooLarge bool) {
	x, y := splitLines(a), splitLines(b)
	if len(x) > maxDiffLines || len(y) > maxDiffLines {
		return nil, true
	}
	// l[i][j] = LCS length of x[i:] and y[j:].
	l := make([][]int, len(x)+1)
	for i := range l {
		l[i] = make([]int, len(y)+1)
	}
	for i := len(x) - 1; i >= 0; i-- {
		for j := len(y) - 1; j >= 0; j-- {
			if x[i] == y[j] {
				l[i][j] = l[i+1][j+1] + 1
			} else {
				l[i][j] = max(l[i+1][j], l[i][j+1])
			}
		}
	}
	out = []DiffLine{}
	i, j := 0, 0
	for i < len(x) && j < len(y) {
		switch {
		case x[i] == y[j]:
			out = append(out, DiffLine{" ", x[i]})
			i, j = i+1, j+1
		case l[i+1][j] >= l[i][j+1]:
			out = append(out, DiffLine{"-", x[i]})
			i++
		default:
			out = append(out, DiffLine{"+", y[j]})
			j++
		}
	}
	for ; i < len(x); i++ {
		out = append(out, DiffLine{"-", x[i]})
	}
	for ; j < len(y); j++ {
		out = append(out, DiffLine{"+", y[j]})
	}
	return out, false
}
