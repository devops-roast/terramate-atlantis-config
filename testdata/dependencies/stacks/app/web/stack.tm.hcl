# before: api must run after web.
stack {
  name   = "web"
  id     = "6a1c3f0e-0d4b-4c43-9a7a-000000000005"
  before = ["../api"]
}
