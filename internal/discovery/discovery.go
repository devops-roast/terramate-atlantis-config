// Package discovery loads Terramate stacks, applies the stack filters and
// reads each stack's atlantis_* globals and after/before ordering.
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

type StackInfo struct {
	// Name is the stack's name attribute, "" when unset (see declaredName).
	Name        string
	ID          string
	Description string
	Dir         string // relative path from root (no leading slash, "." for the root stack)
	Tags        []string
	After       []string // raw stack.after entries as written in HCL
	Before      []string // raw stack.before entries as written in HCL

	// AfterDirs and BeforeDirs are the stack.after/stack.before entries
	// resolved to stack directories (same format as Dir), following
	// Terramate's own ordering semantics: a path expands to every stack at
	// or below it, and a "tag:" query to every stack at or below the
	// matching stacks.
	AfterDirs  []string
	BeforeDirs []string

	// From the stack's atlantis_* globals.
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
	// dir, tags, resolved ordering and skip only), regardless of filters. It
	// gives project naming a stable context, so names don't depend on which
	// stacks a run selected, and lets ordering be followed through stacks
	// that were not selected.
	AllStacks []StackInfo
}

// Discover returns the stacks the filters select, with their overrides, and
// every stack in the repository. Both lists are sorted by Dir.
func Discover(opts DiscoverOptions) (Result, error) {
	rootDir := opts.RootDir
	if rootDir == "" {
		rootDir = "."
	}

	for _, pattern := range []struct{ flag, value string }{
		{"path filter", opts.PathFilter},
		{"exclude path filter", opts.ExcludePath},
	} {
		if err := checkPathPattern(pattern.value); err != nil {
			return Result{}, fmt.Errorf("%s %q: %w", pattern.flag, pattern.value, err)
		}
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

	orderByDir := make(map[string]stackOrder, len(stacks))
	allStacks := make([]StackInfo, 0, len(stacks))
	for _, sortable := range stacks {
		stack := sortable.Stack
		overrides, err := atlantisOverrides(root, stack)
		overridesByDir[normalizeStackPath(stack.Dir.String())] = stackOverrides{overrides: overrides, err: err}

		// Resolved once per stack: resolving logs Terramate warnings (e.g.
		// for a path that doesn't exist), which must not repeat.
		order := resolveOrder(root, stack)
		orderByDir[normalizeStackPath(stack.Dir.String())] = order

		// Errors only fail the run for selected stacks (below); here a stack
		// whose globals or order entries can't be read counts as not skipped
		// and unordered.
		afterDirs, beforeDirs := order.afterDirs, order.beforeDirs
		if order.err != nil {
			afterDirs, beforeDirs = nil, nil
		}

		allStacks = append(allStacks, StackInfo{
			Name:        declaredName(root, stack),
			ID:          stack.ID,
			Description: stack.Description,
			Dir:         normalizeStackPath(stack.Dir.String()),
			Tags:        append([]string(nil), stack.Tags...),
			AfterDirs:   afterDirs,
			BeforeDirs:  beforeDirs,
			Skip:        err == nil && overrides.Skip,
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
			allowedPaths[normalizeStackPath(p.String())] = struct{}{}
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
			changedPaths[normalizeStackPath(entry.Stack.Dir.String())] = struct{}{}
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
		stackDir := normalizeStackPath(stack.Dir.String())
		if allowedPaths != nil {
			if _, ok := allowedPaths[stackDir]; !ok {
				continue
			}
		}

		if opts.PathFilter != "" {
			matched, err := matchesPathOrParent(opts.PathFilter, stackDir)
			if err != nil {
				return Result{}, fmt.Errorf("matching path filter %q for stack %q: %w", opts.PathFilter, stackDir, err)
			}
			if !matched {
				continue
			}
		}

		// Exclusions are checked before the stored globals and order errors,
		// so an excluded stack can't fail the run.
		if _, excluded := excludedPaths[stackDir]; excluded {
			continue
		}
		if opts.ExcludePath != "" {
			matched, err := matchesPathOrParent(opts.ExcludePath, stackDir)
			if err != nil {
				return Result{}, fmt.Errorf("matching exclude path filter %q for stack %q: %w", opts.ExcludePath, stackDir, err)
			}
			if matched {
				continue
			}
		}

		stored := overridesByDir[stackDir]
		if stored.err != nil {
			return Result{}, stored.err
		}
		overrides := stored.overrides

		order := orderByDir[stackDir]
		if order.err != nil {
			return Result{}, order.err
		}
		afterDirs, beforeDirs := order.afterDirs, order.beforeDirs

		discovered = append(discovered, StackInfo{
			Name:              declaredName(root, stack),
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

// declaredName returns the stack's name attribute, or "" when it is unset.
// Terramate fills an unset name with the base name of the stack's absolute
// directory, which for the root stack is the checkout directory: a name that
// differs between a local clone and Atlantis' working copy.
func declaredName(root *config.Root, stack *config.Stack) string {
	if tree, ok := root.Lookup(stack.Dir); ok && tree.Node.Stack != nil {
		return tree.Node.Stack.Name
	}
	return stack.Name
}

// stackOrder is a stack's resolved stack.after and stack.before entries.
type stackOrder struct {
	afterDirs  []string
	beforeDirs []string
	err        error
}

func resolveOrder(root *config.Root, stack *config.Stack) stackOrder {
	afterDirs, err := resolveOrderEntries(root, stack, stack.After)
	if err != nil {
		return stackOrder{err: fmt.Errorf("resolving stack.after for stack %q: %w", stack.Dir.String(), err)}
	}
	beforeDirs, err := resolveOrderEntries(root, stack, stack.Before)
	if err != nil {
		return stackOrder{err: fmt.Errorf("resolving stack.before for stack %q: %w", stack.Dir.String(), err)}
	}
	return stackOrder{afterDirs: afterDirs, beforeDirs: beforeDirs}
}

// resolveOrderEntries resolves stack.after / stack.before entries to stack
// dirs the way `terramate run` orders stacks: a path (relative to the stack,
// or absolute from the project root) selects every stack at or below it, and
// "tag:<filter>" every stack at or below the stacks matching the filter. The
// stack itself is never included. Paths that don't exist are ignored, as
// Terramate does.
func resolveOrderEntries(root *config.Root, stack *config.Stack, entries []string) ([]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	self := stack.Dir.String()
	resolved := map[string]struct{}{}

	for _, entry := range entries {
		paths := []string{entry}
		if filter, ok := strings.CutPrefix(entry, "tag:"); ok {
			// Like `terramate run`, a tag query selects the matching
			// stacks' dirs, which then expand like any path entry: to the
			// matching stacks and every stack nested below them.
			tagged, err := root.StacksByTagsFilters([]string{filter})
			if err != nil {
				return nil, fmt.Errorf("invalid order entry %q: %w", entry, err)
			}
			paths = paths[:0]
			for _, p := range tagged {
				paths = append(paths, p.String())
			}
			if len(paths) == 0 {
				continue
			}
		}

		for _, tree := range root.StacksByPaths(stack.Dir, paths...) {
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
	normalized := filepath.ToSlash(strings.TrimPrefix(p, "/"))
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

// intersectAllowedPaths treats a nil current as "no filter yet".
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
//
// The walk stops before the root, so "." selects the root stack only, not
// every stack below it.
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

func matchesPathFilter(pattern, stackDir string) (bool, error) {
	normalizedPattern := normalizePathPattern(pattern)
	if normalizedPattern == "" {
		return true, nil
	}

	normalizedDir := filepath.ToSlash(strings.TrimPrefix(stackDir, "/"))
	if normalizedDir == RootDir {
		// "." is a placeholder for the root, not a directory named ".":
		// only the pattern "." or one matching the empty path ("**")
		// selects the root stack. Otherwise ".*" or "?" would match it.
		if normalizedPattern == RootDir {
			return true, nil
		}
		normalizedDir = ""
	}
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
// "./infra" and "infra/" are the same filter. It returns "" for an empty
// pattern or "/", which matches every stack. "." stays "." and matches the
// root stack.
func normalizePathPattern(pattern string) string {
	p := filepath.ToSlash(pattern)
	if p == "" {
		return ""
	}
	return strings.TrimPrefix(path.Clean(p), "/")
}

// checkPathPattern rejects a path filter that leads out of the root, such
// as "../infra": stack dirs are relative to the root, so it could never
// match one, and path.Clean would turn "/.." into "/", which matches all.
func checkPathPattern(pattern string) error {
	rel := path.Clean(strings.TrimLeft(filepath.ToSlash(pattern), "/"))
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return fmt.Errorf("the pattern leads outside the root; path filters are relative to the root directory")
	}
	return nil
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

type stackOverrides struct {
	overrides globalsExtract.AtlantisOverrides
	err       error
}
