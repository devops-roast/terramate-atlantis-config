package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileConfig mirrors the generate command's flags. Every key is optional;
// CLI flags that are set explicitly take precedence over these values.
type FileConfig struct {
	Output                    string                 `yaml:"output"`
	Format                    string                 `yaml:"format"`
	NoHeader                  *bool                  `yaml:"no_header"`
	LogLevel                  string                 `yaml:"log_level"`
	Autoplan                  *bool                  `yaml:"autoplan"`
	Parallel                  *bool                  `yaml:"parallel"`
	Automerge                 *bool                  `yaml:"automerge"`
	Workflow                  string                 `yaml:"workflow"`
	TerraformVersion          string                 `yaml:"terraform_version"`
	Filter                    string                 `yaml:"filter"`
	FilterPath                string                 `yaml:"filter_path"`
	ExcludeTags               string                 `yaml:"exclude_tags"`
	ExcludePath               string                 `yaml:"exclude_path"`
	Changed                   *bool                  `yaml:"changed"`
	ChangedBaseRef            string                 `yaml:"changed_base_ref"`
	CreateProjectName         *bool                  `yaml:"create_project_name"`
	CreateWorkspace           *bool                  `yaml:"create_workspace"`
	ExecutionOrderGroups      *bool                  `yaml:"execution_order_groups"`
	DependsOn                 *bool                  `yaml:"depends_on"`
	WhenModified              []string               `yaml:"when_modified"`
	ApplyRequirements         []string               `yaml:"apply_requirements"`
	TagWorkflows              map[string]string      `yaml:"tag_workflows"`
	TagRequirements           map[string]StringList  `yaml:"tag_requirements"`
	PreserveWorkflows         *bool                  `yaml:"preserve_workflows"`
	GenerateWorkflows         *bool                  `yaml:"generate_workflows"`
	WorkflowTerramateWrap     *bool                  `yaml:"workflow_terramate_wrap"`
	WorkflowDisableSafeguards *StringList            `yaml:"workflow_disable_safeguards"`
	Workflows                 map[string]interface{} `yaml:"workflows"`
	SortBy                    string                 `yaml:"sort_by"`
	ProjectNameStrategy       string                 `yaml:"project_name_strategy"`
	ProjectNamePrefix         string                 `yaml:"project_name_prefix"`
	ProjectNameDepth          *int                   `yaml:"project_name_depth"`
}

// StringList is a list of strings that also accepts a single comma-separated
// string, so both `[approved, mergeable]` and `approved,mergeable` work.
type StringList []string

// UnmarshalYAML implements yaml.Unmarshaler.
func (l *StringList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.SequenceNode:
		var items []string
		if err := node.Decode(&items); err != nil {
			return err
		}
		*l = cleanList(items)
		return nil
	case yaml.ScalarNode:
		if node.Tag == "!!null" {
			*l = StringList{}
			return nil
		}
		var raw string
		if err := node.Decode(&raw); err != nil {
			return err
		}
		*l = cleanList(strings.Split(raw, ","))
		return nil
	default:
		return fmt.Errorf("line %d: expected a list of strings or a comma-separated string", node.Line)
	}
}

func cleanList(items []string) StringList {
	cleaned := make(StringList, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			cleaned = append(cleaned, item)
		}
	}
	return cleaned
}

// LoadConfig loads .terramate-atlantis.yaml (or .yml) from rootDir. It returns
// nil without error when neither file exists.
func LoadConfig(rootDir string) (*FileConfig, error) {
	if rootDir == "" {
		rootDir = "."
	}

	candidates := []string{
		filepath.Join(rootDir, ".terramate-atlantis.yaml"),
		filepath.Join(rootDir, ".terramate-atlantis.yml"),
	}

	for _, candidate := range candidates {
		cfg, err := LoadConfigFromPath(candidate)
		if err == nil {
			return cfg, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}

	return nil, nil
}

// LoadConfigFromPath loads a config file. Unknown keys are rejected so typos
// do not silently fall back to defaults.
func LoadConfigFromPath(configPath string) (*FileConfig, error) {
	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	cfg, err := parse(content)
	if err != nil {
		return nil, fmt.Errorf("config %q: %w", configPath, err)
	}
	return cfg, nil
}

func parse(content []byte) (*FileConfig, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return nil, err
	}
	if err := checkKnownKeys(&root); err != nil {
		return nil, err
	}

	var cfg FileConfig
	dec := yaml.NewDecoder(bytes.NewReader(content))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	// Keys in a second document would be silently ignored.
	for {
		var extra yaml.Node
		err := dec.Decode(&extra)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if !isEmptyDocument(&extra) {
			return nil, fmt.Errorf("line %d: the config file must be a single YAML document", extra.Line)
		}
	}
	return &cfg, nil
}

// isEmptyDocument reports whether a decoded document holds no value, like an
// empty file or one with every key commented out.
func isEmptyDocument(doc *yaml.Node) bool {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return true
	}
	value := doc.Content[0]
	return value.Kind == yaml.ScalarNode && value.Tag == "!!null"
}

// checkKnownKeys reports unknown top-level keys with a readable message.
// Nested values (workflows, tag maps) are free-form or typed by the decoder.
func checkKnownKeys(root *yaml.Node) error {
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil
	}
	mapping := root.Content[0]
	// A document with no content ("---" with every key commented out) is
	// an empty config, like an empty file.
	if mapping.Kind == yaml.ScalarNode && mapping.Tag == "!!null" {
		return nil
	}
	if mapping.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: expected a mapping of config keys", mapping.Line)
	}

	known := KnownKeys()
	knownSet := make(map[string]struct{}, len(known))
	for _, key := range known {
		knownSet[key] = struct{}{}
	}

	for i := 0; i+1 < len(mapping.Content); i += 2 {
		key := mapping.Content[i]
		if _, ok := knownSet[key.Value]; !ok {
			return fmt.Errorf("line %d: unknown key %q (valid keys: %s)", key.Line, key.Value, strings.Join(known, ", "))
		}
	}
	return nil
}

// KnownKeys returns the sorted list of supported config file keys.
func KnownKeys() []string {
	t := reflect.TypeOf(FileConfig{})
	keys := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
		if name != "" && name != "-" {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	return keys
}
