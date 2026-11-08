terraform {
  required_version = ">= 1.4"
}

resource "terraform_data" "skipped" {
  input = "e2e-skipped"
}
