stack {
  name  = "app"
  tags  = ["dev"]
  after = ["/stacks/network/vpc"]
}

globals {
  atlantis_workflow = "app-workflow"
}
