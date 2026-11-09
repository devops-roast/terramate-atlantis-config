# atlantis_when_modified replaces when_modified entirely, so nothing is
# derived for this stack.
stack {
  name = "legacy"
  id   = "5e7a1c00-0000-0000-0000-000000000005"
}

globals {
  atlantis_when_modified = ["*.tf"]
}
