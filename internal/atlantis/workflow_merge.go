package atlantis

import (
	"fmt"
	"sort"

	"gopkg.in/yaml.v3"
)

// WorkflowSource labels where a workflow definition came from.
type WorkflowSource string

const (
	// SourcePreserved is a workflow kept from the existing output file.
	SourcePreserved WorkflowSource = "existing output file"
	// SourceGenerated is a workflow produced by --generate-workflows.
	SourceGenerated WorkflowSource = "--generate-workflows"
	// SourceConfig is a workflow defined in the config file.
	SourceConfig WorkflowSource = "config file"
)

// WorkflowLayer is one source of workflow definitions.
type WorkflowLayer struct {
	Source    WorkflowSource
	Workflows map[string]interface{}
}

// WorkflowOverride records a workflow name defined by more than one layer.
type WorkflowOverride struct {
	Name     string
	Winner   WorkflowSource
	Replaced WorkflowSource
}

// MergeWorkflows combines layers by name. Later layers win, so callers pass
// layers from lowest to highest precedence. Overrides are reported sorted by
// name so callers can warn about them deterministically.
func MergeWorkflows(layers ...WorkflowLayer) (map[string]interface{}, []WorkflowOverride) {
	merged := map[string]interface{}{}
	origin := map[string]WorkflowSource{}
	var overrides []WorkflowOverride

	for _, layer := range layers {
		names := make([]string, 0, len(layer.Workflows))
		for name := range layer.Workflows {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			if previous, ok := origin[name]; ok && previous != layer.Source {
				overrides = append(overrides, WorkflowOverride{Name: name, Winner: layer.Source, Replaced: previous})
			}
			merged[name] = layer.Workflows[name]
			origin[name] = layer.Source
		}
	}

	sort.SliceStable(overrides, func(i, j int) bool { return overrides[i].Name < overrides[j].Name })

	if len(merged) == 0 {
		return nil, overrides
	}
	return merged, overrides
}

// ExistingWorkflows extracts the workflows block from an existing
// atlantis.yaml (YAML or JSON). It returns nil when there is none.
func ExistingWorkflows(existing []byte) (map[string]interface{}, error) {
	var doc map[string]interface{}
	if err := yaml.Unmarshal(existing, &doc); err != nil {
		return nil, fmt.Errorf("unmarshaling existing yaml: %w", err)
	}

	raw, ok := doc["workflows"]
	if !ok || raw == nil {
		return nil, nil
	}

	workflows, ok := raw.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("workflows must be a map")
	}
	return workflows, nil
}
