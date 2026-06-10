# Look up a third-party service in the DevHelm status catalog by slug.
# Validates the slug at plan time and exposes the service UUID for use in
# notification-policy match rules.
data "devhelm_service" "stripe" {
  slug = "stripe"
}

# Subscribe to the looked-up service — the data source guarantees the slug
# exists before the dependency is created.
resource "devhelm_dependency" "stripe" {
  service           = data.devhelm_service.stripe.slug
  alert_sensitivity = "INCIDENTS_ONLY"
}

# Route incidents originating from Stripe to the payments on-call channel
# via a service_id_in match rule referencing the catalog UUID.
resource "devhelm_notification_policy" "stripe_incidents" {
  name = "Stripe incidents - payments on-call"

  match_rule {
    type   = "service_id_in"
    values = [data.devhelm_service.stripe.id]
  }

  escalation_step {
    channel_ids = [devhelm_alert_channel.payments_oncall.id]
  }
}

output "stripe_status" {
  description = "Stripe's current overall status and 30-day uptime."
  value = {
    status     = data.devhelm_service.stripe.overall_status
    uptime_30d = data.devhelm_service.stripe.uptime_30d
  }
}
