resource "tsuru_role" "deployer" {
  name        = "deployer"
  context     = "app"
  description = "Application deployment role"

  permissions = [
    "app",
    "app.deploy",
  ]
}
