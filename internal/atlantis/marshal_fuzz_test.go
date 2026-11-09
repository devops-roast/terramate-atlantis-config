package atlantis

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

	"github.com/devops-roast/terramate-atlantis-config/internal/discovery"
)

// trailingWhitespaceFixer mimics pre-commit's trailing-whitespace hook: every
// line loses its trailing ASCII whitespace (Python's bytes.rstrip()), keeping
// its "\n" or "\r\n" ending.
func trailingWhitespaceFixer(in []byte) []byte {
	lines := bytes.SplitAfter(in, []byte("\n"))
	var out bytes.Buffer
	for _, line := range lines {
		eol := []byte{}
		switch {
		case bytes.HasSuffix(line, []byte("\r\n")):
			eol, line = []byte("\r\n"), line[:len(line)-2]
		case bytes.HasSuffix(line, []byte("\n")):
			eol, line = []byte("\n"), line[:len(line)-1]
		}
		out.Write(bytes.TrimRight(line, " \t\n\r\v\f"))
		out.Write(eol)
	}
	return out.Bytes()
}

// endOfFileFixer mimics pre-commit's end-of-file-fixer hook: a non-empty file
// ends with exactly one line break.
func endOfFileFixer(in []byte) []byte {
	if len(in) == 0 {
		return in
	}
	body := bytes.TrimRight(in, "\r\n")
	if len(body) == 0 {
		return nil
	}
	tail := in[len(body):]
	for _, eol := range []string{"\n", "\r\n", "\r"} {
		if strings.HasPrefix(string(tail), eol) {
			return append(append([]byte{}, body...), eol...)
		}
	}
	return append(append([]byte{}, body...), '\n')
}

// FuzzMarshalYAMLSurvivesTextFixers checks that a generated file is left
// unchanged by the pre-commit text fixers users commonly run next to the
// generator hook (trailing-whitespace, end-of-file-fixer, mixed-line-ending).
// Otherwise the fixer rewrites the file, `--check` reports it as stale on
// every run, and the generator and the fixer keep undoing each other. Values
// come from config file workflows, so they are arbitrary strings. Each value
// is tested both in the middle and at the very end of the file.
func FuzzMarshalYAMLSurvivesTextFixers(f *testing.F) {
	for _, seed := range []string{
		"echo hi", "echo hi ", "echo a\t\necho b", "echo hi\n", "echo hi\n\n", "\n", "\n\n", " \n",
		"a\r\nb", "a\rb", "a\vb\n", "a\fb", "a \n\n", "\t", "x\n \n", "\ufeffx", "a\u2028b", "a\x85b\n",
		" echo a\necho b\n", "\techo a\necho b", "\nfirst line is empty",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, value string) {
		if !utf8.ValidString(value) {
			t.Skip()
		}
		cfg, err := Generate([]discovery.StackInfo{{Dir: "a"}}, Options{CreateProjectName: true})
		if err != nil {
			t.Fatal(err)
		}
		cfg.Workflows = map[string]any{
			"a": map[string]any{"plan": map[string]any{"steps": []any{
				map[string]any{"run": value},
				map[string]any{"run": "echo middle"},
			}}},
			"z": map[string]any{"plan": map[string]any{"steps": []any{
				map[string]any{"run": value},
			}}},
		}

		out, err := MarshalYAML(cfg, true)
		if err != nil {
			t.Fatal(err)
		}

		var back Config
		if err := yaml.Unmarshal(out, &back); err != nil {
			t.Fatalf("output does not parse: %v\n%s", err, out)
		}
		if !reflect.DeepEqual(back.Workflows, cfg.Workflows) {
			t.Fatalf("round trip changed %q:\n%s", value, out)
		}

		if fixed := endOfFileFixer(trailingWhitespaceFixer(out)); !bytes.Equal(fixed, out) {
			t.Fatalf("text fixers change the output for %q:\n--- generated\n%s\n--- fixed\n%s", value, out, fixed)
		}
		// mixed-line-ending rewrites files that use more than one kind of
		// line ending; the output must only ever use "\n".
		if bytes.ContainsRune(out, '\r') {
			t.Fatalf("output for %q contains a raw carriage return:\n%q", value, out)
		}
	})
}
