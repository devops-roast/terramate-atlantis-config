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

	stacks, err := Discover(DiscoverOptions{
		RootDir:    root,
		PathFilter: "stacks/network/**",
	})
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}

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

	stacks, err := Discover(DiscoverOptions{
		RootDir: root,
		Changed: true,
		BaseRef: "main",
	})
	if err != nil {
		t.Fatalf("discover failed: %v", err)
	}

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

			stacks, err := Discover(tc.opts)
			if err != nil {
				t.Fatalf("discover failed: %v", err)
			}

			got := make([]string, 0, len(stacks))
			for _, stack := range stacks {
				got = append(got, stack.Dir)
			}

			if !equalStrings(got, tc.wantStacks) {
				t.Fatalf("unexpected stacks: got %#v, want %#v", got, tc.wantStacks)
			}
		})
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
