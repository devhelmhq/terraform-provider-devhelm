# A resource group is a fleet container — it defines defaults that apply to
# every monitor that joins it via `devhelm_resource_group_membership`. Use it
# when you have many similar monitors that share the same retry/alerting
# strategy.
resource "devhelm_resource_group" "checkout" {
  name        = "Checkout"
  description = "All services backing the checkout flow."

  default_frequency      = 60
  default_regions        = ["us-east", "eu-west"]
  default_alert_channels = [devhelm_alert_channel.ops_slack.id]
  default_environment_id = devhelm_environment.production.id

  default_retry_strategy = {
    type        = "fixed"
    interval    = 30
    max_retries = 3
  }

  health_threshold_type      = "PERCENTAGE"
  health_threshold_value     = 50.0
  suppress_member_alerts     = true # group-level policy handles paging
  confirmation_delay_seconds = 60
  recovery_cooldown_minutes  = 5
}
