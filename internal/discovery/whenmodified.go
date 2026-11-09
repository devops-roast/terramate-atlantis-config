package discovery

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/terramate-io/terramate/config"
	"github.com/terramate-io/terramate/hcl"
	"github.com/terramate-io/terramate/tf"
	"github.com/zclconf/go-cty/cty"
)

// configDepsResolver computes the when_modified entries that
// DiscoverOptions.WhenModifiedFromConfig adds to a stack's project: the
// files outside the stack's own *.tf files that change what the stack
// deploys. Results that many stacks share (imports of a parent directory,
// module calls of a module) are cached.
type configDepsResolver struct {
	root    *config.Root
	rootDir string // absolute host path of the project root
	hclOpts []hcl.Option
	tmgen   bool
	warnf   func(format string, args ...any)

	importsByDir  map[string][]string // abs dir -> root-relative files it imports, transitively
	importsByFile map[string][]string // abs file -> root-relative files it imports, transitively
	modulesByDir  map[string][]string // abs dir -> root-relative local module dirs its files call
	warned        map[string]struct{}
}

func newConfigDepsResolver(root *config.Root, warnf func(string, ...any)) *configDepsResolver {
	return &configDepsResolver{
		root:    root,
		rootDir: root.HostDir(),
		hclOpts: append([]hcl.Option{hcl.WithExperiments(root.Tree().Node.Experiments()...)}, root.HCLOptions()...),
		tmgen:   root.HasExperiment("tmgen"),
		warnf:   warnf,

		importsByDir:  map[string][]string{},
		importsByFile: map[string][]string{},
		modulesByDir:  map[string][]string{},
		warned:        map[string]struct{}{},
	}
}

// warnOnce reports a warning the first time it is seen: a module or a
// parent directory is shared by many stacks.
func (r *configDepsResolver) warnOnce(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if _, seen := r.warned[msg]; seen || r.warnf == nil {
		return
	}
	r.warned[msg] = struct{}{}
	r.warnf("%s", msg)
}

// stackDeps returns the when_modified entries for stack, relative to the
// stack directory, with forward slashes, in this order:
//
//  1. "*.tm.hcl" and "*.tm" of the stack directory and of every parent
//     directory up to the root ("../*.tm.hcl", "../../*.tm.hcl", ...), plus
//     "*.tmgen" of the stack directory when the tmgen experiment is on.
//     Terramate reads every config file of these directories for the stack
//     (globals, generate blocks, imports), so a glob also covers files the
//     pull request adds or deletes.
//  2. The files that `import` blocks in those directories pull in,
//     transitively, as exact paths, sorted.
//  3. The stack's `watch` files, sorted.
//  4. Every local module (source "./..." or "../...") that the stack's .tf
//     and .tofu files call, recursively, as "<dir>/**", sorted. Like
//     Terramate's change detection, any file under a module directory
//     counts. A module nested in another listed module is covered by it and
//     left out.
func (r *configDepsResolver) stackDeps(stack *config.Stack) ([]string, error) {
	stackDir := strings.TrimPrefix(stack.Dir.String(), "/") // "" for the root stack

	var deps []string
	ups := ""
	var chain []string // abs dirs from the stack up to the root
	for dir := stack.Dir; ; dir = dir.Dir() {
		deps = append(deps, ups+"*.tm.hcl", ups+"*.tm")
		if ups == "" && r.tmgen {
			deps = append(deps, "*.tmgen")
		}
		chain = append(chain, dir.HostPath(r.rootDir))
		if dir.String() == "/" {
			break
		}
		ups += "../"
	}

	imported := map[string]struct{}{}
	for _, dir := range chain {
		files, err := r.dirImports(dir)
		if err != nil {
			return nil, fmt.Errorf("resolving imports for stack %q: %w", stack.Dir.String(), err)
		}
		for _, file := range files {
			imported[file] = struct{}{}
		}
	}
	for _, file := range sortedSet(imported) {
		deps = append(deps, escapePattern(relativePath(stackDir, file)))
	}

	watched := make([]string, 0, len(stack.Watch))
	for _, file := range stack.Watch {
		watched = append(watched, strings.TrimPrefix(file.String(), "/"))
	}
	sort.Strings(watched)
	for _, file := range watched {
		deps = append(deps, escapePattern(relativePath(stackDir, file)))
	}

	for _, dir := range outermostDirs(r.stackModules(stack.Dir.HostPath(r.rootDir))) {
		rel := relativePath(stackDir, dir)
		if rel == "." {
			deps = append(deps, "**")
			continue
		}
		deps = append(deps, escapePattern(rel)+"/**")
	}

	return deps, nil
}

// dirImports returns the files that the import blocks of the config files
// in absDir pull in, transitively, as root-relative paths. The config files
// are read with the Terramate SDK parser, so import blocks are found the way
// Terramate finds them.
func (r *configDepsResolver) dirImports(absDir string) ([]string, error) {
	if files, ok := r.importsByDir[absDir]; ok {
		return files, nil
	}

	p, err := hcl.NewTerramateParser(r.rootDir, absDir, r.hclOpts...)
	if err != nil {
		return nil, err
	}
	if err := p.AddDir(absDir); err != nil {
		return nil, err
	}
	files, err := r.parserImports(p, absDir, map[string]struct{}{})
	if err != nil {
		return nil, err
	}
	r.importsByDir[absDir] = files
	return files, nil
}

// fileImports returns absFile itself and the files it imports,
// transitively, as root-relative paths.
func (r *configDepsResolver) fileImports(absFile string, visiting map[string]struct{}) ([]string, error) {
	if files, ok := r.importsByFile[absFile]; ok {
		return files, nil
	}
	// Terramate rejects import cycles when it loads the config; this only
	// keeps a cycle from recursing forever.
	if _, ok := visiting[absFile]; ok {
		return nil, nil
	}
	visiting[absFile] = struct{}{}
	defer delete(visiting, absFile)

	fileDir := filepath.Dir(absFile)
	p, err := hcl.NewTerramateParser(r.rootDir, fileDir, r.hclOpts...)
	if err != nil {
		return nil, err
	}
	if err := p.AddFile(absFile); err != nil {
		return nil, err
	}
	nested, err := r.parserImports(p, fileDir, visiting)
	if err != nil {
		return nil, err
	}

	rel, err := r.rootRelative(absFile)
	if err != nil {
		return nil, err
	}
	files := append([]string{rel}, nested...)
	r.importsByFile[absFile] = files
	return files, nil
}

// parserImports resolves the import blocks of the files added to p, which
// all live in absDir, the way Terramate does (hcl.TerramateParser
// handleImport): the source is a literal string, a path starting with "/"
// is relative to the root and any other path to absDir, and its base name
// may be a glob.
func (r *configDepsResolver) parserImports(p *hcl.TerramateParser, absDir string, visiting map[string]struct{}) ([]string, error) {
	if err := p.ParseHCL(); err != nil {
		return nil, err
	}
	blocks, err := p.Imports()
	if err != nil {
		return nil, err
	}

	set := map[string]struct{}{}
	for _, block := range blocks {
		attr, ok := block.Attributes["source"]
		if !ok {
			continue
		}
		val, diags := attr.Expr.Value(nil)
		if diags.HasErrors() || val.IsNull() || !val.Type().Equals(cty.String) {
			// ParseHCL already applied the imports, so this can't happen.
			return nil, fmt.Errorf("import.source in %s must be a string", absDir)
		}
		src := val.AsString()
		srcDir := path.Dir(src)
		if path.IsAbs(srcDir) {
			srcDir = filepath.Join(r.rootDir, srcDir)
		} else {
			srcDir = filepath.Join(absDir, srcDir)
		}
		matches, err := filepath.Glob(filepath.Join(srcDir, path.Base(src)))
		if err != nil {
			return nil, fmt.Errorf("import.source %q in %s: %w", src, absDir, err)
		}
		for _, match := range matches {
			files, err := r.fileImports(match, visiting)
			if err != nil {
				return nil, err
			}
			for _, file := range files {
				set[file] = struct{}{}
			}
		}
	}
	return sortedSet(set), nil
}

// stackModules returns the local module directories that the .tf and .tofu
// files in absDir call, and the local modules those call in turn, as
// root-relative paths.
func (r *configDepsResolver) stackModules(absDir string) []string {
	found := map[string]struct{}{}
	queue := r.directModules(absDir)
	for len(queue) > 0 {
		dir := queue[0]
		queue = queue[1:]
		if _, seen := found[dir]; seen {
			continue
		}
		found[dir] = struct{}{}
		queue = append(queue, r.directModules(filepath.Join(r.rootDir, filepath.FromSlash(dir)))...)
	}
	return sortedSet(found)
}

// directModules returns the local module directories that the .tf and .tofu
// files in absDir call, as root-relative paths. It follows Terramate's change
// detection (stack.Manager tfModuleChanged): only local sources ("./" or
// "../") count, and a module outside the root is skipped with a warning. A
// file that doesn't parse or a source that isn't a directory is skipped with
// a warning too: Terraform reports those on plan, and autoplan patterns
// shouldn't fail the run for them.
func (r *configDepsResolver) directModules(absDir string) []string {
	if dirs, ok := r.modulesByDir[absDir]; ok {
		return dirs
	}

	entries, err := os.ReadDir(absDir)
	if err != nil {
		r.warnOnce("when_modified from config: reading %s: %v", r.displayPath(absDir), err)
		return nil
	}

	set := map[string]struct{}{}
	for _, entry := range entries {
		if entry.IsDir() || !hasTFExt(entry.Name()) {
			continue
		}
		file := filepath.Join(absDir, entry.Name())
		modules, err := tf.ParseModules(file)
		if err != nil {
			// The error names the file by its absolute path.
			msg := strings.ReplaceAll(err.Error(), r.rootDir+string(filepath.Separator), "")
			r.warnOnce("when_modified from config: skipping module calls in %s: %s", r.displayPath(file), msg)
			continue
		}
		for _, mod := range modules {
			if !mod.IsLocal() {
				continue
			}
			modDir := filepath.Join(absDir, filepath.FromSlash(mod.Source))
			rel, err := r.rootRelative(modDir)
			if err != nil {
				r.warnOnce("when_modified from config: skipping module %q called from %s: it is outside the root directory",
					mod.Source, r.displayPath(file))
				continue
			}
			if st, err := os.Stat(modDir); err != nil || !st.IsDir() {
				r.warnOnce("when_modified from config: skipping module %q called from %s: %s is not a directory",
					mod.Source, r.displayPath(file), rel)
				continue
			}
			set[rel] = struct{}{}
		}
	}

	dirs := sortedSet(set)
	r.modulesByDir[absDir] = dirs
	return dirs
}

// rootRelative converts an absolute host path inside the root into a
// root-relative path with forward slashes ("." for the root itself).
func (r *configDepsResolver) rootRelative(abs string) (string, error) {
	rel, err := filepath.Rel(r.rootDir, abs)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("%s is outside the root directory", abs)
	}
	return rel, nil
}

// displayPath shows a host path relative to the root when it is inside it.
func (r *configDepsResolver) displayPath(abs string) string {
	if rel, err := r.rootRelative(abs); err == nil {
		return rel
	}
	return abs
}

// hasTFExt matches the files Terramate reads module calls from.
func hasTFExt(name string) bool {
	ext := filepath.Ext(name)
	return ext == ".tf" || ext == ".tofu"
}

// relativePath returns target relative to dir, both root-relative paths with
// forward slashes ("" or "." for the root). The result uses forward slashes
// and is "." when they are the same directory.
func relativePath(dir, target string) string {
	split := func(p string) []string {
		p = path.Clean("/" + p)
		if p == "/" {
			return nil
		}
		return strings.Split(p[1:], "/")
	}
	from, to := split(dir), split(target)
	common := 0
	for common < len(from) && common < len(to) && from[common] == to[common] {
		common++
	}
	parts := make([]string, 0, len(from)-common+len(to)-common)
	for range from[common:] {
		parts = append(parts, "..")
	}
	parts = append(parts, to[common:]...)
	if len(parts) == 0 {
		return "."
	}
	return strings.Join(parts, "/")
}

// escapePattern escapes an exact path for Atlantis' when_modified matching
// (moby patternmatcher): '*', '?', '[' and '\' are pattern syntax, and a
// leading '!' makes the entry an exclusion.
func escapePattern(p string) string {
	var b strings.Builder
	for i, ch := range p {
		if strings.ContainsRune(`*?[\`, ch) || (i == 0 && ch == '!') {
			b.WriteByte('\\')
		}
		b.WriteRune(ch)
	}
	return b.String()
}

// outermostDirs drops the directories nested in another directory of the
// sorted list dirs: "<dir>/**" already covers them.
func outermostDirs(dirs []string) []string {
	var out []string
	for _, dir := range dirs {
		covered := false
		for _, kept := range out {
			if kept == "." || dir == kept || strings.HasPrefix(dir, kept+"/") {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, dir)
		}
	}
	return out
}

func sortedSet(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for item := range set {
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}
