package atlantis

import (
	"strings"
	"testing"

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
				ProjectNameStrategy: strategy,
				ProjectNamePrefix:   prefix,
				ProjectNameDepth:    depth,
			})
			if err != nil {
				t.Fatalf("%s: %v", strategy, err)
			}
			seen := map[string]string{}
			for _, p := range cfg.Projects {
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
