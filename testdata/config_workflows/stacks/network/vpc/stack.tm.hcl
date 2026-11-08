stack {
  name  = "vpc"
  tags  = ["prod"]
  after = ["/stacks/network"]
}
