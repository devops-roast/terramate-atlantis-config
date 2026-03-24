package atlantis

import "strings"

type WorkflowTemplate struct {
	Name       string
	PlanSteps  []Step
	ApplySteps []Step
}

type Step struct {
	StepType  string
	Command   string
	ExtraArgs []string
}

type WorkflowOptions struct {
	Enabled           bool
	WrapWithTerramate bool
	PlanExtraArgs     []string
	ApplyExtraArgs    []string
	InitExtraArgs     []string
	CustomWorkflows   map[string]WorkflowTemplate
}

func GenerateWorkflows(opts WorkflowOptions) map[string]interface{} {
	if !opts.Enabled && len(opts.CustomWorkflows) == 0 {
		return nil
	}

	workflows := map[string]interface{}{}

	if opts.Enabled {
		workflows["terramate"] = map[string]interface{}{
			"plan": map[string]interface{}{
				"steps": []interface{}{
					map[string]interface{}{"run": terraformCommand("init", opts.WrapWithTerramate, append([]string{"-input=false"}, opts.InitExtraArgs...)...)},
					map[string]interface{}{"run": terraformCommand("plan", opts.WrapWithTerramate, append([]string{"-input=false", "-out=$PLANFILE"}, opts.PlanExtraArgs...)...)},
				},
			},
			"apply": map[string]interface{}{
				"steps": []interface{}{
					map[string]interface{}{"run": terraformCommand("apply", opts.WrapWithTerramate, append([]string{"$PLANFILE"}, opts.ApplyExtraArgs...)...)},
				},
			},
		}
	}

	for name, template := range opts.CustomWorkflows {
		workflowName := name
		if template.Name != "" {
			workflowName = template.Name
		}

		workflows[workflowName] = map[string]interface{}{
			"plan": map[string]interface{}{
				"steps": renderWorkflowSteps(template.PlanSteps),
			},
			"apply": map[string]interface{}{
				"steps": renderWorkflowSteps(template.ApplySteps),
			},
		}
	}

	if len(workflows) == 0 {
		return nil
	}

	return workflows
}

func renderWorkflowSteps(steps []Step) []interface{} {
	rendered := make([]interface{}, 0, len(steps))
	for _, step := range steps {
		if step.StepType == "run" {
			rendered = append(rendered, map[string]interface{}{"run": step.Command})
			continue
		}

		if len(step.ExtraArgs) > 0 {
			rendered = append(rendered, map[string]interface{}{
				step.StepType: map[string]interface{}{"extra_args": step.ExtraArgs},
			})
			continue
		}

		rendered = append(rendered, step.StepType)
	}

	return rendered
}

func terraformCommand(action string, wrap bool, args ...string) string {
	parts := []string{}
	if wrap {
		parts = append(parts, "terramate", "run", "--")
	}
	parts = append(parts, "terraform", action)
	parts = append(parts, args...)
	return strings.Join(parts, " ")
}
