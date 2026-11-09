package atlantis

import (
	"encoding/json"
	"fmt"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Scalar is a non-string YAML scalar from a hand-written workflow (config
// file or existing output file), kept exactly as written. Decoding it into a
// Go value would retype it: `value: 1.10` would be written back as 1.1 and
// `2026-01-01` as a timestamp, while Atlantis reads such fields as the text
// that was written.
type Scalar struct {
	Tag   string
	Value string
}

// MarshalYAML implements yaml.Marshaler.
func (s Scalar) MarshalYAML() (interface{}, error) {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: s.Tag, Value: s.Value}, nil
}

var jsonNumber = regexp.MustCompile(`^-?(0|[1-9]\d*)(\.\d+)?([eE][+-]?\d+)?$`)

// MarshalJSON implements json.Marshaler. Numbers whose text is a JSON number
// and booleans keep their type; anything else becomes the text as written.
func (s Scalar) MarshalJSON() ([]byte, error) {
	switch {
	case (s.Tag == "!!int" || s.Tag == "!!float") && jsonNumber.MatchString(s.Value):
		return []byte(s.Value), nil
	case s.Tag == "!!bool":
		var b bool
		if err := yaml.Unmarshal([]byte(s.Value), &b); err == nil {
			return json.Marshal(b)
		}
	}
	return json.Marshal(s.Value)
}

// WorkflowValue converts a decoded YAML node into the value written to the
// output: mappings become map[string]interface{} (keys as written), sequences
// []interface{}, strings string, nulls nil and other scalars Scalar.
func WorkflowValue(node *yaml.Node) (interface{}, error) {
	return workflowValue(node, map[*yaml.Node]bool{})
}

// workflowValue converts node; expanding holds the aliased nodes being
// expanded, to reject an anchor that contains itself.
func workflowValue(node *yaml.Node, expanding map[*yaml.Node]bool) (interface{}, error) {
	switch node.Kind {
	case yaml.DocumentNode:
		if len(node.Content) == 0 {
			return nil, nil
		}
		return workflowValue(node.Content[0], expanding)
	case yaml.AliasNode:
		if expanding[node.Alias] {
			return nil, fmt.Errorf("line %d: anchor %q contains itself", node.Line, node.Value)
		}
		expanding[node.Alias] = true
		defer delete(expanding, node.Alias)
		return workflowValue(node.Alias, expanding)
	case yaml.SequenceNode:
		out := make([]interface{}, 0, len(node.Content))
		for _, item := range node.Content {
			value, err := workflowValue(item, expanding)
			if err != nil {
				return nil, err
			}
			out = append(out, value)
		}
		return out, nil
	case yaml.MappingNode:
		out := make(map[string]interface{}, len(node.Content)/2)
		if err := mergeMapping(out, node, expanding); err != nil {
			return nil, err
		}
		return out, nil
	case yaml.ScalarNode:
		switch node.ShortTag() {
		case "!!str":
			return node.Value, nil
		case "!!null":
			return nil, nil
		default:
			return Scalar{Tag: node.ShortTag(), Value: node.Value}, nil
		}
	default:
		return nil, fmt.Errorf("line %d: unexpected YAML node", node.Line)
	}
}

// mergeMapping adds the entries of a mapping node to out. Merge keys
// (`<<: *defaults`) are applied first, so the mapping's own keys win.
func mergeMapping(out map[string]interface{}, node *yaml.Node, expanding map[*yaml.Node]bool) error {
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.ShortTag() != "!!merge" {
			continue
		}
		sources := []*yaml.Node{value}
		if value.Kind == yaml.SequenceNode {
			sources = value.Content
		}
		for _, source := range sources {
			merged, err := workflowValue(source, expanding)
			if err != nil {
				return err
			}
			mapping, ok := merged.(map[string]interface{})
			if !ok {
				return fmt.Errorf("line %d: a merge key needs a mapping", source.Line)
			}
			for key, value := range mapping {
				out[key] = value
			}
		}
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.ShortTag() == "!!merge" {
			continue
		}
		if key.Kind != yaml.ScalarNode {
			return fmt.Errorf("line %d: workflow keys must be scalars", key.Line)
		}
		converted, err := workflowValue(value, expanding)
		if err != nil {
			return err
		}
		out[key.Value] = converted
	}
	return nil
}

// WorkflowsValue converts a `workflows` node into workflows by name.
func WorkflowsValue(node *yaml.Node) (map[string]interface{}, error) {
	if node == nil || node.Kind == 0 {
		return nil, nil
	}
	value, err := WorkflowValue(node)
	if err != nil {
		return nil, err
	}
	switch v := value.(type) {
	case nil:
		return nil, nil
	case map[string]interface{}:
		return v, nil
	default:
		return nil, fmt.Errorf("line %d: workflows must be a map", node.Line)
	}
}
