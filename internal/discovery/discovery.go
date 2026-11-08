package discovery

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

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

	// Warnf receives non-fatal warnings, such as unknown atlantis_*
	// globals. A nil Warnf discards them.
	Warnf func(format string, args ...any)
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
	ApplyRequirements []string // nil = use default; empty = explicitly none
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

	// Keyed by StackInfo.Dir.
	overridesByDir := make(map[string]stackOverrides, len(stacks))

	allStacks := make([]StackInfo, 0, len(stacks))
	for _, sortable := range stacks {
		stack := sortable.Stack
		overrides, err := atlantisOverrides(root, stack)
		overridesByDir[normalizeStackPath(stack.Dir.String())] = stackOverrides{overrides: overrides, err: err}

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
			matched, err := matchesPathOrParent(opts.PathFilter, stackDir)
			if err != nil {
				return Result{}, fmt.Errorf("matching path filter %q for stack %q: %w", opts.PathFilter, stackDir, err)
			}
			if !matched {
				continue
			}
		}

		stored := overridesByDir[stackDir]
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
			WhenModified:      cloneKeepingNil(overrides.WhenModified),
			ApplyRequirements: cloneKeepingNil(overrides.ApplyRequirements),
		})
	}

	if opts.ExcludeTags != "" || opts.ExcludePath != "" {
		filtered := make([]StackInfo, 0, len(discovered))
		for _, stack := range discovered {
			if _, excluded := excludedPaths[stack.Dir]; excluded {
				continue
			}

			if opts.ExcludePath != "" {
				matched, err := matchesPathOrParent(opts.ExcludePath, stack.Dir)
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

	if opts.Warnf != nil {
		warnUnknownGlobals(discovered, overridesByDir, opts.Warnf)
	}

	return Result{Stacks: discovered, AllStacks: allStacks}, nil
}

// warnUnknownGlobals reports atlantis_* globals that the generator does not
// read, usually typos (atlantis_workflows), once per global, naming the
// selected stacks that see it. Globals are inherited, so one typo in a
// parent directory reaches many stacks.
func warnUnknownGlobals(stacks []StackInfo, overrides map[string]stackOverrides, warnf func(string, ...any)) {
	dirsByKey := map[string][]string{}
	for _, stack := range stacks {
		for _, key := range overrides[stack.Dir].overrides.UnknownKeys {
			dirsByKey[key] = append(dirsByKey[key], stack.Dir)
		}
	}

	keys := make([]string, 0, len(dirsByKey))
	for key := range dirsByKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	const maxListed = 3
	for _, key := range keys {
		dirs := dirsByKey[key]
		where := strings.Join(dirs, ", ")
		if len(dirs) > maxListed {
			where = fmt.Sprintf("%s and %d more", strings.Join(dirs[:maxListed], ", "), len(dirs)-maxListed)
		}
		warnf("unknown global %q (stacks: %s) is ignored; known atlantis globals: %s",
			key, where, strings.Join(globalsExtract.KnownGlobals, ", "))
	}
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

// cloneKeepingNil copies s, keeping nil and empty distinct: for overrides
// such as WhenModified and ApplyRequirements, nil means "use the default"
// while an empty list is an explicit override.
func cloneKeepingNil(s []string) []string {
	if s == nil {
		return nil
	}
	return append([]string{}, s...)
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

// matchesPathOrParent reports whether pattern matches stackDir or any of its
// parent directories, so a pattern selects the stacks at or under every
// directory it matches: "infra" selects infra and infra/vpc, "*/vpc" selects
// a/vpc and a/vpc/subnet. This mirrors how a directory scopes stacks in
// Terramate (`terramate run` in a directory includes its nested stacks).
func matchesPathOrParent(pattern, stackDir string) (bool, error) {
	dir := filepath.ToSlash(strings.TrimPrefix(stackDir, "/"))
	for {
		matched, err := matchesPathFilter(pattern, dir)
		if err != nil || matched {
			return matched, err
		}
		parent := path.Dir(dir)
		if parent == dir || parent == "." || parent == "/" {
			return false, nil
		}
		dir = parent
	}
}

// matchesPathFilter reports whether pattern matches stackDir itself.
func matchesPathFilter(pattern, stackDir string) (bool, error) {
	normalizedPattern := normalizePathPattern(pattern)
	if normalizedPattern == "" {
		return true, nil
	}

	normalizedDir := filepath.ToSlash(strings.TrimPrefix(stackDir, "/"))
	if !strings.Contains(normalizedPattern, "**") {
		return path.Match(normalizedPattern, normalizedDir)
	}

	expr, err := globToRegexp(normalizedPattern)
	if err != nil {
		return false, err
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return false, err
	}
	return re.MatchString(normalizedDir), nil
}

// normalizePathPattern makes a path filter relative to the project root and
// drops redundant separators and "." segments, so "infra", "/infra",
// "./infra" and "infra/" are the same filter. It returns "" (no filter) for
// an empty pattern or "/". "." stays "." and matches the root stack.
func normalizePathPattern(pattern string) string {
	p := filepath.ToSlash(pattern)
	if p == "" {
		return ""
	}
	return strings.TrimPrefix(path.Clean(p), "/")
}

// globToRegexp translates a path glob containing "**" into a regular
// expression. Everything except "**" follows path.Match: '*' and '?' stay
// within one segment, '[...]' is a character class ('^' negates it) and '\\'
// escapes the next character. "**" as a whole segment matches any number of
// segments, including none: "**/vpc" matches "vpc", "a/**/b" matches "a/b"
// and "a/**" matches "a". Elsewhere "**" matches any run of characters.
func globToRegexp(glob string) (string, error) {
	var b strings.Builder
	b.WriteString("(?s)^")

	for i := 0; i < len(glob); i++ {
		switch ch := glob[i]; ch {
		case '/':
			if glob[i:] == "/**" {
				b.WriteString("(?:/.*)?")
				i = len(glob)
				continue
			}
			b.WriteByte('/')
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				i++
				if i+1 < len(glob) && glob[i+1] == '/' {
					// "**/" matches zero or more whole directories.
					b.WriteString("(?:.*/)?")
					i++
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '[':
			class, n, err := globClassToRegexp(glob[i:])
			if err != nil {
				return "", err
			}
			b.WriteString(class)
			i += n - 1
		case '\\':
			if i+1 == len(glob) {
				return "", path.ErrBadPattern
			}
			i++
			fallthrough
		default:
			r, size := utf8.DecodeRuneInString(glob[i:])
			b.WriteString(regexp.QuoteMeta(string(r)))
			i += size - 1
		}
	}

	b.WriteString("$")
	return b.String(), nil
}

// globClassToRegexp translates the path.Match character class at the start
// of glob ("[...]") into a regular expression and returns how many bytes of
// glob it used. Malformed classes return path.ErrBadPattern, as path.Match
// does.
func globClassToRegexp(glob string) (expr string, n int, err error) {
	rest := glob[1:]
	negated := strings.HasPrefix(rest, "^")
	if negated {
		rest = rest[1:]
	}

	var ranges strings.Builder
	count := 0
	for {
		if strings.HasPrefix(rest, "]") && count > 0 {
			rest = rest[1:]
			break
		}
		var lo, hi rune
		if lo, rest, err = classChar(rest); err != nil {
			return "", 0, err
		}
		hi = lo
		if rest[0] == '-' {
			if hi, rest, err = classChar(rest[1:]); err != nil {
				return "", 0, err
			}
		}
		// path.Match accepts an inverted range and never matches it;
		// regexp would reject it.
		if lo <= hi {
			fmt.Fprintf(&ranges, `\x{%x}-\x{%x}`, lo, hi)
		}
		count++
	}

	used := len(glob) - len(rest)
	switch {
	// Like path.Match, a negated class also matches '/'.
	case ranges.Len() == 0 && negated:
		return `.`, used, nil
	case ranges.Len() == 0:
		return `[^\x{0}-\x{10ffff}]`, used, nil // matches nothing
	case negated:
		return `[^` + ranges.String() + `]`, used, nil
	default:
		return `[` + ranges.String() + `]`, used, nil
	}
}

// classChar reads one (possibly escaped) character of a character class,
// with path.Match's rules for what is malformed.
func classChar(chunk string) (r rune, rest string, err error) {
	if chunk == "" || chunk[0] == '-' || chunk[0] == ']' {
		return 0, "", path.ErrBadPattern
	}
	if chunk[0] == '\\' {
		chunk = chunk[1:]
		if chunk == "" {
			return 0, "", path.ErrBadPattern
		}
	}
	r, size := utf8.DecodeRuneInString(chunk)
	if r == utf8.RuneError && size == 1 {
		return 0, "", path.ErrBadPattern
	}
	rest = chunk[size:]
	if rest == "" {
		return 0, "", path.ErrBadPattern
	}
	return r, rest, nil
}

// stackOverrides is the result of reading one stack's atlantis_* globals.
type stackOverrides struct {
	overrides globalsExtract.AtlantisOverrides
	err       error
}
