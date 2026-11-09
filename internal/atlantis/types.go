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
	// ApplyRequirements is a pointer so an explicit empty list is still
	// written: Atlantis reads a missing key as "use the server-side
	// requirements" but `apply_requirements: []` as "no requirements".
	ApplyRequirements *[]string `yaml:"apply_requirements,omitempty" json:"apply_requirements,omitempty"`
	Autoplan          Autoplan  `yaml:"autoplan" json:"autoplan"`
}

// Autoplan configures automatic planning behavior.
type Autoplan struct {
	Enabled      bool     `yaml:"enabled" json:"enabled"`
	WhenModified []string `yaml:"when_modified" json:"when_modified"`
}

// projectYAML is Project as MarshalYAML encodes it: every string is styled
// before encoding (see styleMultilineStrings), because its values come from
// globals and directory names and may be any string.
type projectYAML struct {
	Name                interface{}    `yaml:"name,omitempty"`
	Dir                 interface{}    `yaml:"dir"`
	Workspace           interface{}    `yaml:"workspace,omitempty"`
	Workflow            interface{}    `yaml:"workflow,omitempty"`
	TerraformVersion    interface{}    `yaml:"terraform_version,omitempty"`
	ExecutionOrderGroup int            `yaml:"execution_order_group,omitempty"`
	DependsOn           []interface{}  `yaml:"depends_on,omitempty"`
	ApplyRequirements   *[]interface{} `yaml:"apply_requirements,omitempty"`
	Autoplan            autoplanYAML   `yaml:"autoplan"`
}

type autoplanYAML struct {
	Enabled      bool          `yaml:"enabled"`
	WhenModified []interface{} `yaml:"when_modified"`
}

// MarshalYAML implements yaml.Marshaler.
func (p Project) MarshalYAML() (interface{}, error) {
	optional := func(s string) interface{} {
		if s == "" {
			return nil
		}
		return styleMultilineStrings(s)
	}
	list := func(values []string) []interface{} {
		if values == nil {
			return nil
		}
		out := make([]interface{}, len(values))
		for i, v := range values {
			out[i] = styleMultilineStrings(v)
		}
		return out
	}

	out := projectYAML{
		Name:                optional(p.Name),
		Dir:                 styleMultilineStrings(p.Dir),
		Workspace:           optional(p.Workspace),
		Workflow:            optional(p.Workflow),
		TerraformVersion:    optional(p.TerraformVersion),
		ExecutionOrderGroup: p.ExecutionOrderGroup,
		DependsOn:           list(p.DependsOn),
		Autoplan:            autoplanYAML{Enabled: p.Autoplan.Enabled, WhenModified: list(p.Autoplan.WhenModified)},
	}
	if p.ApplyRequirements != nil {
		requirements := list(*p.ApplyRequirements)
		if requirements == nil {
			requirements = []interface{}{}
		}
		out.ApplyRequirements = &requirements
	}
	if out.Autoplan.WhenModified == nil {
		out.Autoplan.WhenModified = []interface{}{}
	}
	return out, nil
}
