# The stacks below don't commit their Terraform code: the Atlantis
# pre-workflow hook runs `terramate generate` before generating atlantis.yaml
# (see e2e/repos.yaml), as a repository that doesn't commit generated files
# would.
generate_hcl "main.tf" {
  content {
    terraform {
      required_version = ">= 1.4"
    }

    resource "terraform_data" "this" {
      input = "${global.account_label}-${terramate.stack.name}"
    }
  }
}
