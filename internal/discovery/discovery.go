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
	Dir         string // relative path from root (no leading slash)
	Tags        []string
	After       []string
	Before      []string

	// Atlantis overrides extracted from globals
	Skip              bool
	Workflow          string
	AutoplanEnabled   *bool // nil = use CLI default
	TerraformVersion  string
	ExtraDeps         []string // additional when_modified entries
	WhenModified      []string // full override of when_modified (nil = use default)
	ApplyRequirements []string
}

// Discover loads stacks from a Terramate root and extracts Atlantis overrides.

func Discover(opts DiscoverOptions) ([]StackInfo, error) {
	rootDir := opts.RootDir
	if rootDir == "" {
		rootDir = "."
	}

	absRootDir, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolving root path %q: %w", rootDir, err)
	}

	root, err := config.LoadRoot(absRootDir, false)
	if err != nil {
		return nil, fmt.Errorf("loading terramate root: %w", err)
	}

	stacks, err := config.LoadAllStacks(root, root.Tree())
	if err != nil {
		return nil, fmt.Errorf("loading stacks: %w", err)
	}

	var allowedPaths map[string]struct{}
	if opts.TagFilter != "" {
		filteredPaths, err := root.StacksByTagsFilters([]string{opts.TagFilter})
		if err != nil {
			return nil, fmt.Errorf("filtering stacks by tag %q: %w", opts.TagFilter, err)
		}

		allowedPaths = make(map[string]struct{}, len(filteredPaths))
		for _, p := range filteredPaths {
			allowedPaths[p.String()] = struct{}{}
		}
	}

	if opts.Changed {
		gitWrapper, err := git.WithConfig(git.Config{WorkingDir: absRootDir})
		if err != nil {
			return nil, fmt.Errorf("initializing git wrapper: %w", err)
		}

		baseRef := opts.BaseRef
		if baseRef == "" {
			baseRef = "main"
		}

		mgr := tmstack.NewGitAwareManager(root, gitWrapper)
		report, err := mgr.ListChanged(tmstack.ChangeConfig{BaseRef: baseRef})
		if err != nil {
			return nil, fmt.Errorf("listing changed stacks against %q: %w", baseRef, err)
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
			return nil, fmt.Errorf("filtering stacks by excluded tag %q: %w", opts.ExcludeTags, err)
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

		stackDir := strings.TrimPrefix(stack.Dir.String(), "/")
		if opts.PathFilter != "" {
			matched, err := matchesPathFilter(opts.PathFilter, stackDir)
			if err != nil {
				return nil, fmt.Errorf("matching path filter %q for stack %q: %w", opts.PathFilter, stackDir, err)
			}
			if !matched {
				continue
			}
		}

		report := globals.ForStack(root, stack)
		if err := report.AsError(); err != nil {
			return nil, fmt.Errorf("loading globals for stack %q: %w", stack.Dir.String(), err)
		}

		overrides := globalsExtract.AtlantisOverrides{}
		if report.Globals != nil {
			globalsMap := report.Globals.AsValueMap()
			overrides, err = globalsExtract.ExtractAtlantisGlobals(globalsMap)
			if err != nil {
				return nil, fmt.Errorf("extracting atlantis globals for stack %q: %w", stack.Dir.String(), err)
			}
		}

		discovered = append(discovered, StackInfo{
			Name:              stack.Name,
			ID:                stack.ID,
			Description:       stack.Description,
			Dir:               stackDir,
			Tags:              append([]string(nil), stack.Tags...),
			After:             append([]string(nil), stack.After...),
			Before:            append([]string(nil), stack.Before...),
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
			if _, excluded := excludedPaths[normalizeStackPath(stack.Dir)]; excluded {
				continue
			}

			if opts.ExcludePath != "" {
				matched, err := matchesPathFilter(opts.ExcludePath, stack.Dir)
				if err != nil {
					return nil, fmt.Errorf("matching exclude path filter %q for stack %q: %w", opts.ExcludePath, stack.Dir, err)
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

	return discovered, nil
}

func normalizeStackPath(p string) string {
	return filepath.ToSlash(strings.TrimPrefix(strings.TrimSpace(p), "/"))
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
