package atlantis

import (
	"strings"
	"testing"
)

func TestDiffMissingFinalNewline(t *testing.T) {
	t.Parallel()

	diff, changed := Diff([]byte("a\nb"), []byte("a\nb\n"), "atlantis.yaml")
	if !changed {
		t.Fatal("expected a change")
	}
	want := "-b\n\\ No newline at end of file\n+b\n"
	if !strings.Contains(diff, want) {
		t.Fatalf("diff must show the missing final newline:\n%s", diff)
	}
	if strings.Contains(diff, "\n+\n") {
		t.Fatalf("diff must not show a phantom added blank line:\n%s", diff)
	}
}

func TestDiffLines(t *testing.T) {
	t.Parallel()

	diff, changed := Diff([]byte("a\nb\n"), []byte("a\nc\n"), "f")
	if !changed {
		t.Fatal("expected a change")
	}
	if !strings.HasSuffix(diff, " a\n-b\n+c\n") {
		t.Fatalf("unexpected diff:\n%q", diff)
	}
	if diff, changed := Diff([]byte("x\n"), []byte("x\n"), "f"); changed || diff != "" {
		t.Fatalf("identical content: got %q, %v", diff, changed)
	}
	if diff, _ := Diff(nil, []byte("x\n"), "f"); !strings.HasSuffix(diff, "@@ -0,0 +1 @@\n+x\n") {
		t.Fatalf("missing file: got %q", diff)
	}
}
