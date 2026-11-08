package atlantis

import (
	"fmt"
	"strings"
)

// GeneratedWorkflowName is the name of the workflow emitted by
// --generate-workflows.
const GeneratedWorkflowName = "terramate"

// DefaultDisableSafeguards is the set of Terramate safeguards the generated
// workflow disables by default. Inside Atlantis the git safeguards cannot
// pass: the pre-workflow hook writes atlantis.yaml (untracked or modified),
// terraform init and plan create lock and plan files, and Atlantis clones
// do not track the default branch the way a developer checkout does.
var DefaultDisableSafeguards = []string{"git"}

// ValidSafeguardKeywords are the keywords accepted by
// `terramate run --disable-safeguards`.
var ValidSafeguardKeywords = []string{
	"all", "none", "git", "git-untracked", "git-uncommitted", "git-out-of-sync", "outdated-code",
}

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
	// DisableSafeguards is passed to `terramate run --disable-safeguards`.
	// Empty keeps all Terramate safeguards enabled.
	DisableSafeguards []string
	PlanExtraArgs     []string
	ApplyExtraArgs    []string
	InitExtraArgs     []string
	CustomWorkflows   map[string]WorkflowTemplate
}

// ValidateSafeguards checks keywords against the ones Terramate accepts.
func ValidateSafeguards(keywords []string) error {
	for _, keyword := range keywords {
		valid := false
		for _, known := range ValidSafeguardKeywords {
			if keyword == known {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid safeguard keyword %q, expected one of: %s",
				keyword, strings.Join(ValidSafeguardKeywords, ", "))
		}
	}
	return nil
}

func GenerateWorkflows(opts WorkflowOptions) map[string]interface{} {
	if !opts.Enabled && len(opts.CustomWorkflows) == 0 {
		return nil
	}

	workflows := map[string]interface{}{}

	if opts.Enabled {
		cmd := func(action string, args ...string) string {
			return terraformCommand(action, opts.WrapWithTerramate, opts.DisableSafeguards, args...)
		}
		workflows[GeneratedWorkflowName] = map[string]interface{}{
			"plan": map[string]interface{}{
				"steps": []interface{}{
					map[string]interface{}{"run": cmd("init", append([]string{"-input=false"}, opts.InitExtraArgs...)...)},
					map[string]interface{}{"run": cmd("plan", append([]string{"-input=false", "-out=$PLANFILE"}, opts.PlanExtraArgs...)...)},
				},
			},
			"apply": map[string]interface{}{
				"steps": []interface{}{
					map[string]interface{}{"run": cmd("apply", append([]string{"$PLANFILE"}, opts.ApplyExtraArgs...)...)},
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

// terraformCommand renders a terraform invocation. When wrapped, it runs via
// `terramate run --no-recursive` so only the project's own stack executes:
// nested child stacks are separate Atlantis projects.
func terraformCommand(action string, wrap bool, disableSafeguards []string, args ...string) string {
	parts := []string{}
	if wrap {
		parts = append(parts, "terramate", "run", "--no-recursive")
		if len(disableSafeguards) > 0 {
			parts = append(parts, "--disable-safeguards="+strings.Join(disableSafeguards, ","))
		}
		parts = append(parts, "--")
	}
	parts = append(parts, "terraform", action)
	parts = append(parts, args...)
	return strings.Join(parts, " ")
}
