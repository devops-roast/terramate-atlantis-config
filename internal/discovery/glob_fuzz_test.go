package discovery

import (
	"path"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzGlobToRegexpMatchesPathMatch checks that, for patterns without "**",
// the regular expression translation behaves exactly like path.Match:
// same malformed patterns, same matches. Patterns that contain "**" use the
// translation, so classes, escapes and wildcards must mean the same thing
// with or without a "**" elsewhere in the pattern.
func FuzzGlobToRegexpMatchesPathMatch(f *testing.F) {
	for _, seed := range [][2]string{
		{"[ab]pp", "app"}, {"[^a]pp", "bpp"}, {"[a-c]", "b"}, {"[c-a]", "b"},
		{`\*`, "*"}, {`a\`, "a"}, {"[", "a"}, {"[]a]", "]"}, {"[-]", "-"},
		{"*/vpc", "a/vpc"}, {"a?c", "a/c"}, {"a.b+c", "a.b+c"}, {"(x|y)", "x"},
		{"[^/]", "/"}, {`[\]]`, "]"}, {"ü*", "über"}, {"[!a]", "!"},
	} {
		f.Add(seed[0], seed[1])
	}

	f.Fuzz(func(t *testing.T, pattern, dir string) {
		if strings.Contains(pattern, "**") || !utf8.ValidString(pattern) || !utf8.ValidString(dir) {
			t.Skip()
		}
		want, wantErr := path.Match(pattern, dir)
		expr, err := globToRegexp(pattern)
		if (err != nil) != (wantErr != nil) {
			t.Fatalf("pattern %q: path.Match error %v, translation error %v", pattern, wantErr, err)
		}
		if err != nil {
			return
		}
		re, err := regexp.Compile(expr)
		if err != nil {
			t.Fatalf("pattern %q: invalid regexp %q: %v", pattern, expr, err)
		}
		if got := re.MatchString(dir); got != want {
			t.Fatalf("pattern %q (regexp %q) on %q: got %v, path.Match %v", pattern, expr, dir, got, want)
		}
	})
}

// FuzzTrailingDoubleStar checks that "P/**" matches a dir exactly when P
// matches the dir itself or one of its parents, whatever wildcards P has.
func FuzzTrailingDoubleStar(f *testing.F) {
	for _, seed := range [][2]string{
		{"*/vpc", "prod/vpc/subnet"}, {"infra", "infra"}, {"[ab]", "a/x"},
		{"?", "a"}, {"a/*", "a/b/c"}, {"*", "x"},
	} {
		f.Add(seed[0], seed[1])
	}

	f.Fuzz(func(t *testing.T, pattern, dir string) {
		pattern = normalizePathPattern(pattern)
		dir = strings.Trim(path.Clean("/"+dir), "/")
		if pattern == "" || pattern == "." || dir == "" || strings.Contains(pattern, "**") ||
			!utf8.ValidString(pattern) || !utf8.ValidString(dir) {
			t.Skip()
		}
		if _, err := path.Match(pattern, ""); err != nil {
			t.Skip()
		}

		want := false
		for d := dir; ; d = path.Dir(d) {
			if ok, _ := path.Match(pattern, d); ok {
				want = true
				break
			}
			if !strings.Contains(d, "/") {
				break
			}
		}

		got, err := matchesPathFilter(pattern+"/**", dir)
		if err != nil {
			t.Fatalf("%q: %v", pattern+"/**", err)
		}
		if got != want {
			t.Fatalf("%q on %q: got %v, want %v", pattern+"/**", dir, got, want)
		}
	})
}

// Regression cases for path filters that matched nothing.
func TestPathFilterNormalizationAndWildcards(t *testing.T) {
	t.Parallel()

	cases := []struct {
		pattern, dir string
		want         bool
	}{
		// Leading "./", trailing and doubled slashes.
		{"./infra", "infra/vpc", true},
		{"infra/", "infra/vpc", true},
		{"./infra/", "infra", true},
		{"infra//vpc", "infra/vpc", true},
		{"./", ".", true},
		{"./", "infra", false},
		// Wildcards before a trailing "/**".
		{"*/rds/**", "infra/rds", true},
		{"*/rds/**", "infra/rds/replica", true},
		{"*/rds/**", "rds", false},
		{"[ai]*/**", "infra/vpc", true},
		// Classes and escapes in patterns containing "**".
		{"**/[r]ds", "infra/rds", true},
		{"**/[^r]ds", "infra/rds", false},
		{`**/a\*b`, "x/a*b", true},
		{`**/a\*b`, "x/axb", false},
		{"**/v.c", "infra/vxc", false},
	}

	for _, tc := range cases {
		got, err := matchesPathOrParent(tc.pattern, tc.dir)
		if err != nil {
			t.Fatalf("matchesPathOrParent(%q, %q): %v", tc.pattern, tc.dir, err)
		}
		if got != tc.want {
			t.Errorf("matchesPathOrParent(%q, %q) = %v, want %v", tc.pattern, tc.dir, got, tc.want)
		}
	}

	// The root stack's dir "." is a placeholder, not a directory named ".":
	// only "." itself (or a pattern matching the empty path, such as "**")
	// selects it, so excluding hidden dirs with ".*" keeps the root stack.
	for _, tc := range []struct {
		pattern string
		want    bool
	}{
		{".", true}, {"./", true}, {"**", true}, {"**/*", true},
		{".*", false}, {"?", false}, {"[.]", false}, {"**/.*", false}, {"\\.", false},
	} {
		got, err := matchesPathOrParent(tc.pattern, RootDir)
		if err != nil {
			t.Fatalf("matchesPathOrParent(%q, root): %v", tc.pattern, err)
		}
		if got != tc.want {
			t.Errorf("matchesPathOrParent(%q, root) = %v, want %v", tc.pattern, got, tc.want)
		}
	}

	if _, err := matchesPathFilter("**/[a", "x"); err == nil {
		t.Errorf("malformed class in a ** pattern must be an error, as it is without **")
	}
}
