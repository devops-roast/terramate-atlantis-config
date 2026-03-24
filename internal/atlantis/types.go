package atlantis

// Config represents a complete atlantis.yaml configuration.
type Config struct {
	Version       int                    `yaml:"version" json:"version"`
	Automerge     bool                   `yaml:"automerge" json:"automerge"`
	ParallelPlan  bool                   `yaml:"parallel_plan" json:"parallel_plan"`
	ParallelApply bool                   `yaml:"parallel_apply" json:"parallel_apply"`
	Projects      []Project              `yaml:"projects" json:"projects"`
	Workflows     map[string]interface{} `yaml:"workflows,omitempty" json:"workflows,omitempty"`
}

// Project represents a single Atlantis project entry.
type Project struct {
	Name                string   `yaml:"name,omitempty" json:"name,omitempty"`
	Dir                 string   `yaml:"dir" json:"dir"`
	Workspace           string   `yaml:"workspace,omitempty" json:"workspace,omitempty"`
	Workflow            string   `yaml:"workflow,omitempty" json:"workflow,omitempty"`
	TerraformVersion    string   `yaml:"terraform_version,omitempty" json:"terraform_version,omitempty"`
	ExecutionOrderGroup int      `yaml:"execution_order_group,omitempty" json:"execution_order_group,omitempty"`
	DependsOn           []string `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
	ApplyRequirements   []string `yaml:"apply_requirements,omitempty" json:"apply_requirements,omitempty"`
	Autoplan            Autoplan `yaml:"autoplan" json:"autoplan"`
}

// Autoplan configures automatic planning behavior.
type Autoplan struct {
	Enabled      bool     `yaml:"enabled" json:"enabled"`
	WhenModified []string `yaml:"when_modified" json:"when_modified"`
}
