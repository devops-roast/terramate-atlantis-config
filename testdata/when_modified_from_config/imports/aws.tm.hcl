# Imported by stacks/aws/acme-development/account.tm.hcl; imports tags.tm.hcl in
# turn, so stacks below acme-development depend on both files.
import {
  source = "/imports/common/tags.tm.hcl"
}

globals {
  region = "eu-west-1"
}
