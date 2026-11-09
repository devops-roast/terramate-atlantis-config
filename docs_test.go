package main

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/devops-roast/terramate-atlantis-config/cmd"
	"github.com/devops-roast/terramate-atlantis-config/internal/config"
)

// These tests keep README.md, action.yml and .pre-commit-hooks.yaml in step
// with the flags and config keys the CLI accepts.

var (
	// A flag table row: | `--name` | default | description |
	readmeFlagRow = regexp.MustCompile("(?m)^\\| `--([a-z-]+)` +\\|([^|]*)\\|")
	// A config key table row: | `key` | type | `--flag` or "none ..." |
	readmeKeyRow = regexp.MustCompile("(?m)^\\| `([a-z_]+)` +\\|[^|]*\\| *([^|]*?) *\\|")
	// An action input table row: | `name` | default | description |
	readmeInputRow = regexp.MustCompile("(?m)^\\| `([a-z]+)` +\\|([^|]*)\\|")
	cliFlag        = regexp.MustCompile(`(?:^|\s)--([a-z][a-z-]*)`)
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(content)
}

// section returns the text of README between the start and end headings.
func section(t *testing.T, readme, start, end string) string {
	t.Helper()
	_, rest, ok := strings.Cut(readme, start)
	require.True(t, ok, "README has no %q", start)
	body, _, ok := strings.Cut(rest, end)
	require.True(t, ok, "README has no %q after %q", end, start)
	return body
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

// normalizeDefault maps a README default and a pflag DefValue to one form:
// no backticks, list brackets or spaces, and "stdout" or "[]" as "".
func normalizeDefault(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "`", ""))
	value = strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
	value = strings.ReplaceAll(value, " ", "")
	if value == "stdout" {
		return ""
	}
	return value
}

func TestReadmeFlagTablesMatchGenerateFlags(t *testing.T) {
	readme := readFile(t, "README.md")
	table := section(t, readme, "### CLI flags", "**Other commands**")
	flags := generateFlags(t)

	documented := map[string]bool{}
	for _, m := range readmeFlagRow.FindAllStringSubmatch(table, -1) {
		name, readmeDefault := m[1], m[2]
		documented[name] = true
		flag, ok := flags[name]
		if !assert.True(t, ok, "README documents --%s, which generate doesn't accept", name) {
			continue
		}
		assert.Equal(t, normalizeDefault(flag.DefValue), normalizeDefault(readmeDefault),
			"default of --%s: --help vs README", name)
	}

	for name := range flags {
		assert.True(t, documented[name], "--%s is missing from the README flag tables", name)
	}
}

func TestReadmeConfigKeysMatchConfigFile(t *testing.T) {
	readme := readFile(t, "README.md")
	table := section(t, readme, "Every key matches a `generate` flag:", "### CLI flags")
	flags := generateFlags(t)

	var documented []string
	for _, m := range readmeKeyRow.FindAllStringSubmatch(table, -1) {
		key, flagColumn := m[1], m[2]
		documented = append(documented, key)
		if strings.HasPrefix(flagColumn, "none") {
			continue
		}
		name := strings.TrimPrefix(strings.Trim(flagColumn, "`"), "--")
		_, ok := flags[name]
		assert.True(t, ok, "README maps config key %s to --%s, which generate doesn't accept", key, name)
	}
	sort.Strings(documented)
	assert.Equal(t, config.KnownKeys(), documented, "README config key table vs config.KnownKeys()")
}

// The action and the pre-commit hook pass flags to generate; each one must
// exist, and the README's action input table must list the action's inputs
// with their defaults.
func TestIntegrationsUseExistingFlags(t *testing.T) {
	flags := generateFlags(t)

	var action struct {
		Inputs map[string]struct {
			Default string `yaml:"default"`
		} `yaml:"inputs"`
		Runs struct {
			Steps []struct {
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"runs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(readFile(t, "action.yml")), &action))

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

	readme := readFile(t, "README.md")
	table := section(t, readme, "\n| Input ", "\n\n| Output ")
	documented := map[string]string{}
	for _, m := range readmeInputRow.FindAllStringSubmatch(table, -1) {
		documented[m[1]] = strings.Trim(strings.TrimSpace(m[2]), "`")
	}
	require.Len(t, documented, len(action.Inputs), "README action inputs vs action.yml")
	for name, input := range action.Inputs {
		want := input.Default
		if want == "${{ github.token }}" {
			want = "github.token"
		}
		assert.Equal(t, want, documented[name], "default of action input %s: action.yml vs README", name)
	}
}
