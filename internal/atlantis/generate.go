package atlantis

import (
	"bytes"
	"encoding/json"
	"errors"
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

	// NamingStacks is the set of stacks project names are computed against,
	// usually every stack in the repository. Naming against the full set keeps
	// a stack's name the same no matter which filters (--changed, --filter,
	// --filter-path, --exclude-*) selected it. Defaults to the stacks passed
	// to Generate.
	NamingStacks []discovery.StackInfo

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

	namer := newProjectNamer(namingContext(stacks, opts.NamingStacks), opts)

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
			project.Autoplan.WhenModified = append([]string(nil), stack.WhenModified...)
		} else {
			project.Autoplan.WhenModified = mergeWhenModified(opts.WhenModified, stack.ExtraDeps)
		}

		if len(stack.ApplyRequirements) > 0 {
			project.ApplyRequirements = append([]string(nil), stack.ApplyRequirements...)
		} else if requirements, ok := requirementsFromTags(stack.Tags, opts.TagRequirements); ok {
			project.ApplyRequirements = append([]string(nil), requirements...)
		} else if len(opts.ApplyRequirements) > 0 {
			project.ApplyRequirements = append([]string(nil), opts.ApplyRequirements...)
		}

		projects = append(projects, project)
	}

	// Index after all appends so the pointers can't be invalidated by a
	// reallocation of the projects slice.
	for i := range projects {
		projectByDir[projects[i].Dir] = &projects[i]
	}

	if opts.ExecutionOrderGroups || opts.DependsOn {
		dependencies := buildDependencies(includedStacks, projectByDir)
		groups, cyclic := computeExecutionOrderGroups(dependencies)
		if len(cyclic) > 0 {
			opts.warnf(
				"dependency cycle detected involving stacks %s; their ordering can't be honored and they are placed in execution_order_group 0",
				strings.Join(cyclic, ", "),
			)
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
// The root stack is "." because Atlantis requires a non-empty dir.
func stackDir(dir string) string {
	dir = strings.Trim(strings.ReplaceAll(dir, "\\", "/"), "/")
	if dir == "" {
		return discovery.RootDir
	}
	return dir
}

// MarshalYAML marshals an Atlantis Config into YAML.
func MarshalYAML(cfg Config, addHeader bool) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(cfg); err != nil {
		return nil, err
	}
	marshaled := buf.Bytes()

	if !addHeader {
		return marshaled, nil
	}

	header := "# Generated by terramate-atlantis-config. DO NOT EDIT.\n# Re-generate with: terramate-atlantis-config generate\n"
	return append([]byte(header), marshaled...), nil
}

func MarshalJSON(cfg Config) ([]byte, error) {
	return json.MarshalIndent(cfg, "", "  ")
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
// resolved by discovery (AfterDirs/BeforeDirs); dependencies on stacks that
// are not part of the generated config (filtered out or skipped) are dropped.
func buildDependencies(stacks []discovery.StackInfo, projectByDir map[string]*Project) map[string]map[string]struct{} {
	dependencies := make(map[string]map[string]struct{}, len(projectByDir))
	for dir := range projectByDir {
		dependencies[dir] = make(map[string]struct{})
	}

	for _, stack := range stacks {
		dir := stackDir(stack.Dir)
		if _, ok := projectByDir[dir]; !ok {
			continue
		}

		for _, after := range stack.AfterDirs {
			after = stackDir(after)
			if _, ok := projectByDir[after]; ok && after != dir {
				dependencies[dir][after] = struct{}{}
			}
		}

		for _, before := range stack.BeforeDirs {
			before = stackDir(before)
			if _, ok := projectByDir[before]; ok && before != dir {
				dependencies[before][dir] = struct{}{}
			}
		}
	}

	return dependencies
}

// computeExecutionOrderGroups assigns each dir the length of its longest
// dependency chain. Dirs that can't be ordered because they are part of, or
// depend on, a dependency cycle are placed in group 0 and returned (sorted)
// as the second value.
func computeExecutionOrderGroups(dependencies map[string]map[string]struct{}) (groups map[string]int, unresolved []string) {
	groups = make(map[string]int, len(dependencies))
	resolved := make(map[string]struct{}, len(dependencies))
	dirs := sortedDependencyDirs(dependencies)

	for {
		progressed := false

		for _, dir := range dirs {
			if _, done := resolved[dir]; done {
				continue
			}

			allResolved := true
			group := 0
			for dep := range dependencies[dir] {
				depGroup, ok := groups[dep]
				if !ok {
					allResolved = false
					break
				}
				if depGroup+1 > group {
					group = depGroup + 1
				}
			}

			if !allResolved {
				continue
			}

			groups[dir] = group
			resolved[dir] = struct{}{}
			progressed = true
		}

		if !progressed {
			break
		}
	}

	for _, dir := range dirs {
		if _, done := resolved[dir]; done {
			continue
		}
		groups[dir] = 0
		unresolved = append(unresolved, dir)
	}

	return groups, unresolved
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
