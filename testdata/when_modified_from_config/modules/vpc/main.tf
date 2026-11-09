module "subnets" {
  source = "../subnets"
}

# Nested in modules/vpc, so modules/vpc/** already covers it.
module "flow_logs" {
  source = "./nested"
}

# Not a local module: never a when_modified entry.
module "remote" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "5.0.0"
}
