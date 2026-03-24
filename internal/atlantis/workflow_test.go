package atlantis

import "testing"

func TestGenerateWorkflows(t *testing.T) {
	t.Parallel()

	t.Run("default generated workflow with terramate wrap", func(t *testing.T) {
		t.Parallel()

		workflows := GenerateWorkflows(WorkflowOptions{
			Enabled:           true,
			WrapWithTerramate: true,
		})

		terramate, ok := workflows["terramate"].(map[string]interface{})
		if !ok {
			t.Fatalf("missing terramate workflow: %#v", workflows)
		}

		plan := terramate["plan"].(map[string]interface{})
		planSteps := plan["steps"].([]interface{})
		if planSteps[0].(map[string]interface{})["run"] != "terramate run -- terraform init -input=false" {
			t.Fatalf("unexpected init step: %#v", planSteps[0])
		}
		if planSteps[1].(map[string]interface{})["run"] != "terramate run -- terraform plan -input=false -out=$PLANFILE" {
			t.Fatalf("unexpected plan step: %#v", planSteps[1])
		}

		apply := terramate["apply"].(map[string]interface{})
		applySteps := apply["steps"].([]interface{})
		if applySteps[0].(map[string]interface{})["run"] != "terramate run -- terraform apply $PLANFILE" {
			t.Fatalf("unexpected apply step: %#v", applySteps[0])
		}
	})

	t.Run("default generated workflow without terramate wrap", func(t *testing.T) {
		t.Parallel()

		workflows := GenerateWorkflows(WorkflowOptions{
			Enabled:           true,
			WrapWithTerramate: false,
		})

		terramate := workflows["terramate"].(map[string]interface{})
		plan := terramate["plan"].(map[string]interface{})
		planSteps := plan["steps"].([]interface{})
		if planSteps[1].(map[string]interface{})["run"] != "terraform plan -input=false -out=$PLANFILE" {
			t.Fatalf("unexpected plan step: %#v", planSteps[1])
		}
	})

	t.Run("custom workflows", func(t *testing.T) {
		t.Parallel()

		workflows := GenerateWorkflows(WorkflowOptions{
			CustomWorkflows: map[string]WorkflowTemplate{
				"custom": {
					PlanSteps:  []Step{{StepType: "run", Command: "echo plan"}},
					ApplySteps: []Step{{StepType: "run", Command: "echo apply"}},
				},
			},
		})

		custom := workflows["custom"].(map[string]interface{})
		plan := custom["plan"].(map[string]interface{})
		planSteps := plan["steps"].([]interface{})
		if planSteps[0].(map[string]interface{})["run"] != "echo plan" {
			t.Fatalf("unexpected custom plan step: %#v", planSteps[0])
		}
	})
}
