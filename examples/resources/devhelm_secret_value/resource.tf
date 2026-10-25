resource "devhelm_secret" "card_token" {
  key   = "CARD_TOKEN"
  value = var.card_token_default
}

# Per-environment value. `value` is write-only (Terraform 1.11+): it never
# enters the plan or the state. Bump `value_version` to write a new value.
resource "devhelm_secret_value" "card_token_production" {
  key            = devhelm_secret.card_token.key
  environment_id = devhelm_environment.production.id
  value          = var.card_token_production
  value_version  = "2026-10-25"
}
