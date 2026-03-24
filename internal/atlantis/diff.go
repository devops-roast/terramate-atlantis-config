package atlantis

import (
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

func Diff(oldContent, newContent []byte, filename string) (string, bool) {
	if string(oldContent) == string(newContent) {
		return "", false
	}

	oldLines := difflib.SplitLines(string(oldContent))
	newLines := difflib.SplitLines(string(newContent))

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
