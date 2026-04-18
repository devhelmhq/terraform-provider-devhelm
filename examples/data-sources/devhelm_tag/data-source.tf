data "devhelm_tag" "production" {
  name = "production"
}

resource "devhelm_monitor" "api" {
  name              = "API"
  type              = "HTTP"
  frequency_seconds = 60
  config            = jsonencode({ url = "https://api.example.com/health" })
  tag_ids           = [data.devhelm_tag.production.id]
}
