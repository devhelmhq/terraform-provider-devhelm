# Single-step policy: page #alerts-prod the moment any monitor goes DOWN.
resource "devhelm_notification_policy" "prod_down" {
  name = "Production - Down"

  match_rule {
    type  = "severity_gte"
    value = "DOWN"
  }

  escalation_step {
    channel_ids = [devhelm_alert_channel.ops_slack.id]
  }
}

# Two-step policy with escalation: notify Slack first, escalate to PagerDuty
# if no one acknowledges within 10 minutes.
resource "devhelm_notification_policy" "tiered" {
  name     = "Production - Tiered"
  priority = 100 # higher priority is evaluated first

  match_rule {
    type   = "tag"
    values = [devhelm_tag.production.id]
  }

  match_rule {
    type  = "severity_gte"
    value = "DEGRADED"
  }

  # Step 1: notify Slack and require acknowledgement.
  escalation_step {
    channel_ids   = [devhelm_alert_channel.ops_slack.id]
    require_ack   = true
    delay_minutes = 10 # if not acked in 10m, escalate to step 2
  }

  # Step 2: page PagerDuty, repeating every 5 minutes until ack/resolved.
  escalation_step {
    channel_ids             = [devhelm_alert_channel.pagerduty.id]
    repeat_interval_seconds = 300
  }

  on_resolve = "notify"
}
