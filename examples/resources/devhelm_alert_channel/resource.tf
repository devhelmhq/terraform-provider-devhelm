# Email channel — recipients is required.
resource "devhelm_alert_channel" "ops_email" {
  name         = "Ops Email"
  channel_type = "email"
  recipients   = ["ops@example.com", "oncall@example.com"]
}

# Slack channel — webhook_url required; mention_text is optional.
resource "devhelm_alert_channel" "ops_slack" {
  name         = "#alerts-prod"
  channel_type = "slack"
  webhook_url  = var.slack_webhook_url
  mention_text = "<!subteam^S0123ABCD|oncall>"
}

# PagerDuty channel — routing_key is sensitive and never returned by the API
# in plaintext; the provider tracks `config_hash` to detect drift.
resource "devhelm_alert_channel" "pagerduty" {
  name         = "PagerDuty - Tier 1"
  channel_type = "pagerduty"
  routing_key  = var.pagerduty_routing_key
}

# OpsGenie channel.
resource "devhelm_alert_channel" "opsgenie" {
  name         = "OpsGenie - Platform"
  channel_type = "opsgenie"
  api_key      = var.opsgenie_api_key
  region       = "us"
}

# Discord channel.
resource "devhelm_alert_channel" "discord" {
  name            = "Discord - Eng"
  channel_type    = "discord"
  webhook_url     = var.discord_webhook_url
  mention_role_id = "0123456789012345678"
}

# Microsoft Teams channel.
resource "devhelm_alert_channel" "teams" {
  name         = "Teams - Platform"
  channel_type = "teams"
  webhook_url  = var.teams_webhook_url
}

# Generic webhook channel — for routing alerts into a custom system. The
# signing_secret is used to compute an HMAC over the payload so the receiver
# can verify the call originated from DevHelm.
resource "devhelm_alert_channel" "internal_webhook" {
  name           = "Internal Pager Bridge"
  channel_type   = "webhook"
  url            = "https://pager-bridge.internal.example.com/devhelm"
  signing_secret = var.bridge_signing_secret

  custom_headers = {
    "X-Tenant" = "production"
  }
}
