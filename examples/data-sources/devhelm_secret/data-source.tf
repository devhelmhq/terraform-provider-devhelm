# Look up a secret by key instead of hardcoding its UUID. The plaintext
# value is never returned by the API; only the id and a value_hash for
# change detection are available.
data "devhelm_secret" "api_token" {
  key = "PROD_API_TOKEN"
}

resource "devhelm_monitor" "api" {
  name              = "API"
  type              = "HTTP"
  frequency_seconds = 60
  config            = jsonencode({ url = "https://api.example.com/health" })

  auth = {
    bearer = {
      vault_secret_id = data.devhelm_secret.api_token.id
    }
  }
}
