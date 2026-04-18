data "devhelm_alert_channel" "ops_slack" {
  name = "#alerts-prod"
}

resource "devhelm_monitor" "api" {
  name              = "API"
  type              = "HTTP"
  frequency_seconds = 60
  config            = jsonencode({ url = "https://api.example.com/health", method = "GET" })
  alert_channel_ids = [data.devhelm_alert_channel.ops_slack.id]
}
