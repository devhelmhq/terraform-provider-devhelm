# Outbound webhook for forwarding incident events into your own systems.
resource "devhelm_webhook" "audit_sink" {
  url               = "https://audit.internal.example.com/devhelm/events"
  description       = "Mirrors all incident lifecycle events into the audit log."
  enabled           = true
  subscribed_events = ["incident.opened", "incident.resolved", "incident.acknowledged"]
}
