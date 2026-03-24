package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type FileConfig struct {
	Output               string                 `yaml:"output"`
	Autoplan             *bool                  `yaml:"autoplan"`
	Parallel             *bool                  `yaml:"parallel"`
	Automerge            *bool                  `yaml:"automerge"`
	Workflow             string                 `yaml:"workflow"`
	TerraformVersion     string                 `yaml:"terraform_version"`
	Filter               string                 `yaml:"filter"`
	FilterPath           string                 `yaml:"filter_path"`
	ExcludeTags          string                 `yaml:"exclude_tags"`
	ExcludePath          string                 `yaml:"exclude_path"`
	CreateProjectName    *bool                  `yaml:"create_project_name"`
	CreateWorkspace      *bool                  `yaml:"create_workspace"`
	ExecutionOrderGroups *bool                  `yaml:"execution_order_groups"`
	DependsOn            *bool                  `yaml:"depends_on"`
	WhenModified         []string               `yaml:"when_modified"`
	ApplyRequirements    []string               `yaml:"apply_requirements"`
	TagWorkflows         map[string]string      `yaml:"tag_workflows"`
	TagRequirements      map[string]string      `yaml:"tag_requirements"`
	Workflows            map[string]interface{} `yaml:"workflows"`
	SortBy               string                 `yaml:"sort_by"`
	Format               string                 `yaml:"format"`
	ProjectNameStrategy  string                 `yaml:"project_name_strategy"`
	ProjectNamePrefix    string                 `yaml:"project_name_prefix"`
	ProjectNameDepth     *int                   `yaml:"project_name_depth"`
}

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
		if !os.IsNotExist(err) {
			return nil, err
		}
	}

	return nil, nil
}

func LoadConfigFromPath(configPath string) (*FileConfig, error) {
	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var cfg FileConfig
	if err := yaml.Unmarshal(content, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshaling config %q: %w", configPath, err)
	}

	return &cfg, nil
}
