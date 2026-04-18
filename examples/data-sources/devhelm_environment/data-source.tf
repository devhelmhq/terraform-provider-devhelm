# Look up an environment created elsewhere — e.g. a shared "production"
# environment owned by a platform team — by its slug.
data "devhelm_environment" "production" {
  slug = "prod"
}

resource "devhelm_monitor" "api" {
  name              = "API"
  type              = "HTTP"
  environment_id    = data.devhelm_environment.production.id
  frequency_seconds = 60
  config            = jsonencode({ url = "{{ env.BASE_URL }}/health" })
}
