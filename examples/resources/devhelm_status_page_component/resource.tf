# Monitor-backed component — status follows the underlying monitor.
resource "devhelm_status_page_component" "api" {
  status_page_id = devhelm_status_page.public.id
  group_id       = devhelm_status_page_component_group.infra.id
  name           = "Public API"
  type           = "MONITOR"
  monitor_id     = devhelm_monitor.api.id
}

# Static component — text-only badge you flip manually (or via the API).
resource "devhelm_status_page_component" "marketing_site" {
  status_page_id = devhelm_status_page.public.id
  name           = "Marketing Site"
  type           = "STATIC"
  description    = "Hand-managed; updated when our marketing CDN has issues."
}

# Group-backed component — rolls up the health of every monitor in a
# devhelm_resource_group into a single bar on the status page.
resource "devhelm_status_page_component" "checkout" {
  status_page_id    = devhelm_status_page.public.id
  group_id          = devhelm_status_page_component_group.apps.id
  name              = "Checkout"
  type              = "GROUP"
  resource_group_id = devhelm_resource_group.checkout.id
  show_uptime       = true
  start_date        = "2025-01-01" # hide uptime data older than this
}
