# HTTP monitor — the most common case.
resource "devhelm_monitor" "api" {
  name              = "Public API"
  type              = "HTTP"
  frequency_seconds = 60
  regions           = ["us-east", "eu-west"]

  config = jsonencode({
    url             = "https://api.example.com/health"
    method          = "GET"
    timeout_seconds = 10
  })

  assertions {
    type   = "status_code"
    config = jsonencode({ expected = 200 })
  }

  assertions {
    type   = "response_time"
    config = jsonencode({ threshold_ms = 500 })
  }

  alert_channel_ids = [devhelm_alert_channel.ops.id]
  tag_ids           = [devhelm_tag.production.id]
}

# Heartbeat monitor — for cron jobs and batch workers. The job POSTs to
# `ping_url` after each successful run; if no ping arrives within
# `expectedInterval + gracePeriod`, the monitor fires an incident.
resource "devhelm_monitor" "nightly_etl" {
  name = "Nightly ETL"
  type = "HEARTBEAT"

  config = jsonencode({
    expectedInterval = 86400 # 24h
    gracePeriod      = 3600  # 1h slack for slow runs
  })
}

output "etl_ping_url" {
  description = "Hit this URL from your cron job after a successful run."
  value       = devhelm_monitor.nightly_etl.ping_url
}

# Authenticated HTTP monitor with a secret-backed bearer token. Storing the
# token in `devhelm_secret` keeps it out of state files and CI logs.
resource "devhelm_secret" "api_token" {
  key   = "api_token"
  value = var.api_token # mark as sensitive in your variables.tf
}

resource "devhelm_monitor" "private_api" {
  name              = "Private API"
  type              = "HTTP"
  frequency_seconds = 60

  config = jsonencode({ url = "https://api.example.com/internal/health" })

  auth = jsonencode({
    type  = "bearer"
    token = "{{ secret.api_token }}" # interpolated by the API at probe time
  })
}
