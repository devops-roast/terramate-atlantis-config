package discovery

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/terramate-io/terramate/config"
	"github.com/terramate-io/terramate/git"
	"github.com/terramate-io/terramate/globals"
	tmstack "github.com/terramate-io/terramate/stack"

	globalsExtract "github.com/devops-roast/terramate-atlantis-config/internal/globals"
)

// RootDir is the Dir value used for a stack defined at the repository root.
const RootDir = "."

type DiscoverOptions struct {
	RootDir     string
	TagFilter   string
	PathFilter  string
	ExcludeTags string
	ExcludePath string
	Changed     bool
	BaseRef     string
}

// StackInfo contains discovered stack metadata and atlantis overrides.
type StackInfo struct {
	// Core metadata from Terramate SDK
	Name        string
	ID          string
	Description string
	Dir         string // relative path from root (no leading slash, "." for the root stack)
	Tags        []string
	After       []string // raw stack.after entries as written in HCL
	Before      []string // raw stack.before entries as written in HCL

	// AfterDirs and BeforeDirs are the stack.after/stack.before entries
	// resolved to stack directories (same format as Dir), following
	// Terramate's own ordering semantics: "tag:" queries expand to every
	// matching stack, and a path expands to every stack at or below it.
	AfterDirs  []string
	BeforeDirs []string

	// Atlantis overrides extracted from globals
	Skip              bool
	Workflow          string
	AutoplanEnabled   *bool // nil = use CLI default
	TerraformVersion  string
	ExtraDeps         []string // additional when_modified entries
	WhenModified      []string // full override of when_modified (nil = use default)
	ApplyRequirements []string
}

// Result is the outcome of a discovery run.
type Result struct {
	// Stacks are the stacks selected by the filters, with full metadata.
	Stacks []StackInfo
	// AllStacks lists every stack in the repository (name, ID, description,
	// dir, tags and skip only), regardless of filters. It gives project naming
	// a stable context so names don't depend on which stacks a run selected.
	AllStacks []StackInfo
}

// Discover loads stacks from a Terramate root and extracts Atlantis overrides.
func Discover(opts DiscoverOptions) (Result, error) {
	rootDir := opts.RootDir
	if rootDir == "" {
		rootDir = "."
	}

	absRootDir, err := filepath.Abs(rootDir)
	if err != nil {
		return Result{}, fmt.Errorf("resolving root path %q: %w", rootDir, err)
	}

	root, err := config.LoadRoot(absRootDir, false)
	if err != nil {
		return Result{}, fmt.Errorf("loading terramate root: %w", err)
	}

	stacks, err := config.LoadAllStacks(root, root.Tree())
	if err != nil {
		return Result{}, fmt.Errorf("loading stacks: %w", err)
	}

	type stackOverrides struct {
		overrides globalsExtract.AtlantisOverrides
		err       error
	}
	overridesByDir := make(map[string]stackOverrides, len(stacks))

	allStacks := make([]StackInfo, 0, len(stacks))
	for _, sortable := range stacks {
		stack := sortable.Stack
		overrides, err := atlantisOverrides(root, stack)
		overridesByDir[stack.Dir.String()] = stackOverrides{overrides: overrides, err: err}

		allStacks = append(allStacks, StackInfo{
			Name:        stack.Name,
			ID:          stack.ID,
			Description: stack.Description,
			Dir:         normalizeStackPath(stack.Dir.String()),
			Tags:        append([]string(nil), stack.Tags...),
			// Errors only fail the run for selected stacks (below); here a
			// stack whose globals can't be read is simply not skipped.
			Skip: err == nil && overrides.Skip,
		})
	}
	sort.Slice(allStacks, func(i, j int) bool {
		return allStacks[i].Dir < allStacks[j].Dir
	})

	var allowedPaths map[string]struct{}
	if opts.TagFilter != "" {
		filteredPaths, err := root.StacksByTagsFilters([]string{opts.TagFilter})
		if err != nil {
			return Result{}, fmt.Errorf("filtering stacks by tag %q: %w", opts.TagFilter, err)
		}

		allowedPaths = make(map[string]struct{}, len(filteredPaths))
		for _, p := range filteredPaths {
			allowedPaths[p.String()] = struct{}{}
		}
	}

	if opts.Changed {
		gitWrapper, err := git.WithConfig(git.Config{WorkingDir: absRootDir})
		if err != nil {
			return Result{}, fmt.Errorf("initializing git wrapper: %w", err)
		}

		baseRef := opts.BaseRef
		if baseRef == "" {
			baseRef = "main"
		}

		mgr := tmstack.NewGitAwareManager(root, gitWrapper)
		report, err := mgr.ListChanged(tmstack.ChangeConfig{BaseRef: baseRef})
		if err != nil {
			return Result{}, fmt.Errorf("listing changed stacks against %q: %w", baseRef, err)
		}

		changedPaths := make(map[string]struct{}, len(report.Stacks))
		for _, entry := range report.Stacks {
			changedPaths[entry.Stack.Dir.String()] = struct{}{}
		}

		allowedPaths = intersectAllowedPaths(allowedPaths, changedPaths)
	}

	excludedPaths := map[string]struct{}{}
	if opts.ExcludeTags != "" {
		filteredPaths, err := root.StacksByTagsFilters([]string{opts.ExcludeTags})
		if err != nil {
			return Result{}, fmt.Errorf("filtering stacks by excluded tag %q: %w", opts.ExcludeTags, err)
		}

		excludedPaths = make(map[string]struct{}, len(filteredPaths))
		for _, p := range filteredPaths {
			excludedPaths[normalizeStackPath(p.String())] = struct{}{}
		}
	}

	discovered := make([]StackInfo, 0, len(stacks))
	for _, sortable := range stacks {
		stack := sortable.Stack
		if allowedPaths != nil {
			if _, ok := allowedPaths[stack.Dir.String()]; !ok {
				continue
			}
		}

		stackDir := normalizeStackPath(stack.Dir.String())
		if opts.PathFilter != "" {
			matched, err := matchesPathFilter(opts.PathFilter, stackDir)
			if err != nil {
				return Result{}, fmt.Errorf("matching path filter %q for stack %q: %w", opts.PathFilter, stackDir, err)
			}
			if !matched {
				continue
			}
		}

		stored := overridesByDir[stack.Dir.String()]
		if stored.err != nil {
			return Result{}, stored.err
		}
		overrides := stored.overrides

		afterDirs, err := resolveOrderEntries(root, stack, stack.After)
		if err != nil {
			return Result{}, fmt.Errorf("resolving stack.after for stack %q: %w", stack.Dir.String(), err)
		}
		beforeDirs, err := resolveOrderEntries(root, stack, stack.Before)
		if err != nil {
			return Result{}, fmt.Errorf("resolving stack.before for stack %q: %w", stack.Dir.String(), err)
		}

		discovered = append(discovered, StackInfo{
			Name:              stack.Name,
			ID:                stack.ID,
			Description:       stack.Description,
			Dir:               stackDir,
			Tags:              append([]string(nil), stack.Tags...),
			After:             append([]string(nil), stack.After...),
			Before:            append([]string(nil), stack.Before...),
			AfterDirs:         afterDirs,
			BeforeDirs:        beforeDirs,
			Skip:              overrides.Skip,
			Workflow:          overrides.Workflow,
			AutoplanEnabled:   overrides.AutoplanEnabled,
			TerraformVersion:  overrides.TerraformVersion,
			ExtraDeps:         append([]string(nil), overrides.ExtraDeps...),
			WhenModified:      append([]string(nil), overrides.WhenModified...),
			ApplyRequirements: append([]string(nil), overrides.ApplyRequirements...),
		})
	}

	if opts.ExcludeTags != "" || opts.ExcludePath != "" {
		filtered := make([]StackInfo, 0, len(discovered))
		for _, stack := range discovered {
			if _, excluded := excludedPaths[stack.Dir]; excluded {
				continue
			}

			if opts.ExcludePath != "" {
				matched, err := matchesPathFilter(opts.ExcludePath, stack.Dir)
				if err != nil {
					return Result{}, fmt.Errorf("matching exclude path filter %q for stack %q: %w", opts.ExcludePath, stack.Dir, err)
				}
				if matched {
					continue
				}
			}

			filtered = append(filtered, stack)
		}
		discovered = filtered
	}

	sort.Slice(discovered, func(i, j int) bool {
		return discovered[i].Dir < discovered[j].Dir
	})

	return Result{Stacks: discovered, AllStacks: allStacks}, nil
}

// atlantisOverrides evaluates a stack's globals and extracts the atlantis_*
// overrides from them.
func atlantisOverrides(root *config.Root, stack *config.Stack) (globalsExtract.AtlantisOverrides, error) {
	report := globals.ForStack(root, stack)
	if err := report.AsError(); err != nil {
		return globalsExtract.AtlantisOverrides{}, fmt.Errorf("loading globals for stack %q: %w", stack.Dir.String(), err)
	}

	if report.Globals == nil {
		return globalsExtract.AtlantisOverrides{}, nil
	}

	overrides, err := globalsExtract.ExtractAtlantisGlobals(report.Globals.AsValueMap())
	if err != nil {
		return globalsExtract.AtlantisOverrides{}, fmt.Errorf("extracting atlantis globals for stack %q: %w", stack.Dir.String(), err)
	}
	return overrides, nil
}

// resolveOrderEntries resolves stack.after / stack.before entries to stack
// directories the same way `terramate run` orders stacks: "tag:<filter>"
// entries select every stack matching the tag filter, and path entries
// (relative to the stack, or absolute from the project root) select every
// stack at or below that directory. The stack itself is never included.
// Paths that don't exist are ignored, as Terramate does.
func resolveOrderEntries(root *config.Root, stack *config.Stack, entries []string) ([]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	self := stack.Dir.String()
	resolved := map[string]struct{}{}

	for _, entry := range entries {
		if filter, ok := strings.CutPrefix(entry, "tag:"); ok {
			paths, err := root.StacksByTagsFilters([]string{filter})
			if err != nil {
				return nil, fmt.Errorf("invalid order entry %q: %w", entry, err)
			}
			for _, p := range paths {
				if p.String() != self {
					resolved[normalizeStackPath(p.String())] = struct{}{}
				}
			}
			continue
		}

		for _, tree := range root.StacksByPaths(stack.Dir, entry) {
			if dir := tree.Dir().String(); dir != self {
				resolved[normalizeStackPath(dir)] = struct{}{}
			}
		}
	}

	dirs := make([]string, 0, len(resolved))
	for dir := range resolved {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs, nil
}

// normalizeStackPath converts a Terramate project path ("/a/b", "/") into
// the Dir format used by StackInfo ("a/b", ".").
func normalizeStackPath(p string) string {
	normalized := filepath.ToSlash(strings.TrimPrefix(strings.TrimSpace(p), "/"))
	if normalized == "" {
		return RootDir
	}
	return normalized
}

func intersectAllowedPaths(current, incoming map[string]struct{}) map[string]struct{} {
	if current == nil {
		return incoming
	}

	intersection := make(map[string]struct{})
	for p := range current {
		if _, ok := incoming[p]; ok {
			intersection[p] = struct{}{}
		}
	}

	return intersection
}

func matchesPathFilter(pattern, stackDir string) (bool, error) {
	normalizedPattern := filepath.ToSlash(strings.TrimPrefix(pattern, "/"))
	if normalizedPattern == "" {
		return true, nil
	}

	normalizedDir := filepath.ToSlash(strings.TrimPrefix(stackDir, "/"))
	if strings.HasSuffix(normalizedPattern, "/**") {
		prefix := strings.TrimSuffix(normalizedPattern, "/**")
		if normalizedDir == prefix {
			return true, nil
		}
		return strings.HasPrefix(normalizedDir, prefix+"/"), nil
	}

	if strings.Contains(normalizedPattern, "**") {
		re, err := regexp.Compile(globToRegexp(normalizedPattern))
		if err != nil {
			return false, err
		}
		return re.MatchString(normalizedDir), nil
	}

	return path.Match(normalizedPattern, normalizedDir)
}

func globToRegexp(glob string) string {
	var b strings.Builder
	b.WriteString("^")

	for i := 0; i < len(glob); i++ {
		ch := glob[i]
		switch ch {
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '^', '$', '{', '}', '[', ']', '\\':
			b.WriteByte('\\')
			b.WriteByte(ch)
		default:
			b.WriteByte(ch)
		}
	}

	b.WriteString("$")
	return b.String()
}
