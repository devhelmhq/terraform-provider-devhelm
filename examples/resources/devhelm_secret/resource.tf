# Secrets are referenced by `key` from monitor `auth` blocks via the
# `{{ secret.<key> }}` interpolation syntax. The value is write-only — the
# API never returns it, and the provider tracks `value_hash` to detect drift
# without ever pulling the plaintext back into state.
resource "devhelm_secret" "api_token" {
  key   = "api_token"
  value = var.api_token
}

resource "devhelm_secret" "github_pat" {
  key   = "github_pat"
  value = var.github_pat
}

# Reference a secret from a monitor.
resource "devhelm_monitor" "api" {
  name              = "API"
  type              = "HTTP"
  frequency_seconds = 60
  config            = jsonencode({ url = "https://api.example.com/health", method = "GET" })
  auth = jsonencode({
    type  = "bearer"
    token = "{{ secret.${devhelm_secret.api_token.key} }}"
  })
}
