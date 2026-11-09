package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/devops-roast/terramate-atlantis-config/cmd"
	"github.com/devops-roast/terramate-atlantis-config/internal/config"
)

// These tests keep the documentation (README.md, CONTRIBUTING.md, docs/),
// action.yml and .pre-commit-hooks.yaml in step with the flags and config
// keys the CLI accepts, and check that every relative link resolves.

const (
	configurationDoc = "docs/configuration.md"
	setupDoc         = "docs/setup.md"
)

var (
	// A flag table row: | `--name` | default | description |
	docFlagRow = regexp.MustCompile("(?m)^\\| `--([a-z-]+)` +\\|([^|]*)\\|")
	// A config key table row: | `key` | type | `--flag` or "none ..." |
	docKeyRow = regexp.MustCompile("(?m)^\\| `([a-z_]+)` +\\|[^|]*\\| *([^|]*?) *\\|")
	// An action input or output table row: | `name` | default or description |
	docNameRow = regexp.MustCompile("(?m)^\\| `([a-z]+)` +\\|([^|]*)\\|")
	cliFlag    = regexp.MustCompile(`(?:^|\s)--([a-z][a-z-]*)`)

	// An inline Markdown link or image: [text](target "title").
	markdownLink = regexp.MustCompile(`\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	// A link in an HTML element: <a href="target"> or <img src="target">.
	htmlLink    = regexp.MustCompile(`\b(?:href|src)="([^"]+)"`)
	inlineCode  = regexp.MustCompile("`[^`\n]*`")
	headingLine = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	headingLink = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(content)
}

// withoutCodeBlocks blanks the lines inside fenced code blocks, so a "# comment"
// in a shell or HCL example isn't read as a heading and code isn't read as a
// link. Line numbers are kept.
func withoutCodeBlocks(doc string) string {
	lines := strings.Split(doc, "\n")
	fence := ""
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case fence == "" && (strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")):
			fence = trimmed[:3]
			lines[i] = ""
		case fence != "":
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			lines[i] = ""
		}
	}
	return strings.Join(lines, "\n")
}

// section returns the body of the heading line `heading` (such as
// "## CLI flags"): everything up to the next heading of the same or a higher
// level.
func section(t *testing.T, path, heading string) string {
	t.Helper()
	doc := readFile(t, path)
	level := strings.Count(strings.Fields(heading)[0], "#")

	lines := strings.Split(withoutCodeBlocks(doc), "\n")
	raw := strings.Split(doc, "\n")
	start := -1
	for i, line := range lines {
		if start < 0 {
			if strings.TrimSpace(line) == heading {
				start = i + 1
			}
			continue
		}
		if m := headingLine.FindStringSubmatch(line); m != nil && len(m[1]) <= level {
			return strings.Join(raw[start:i], "\n")
		}
	}
	require.GreaterOrEqual(t, start, 0, "%s has no heading %q", path, heading)
	return strings.Join(raw[start:], "\n")
}

// table returns the Markdown table in text whose first header cell is header.
func table(t *testing.T, text, header string) string {
	t.Helper()
	var rows []string
	inTable := false
	for line := range strings.SplitSeq(text, "\n") {
		if !inTable {
			if cells := strings.Split(line, "|"); len(cells) > 2 && strings.HasPrefix(line, "|") &&
				strings.TrimSpace(cells[1]) == header {
				inTable = true
			}
			continue
		}
		if !strings.HasPrefix(line, "|") {
			break
		}
		rows = append(rows, line)
	}
	require.NotEmpty(t, rows, "no table with header %q", header)
	return strings.Join(rows, "\n")
}

func generateFlags(t *testing.T) map[string]*pflag.Flag {
	t.Helper()
	root := cmd.NewRootCmd()
	generate, _, err := root.Find([]string{"generate"})
	require.NoError(t, err)

	flags := map[string]*pflag.Flag{}
	collect := func(f *pflag.Flag) {
		if f.Name != "help" {
			flags[f.Name] = f
		}
	}
	generate.LocalFlags().VisitAll(collect)
	generate.InheritedFlags().VisitAll(collect)
	return flags
}

// normalizeDefault maps a documented default and a pflag DefValue to one
// form: no backticks, list brackets or spaces, and "stdout" or "[]" as "".
func normalizeDefault(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "`", ""))
	value = strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
	value = strings.ReplaceAll(value, " ", "")
	if value == "stdout" {
		return ""
	}
	return value
}

func TestDocsFlagTablesMatchGenerateFlags(t *testing.T) {
	tables := section(t, configurationDoc, "## CLI flags")
	flags := generateFlags(t)

	documented := map[string]bool{}
	for _, m := range docFlagRow.FindAllStringSubmatch(tables, -1) {
		name, docDefault := m[1], m[2]
		assert.False(t, documented[name], "--%s is documented twice in %s", name, configurationDoc)
		documented[name] = true
		flag, ok := flags[name]
		if !assert.True(t, ok, "%s documents --%s, which generate doesn't accept", configurationDoc, name) {
			continue
		}
		assert.Equal(t, normalizeDefault(flag.DefValue), normalizeDefault(docDefault),
			"default of --%s: --help vs %s", name, configurationDoc)
	}

	for name := range flags {
		assert.True(t, documented[name], "--%s is missing from the flag tables in %s", name, configurationDoc)
	}
}

func TestDocsConfigKeysMatchConfigFile(t *testing.T) {
	keys := table(t, section(t, configurationDoc, "### Keys"), "Key")
	flags := generateFlags(t)

	var documented []string
	for _, m := range docKeyRow.FindAllStringSubmatch(keys, -1) {
		key, flagColumn := m[1], m[2]
		documented = append(documented, key)
		if strings.HasPrefix(flagColumn, "none") {
			continue
		}
		name := strings.TrimPrefix(strings.Trim(flagColumn, "`"), "--")
		_, ok := flags[name]
		assert.True(t, ok, "%s maps config key %s to --%s, which generate doesn't accept", configurationDoc, key, name)
	}
	sort.Strings(documented)
	assert.Equal(t, config.KnownKeys(), documented, "%s config key table vs config.KnownKeys()", configurationDoc)
}

type actionDefinition struct {
	Inputs map[string]struct {
		Default string `yaml:"default"`
	} `yaml:"inputs"`
	Outputs map[string]struct {
		Value string `yaml:"value"`
	} `yaml:"outputs"`
	Runs struct {
		Steps []struct {
			Run string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"runs"`
}

func loadAction(t *testing.T) actionDefinition {
	t.Helper()
	var action actionDefinition
	require.NoError(t, yaml.Unmarshal([]byte(readFile(t, "action.yml")), &action))
	return action
}

// The action and the pre-commit hook pass flags to generate; each one must
// exist.
func TestActionAndHookUseExistingFlags(t *testing.T) {
	flags := generateFlags(t)
	action := loadAction(t)

	var hooks []struct {
		Entry string `yaml:"entry"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(readFile(t, ".pre-commit-hooks.yaml")), &hooks))

	var commands []string
	for _, step := range action.Runs.Steps {
		for line := range strings.SplitSeq(step.Run, "\n") {
			if strings.Contains(line, "terramate-atlantis-config generate") || strings.Contains(line, "cmd+=(") {
				commands = append(commands, line)
			}
		}
	}
	for _, hook := range hooks {
		commands = append(commands, hook.Entry)
	}
	require.NotEmpty(t, commands)
	for _, command := range commands {
		for _, m := range cliFlag.FindAllStringSubmatch(command, -1) {
			_, ok := flags[m[1]]
			assert.True(t, ok, "--%s in %q is not a generate flag", m[1], strings.TrimSpace(command))
		}
	}
}

// The action input table must list the action's inputs with their defaults,
// and the output table its outputs.
func TestDocsActionTablesMatchAction(t *testing.T) {
	action := loadAction(t)
	body := section(t, setupDoc, "### GitHub Action")

	inputs := map[string]string{}
	for _, m := range docNameRow.FindAllStringSubmatch(table(t, body, "Input"), -1) {
		inputs[m[1]] = strings.Trim(strings.TrimSpace(m[2]), "`")
	}
	require.Len(t, inputs, len(action.Inputs), "%s action inputs vs action.yml", setupDoc)
	for name, input := range action.Inputs {
		want := input.Default
		if want == "${{ github.token }}" {
			want = "github.token"
		}
		got, ok := inputs[name]
		if assert.True(t, ok, "action input %s is missing from %s", name, setupDoc) {
			assert.Equal(t, want, got, "default of action input %s: action.yml vs %s", name, setupDoc)
		}
	}

	var outputs []string
	for _, m := range docNameRow.FindAllStringSubmatch(table(t, body, "Output"), -1) {
		outputs = append(outputs, m[1])
	}
	var want []string
	for name := range action.Outputs {
		want = append(want, name)
	}
	sort.Strings(outputs)
	sort.Strings(want)
	assert.Equal(t, want, outputs, "%s action outputs vs action.yml", setupDoc)
}

// githubAnchor returns the anchor GitHub generates for a heading: lower case,
// punctuation other than '-' and '_' removed, spaces turned into '-'.
func githubAnchor(heading string) string {
	// Link text keeps only the text: [text](url) -> text.
	heading = headingLink.ReplaceAllString(heading, "$1")
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case r == ' ':
			b.WriteRune('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.Is(unicode.Mn, r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// anchors returns the heading anchors of a Markdown document, numbered the
// way GitHub numbers duplicates ("name", "name-1", ...).
func anchors(doc string) map[string]bool {
	result := map[string]bool{}
	seen := map[string]int{}
	for line := range strings.SplitSeq(withoutCodeBlocks(doc), "\n") {
		m := headingLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		anchor := githubAnchor(m[2])
		if n := seen[anchor]; n > 0 {
			result[fmt.Sprintf("%s-%d", anchor, n)] = true
		} else {
			result[anchor] = true
		}
		seen[anchor]++
	}
	return result
}

func TestGithubAnchor(t *testing.T) {
	for heading, want := range map[string]string{
		"A. Commit the file":                         "a-commit-the-file",
		"`--changed` in shallow clones":              "--changed-in-shallow-clones",
		"Atlantis rejects atlantis.yaml":             "atlantis-rejects-atlantisyaml",
		"`--changed` lists stacks you didn't touch":  "--changed-lists-stacks-you-didnt-touch",
		"Plan fails: current HEAD is out-of-date":    "plan-fails-current-head-is-out-of-date",
		"How `after` and `before` are resolved":      "how-after-and-before-are-resolved",
		"See [Setup](docs/setup.md) for_the details": "see-setup-for_the-details",
	} {
		assert.Equal(t, want, githubAnchor(heading), heading)
	}
	assert.Equal(t, map[string]bool{"edge-cases": true, "edge-cases-1": true},
		anchors("## Edge cases\n\n```sh\n# Edge cases\n```\n\n### Edge cases\n"))
}

// Every relative link in the documentation, Markdown or HTML, must point at
// an existing file, and every #anchor at a heading of the target document.
func TestDocsLinksResolve(t *testing.T) {
	docs, err := filepath.Glob("docs/*.md")
	require.NoError(t, err)
	require.NotEmpty(t, docs)
	docs = append([]string{"README.md", "CONTRIBUTING.md"}, docs...)

	anchorCache := map[string]map[string]bool{}
	anchorsOf := func(path string) map[string]bool {
		if _, ok := anchorCache[path]; !ok {
			anchorCache[path] = anchors(readFile(t, path))
		}
		return anchorCache[path]
	}

	for _, doc := range docs {
		text := inlineCode.ReplaceAllString(withoutCodeBlocks(readFile(t, doc)), "``")
		links := append(markdownLink.FindAllStringSubmatch(text, -1), htmlLink.FindAllStringSubmatch(text, -1)...)
		for _, m := range links {
			target := m[1]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			path, fragment, _ := strings.Cut(target, "#")
			resolved := doc
			if path != "" {
				resolved = filepath.Join(filepath.Dir(doc), filepath.FromSlash(path))
				_, err := os.Stat(resolved)
				if !assert.NoError(t, err, "%s links to %s, which doesn't exist", doc, target) {
					continue
				}
			}
			if fragment == "" {
				continue
			}
			if !assert.Equal(t, ".md", filepath.Ext(resolved), "%s links to anchor %s in a non-Markdown file", doc, target) {
				continue
			}
			assert.True(t, anchorsOf(resolved)[fragment], "%s links to %s, but %s has no heading #%s",
				doc, target, resolved, fragment)
		}
	}
}

// The README's documentation table must link every page in docs/, so a new
// page can't be left unreachable.
func TestReadmeListsEveryDocsPage(t *testing.T) {
	pages, err := filepath.Glob("docs/*.md")
	require.NoError(t, err)
	require.NotEmpty(t, pages)

	listed := table(t, section(t, "README.md", "## Documentation"), "Page")
	for _, page := range pages {
		assert.Contains(t, listed, "]("+filepath.ToSlash(page)+")",
			"README.md's documentation table doesn't link %s", page)
	}
}
