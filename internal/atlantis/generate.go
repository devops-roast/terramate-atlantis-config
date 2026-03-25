package atlantis

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/devops-roast/terramate-atlantis-config/internal/discovery"
)

// ProjectNameStrategy controls how project names are generated from stack paths.
const (
	// NameStrategyAutoStrip auto-detects and removes the longest common path prefix.
	NameStrategyAutoStrip = "auto-strip"
	// NameStrategyStackName uses the stack's metadata name, prefixed with parent dirs for uniqueness.
	NameStrategyStackName = "stack-name"
	// NameStrategyLastN uses only the last N path segments.
	NameStrategyLastN = "last-n"
	// NameStrategyFull uses the full directory path (original behavior).
	NameStrategyFull = "full"
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
}

// Generate builds an Atlantis config from discovered stack metadata.
func Generate(stacks []discovery.StackInfo, opts Options) Config {
	includedStacks := make([]discovery.StackInfo, 0, len(stacks))
	for _, stack := range stacks {
		if stack.Skip {
			continue
		}
		includedStacks = append(includedStacks, stack)
	}

	projects := make([]Project, 0, len(includedStacks))
	projectByDir := make(map[string]*Project, len(includedStacks))

	namer := newProjectNamer(includedStacks, opts)

	for _, stack := range includedStacks {

		dir := strings.TrimPrefix(stack.Dir, "/")

		project := Project{
			Dir: dir,
			Autoplan: Autoplan{
				Enabled: opts.Autoplan,
			},
		}

		if opts.CreateProjectName {
			project.Name = namer.nameFor(stack, dir)
		}

		if opts.CreateWorkspace {
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
		projectByDir[dir] = &projects[len(projects)-1]
	}

	if opts.ExecutionOrderGroups || opts.DependsOn {
		dependencies := buildDependencies(includedStacks, projectByDir)

		if opts.ExecutionOrderGroups {
			groups := computeExecutionOrderGroups(dependencies)
			for dir, group := range groups {
				if project, ok := projectByDir[dir]; ok {
					project.ExecutionOrderGroup = group
				}
			}
		}

		if opts.DependsOn {
			for dir, deps := range dependencies {
				if project, ok := projectByDir[dir]; ok {
					project.DependsOn = sortedKeys(deps)
				}
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
	}
}

func PreserveWorkflows(existingYAML []byte, cfg *Config) error {
	var existing map[string]interface{}
	if err := yaml.Unmarshal(existingYAML, &existing); err != nil {
		return fmt.Errorf("unmarshaling existing yaml: %w", err)
	}

	workflowsRaw, ok := existing["workflows"]
	if !ok {
		return nil
	}

	workflows, ok := workflowsRaw.(map[string]interface{})
	if !ok {
		return fmt.Errorf("workflows must be a map")
	}

	cfg.Workflows = workflows
	return nil
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

func sanitizeProjectName(dir string) string {
	name := strings.ReplaceAll(dir, "/", "-")
	return strings.TrimLeft(name, "-")
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

func buildDependencies(stacks []discovery.StackInfo, projectByDir map[string]*Project) map[string]map[string]struct{} {
	dependencies := make(map[string]map[string]struct{}, len(projectByDir))
	for dir := range projectByDir {
		dependencies[dir] = make(map[string]struct{})
	}

	for _, stack := range stacks {
		stackDir := strings.TrimPrefix(filepath.ToSlash(stack.Dir), "/")
		if _, ok := projectByDir[stackDir]; !ok {
			continue
		}

		for _, after := range stack.After {
			resolved := resolveDependencyPath(stackDir, after)
			if _, ok := projectByDir[resolved]; ok {
				dependencies[stackDir][resolved] = struct{}{}
			}
		}

		for _, before := range stack.Before {
			resolved := resolveDependencyPath(stackDir, before)
			if _, ok := projectByDir[resolved]; ok {
				dependencies[resolved][stackDir] = struct{}{}
			}
		}
	}

	return dependencies
}

func resolveDependencyPath(stackDir, rawPath string) string {
	normalized := filepath.ToSlash(strings.TrimSpace(rawPath))
	if normalized == "" {
		return ""
	}

	if strings.HasPrefix(normalized, "/") {
		return strings.TrimPrefix(filepath.ToSlash(filepath.Clean(normalized)), "/")
	}

	joined := filepath.Join(stackDir, normalized)
	return strings.TrimPrefix(filepath.ToSlash(filepath.Clean(joined)), "./")
}

func computeExecutionOrderGroups(dependencies map[string]map[string]struct{}) map[string]int {
	groups := make(map[string]int, len(dependencies))
	resolved := make(map[string]struct{}, len(dependencies))

	for len(resolved) < len(dependencies) {
		progressed := false

		dirs := sortedDependencyDirs(dependencies)
		for _, dir := range dirs {
			if _, done := resolved[dir]; done {
				continue
			}

			deps := dependencies[dir]
			allResolved := true
			group := 0
			for dep := range deps {
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

		if progressed {
			continue
		}

		for _, dir := range dirs {
			if _, done := resolved[dir]; done {
				continue
			}
			groups[dir] = 0
			resolved[dir] = struct{}{}
		}
	}

	return groups
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

type projectNamer struct {
	strategy     string
	prefix       string
	depth        int
	commonPrefix string
	namesByDir   map[string]string
}

func newProjectNamer(stacks []discovery.StackInfo, opts Options) *projectNamer {
	strategy := opts.ProjectNameStrategy
	if strategy == "" {
		strategy = NameStrategyAutoStrip
	}

	n := &projectNamer{
		strategy: strategy,
		prefix:   opts.ProjectNamePrefix,
		depth:    opts.ProjectNameDepth,
	}

	if strategy == NameStrategyAutoStrip && opts.ProjectNamePrefix == "" {
		n.commonPrefix = detectCommonPrefix(stacks)
	}

	n.namesByDir = n.buildUniqueNames(stacks)
	return n
}

func (n *projectNamer) nameFor(_ discovery.StackInfo, dir string) string {
	if name, ok := n.namesByDir[dir]; ok {
		return name
	}
	return sanitizeProjectName(dir)
}

func (n *projectNamer) buildUniqueNames(stacks []discovery.StackInfo) map[string]string {
	rawNames := make(map[string]string, len(stacks))
	for _, s := range stacks {
		dir := strings.TrimPrefix(s.Dir, "/")
		rawNames[dir] = n.rawName(s, dir)
	}

	seen := make(map[string][]string)
	for dir, name := range rawNames {
		seen[name] = append(seen[name], dir)
	}

	result := make(map[string]string, len(stacks))
	for dir, name := range rawNames {
		if len(seen[name]) > 1 {
			result[dir] = n.disambiguate(dir, seen[name])
		} else {
			result[dir] = name
		}
	}
	return result
}

func (n *projectNamer) rawName(stack discovery.StackInfo, dir string) string {
	switch n.strategy {
	case NameStrategyStackName:
		return n.stackNameRaw(stack, dir)
	case NameStrategyLastN:
		return n.lastNRaw(dir)
	case NameStrategyFull:
		return sanitizeProjectName(dir)
	default:
		return n.autoStripRaw(dir)
	}
}

func (n *projectNamer) disambiguate(dir string, conflicting []string) string {
	parts := strings.Split(filepath.ToSlash(dir), "/")

	for depth := 1; depth <= len(parts); depth++ {
		candidate := sanitizeProjectName(strings.Join(parts[max(0, len(parts)-depth):], "/"))
		unique := true
		for _, other := range conflicting {
			if other == dir {
				continue
			}
			otherParts := strings.Split(filepath.ToSlash(other), "/")
			otherCandidate := sanitizeProjectName(strings.Join(otherParts[max(0, len(otherParts)-depth):], "/"))
			if candidate == otherCandidate {
				unique = false
				break
			}
		}
		if unique {
			return candidate
		}
	}
	return sanitizeProjectName(dir)
}

func (n *projectNamer) autoStripRaw(dir string) string {
	stripped := dir
	if n.prefix != "" {
		stripped = strings.TrimPrefix(dir, n.prefix)
		stripped = strings.TrimPrefix(stripped, "/")
	} else if n.commonPrefix != "" {
		stripped = strings.TrimPrefix(dir, n.commonPrefix)
		stripped = strings.TrimPrefix(stripped, "/")
	}
	if stripped == "" {
		stripped = dir
	}
	return sanitizeProjectName(stripped)
}

func (n *projectNamer) stackNameRaw(stack discovery.StackInfo, dir string) string {
	stackName := stack.Name
	if stackName == "" {
		stackName = filepath.Base(dir)
	}

	stripped := dir
	if n.prefix != "" {
		stripped = strings.TrimPrefix(dir, n.prefix)
		stripped = strings.TrimPrefix(stripped, "/")
	}

	parent := filepath.Dir(stripped)
	if parent == "." || parent == "" {
		return sanitizeProjectName(stackName)
	}

	parentParts := strings.Split(filepath.ToSlash(parent), "/")
	contextParts := parentParts
	if len(contextParts) > 3 {
		contextParts = contextParts[len(contextParts)-3:]
	}

	return sanitizeProjectName(strings.Join(contextParts, "/") + "/" + stackName)
}

func (n *projectNamer) lastNRaw(dir string) string {
	depth := n.depth
	if depth <= 0 {
		depth = 3
	}

	parts := strings.Split(filepath.ToSlash(dir), "/")
	if len(parts) <= depth {
		return sanitizeProjectName(dir)
	}

	return sanitizeProjectName(strings.Join(parts[len(parts)-depth:], "/"))
}

func detectCommonPrefix(stacks []discovery.StackInfo) string {
	if len(stacks) == 0 {
		return ""
	}

	dirs := make([]string, 0, len(stacks))
	for _, s := range stacks {
		dir := strings.TrimPrefix(s.Dir, "/")
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}
	if len(dirs) == 0 {
		return ""
	}

	first := strings.Split(dirs[0], "/")

	prefixLen := 0
	for i := range first {
		candidate := strings.Join(first[:i+1], "/")
		allMatch := true
		for _, d := range dirs[1:] {
			if !strings.HasPrefix(d, candidate+"/") {
				allMatch = false
				break
			}
		}
		if !allMatch {
			break
		}
		prefixLen = i + 1
	}

	if prefixLen == 0 {
		return ""
	}
	return strings.Join(first[:prefixLen], "/")
}
