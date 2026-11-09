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

type WorkflowOptions struct {
	Enabled           bool
	WrapWithTerramate bool
	// DisableSafeguards is passed to `terramate run --disable-safeguards`.
	// Empty keeps all Terramate safeguards enabled.
	DisableSafeguards []string
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
	if !opts.Enabled {
		return nil
	}

	cmd := func(action string, args ...string) string {
		return terraformCommand(action, opts.WrapWithTerramate, opts.DisableSafeguards, args...)
	}
	return map[string]interface{}{
		GeneratedWorkflowName: map[string]interface{}{
			"plan": map[string]interface{}{
				"steps": []interface{}{
					map[string]interface{}{"run": cmd("init", "-input=false")},
					map[string]interface{}{"run": cmd("plan", "-input=false", `-out="$PLANFILE"`)},
				},
			},
			"apply": map[string]interface{}{
				"steps": []interface{}{
					map[string]interface{}{"run": cmd("apply", `"$PLANFILE"`)},
				},
			},
		},
	}
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
