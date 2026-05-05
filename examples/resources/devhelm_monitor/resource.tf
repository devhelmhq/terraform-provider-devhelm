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
    type = "status_code"
    # `expected` is a STRING in the API contract — it can hold "200", "2xx",
    # or "200-299". Always quote the value, even for plain numeric codes;
    # `jsonencode({ expected = 200, ... })` (number) plans cleanly but
    # apply fails with "Provider produced inconsistent result" because the
    # API normalizes the value to "200" (string) on the round-trip.
    config = jsonencode({ expected = "200", operator = "equals" })
  }

  assertions {
    type = "response_time"
    # API field names are camelCase inside `config` (the API contract is JSON,
    # not Terraform). Use `thresholdMs`, not `threshold_ms`.
    config = jsonencode({ thresholdMs = 500 })
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

# Authenticated HTTP monitor with a vault-backed bearer token. The actual
# secret material lives in `devhelm_secret` and is referenced by ID — it
# never enters Terraform state.
resource "devhelm_secret" "api_token" {
  key   = "api_token"
  value = var.api_token # mark as sensitive in your variables.tf
}

resource "devhelm_monitor" "private_api" {
  name              = "Private API"
  type              = "HTTP"
  frequency_seconds = 60

  config = jsonencode({ url = "https://api.example.com/internal/health" })

  # Pick exactly one of: bearer / basic / header / api_key.
  # The API resolves `vault_secret_id` at probe time and attaches the
  # credential — no plaintext ever transits the wire from Terraform.
  auth = {
    bearer = {
      vault_secret_id = devhelm_secret.api_token.id
    }
  }
}

# Custom header (e.g. CF-Access-Client-Secret, X-Auth-Token, …).
resource "devhelm_monitor" "header_protected" {
  name = "Header-protected service"
  type = "HTTP"

  config = jsonencode({ url = "https://internal.example.com/health" })

  auth = {
    header = {
      header_name     = "X-Auth-Token"
      vault_secret_id = devhelm_secret.api_token.id
    }
  }
}

# Basic auth — username:password are stored in the vault entry as a
# colon-separated pair. Provider only references the secret by ID.
resource "devhelm_monitor" "basic_auth" {
  name = "Legacy admin endpoint"
  type = "HTTP"

  config = jsonencode({ url = "https://legacy.example.com/admin/health" })

  auth = {
    basic = {
      vault_secret_id = devhelm_secret.api_token.id
    }
  }
}
