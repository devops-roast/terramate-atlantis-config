package atlantis

import (
	"bytes"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

func Diff(oldContent, newContent []byte, filename string) (string, bool) {
	if bytes.Equal(oldContent, newContent) {
		return "", false
	}

	oldLines := splitLines(string(oldContent))
	newLines := splitLines(string(newContent))

	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        oldLines,
		B:        newLines,
		FromFile: filename,
		ToFile:   filename,
		Context:  3,
	})
	if err != nil {
		return strings.Join([]string{"--- " + filename, "+++ " + filename, "@@", "-diff generation failed", "+diff generation failed", ""}, "\n"), true
	}

	return diff, true
}

// splitLines splits s into lines that keep their newline. Unlike
// difflib.SplitLines it adds no phantom empty line at the end, and a last
// line without a newline is marked the way diff(1) does, so a missing final
// newline shows up as a change instead of as an added blank line.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if last := lines[len(lines)-1]; last == "" {
		lines = lines[:len(lines)-1]
	} else {
		lines[len(lines)-1] = last + "\n\\ No newline at end of file\n"
	}
	return lines
}
