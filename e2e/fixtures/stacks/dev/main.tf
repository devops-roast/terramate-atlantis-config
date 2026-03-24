terraform {
  required_version = ">= 1.4"
}

resource "terraform_data" "dev" {
  input = "e2e-dev"
}
