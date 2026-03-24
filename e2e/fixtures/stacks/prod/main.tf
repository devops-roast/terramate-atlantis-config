terraform {
  required_version = ">= 1.4"
}

resource "terraform_data" "prod" {
  input = "e2e-prod"
}
