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
