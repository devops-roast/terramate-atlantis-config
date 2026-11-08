package atlantis

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/devops-roast/terramate-atlantis-config/internal/discovery"
)

// ErrDependsOnRequiresNames is returned when depends_on generation is
// requested without project names. Atlantis matches depends_on entries
// against project names, so a depends_on list is meaningless without them.
var ErrDependsOnRequiresNames = errors.New(
	"depends_on requires project names: Atlantis matches depends_on entries against project names, " +
		"so --depends-on cannot be used with --create-project-name=false",
)

// Options configures Atlantis config generation behavior.
type Options struct {
	Autoplan             bool
	Parallel             bool
	Automerge            bool
	DefaultWorkflow      string
	TerraformVersion     string
	CreateProjectName    bool
	CreateWorkspace      bool
	WhenModified         []string
	ExecutionOrderGroups bool
	DependsOn            bool
	ApplyRequirements    []string
	TagWorkflows         map[string]string
	TagRequirements      map[string][]string
	SortBy               string
	ProjectNameStrategy  string // auto-strip, stack-name, last-n, full
	ProjectNamePrefix    string // explicit prefix to strip (used with any strategy)
	ProjectNameDepth     int    // number of trailing path segments to use (for last-n)

	// AllStacks is every stack in the repository, whatever the filters
	// (--changed, --filter, --filter-path, --exclude-*) selected:
	//   - project names are computed against it (minus skipped stacks), so a
	//     stack keeps its name no matter which stacks a run selected;
	//   - ordering is followed through its stacks, so when a runs after m and
	//     m after z, a still runs after z if m is filtered out or skipped.
	// Defaults to the stacks passed to Generate.
	AllStacks []discovery.StackInfo

	// Warnf receives non-fatal warnings, such as dependency cycles.
	// A nil Warnf discards them.
	Warnf func(format string, args ...any)
}

func (o Options) warnf(format string, args ...any) {
	if o.Warnf != nil {
		o.Warnf(format, args...)
	}
}

// Generate builds an Atlantis config from discovered stack metadata.
func Generate(stacks []discovery.StackInfo, opts Options) (Config, error) {
	if opts.DependsOn && !opts.CreateProjectName {
		return Config{}, ErrDependsOnRequiresNames
	}

	includedStacks := make([]discovery.StackInfo, 0, len(stacks))
	for _, stack := range stacks {
		if stack.Skip {
			continue
		}
		includedStacks = append(includedStacks, stack)
	}

	projects := make([]Project, 0, len(includedStacks))
	projectByDir := make(map[string]*Project, len(includedStacks))

	namer := newProjectNamer(namingContext(stacks, opts.AllStacks), opts)

	for _, stack := range includedStacks {
		dir := stackDir(stack.Dir)

		project := Project{
			Dir: dir,
			Autoplan: Autoplan{
				Enabled: opts.Autoplan,
			},
		}

		if opts.CreateProjectName {
			project.Name = namer.nameFor(dir)
		}

		if opts.CreateWorkspace && dir != discovery.RootDir {
			project.Workspace = sanitizeProjectName(dir)
		}

		if stack.Workflow != "" {
			project.Workflow = stack.Workflow
		} else if workflow, ok := workflowFromTags(stack.Tags, opts.TagWorkflows); ok {
			project.Workflow = workflow
		} else if opts.DefaultWorkflow != "" {
			project.Workflow = opts.DefaultWorkflow
		}

		if stack.TerraformVersion != "" {
			project.TerraformVersion = stack.TerraformVersion
		} else if opts.TerraformVersion != "" {
			project.TerraformVersion = opts.TerraformVersion
		}

		if stack.AutoplanEnabled != nil {
			project.Autoplan.Enabled = *stack.AutoplanEnabled
		}

		if stack.WhenModified != nil {
			// Non-nil even when empty: JSON would render nil as null, which
			// Atlantis reads as "use its default patterns".
			project.Autoplan.WhenModified = append([]string{}, stack.WhenModified...)
		} else {
			project.Autoplan.WhenModified = mergeWhenModified(opts.WhenModified, stack.ExtraDeps)
		}

		// A stack's atlantis_apply_requirements wins even when empty: `[]`
		// is an explicit "no requirements". An empty default is omitted so
		// Atlantis falls back to the server-side requirements.
		if stack.ApplyRequirements != nil {
			project.ApplyRequirements = copyToPtr(stack.ApplyRequirements)
		} else if requirements, ok := requirementsFromTags(stack.Tags, opts.TagRequirements); ok {
			project.ApplyRequirements = copyToPtr(requirements)
		} else if len(opts.ApplyRequirements) > 0 {
			project.ApplyRequirements = copyToPtr(opts.ApplyRequirements)
		}

		projects = append(projects, project)
	}

	// Index after all appends so the pointers can't be invalidated by a
	// reallocation of the projects slice.
	for i := range projects {
		projectByDir[projects[i].Dir] = &projects[i]
	}

	if opts.ExecutionOrderGroups || opts.DependsOn {
		dependencies := buildDependencies(stacks, opts.AllStacks, projectByDir)
		groups, cyclic := computeExecutionOrderGroups(dependencies)
		if len(cyclic) > 0 {
			consequence := "their order among themselves can't be honored, so they share an execution_order_group"
			if opts.DependsOn {
				consequence = "their depends_on entries reference each other, and Atlantis won't apply a project " +
					"before its dependencies, so they can't be applied until the cycle is broken"
			}
			opts.warnf("dependency cycle detected between stacks %s; %s", strings.Join(cyclic, ", "), consequence)
		}

		if opts.ExecutionOrderGroups {
			for dir, group := range groups {
				if project, ok := projectByDir[dir]; ok {
					project.ExecutionOrderGroup = group
				}
			}
		}

		if opts.DependsOn {
			for dir, deps := range dependencies {
				project, ok := projectByDir[dir]
				if !ok || len(deps) == 0 {
					continue
				}
				names := make([]string, 0, len(deps))
				for _, depDir := range sortedKeys(deps) {
					names = append(names, projectByDir[depDir].Name)
				}
				sort.Strings(names)
				project.DependsOn = names
			}
		}
	}

	sort.Slice(projects, func(i, j int) bool {
		sortBy := opts.SortBy
		if sortBy == "" {
			sortBy = "dir"
		}

		switch sortBy {
		case "name":
			left := projects[i].Name
			right := projects[j].Name
			if left == right {
				return projects[i].Dir < projects[j].Dir
			}
			return left < right
		case "execution_order_group":
			if projects[i].ExecutionOrderGroup == projects[j].ExecutionOrderGroup {
				return projects[i].Dir < projects[j].Dir
			}
			return projects[i].ExecutionOrderGroup < projects[j].ExecutionOrderGroup
		default:
			return projects[i].Dir < projects[j].Dir
		}
	})

	return Config{
		Version:       3,
		Automerge:     opts.Automerge,
		ParallelPlan:  opts.Parallel,
		ParallelApply: opts.Parallel,
		Projects:      projects,
	}, nil
}

// stackDir normalizes a StackInfo.Dir into an Atlantis project dir.
// The root stack is "." because Atlantis requires a non-empty dir. Only the
// OS separator is converted: on Linux and macOS a backslash is a valid
// character in a directory name, not a separator.
func stackDir(dir string) string {
	dir = strings.Trim(filepath.ToSlash(dir), "/")
	if dir == "" {
		return discovery.RootDir
	}
	return dir
}

// MarshalYAML marshals an Atlantis Config into YAML.
func MarshalYAML(cfg Config, addHeader bool) ([]byte, error) {
	var node yaml.Node
	if err := node.Encode(cfg); err != nil {
		return nil, err
	}
	quoteTrailingWhitespace(&node)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&node); err != nil {
		return nil, err
	}
	marshaled := buf.Bytes()

	if !addHeader {
		return marshaled, nil
	}

	header := "# Generated by terramate-atlantis-config. DO NOT EDIT.\n# Re-generate with: terramate-atlantis-config generate\n"
	return append([]byte(header), marshaled...), nil
}

// quoteTrailingWhitespace double-quotes multi-line strings that have
// whitespace at the end of a line. yaml.v3 writes them as literal blocks,
// which puts that whitespace at the end of an output line, where trailing
// whitespace fixers (e.g. the pre-commit trailing-whitespace hook) strip it,
// changing the value and making --check fail.
func quoteTrailingWhitespace(node *yaml.Node) {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!str" && strings.Contains(node.Value, "\n") {
		for _, line := range strings.Split(node.Value, "\n") {
			if strings.TrimRight(line, " \t") != line {
				node.Style = yaml.DoubleQuotedStyle
				break
			}
		}
	}
	for _, child := range node.Content {
		quoteTrailingWhitespace(child)
	}
}

// MarshalJSON marshals an Atlantis Config into indented JSON. Like the YAML
// output it ends with a newline, so end-of-file fixers (e.g. the pre-commit
// end-of-file-fixer hook) leave a generated file unchanged and --check keeps
// passing.
func MarshalJSON(cfg Config) ([]byte, error) {
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// copyToPtr returns a pointer to a non-nil copy of s.
func copyToPtr(s []string) *[]string {
	c := append([]string{}, s...)
	return &c
}

func mergeWhenModified(defaults, extraDeps []string) []string {
	merged := make([]string, 0, len(defaults)+len(extraDeps))
	merged = append(merged, defaults...)
	merged = append(merged, extraDeps...)
	return merged
}

func workflowFromTags(tags []string, mapping map[string]string) (string, bool) {
	for _, tag := range tags {
		if workflow, ok := mapping[tag]; ok {
			return workflow, true
		}
	}

	return "", false
}

func requirementsFromTags(tags []string, mapping map[string][]string) ([]string, bool) {
	for _, tag := range tags {
		if requirements, ok := mapping[tag]; ok {
			return requirements, true
		}
	}

	return nil, false
}

// buildDependencies returns, for every project dir, the set of project dirs
// it must run after. It uses the stack.after/stack.before entries already
// resolved by discovery (AfterDirs/BeforeDirs) of every known stack. Stacks
// that are not part of the generated config (filtered out or skipped) are
// not listed, but ordering is followed through them, as `terramate run`
// does: if a runs after m and m after z, a depends on z when m is excluded.
func buildDependencies(stacks, all []discovery.StackInfo, projectByDir map[string]*Project) map[string]map[string]struct{} {
	// after[dir] is the set of stacks dir runs directly after.
	after := map[string]map[string]struct{}{}
	addEdge := func(dir, dep string) {
		if dir == dep {
			return
		}
		if after[dir] == nil {
			after[dir] = map[string]struct{}{}
		}
		after[dir][dep] = struct{}{}
	}
	for _, list := range [][]discovery.StackInfo{all, stacks} {
		for _, stack := range list {
			dir := stackDir(stack.Dir)
			for _, dep := range stack.AfterDirs {
				addEdge(dir, stackDir(dep))
			}
			for _, later := range stack.BeforeDirs {
				addEdge(stackDir(later), dir)
			}
		}
	}

	dependencies := make(map[string]map[string]struct{}, len(projectByDir))
	for dir := range projectByDir {
		deps := map[string]struct{}{}
		visited := map[string]struct{}{dir: {}}
		queue := sortedKeys(after[dir])
		for len(queue) > 0 {
			next := queue[0]
			queue = queue[1:]
			if _, seen := visited[next]; seen {
				continue
			}
			visited[next] = struct{}{}
			if _, generated := projectByDir[next]; generated {
				deps[next] = struct{}{}
				continue
			}
			queue = append(queue, sortedKeys(after[next])...)
		}
		dependencies[dir] = deps
	}

	return dependencies
}

// computeExecutionOrderGroups assigns each dir the length of its longest
// dependency chain, so every dependency lands in a lower group. Dirs on a
// dependency cycle can't be ordered among themselves: each cycle (strongly
// connected component) is collapsed into one node, so its dirs share a group
// that is still above everything the cycle depends on, and dirs that depend
// on a cycle are still placed above it. The dirs on a cycle are returned
// (sorted) as the second value.
func computeExecutionOrderGroups(dependencies map[string]map[string]struct{}) (groups map[string]int, cyclic []string) {
	groups = make(map[string]int, len(dependencies))

	// Components come out of Tarjan's algorithm in reverse topological
	// order: a component is emitted after every component it depends on, so
	// their groups are already known.
	for _, component := range stronglyConnectedComponents(dependencies) {
		members := make(map[string]struct{}, len(component))
		for _, dir := range component {
			members[dir] = struct{}{}
		}

		group := 0
		for _, dir := range component {
			for dep := range dependencies[dir] {
				if _, same := members[dep]; same {
					continue
				}
				group = max(group, groups[dep]+1)
			}
		}

		for _, dir := range component {
			groups[dir] = group
		}
		if len(component) > 1 {
			cyclic = append(cyclic, component...)
		}
	}

	sort.Strings(cyclic)
	return groups, cyclic
}

// stronglyConnectedComponents returns the strongly connected components of
// the dependency graph (Tarjan's algorithm), each one before any component
// that depends on it. Dirs and edges are visited in sorted order so the
// result is deterministic.
func stronglyConnectedComponents(dependencies map[string]map[string]struct{}) [][]string {
	var (
		index      = map[string]int{}
		lowlink    = map[string]int{}
		onStack    = map[string]bool{}
		stack      []string
		components [][]string
		next       int
	)

	var visit func(dir string)
	visit = func(dir string) {
		index[dir] = next
		lowlink[dir] = next
		next++
		stack = append(stack, dir)
		onStack[dir] = true

		for _, dep := range sortedKeys(dependencies[dir]) {
			if _, seen := index[dep]; !seen {
				visit(dep)
				lowlink[dir] = min(lowlink[dir], lowlink[dep])
			} else if onStack[dep] {
				lowlink[dir] = min(lowlink[dir], index[dep])
			}
		}

		if lowlink[dir] != index[dir] {
			return
		}
		var component []string
		for {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[top] = false
			component = append(component, top)
			if top == dir {
				break
			}
		}
		sort.Strings(component)
		components = append(components, component)
	}

	for _, dir := range sortedDependencyDirs(dependencies) {
		if _, seen := index[dir]; !seen {
			visit(dir)
		}
	}
	return components
}

func sortedDependencyDirs(m map[string]map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
