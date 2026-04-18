# Reference an existing monitor by name (e.g. one created by another team in
# a different repo) so you can attach it to a status page or notification
# policy without owning its lifecycle.
data "devhelm_monitor" "shared_api" {
  name = "Public API"
}

resource "devhelm_status_page_component" "api" {
  status_page_id = devhelm_status_page.public.id
  name           = "Public API"
  type           = "MONITOR"
  monitor_id     = data.devhelm_monitor.shared_api.id
}
