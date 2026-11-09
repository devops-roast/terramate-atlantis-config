package atlantis

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/devops-roast/terramate-atlantis-config/internal/discovery"
)

// Values for Options.ProjectNameStrategy.
const (
	// NameStrategyAutoStrip strips ProjectNamePrefix, or else the longest
	// path prefix every stack shares.
	NameStrategyAutoStrip = "auto-strip"
	// NameStrategyStackName uses the stack's name (or dir name), prefixed
	// with up to 3 parent dirs.
	NameStrategyStackName = "stack-name"
	// NameStrategyLastN uses the last ProjectNameDepth path segments.
	NameStrategyLastN = "last-n"
	// NameStrategyFull uses the whole path.
	NameStrategyFull = "full"
)

// rootStackName is the naming path used for a stack at the repository root,
// whose dir (".") carries no usable name.
const rootStackName = "root"

// ValidProjectName reports whether Atlantis accepts name as a project name.
// It mirrors Atlantis' own rule (server/core/config/raw/project.go): once
// slashes are replaced by dashes, the name must be URL safe, i.e. unchanged
// by url.QueryEscape. That allows ASCII letters, digits, '-', '_', '.', '~'
// and '/'.
func ValidProjectName(name string) bool {
	if name == "" {
		return false
	}
	withoutSlashes := strings.ReplaceAll(name, "/", "-")
	return withoutSlashes == url.QueryEscape(withoutSlashes)
}

// sanitizeProjectName turns a path into a project name Atlantis accepts: any
// character that isn't URL safe, '/' included, becomes '-', and leading
// dashes are trimmed. The result may be empty or equal another path's, so
// callers must run ensureUniqueNames.
func sanitizeProjectName(p string) string {
	var b strings.Builder
	b.Grow(len(p))
	for _, r := range p {
		if isURLSafe(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return strings.TrimLeft(b.String(), "-")
}

func isURLSafe(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r == '-', r == '_', r == '.', r == '~':
		return true
	default:
		return false
	}
}

// namingPath is the path a project name is derived from. It is the stack dir,
// except for the root stack, which is named "root".
func namingPath(dir string) string {
	if dir == "" || dir == discovery.RootDir {
		return rootStackName
	}
	return dir
}

// namingContext returns the stacks names are computed against: the explicit
// naming set plus any generated stack missing from it. Skipped stacks never
// produce a project, so they don't take part in naming either.
func namingContext(stacks, naming []discovery.StackInfo) []discovery.StackInfo {
	seen := make(map[string]struct{}, len(naming)+len(stacks))
	ctx := make([]discovery.StackInfo, 0, len(naming)+len(stacks))
	for _, list := range [][]discovery.StackInfo{naming, stacks} {
		for _, s := range list {
			dir := stackDir(s.Dir)
			if _, ok := seen[dir]; ok {
				continue
			}
			seen[dir] = struct{}{}
			if !s.Skip {
				ctx = append(ctx, s)
			}
		}
	}
	return ctx
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
		prefix:   normalizePrefix(opts.ProjectNamePrefix),
		depth:    opts.ProjectNameDepth,
	}

	if strategy == NameStrategyAutoStrip && n.prefix == "" {
		n.commonPrefix = detectCommonPrefix(stacks)
	}

	n.namesByDir = n.buildUniqueNames(stacks)
	return n
}

// normalizePrefix cleans --project-name-prefix the way stack dirs are
// written, so "stacks", "/stacks", "./stacks" and "stacks/" all strip
// "stacks". "" and "." mean no prefix.
func normalizePrefix(prefix string) string {
	prefix = filepath.ToSlash(prefix)
	if prefix == "" {
		return ""
	}
	prefix = strings.Trim(path.Clean(prefix), "/")
	if prefix == "." {
		return ""
	}
	return prefix
}

func (n *projectNamer) nameFor(dir string) string {
	if name, ok := n.namesByDir[dir]; ok {
		return name
	}
	return sanitizeProjectName(namingPath(dir))
}

// buildUniqueNames maps each stack dir to its name; the names are unique and
// valid after this returns.
func (n *projectNamer) buildUniqueNames(stacks []discovery.StackInfo) map[string]string {
	rawNames := make(map[string]string, len(stacks))
	for _, s := range stacks {
		dir := stackDir(s.Dir)
		rawNames[dir] = n.rawName(s, dir)
	}

	seen := make(map[string][]string)
	for dir, name := range rawNames {
		seen[name] = append(seen[name], dir)
	}

	stackByDir := make(map[string]discovery.StackInfo, len(stacks))
	for _, s := range stacks {
		stackByDir[stackDir(s.Dir)] = s
	}

	result := make(map[string]string, len(stacks))
	for dir, name := range rawNames {
		if len(seen[name]) > 1 {
			result[dir] = n.disambiguate(stackByDir, dir, seen[name])
		} else {
			result[dir] = name
		}
	}

	ensureUniqueNames(result)
	return result
}

func (n *projectNamer) rawName(stack discovery.StackInfo, dir string) string {
	switch n.strategy {
	case NameStrategyStackName:
		return n.stackNameRaw(stack, dir)
	case NameStrategyLastN:
		return n.lastNRaw(dir)
	case NameStrategyFull:
		return sanitizeProjectName(namingPath(dir))
	default:
		return n.autoStripRaw(dir)
	}
}

// disambiguate resolves a collision between the raw names of the dirs in
// conflicting: starting from the strategy's own name, it adds one parent
// segment at a time until dir's name differs from every other one, so the
// result is never shorter than the strategy's name and keeps its form (e.g.
// the stack name for stack-name). When dir has no parent segments left, it
// falls back to the full path.
func (n *projectNamer) disambiguate(stacks map[string]discovery.StackInfo, dir string, conflicting []string) string {
	for extra := 1; ; extra++ {
		candidate, ok := n.nameCandidate(stacks[dir], dir, extra)
		if !ok {
			return sanitizeProjectName(namingPath(dir))
		}
		unique := true
		for _, other := range conflicting {
			if other == dir {
				continue
			}
			otherCandidate, ok := n.nameCandidate(stacks[other], other, extra)
			if !ok {
				otherCandidate = sanitizeProjectName(namingPath(other))
			}
			if candidate == otherCandidate {
				unique = false
				break
			}
		}
		if unique {
			return candidate
		}
	}
}

// nameCandidate returns the strategy's name for dir with extra more parent
// segments, and false when the path has fewer segments than that. With
// extra = 0 it equals rawName.
func (n *projectNamer) nameCandidate(stack discovery.StackInfo, dir string, extra int) (string, bool) {
	p := namingPath(dir)
	parts := strings.Split(p, "/")

	if n.strategy == NameStrategyStackName {
		parents := parts[:len(parts)-1]
		need := len(n.stackNameContext(p)) + extra
		if need > len(parents) {
			return "", false
		}
		context := strings.Join(parents[len(parents)-need:], "/")
		if context == "" {
			return sanitizeProjectName(stackNameOf(stack, p)), true
		}
		return sanitizeProjectName(context + "/" + stackNameOf(stack, p)), true
	}

	var base int
	switch n.strategy {
	case NameStrategyFull:
		base = len(parts)
	case NameStrategyLastN:
		base = min(n.lastNDepth(), len(parts))
	default:
		base = len(strings.Split(n.autoStripPath(p), "/"))
	}
	need := base + extra
	if need > len(parts) {
		return "", false
	}
	return trailingName(dir, need), true
}

func trailingName(dir string, depth int) string {
	parts := strings.Split(namingPath(dir), "/")
	return sanitizeProjectName(strings.Join(parts[max(0, len(parts)-depth):], "/"))
}

// ensureUniqueNames is the final guarantee that no two projects share a name.
// Disambiguation only compares stacks whose raw names collide, so its output
// can still clash with another stack's name, and sanitizing can map distinct
// paths ("a/b" and "a-b") to the same name. Any remaining clash first falls
// back to the full path ("stack" if that sanitizes to nothing), and if that
// still clashes, a numeric suffix is added in dir order so the result is
// deterministic.
func ensureUniqueNames(names map[string]string) {
	dirs := make([]string, 0, len(names))
	for dir := range names {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	counts := make(map[string]int, len(names))
	for _, name := range names {
		counts[name]++
	}
	for _, dir := range dirs {
		if names[dir] == "" || counts[names[dir]] > 1 {
			names[dir] = sanitizeProjectName(namingPath(dir))
		}
		if names[dir] == "" {
			names[dir] = "stack"
		}
	}

	used := make(map[string]struct{}, len(names))
	for _, dir := range dirs {
		name := names[dir]
		if _, taken := used[name]; taken {
			for i := 2; ; i++ {
				candidate := fmt.Sprintf("%s-%d", name, i)
				if _, taken := used[candidate]; !taken {
					name = candidate
					break
				}
			}
		}
		used[name] = struct{}{}
		names[dir] = name
	}
}

// stripPathPrefix removes prefix from p only on a path segment boundary.
// It returns "" when p equals prefix, and p unchanged when p is not under it.
func stripPathPrefix(p, prefix string) string {
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return p
	}
	if p == prefix {
		return ""
	}
	if rest, ok := strings.CutPrefix(p, prefix+"/"); ok {
		return rest
	}
	return p
}

func (n *projectNamer) autoStripRaw(dir string) string {
	return sanitizeProjectName(n.autoStripPath(namingPath(dir)))
}

// autoStripPath is p without the explicit or detected common prefix.
func (n *projectNamer) autoStripPath(p string) string {
	prefix := n.prefix
	if prefix == "" {
		prefix = n.commonPrefix
	}

	stripped := stripPathPrefix(p, prefix)
	if stripped == "" {
		// The stack is the prefix itself (a lone stack, or a parent stack
		// whose children share its path): never strip a name to nothing.
		stripped = p
	}
	return stripped
}

func (n *projectNamer) stackNameRaw(stack discovery.StackInfo, dir string) string {
	p := namingPath(dir)
	context := n.stackNameContext(p)
	if len(context) == 0 {
		return sanitizeProjectName(stackNameOf(stack, p))
	}
	return sanitizeProjectName(strings.Join(context, "/") + "/" + stackNameOf(stack, p))
}

// stackNameContext returns the parent dirs (at most 3, after stripping the
// explicit prefix) that prefix a stack-name project name.
func (n *projectNamer) stackNameContext(p string) []string {
	stripped := p
	if n.prefix != "" {
		stripped = stripPathPrefix(p, n.prefix)
	}

	parent := path.Dir(stripped)
	if stripped == "" || parent == "." || parent == "/" {
		return nil
	}

	contextParts := strings.Split(parent, "/")
	if len(contextParts) > 3 {
		contextParts = contextParts[len(contextParts)-3:]
	}
	return contextParts
}

// stackNameOf is the stack's name field, or its dir name when unset.
func stackNameOf(stack discovery.StackInfo, p string) string {
	if stack.Name != "" {
		return stack.Name
	}
	return path.Base(p)
}

func (n *projectNamer) lastNRaw(dir string) string {
	return trailingName(dir, n.lastNDepth())
}

// lastNDepth defaults a ProjectNameDepth below 1 to the CLI default of 3.
func (n *projectNamer) lastNDepth() int {
	if n.depth <= 0 {
		return 3
	}
	return n.depth
}

// detectCommonPrefix returns the longest path prefix shared by every stack
// dir, on segment boundaries (the root stack is ignored). Dirs are sorted so
// a parent stack comes first and may itself be the prefix of its children;
// autoStripPath then falls back to the full path for it.
func detectCommonPrefix(stacks []discovery.StackInfo) string {
	dirs := make([]string, 0, len(stacks))
	for _, s := range stacks {
		dir := stackDir(s.Dir)
		if dir != discovery.RootDir {
			dirs = append(dirs, dir)
		}
	}
	if len(dirs) == 0 {
		return ""
	}
	sort.Strings(dirs)

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
