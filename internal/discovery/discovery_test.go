package discovery

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDiscoverPathFilterAndStackMetadata(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "terramate.tm.hcl"), "terramate {}\n")
	writeFile(t, filepath.Join(root, "stacks/network/stack.tm.hcl"), `stack {
  name   = "network"
  after  = ["../bootstrap"]
  before = ["../app"]
}

globals {
  atlantis_apply_requirements = ["approved", "mergeable"]
}
`)
	writeFile(t, filepath.Join(root, "stacks/app/stack.tm.hcl"), `stack {
  name = "app"
}
`)

	result, err := Discover(DiscoverOptions{
		RootDir:    root,
		PathFilter: "stacks/network/**",
	})
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}
	stacks := result.Stacks

	if len(stacks) != 1 {
		t.Fatalf("expected 1 stack, got %d", len(stacks))
	}

	stack := stacks[0]
	if stack.Dir != "stacks/network" {
		t.Fatalf("unexpected dir: got %q", stack.Dir)
	}

	if !equalStrings(stack.After, []string{"../bootstrap"}) {
		t.Fatalf("unexpected after: got %#v", stack.After)
	}

	if !equalStrings(stack.Before, []string{"../app"}) {
		t.Fatalf("unexpected before: got %#v", stack.Before)
	}

	if !equalStrings(stack.ApplyRequirements, []string{"approved", "mergeable"}) {
		t.Fatalf("unexpected apply requirements: got %#v", stack.ApplyRequirements)
	}
}

func TestDiscoverChangedFiltersStacks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "terramate.tm.hcl"), "terramate {}\n")
	writeFile(t, filepath.Join(root, "stacks/unchanged/stack.tm.hcl"), "stack { name = \"unchanged\" }\n")
	writeFile(t, filepath.Join(root, "stacks/changed/stack.tm.hcl"), "stack { name = \"changed\" }\n")

	runCmd(t, root, "git", "init", "-b", "main")
	runCmd(t, root, "git", "add", ".")
	runCmd(t, root, "git", "-c", "user.name=test", "-c", "user.email=test@test.com", "commit", "-m", "init")

	writeFile(t, filepath.Join(root, "stacks/changed/locals.tf"), "locals { value = 1 }\n")

	result, err := Discover(DiscoverOptions{
		RootDir: root,
		Changed: true,
		BaseRef: "main",
	})
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}
	stacks := result.Stacks

	if len(stacks) != 1 {
		t.Fatalf("expected 1 changed stack, got %d", len(stacks))
	}

	if stacks[0].Dir != "stacks/changed" {
		t.Fatalf("unexpected changed stack: got %q", stacks[0].Dir)
	}
}

func TestDiscoverExcludeFilters(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "terramate.tm.hcl"), "terramate {}\n")
	writeFile(t, filepath.Join(root, "stacks/prod/stack.tm.hcl"), `stack {
  name = "prod"
  tags = ["prod"]
}
`)
	writeFile(t, filepath.Join(root, "stacks/dev/stack.tm.hcl"), `stack {
  name = "dev"
  tags = ["dev"]
}
`)
	writeFile(t, filepath.Join(root, "apps/web/stack.tm.hcl"), `stack {
  name = "web"
  tags = ["prod", "web"]
}
`)

	testcases := []struct {
		name       string
		opts       DiscoverOptions
		wantStacks []string
	}{
		{
			name: "exclude tags",
			opts: DiscoverOptions{
				RootDir:     root,
				ExcludeTags: "prod",
			},
			wantStacks: []string{"stacks/dev"},
		},
		{
			name: "exclude path",
			opts: DiscoverOptions{
				RootDir:     root,
				ExcludePath: "stacks/**",
			},
			wantStacks: []string{"apps/web"},
		},
		{
			name: "exclude tags and path combined",
			opts: DiscoverOptions{
				RootDir:     root,
				ExcludeTags: "web",
				ExcludePath: "stacks/prod/**",
			},
			wantStacks: []string{"stacks/dev"},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result, err := Discover(tc.opts)
			if err != nil {
				t.Fatalf("discover failed: %v", err)
			}

			got := make([]string, 0, len(result.Stacks))
			for _, stack := range result.Stacks {
				got = append(got, stack.Dir)
			}

			if !equalStrings(got, tc.wantStacks) {
				t.Fatalf("unexpected stacks: got %#v, want %#v", got, tc.wantStacks)
			}
		})
	}
}

func TestDiscoverResolvesOrderEntries(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "terramate.tm.hcl"), "terramate {}\n")
	writeFile(t, filepath.Join(root, "stacks/network/vpc/stack.tm.hcl"), `stack {
  tags = ["network"]
}
`)
	writeFile(t, filepath.Join(root, "stacks/network/dns/stack.tm.hcl"), `stack {
  tags  = ["network"]
  after = ["../vpc", "tag:network"]
}
`)
	writeFile(t, filepath.Join(root, "stacks/data/rds/stack.tm.hcl"), `stack {
  tags = ["database"]
}
`)
	writeFile(t, filepath.Join(root, "stacks/data/cache/stack.tm.hcl"), `stack {
  tags = ["database"]
}
`)
	writeFile(t, filepath.Join(root, "stacks/app/stack.tm.hcl"), `stack {
  after  = ["/stacks/network/dns", "tag:database", "../does-not-exist"]
  before = ["/stacks/data"]
}
`)

	result, err := Discover(DiscoverOptions{RootDir: root})
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}

	byDir := map[string]StackInfo{}
	for _, s := range result.Stacks {
		byDir[s.Dir] = s
	}

	dns := byDir["stacks/network/dns"]
	if !equalStrings(dns.AfterDirs, []string{"stacks/network/vpc"}) {
		t.Fatalf("dns after: got %#v (relative path + tag query, self excluded)", dns.AfterDirs)
	}

	app := byDir["stacks/app"]
	if !equalStrings(app.AfterDirs, []string{"stacks/data/cache", "stacks/data/rds", "stacks/network/dns"}) {
		t.Fatalf("app after: got %#v", app.AfterDirs)
	}
	if !equalStrings(app.BeforeDirs, []string{"stacks/data/cache", "stacks/data/rds"}) {
		t.Fatalf("app before: got %#v (a directory expands to its stacks)", app.BeforeDirs)
	}
	if !equalStrings(app.After, []string{"/stacks/network/dns", "tag:database", "../does-not-exist"}) {
		t.Fatalf("raw after entries must be preserved: got %#v", app.After)
	}

	if vpc := byDir["stacks/network/vpc"]; vpc.AfterDirs != nil || vpc.BeforeDirs != nil {
		t.Fatalf("vpc has no ordering, got after=%#v before=%#v", vpc.AfterDirs, vpc.BeforeDirs)
	}
}

func TestDiscoverInvalidTagOrderEntry(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "terramate.tm.hcl"), "terramate {}\n")
	writeFile(t, filepath.Join(root, "stacks/app/stack.tm.hcl"), `stack {
  after = ["tag:Not Valid!"]
}
`)

	if _, err := Discover(DiscoverOptions{RootDir: root}); err == nil {
		t.Fatal("expected an error for an invalid tag order entry")
	}
}

func TestDiscoverRootStack(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "terramate.tm.hcl"), `terramate {}

stack {
  name = "bootstrap"
}
`)
	writeFile(t, filepath.Join(root, "infra/stack.tm.hcl"), `stack {
  after = ["/"]
}
`)

	result, err := Discover(DiscoverOptions{RootDir: root})
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}

	if len(result.Stacks) != 2 || result.Stacks[0].Dir != RootDir || result.Stacks[0].Name != "bootstrap" {
		t.Fatalf("root stack must have Dir %q: got %#v", RootDir, result.Stacks)
	}
	if !equalStrings(result.Stacks[1].AfterDirs, []string{RootDir}) {
		t.Fatalf("after = [\"/\"] must resolve to the root stack only: got %#v", result.Stacks[1].AfterDirs)
	}

	// Path filters still apply to the root stack.
	result, err = Discover(DiscoverOptions{RootDir: root, PathFilter: "infra/**"})
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}
	if len(result.Stacks) != 1 || result.Stacks[0].Dir != "infra" {
		t.Fatalf("unexpected stacks with path filter: %#v", result.Stacks)
	}
}

func TestDiscoverAllStacksIgnoresFilters(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "terramate.tm.hcl"), "terramate {}\n")
	writeFile(t, filepath.Join(root, "stacks/prod/stack.tm.hcl"), `stack {
  name = "prod"
  tags = ["prod"]
}
`)
	writeFile(t, filepath.Join(root, "stacks/dev/stack.tm.hcl"), `stack {
  name = "dev"
  tags = ["dev"]
}
`)
	writeFile(t, filepath.Join(root, "stacks/legacy/stack.tm.hcl"), `stack {
  name = "legacy"
}

globals {
  atlantis_skip = true
}
`)

	result, err := Discover(DiscoverOptions{RootDir: root, TagFilter: "dev"})
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}

	if len(result.Stacks) != 1 || result.Stacks[0].Dir != "stacks/dev" {
		t.Fatalf("unexpected filtered stacks: %#v", result.Stacks)
	}

	got := make([]string, 0, len(result.AllStacks))
	skipped := map[string]bool{}
	for _, s := range result.AllStacks {
		got = append(got, s.Dir)
		skipped[s.Dir] = s.Skip
	}
	if !equalStrings(got, []string{"stacks/dev", "stacks/legacy", "stacks/prod"}) {
		t.Fatalf("AllStacks must list every stack: got %#v", got)
	}
	if !skipped["stacks/legacy"] || skipped["stacks/prod"] {
		t.Fatalf("AllStacks must carry atlantis_skip: got %#v", skipped)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating directory for %q: %v", path, err)
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %q: %v", path, err)
	}
}

func runCmd(t *testing.T, dir string, name string, args ...string) {
	t.Helper()

	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running %s %v: %v\n%s", name, args, err, string(out))
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}

	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}

	return true
}

func TestMatchesPathFilterDoubleStar(t *testing.T) {
	t.Parallel()

	cases := []struct {
		pattern, dir string
		want         bool
	}{
		{"**/vpc", "vpc", true},
		{"**/vpc", "infra/vpc", true},
		{"**/vpc", "infra/eu/vpc", true},
		{"**/vpc", "infra/vpc2", false},
		{"a/**/b", "a/b", true},
		{"a/**/b", "a/x/y/b", true},
		{"a/**/b", "ab", false},
		{"a/**/b", "a/xb", false},
		{"a/**", "a", true},
		{"a/**", "a/b/c", true},
		{"**", "any/thing", true},
		{"a/**b", "a/x/yb", true},
	}

	for _, tc := range cases {
		got, err := matchesPathFilter(tc.pattern, tc.dir)
		if err != nil {
			t.Fatalf("matchesPathFilter(%q, %q): %v", tc.pattern, tc.dir, err)
		}
		if got != tc.want {
			t.Errorf("matchesPathFilter(%q, %q) = %v, want %v", tc.pattern, tc.dir, got, tc.want)
		}
	}
}

func TestDiscoverEmptyWhenModifiedIsAnOverride(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "terramate.tm.hcl"), "terramate {}\n")
	writeFile(t, filepath.Join(root, "empty/stack.tm.hcl"), `stack {}

globals {
  atlantis_when_modified      = []
  atlantis_apply_requirements = []
}
`)
	writeFile(t, filepath.Join(root, "unset/stack.tm.hcl"), "stack {}\n")

	result, err := Discover(DiscoverOptions{RootDir: root})
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}
	if len(result.Stacks) != 2 {
		t.Fatalf("expected 2 stacks, got %#v", result.Stacks)
	}
	if wm := result.Stacks[0].WhenModified; wm == nil || len(wm) != 0 {
		t.Fatalf("atlantis_when_modified = [] must be kept as an empty override, got %#v", wm)
	}
	if wm := result.Stacks[1].WhenModified; wm != nil {
		t.Fatalf("unset atlantis_when_modified must stay nil, got %#v", wm)
	}
	if ar := result.Stacks[0].ApplyRequirements; ar == nil || len(ar) != 0 {
		t.Fatalf("atlantis_apply_requirements = [] must be kept as an empty override, got %#v", ar)
	}
	if ar := result.Stacks[1].ApplyRequirements; ar != nil {
		t.Fatalf("unset atlantis_apply_requirements must stay nil, got %#v", ar)
	}
}

func TestMatchesPathOrParent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		pattern, dir string
		want         bool
	}{
		{"infra", "infra", true},
		{"infra", "infra/vpc", true},
		{"infra", "infra/vpc/subnet", true},
		{"infra", "infrastructure/vpc", false},
		{"infra", "apps/infra", false},
		{"infra/vpc", "infra", false},
		{"infra/*", "infra", false},
		{"infra/*", "infra/vpc/subnet", true},
		{"*/vpc", "a/vpc/subnet", true},
		{"**/vpc", "a/b/vpc/subnet", true},
		{"infra/**", "infra", true},
		{"/infra", "infra/vpc", true},
		{"infra", ".", false},
		{"**", ".", true},
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
}

func TestDiscoverPathFiltersIncludeNestedStacks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "terramate.tm.hcl"), "terramate {}\n")
	for _, dir := range []string{"infra", "infra/vpc", "infra/vpc/subnet", "infrastructure", "apps/web"} {
		writeFile(t, filepath.Join(root, dir, "stack.tm.hcl"), "stack {}\n")
	}

	dirs := func(r Result) []string {
		out := make([]string, 0, len(r.Stacks))
		for _, s := range r.Stacks {
			out = append(out, s.Dir)
		}
		return out
	}

	result, err := Discover(DiscoverOptions{RootDir: root, PathFilter: "infra"})
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}
	if got, want := dirs(result), []string{"infra", "infra/vpc", "infra/vpc/subnet"}; !equalStrings(got, want) {
		t.Fatalf("--filter-path infra: got %v, want %v", got, want)
	}

	result, err = Discover(DiscoverOptions{RootDir: root, ExcludePath: "infra/vpc"})
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}
	if got, want := dirs(result), []string{"apps/web", "infra", "infrastructure"}; !equalStrings(got, want) {
		t.Fatalf("--exclude-path infra/vpc: got %v, want %v", got, want)
	}
}

// Errors only fail the run for selected stacks: excluding a stack with
// invalid globals, by path or by tag, must work like --filter-path does.
func TestDiscoverExcludedStackWithInvalidGlobals(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "terramate.tm.hcl"), "terramate {}\n")
	writeFile(t, filepath.Join(root, "good/stack.tm.hcl"), "stack {}\n")
	writeFile(t, filepath.Join(root, "bad/stack.tm.hcl"), "stack {\n  tags = [\"bad\"]\n}\n\nglobals {\n  atlantis_workflow = 1\n}\n")

	for name, opts := range map[string]DiscoverOptions{
		"filter-path":  {RootDir: root, PathFilter: "good"},
		"exclude-path": {RootDir: root, ExcludePath: "bad"},
		"exclude-tags": {RootDir: root, ExcludeTags: "bad"},
	} {
		result, err := Discover(opts)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(result.Stacks) != 1 || result.Stacks[0].Dir != "good" {
			t.Fatalf("%s: got %#v", name, result.Stacks)
		}
	}

	if _, err := Discover(DiscoverOptions{RootDir: root}); err == nil {
		t.Fatal("a selected stack with invalid globals must still fail")
	}
}
