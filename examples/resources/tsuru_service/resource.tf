resource "tsuru_service" "authorization_service" {
  name     = "authorization-service"
  endpoint = "https://authorization-service.example.com"
  team     = "platform"

  manifest {
    enabled        = true
    strict_actions = true
    legacy_compat  = false

    operations {
      method = "GET"
      path   = "/rules"
      action = "rules.list"
    }

    operations {
      method = "POST"
      path   = "/rules/{ruleId}/sync"
      action = "rules.sync"
    }
  }
}
