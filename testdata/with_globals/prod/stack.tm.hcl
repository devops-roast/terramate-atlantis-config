stack {
  name = "prod"
  id   = "a1b2c3d4-0000-0000-0000-000000000010"
}

globals {
  atlantis_workflow          = "production"
  atlantis_terraform_version = "1.5.0"
  atlantis_extra_deps        = ["../modules/**/*.tf"]
}
