stack {
  name = "dns"
  id   = "5e7a1c00-0000-0000-0000-000000000002"
}

globals {
  # The first entry is derived anyway and is listed once.
  atlantis_extra_deps = ["../*.tm.hcl", "records.yaml"]
}
