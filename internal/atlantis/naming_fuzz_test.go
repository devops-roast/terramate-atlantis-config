package atlantis

import (
	"strings"
	"testing"
	"unicode"

	"github.com/devops-roast/terramate-atlantis-config/internal/discovery"
)

// fuzzDirs turns arbitrary fuzz input into a set of distinct stack dirs.
// Each line is a dir; an empty line is the root stack ("."). Every other
// stack also gets an explicit name, so stack-name collisions are exercised.
func fuzzDirs(raw string) []discovery.StackInfo {
	seen := map[string]struct{}{}
	var stacks []discovery.StackInfo
	i := 0
	for line := range strings.SplitSeq(raw, "\n") {
		i++
		var segs []string
		for seg := range strings.SplitSeq(line, "/") {
			if seg = strings.TrimSpace(seg); seg != "" && seg != "." && seg != ".." {
				segs = append(segs, seg)
			}
		}
		dir := strings.Join(segs, "/")
		if dir == "" {
			dir = discovery.RootDir
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		name := ""
		if i%2 == 1 && len(segs) > 0 {
			// Every other stack carries an explicit (possibly colliding) name.
			name = segs[0]
		}
		stacks = append(stacks, discovery.StackInfo{Dir: dir, Name: name})
	}
	return stacks
}

func FuzzProjectNamesUniqueAndValid(f *testing.F) {
	f.Add("a/b\na-b\nb\n.", "", 1)
	f.Add("stacks/aws/prod/vpc\nstacks/aws/production/vpc\nstacks/gcp/prod/vpc", "stacks/aws", 2)
	f.Add("x/y/z\nx-y/z\nx/y-z\nx-y-z\n-/-\n~\nü/ü", "x", 3)
	f.Add("a\na/a\na/a/a\na-a\na-a-2\na-2", "a", 1)
	f.Add("root\n.\nstack\n-", "", 1)
	f.Add("a..b\n~tilde\n-.\nx/~y\n~\n.~", "", 1)

	strategies := []string{NameStrategyAutoStrip, NameStrategyStackName, NameStrategyLastN, NameStrategyFull}

	f.Fuzz(func(t *testing.T, raw, prefix string, depth int) {
		stacks := fuzzDirs(raw)
		if len(stacks) == 0 || len(stacks) > 64 {
			t.Skip()
		}
		if depth < 1 || depth > 8 {
			depth = 1 + (depth&0x7+8)%8
		}
		for _, strategy := range strategies {
			cfg, err := Generate(stacks, Options{
				CreateProjectName:   true,
				CreateWorkspace:     true,
				ProjectNameStrategy: strategy,
				ProjectNamePrefix:   prefix,
				ProjectNameDepth:    depth,
			})
			if err != nil {
				t.Fatalf("%s: %v", strategy, err)
			}
			seen := map[string]string{}
			for _, p := range cfg.Projects {
				if err := atlantisWorkspaceError(p.Workspace); err != "" {
					t.Fatalf("dir %q got workspace %q, which Atlantis rejects: %s", p.Dir, p.Workspace, err)
				}
				if !ValidProjectName(p.Name) {
					t.Fatalf("%s: dir %q got invalid name %q", strategy, p.Dir, p.Name)
				}
				if other, dup := seen[p.Name]; dup {
					t.Fatalf("%s: dirs %q and %q share name %q", strategy, other, p.Dir, p.Name)
				}
				seen[p.Name] = p.Dir
			}
		}
	})
}

// FuzzProjectNamesAreStable checks the README's "names are stable" claim:
// with AllStacks set, a stack's name doesn't depend on which stacks a run
// selected (--changed, --filter, ...) nor on the order stacks are listed in.
func FuzzProjectNamesAreStable(f *testing.F) {
	f.Add("a/b\na-b\nb\n.", "", 1, uint64(0b1010))
	f.Add("stacks/aws/prod/vpc\nstacks/aws/production/vpc\nstacks/gcp/prod/vpc", "stacks/aws", 2, uint64(1))
	f.Add("x/y/z\nx-y/z\nx/y-z\nx-y-z\n-/-\n~\nü/ü", "x", 3, uint64(0b110))
	f.Add("root\n.\nstack\n-", "", 1, uint64(0b10))

	strategies := []string{NameStrategyAutoStrip, NameStrategyStackName, NameStrategyLastN, NameStrategyFull}

	f.Fuzz(func(t *testing.T, raw, prefix string, depth int, selection uint64) {
		all := fuzzDirs(raw)
		if len(all) == 0 || len(all) > 64 {
			t.Skip()
		}
		if depth < 1 || depth > 8 {
			depth = 1 + (depth&0x7+8)%8
		}
		var selected []discovery.StackInfo
		for i, s := range all {
			if selection&(1<<(uint(i)%64)) != 0 {
				selected = append(selected, s)
			}
		}
		reversed := make([]discovery.StackInfo, len(all))
		for i, s := range all {
			reversed[len(all)-1-i] = s
		}

		for _, strategy := range strategies {
			opts := Options{
				CreateProjectName:   true,
				ProjectNameStrategy: strategy,
				ProjectNamePrefix:   prefix,
				ProjectNameDepth:    depth,
			}
			full := namesByDirOf(mustGenerate(t, all, opts))

			opts.AllStacks = reversed
			for dir, name := range namesByDirOf(mustGenerate(t, reversed, opts)) {
				if full[dir] != name {
					t.Fatalf("%s: %q is named %q, or %q when stacks are listed in another order", strategy, dir, full[dir], name)
				}
			}
			for dir, name := range namesByDirOf(mustGenerate(t, selected, opts)) {
				if full[dir] != name {
					t.Fatalf("%s: %q is named %q, or %q when only some stacks are selected", strategy, dir, full[dir], name)
				}
			}
		}
	})
}

// atlantisWorkspaceError mirrors Atlantis' ValidateWorkspaceName
// (server/core/config/valid/workspace.go, v0.49.0) and returns why it
// rejects workspace, or "".
func atlantisWorkspaceError(workspace string) string {
	switch {
	case workspace == "":
		return ""
	case strings.ContainsAny(workspace, `/\`):
		return `cannot contain '/' or '\'`
	case strings.Contains(workspace, ".."):
		return "cannot contain '..'"
	case workspace == ".":
		return "cannot be '.'"
	case strings.HasPrefix(workspace, "-"):
		return "cannot start with '-'"
	case strings.HasPrefix(workspace, "~"):
		return "cannot start with '~'"
	case strings.Contains(workspace, "$"):
		return "cannot contain '$'"
	}
	for _, r := range workspace {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return "cannot contain whitespace or control characters"
		}
	}
	return ""
}
