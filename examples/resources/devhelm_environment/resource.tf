# Environments hold variables that monitors can interpolate at probe time.
# The same monitor config can target staging or production by switching
# `environment_id`, instead of forking the HCL.
resource "devhelm_environment" "production" {
  name       = "Production"
  slug       = "prod"
  is_default = true

  variables = {
    BASE_URL    = "https://api.example.com"
    HEALTH_PATH = "/health"
  }
}

resource "devhelm_environment" "staging" {
  name = "Staging"
  slug = "staging"

  variables = {
    BASE_URL    = "https://api.staging.example.com"
    HEALTH_PATH = "/health"
  }
}

resource "devhelm_monitor" "api" {
  name              = "API"
  type              = "HTTP"
  environment_id    = devhelm_environment.production.id
  frequency_seconds = 60
  config            = jsonencode({ url = "{{ env.BASE_URL }}{{ env.HEALTH_PATH }}", method = "GET" })
}
