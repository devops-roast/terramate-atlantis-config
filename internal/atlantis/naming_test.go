package atlantis

import (
	"reflect"
	"runtime"
	"sort"
	"testing"

	"github.com/devops-roast/terramate-atlantis-config/internal/discovery"
)

func TestValidProjectName(t *testing.T) {
	t.Parallel()

	// Mirrors Atlantis' validProjectName: URL-safe once '/' becomes '-'.
	valid := []string{"app", "prod-vpc", "dev-v1.2", "a_b", "x~y", "stacks/aws/vpc", "A-Z-0-9"}
	invalid := []string{"", "my stack", "a+b", "a:b", "a*b", "a?b", "a[b]", "a@b", "ü"}

	for _, name := range valid {
		if !ValidProjectName(name) {
			t.Errorf("ValidProjectName(%q) = false, want true", name)
		}
	}
	for _, name := range invalid {
		if ValidProjectName(name) {
			t.Errorf("ValidProjectName(%q) = true, want false", name)
		}
	}
}

func TestSanitizeProjectName(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"stacks/aws/vpc": "stacks-aws-vpc",
		"dev/v1.2":       "dev-v1.2",
		"dev/my stack":   "dev-my-stack",
		"/leading/slash": "leading-slash",
		"a+b:c":          "a-b-c",
		"ünïcode/x":      "n-code-x",
	}

	for in, want := range cases {
		got := sanitizeProjectName(in)
		if got != want {
			t.Errorf("sanitizeProjectName(%q) = %q, want %q", in, got, want)
		}
		if !ValidProjectName(got) {
			t.Errorf("sanitizeProjectName(%q) = %q is not a valid Atlantis name", in, got)
		}
	}
}

func TestStripPathPrefix(t *testing.T) {
	t.Parallel()

	cases := []struct{ path, prefix, want string }{
		{"stacks/aws/prod/app", "stacks/aws", "prod/app"},
		{"stacks/aws/prod/app", "stacks/aws/", "prod/app"},
		{"stacks/aws/prod/app", "/stacks/aws", "prod/app"},
		{"stacks/aws/prod/app", "stacks/aws/pro", "stacks/aws/prod/app"},
		{"stacks/aws-legacy/app", "stacks/aws", "stacks/aws-legacy/app"},
		{"stacks/aws", "stacks/aws", ""},
		{"stacks/aws", "", "stacks/aws"},
	}

	for _, tc := range cases {
		if got := stripPathPrefix(tc.path, tc.prefix); got != tc.want {
			t.Errorf("stripPathPrefix(%q, %q) = %q, want %q", tc.path, tc.prefix, got, tc.want)
		}
	}
}

func TestDetectCommonPrefix(t *testing.T) {
	t.Parallel()

	cases := []struct {
		dirs []string
		want string
	}{
		{[]string{"stacks/aws/prod/vpc", "stacks/aws/dev/vpc"}, "stacks/aws"},
		{[]string{"stacks/aws/dev/vpc", "stacks/aws/prod/vpc", "."}, "stacks/aws"},
		{[]string{"stacks/aws/prod", "stacks/aws/production"}, "stacks/aws"},
		{[]string{"a/b", "c/d"}, ""},
		{[]string{"."}, ""},
		{nil, ""},
	}

	for _, tc := range cases {
		stacks := make([]discovery.StackInfo, 0, len(tc.dirs))
		for _, dir := range tc.dirs {
			stacks = append(stacks, discovery.StackInfo{Dir: dir})
		}
		if got := detectCommonPrefix(stacks); got != tc.want {
			t.Errorf("detectCommonPrefix(%v) = %q, want %q", tc.dirs, got, tc.want)
		}
	}
}

func TestEnsureUniqueNames(t *testing.T) {
	t.Parallel()

	names := map[string]string{
		"a/b": "a-b",
		"a-b": "a-b",
		"x/y": "y",
		"z/y": "z-y",
	}
	ensureUniqueNames(names)

	want := map[string]string{
		"a-b": "a-b",
		"a/b": "a-b-2",
		"x/y": "y",
		"z/y": "z-y",
	}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("ensureUniqueNames: got %v, want %v", names, want)
	}
}

func TestProjectNamesAreUniqueAcrossStrategies(t *testing.T) {
	t.Parallel()

	// "a/b" and "a-b" sanitize to the same name, and disambiguating the
	// two "vpc" stacks produces "prod-vpc", which clashes with "prod-vpc".
	stacks := []discovery.StackInfo{
		{Dir: "a/b"},
		{Dir: "a-b"},
		{Dir: "us/prod/vpc"},
		{Dir: "eu/prod/vpc"},
		{Dir: "prod-vpc"},
		{Dir: "."},
		{Dir: "root"},
	}

	for _, strategy := range []string{NameStrategyAutoStrip, NameStrategyStackName, NameStrategyLastN, NameStrategyFull} {
		t.Run(strategy, func(t *testing.T) {
			t.Parallel()

			cfg := mustGenerate(t, stacks, Options{
				CreateProjectName:   true,
				ProjectNameStrategy: strategy,
				ProjectNameDepth:    1,
			})

			seen := map[string]string{}
			for _, p := range cfg.Projects {
				if other, dup := seen[p.Name]; dup {
					t.Fatalf("duplicate name %q for %s and %s", p.Name, other, p.Dir)
				}
				seen[p.Name] = p.Dir
				if !ValidProjectName(p.Name) {
					t.Fatalf("invalid name %q for %s", p.Name, p.Dir)
				}
			}
		})
	}
}

func TestProjectNamesStableWithNamingStacks(t *testing.T) {
	t.Parallel()

	all := []discovery.StackInfo{
		{Dir: "stacks/aws/acme-production/eu-west-1/acm", Name: "acm"},
		{Dir: "stacks/aws/acme-production/eu-west-1/dns", Name: "dns"},
		{Dir: "stacks/aws/acme-development/eu-west-1/acm", Name: "acm"},
		{Dir: "stacks/aws/acme-development/eu-west-1/dns", Name: "dns"},
		{Dir: "stacks/gcp/dev/acm", Name: "acm", Skip: true},
	}

	for _, strategy := range []string{NameStrategyAutoStrip, NameStrategyStackName, NameStrategyLastN, NameStrategyFull} {
		t.Run(strategy, func(t *testing.T) {
			t.Parallel()

			opts := Options{CreateProjectName: true, ProjectNameStrategy: strategy, ProjectNameDepth: 1, NamingStacks: all}
			full := namesByDirOf(mustGenerate(t, all, opts))

			for i := range all[:4] {
				subset := mustGenerate(t, all[i:i+1], opts)
				if len(subset.Projects) != 1 {
					t.Fatalf("expected 1 project, got %d", len(subset.Projects))
				}
				p := subset.Projects[0]
				if p.Name != full[p.Dir] {
					t.Fatalf("name of %s changed when filtered: got %q, full run %q", p.Dir, p.Name, full[p.Dir])
				}
			}
		})
	}

	// Without NamingStacks the lone stack is named from its full path,
	// which is exactly the instability NamingStacks fixes.
	alone := mustGenerate(t, all[2:3], Options{CreateProjectName: true})
	if alone.Projects[0].Name != "stacks-aws-acme-development-eu-west-1-acm" {
		t.Fatalf("unexpected lone stack name: %q", alone.Projects[0].Name)
	}
	withCtx := mustGenerate(t, all[2:3], Options{CreateProjectName: true, NamingStacks: all})
	if withCtx.Projects[0].Name != "acme-development-eu-west-1-acm" {
		t.Fatalf("unexpected name with naming context: %q", withCtx.Projects[0].Name)
	}
}

func TestSkippedStacksDoNotAffectNames(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "stacks/legacy/app", Skip: true},
		{Dir: "stacks/aws/prod/vpc"},
		{Dir: "stacks/aws/dev/vpc"},
	}

	cfg := mustGenerate(t, stacks, Options{CreateProjectName: true, NamingStacks: stacks})
	got := projectNames(cfg)
	want := []string{"dev-vpc", "prod-vpc"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("names: got %v, want %v", got, want)
	}
}

func TestRootStackNaming(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: ".", Name: "bootstrap"},
		{Dir: "stacks/aws/prod/vpc", Name: "vpc"},
		{Dir: "stacks/aws/dev/vpc", Name: "vpc"},
	}

	cases := map[string]string{
		NameStrategyAutoStrip: "root",
		NameStrategyFull:      "root",
		NameStrategyLastN:     "root",
		NameStrategyStackName: "bootstrap",
	}

	for strategy, want := range cases {
		cfg := mustGenerate(t, stacks, Options{CreateProjectName: true, CreateWorkspace: true, ProjectNameStrategy: strategy})
		root := cfg.Projects[0]
		if root.Dir != "." {
			t.Fatalf("%s: root project dir: got %q, want %q", strategy, root.Dir, ".")
		}
		if root.Name != want {
			t.Fatalf("%s: root project name: got %q, want %q", strategy, root.Name, want)
		}
		if root.Workspace != "" {
			t.Fatalf("%s: root project must keep the default workspace, got %q", strategy, root.Workspace)
		}
	}

	// The root stack doesn't count towards the common prefix.
	cfg := mustGenerate(t, stacks, Options{CreateProjectName: true})
	if got := sortedNames(cfg); !reflect.DeepEqual(got, []string{"dev-vpc", "prod-vpc", "root"}) {
		t.Fatalf("names: got %v", got)
	}

	// An empty Dir (as older discovery code produced) is treated as root.
	cfg = mustGenerate(t, []discovery.StackInfo{{Dir: ""}}, Options{CreateProjectName: true})
	if cfg.Projects[0].Dir != "." || cfg.Projects[0].Name != "root" {
		t.Fatalf("empty dir: got %+v", cfg.Projects[0])
	}
}

func TestProjectNamePrefixSegmentBoundary(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{
		{Dir: "stacks/aws/prod/app", Name: "app"},
		{Dir: "stacks/aws/production/db", Name: "db"},
		{Dir: "stacks/aws/prod", Name: "prod"},
	}

	for _, strategy := range []string{NameStrategyAutoStrip, NameStrategyStackName} {
		cfg := mustGenerate(t, stacks, Options{
			CreateProjectName:   true,
			ProjectNameStrategy: strategy,
			ProjectNamePrefix:   "stacks/aws/prod",
		})
		names := namesByDirOf(cfg)
		if names["stacks/aws/prod/app"] != "app" {
			t.Errorf("%s: prod/app: got %q, want %q", strategy, names["stacks/aws/prod/app"], "app")
		}
		if got := names["stacks/aws/production/db"]; got == "uction-db" || got == "uction/db" {
			t.Errorf("%s: prefix cut a segment in half: %q", strategy, got)
		}
		if names["stacks/aws/prod"] == "" {
			t.Errorf("%s: stack equal to the prefix got an empty name", strategy)
		}
	}

	cfg := mustGenerate(t, stacks, Options{CreateProjectName: true, ProjectNamePrefix: "stacks/aws/prod"})
	names := namesByDirOf(cfg)
	if names["stacks/aws/production/db"] != "stacks-aws-production-db" {
		t.Fatalf("auto-strip production/db: got %q", names["stacks/aws/production/db"])
	}
	if names["stacks/aws/prod"] != "stacks-aws-prod" {
		t.Fatalf("auto-strip prod: got %q", names["stacks/aws/prod"])
	}
}

func namesByDirOf(cfg Config) map[string]string {
	names := make(map[string]string, len(cfg.Projects))
	for _, p := range cfg.Projects {
		names[p.Dir] = p.Name
	}
	return names
}

func sortedNames(cfg Config) []string {
	names := projectNames(cfg)
	sort.Strings(names)
	return names
}

// On Linux and macOS a backslash is part of a directory name, so a stack in
// `a\b` must keep that dir instead of pointing Atlantis at a nonexistent
// `a/b`.
func TestBackslashInDirIsNotASeparator(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("backslash is the path separator on Windows")
	}

	cfg, err := Generate([]discovery.StackInfo{{Dir: `a\b`}, {Dir: "c"}}, Options{CreateProjectName: true})
	if err != nil {
		t.Fatal(err)
	}
	dirs := []string{cfg.Projects[0].Dir, cfg.Projects[1].Dir}
	if want := []string{`a\b`, "c"}; !reflect.DeepEqual(dirs, want) {
		t.Fatalf("dirs: got %q, want %q", dirs, want)
	}
	for _, p := range cfg.Projects {
		if !ValidProjectName(p.Name) {
			t.Fatalf("invalid name %q for %q", p.Name, p.Dir)
		}
	}
}

// Disambiguation adds parent segments to the strategy's own name. It must
// never drop the stack name (stack-name) or shorten a name below what the
// strategy produces (full, last-n).
func TestDisambiguationKeepsStrategyName(t *testing.T) {
	t.Parallel()

	names := func(stacks []discovery.StackInfo, strategy string, depth int) map[string]string {
		t.Helper()
		cfg, err := Generate(stacks, Options{CreateProjectName: true, ProjectNameStrategy: strategy, ProjectNameDepth: depth})
		if err != nil {
			t.Fatal(err)
		}
		return namesByDirOf(cfg)
	}

	got := names([]discovery.StackInfo{
		{Dir: "stacks/prod/eu/main", Name: "vpc"},
		{Dir: "stacks/prod/eu/backup", Name: "vpc"},
		{Dir: "stacks/x"},
	}, NameStrategyStackName, 0)
	want := map[string]string{
		"stacks/prod/eu/main":   "stacks-prod-eu-main",
		"stacks/prod/eu/backup": "stacks-prod-eu-backup",
		"stacks/x":              "stacks-x",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stack-name: got %v, want %v", got, want)
	}

	got = names([]discovery.StackInfo{
		{Dir: "org/a/stacks/prod/eu/vpc", Name: "net"},
		{Dir: "org/b/stacks/prod/eu/vpc", Name: "net"},
	}, NameStrategyStackName, 0)
	want = map[string]string{
		"org/a/stacks/prod/eu/vpc": "a-stacks-prod-eu-net",
		"org/b/stacks/prod/eu/vpc": "b-stacks-prod-eu-net",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stack-name with more context: got %v, want %v", got, want)
	}

	got = names([]discovery.StackInfo{{Dir: "stacks/a/b"}, {Dir: "stacks/a-b"}, {Dir: "stacks/c"}}, NameStrategyFull, 0)
	want = map[string]string{"stacks/a-b": "stacks-a-b", "stacks/a/b": "stacks-a-b-2", "stacks/c": "stacks-c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("full: got %v, want %v", got, want)
	}

	// "q/b/c" and "b-c" both give "b-c" for depth 2.
	got = names([]discovery.StackInfo{{Dir: "q/b/c"}, {Dir: "b-c"}}, NameStrategyLastN, 2)
	want = map[string]string{"q/b/c": "q-b-c", "b-c": "b-c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("last-n: got %v, want %v", got, want)
	}
}

// The prefix is a path like --filter-path: "./stacks", "/stacks/" and
// "stacks" strip the same segments.
func TestProjectNamePrefixIsNormalized(t *testing.T) {
	t.Parallel()

	stacks := []discovery.StackInfo{{Dir: "stacks/a"}, {Dir: "stacks/b/c"}}
	want := map[string]string{"stacks/a": "a", "stacks/b/c": "b-c"}
	for _, prefix := range []string{"stacks", "./stacks", "/stacks/", "stacks//", "./stacks/./"} {
		for _, strategy := range []string{NameStrategyAutoStrip, NameStrategyStackName} {
			cfg := mustGenerate(t, stacks, Options{CreateProjectName: true, ProjectNameStrategy: strategy, ProjectNamePrefix: prefix})
			if got := namesByDirOf(cfg); !reflect.DeepEqual(got, want) {
				t.Errorf("%s, prefix %q: got %v, want %v", strategy, prefix, got, want)
			}
		}
	}
}
