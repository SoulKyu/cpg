// Package diff renders unified diffs for YAML documents. Used by the dry-run
// mode of `cpg generate` and `cpg replay` to preview what would change on
// disk without writing.
package diff

import (
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// UnifiedYAML returns a unified diff between a and b, labeled with aName and
// bName. When color is true, '+' lines are wrapped in green ANSI and '-' lines
// in red; header lines ('+++', '---') are left uncolored. Returns the empty
// string when a and b are identical.
func UnifiedYAML(aName, bName string, a, b []byte, color bool) (string, error) {
	if string(a) == string(b) {
		return "", nil
	}
	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(a)),
		B:        difflib.SplitLines(string(b)),
		FromFile: aName,
		ToFile:   bName,
		Context:  3,
	}
	out, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return "", err
	}
	if !color {
		return out, nil
	}
	return colorize(out), nil
}

func colorize(s string) string {
	const (
		red   = "\x1b[31m"
		green = "\x1b[32m"
		reset = "\x1b[0m"
	)
	var b strings.Builder
	b.Grow(len(s))
	for _, line := range strings.SplitAfter(s, "\n") {
		switch {
		case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
			b.WriteString(line)
		case strings.HasPrefix(line, "+"):
			b.WriteString(green)
			b.WriteString(line)
			b.WriteString(reset)
		case strings.HasPrefix(line, "-"):
			b.WriteString(red)
			b.WriteString(line)
			b.WriteString(reset)
		default:
			b.WriteString(line)
		}
	}
	return b.String()
}
