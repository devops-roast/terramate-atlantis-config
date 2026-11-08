# Absolute path, tag query and a path that does not exist (ignored, as
# Terramate does).
stack {
  name  = "api"
  id    = "6a1c3f0e-0d4b-4c43-9a7a-000000000004"
  after = ["/stacks/network/dns", "tag:database", "../does-not-exist"]
}
