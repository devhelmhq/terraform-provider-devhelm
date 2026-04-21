# Secrets are referenced by `id` from monitor `auth` blocks. The value is
# write-only — the API never returns it, and the provider tracks
# `value_hash` to detect drift without ever pulling the plaintext back
# into state.
resource "devhelm_secret" "api_token" {
  key   = "api_token"
  value = var.api_token
}

resource "devhelm_secret" "github_pat" {
  key   = "github_pat"
  value = var.github_pat
}

# Reference a secret from a monitor. The provider passes
# `vault_secret_id` to the API; the API resolves it to the actual
# credential at probe time so it never transits via Terraform.
resource "devhelm_monitor" "api" {
  name              = "API"
  type              = "HTTP"
  frequency_seconds = 60
  config            = jsonencode({ url = "https://api.example.com/health", method = "GET" })

  auth = {
    bearer = {
      vault_secret_id = devhelm_secret.api_token.id
    }
  }
}
