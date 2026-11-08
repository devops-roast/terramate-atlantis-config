# Tag query dependency: runs after every stack tagged "network".
stack {
  name  = "rds"
  id    = "6a1c3f0e-0d4b-4c43-9a7a-000000000003"
  tags  = ["database"]
  after = ["tag:network"]
}
