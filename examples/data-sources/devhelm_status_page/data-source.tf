data "devhelm_status_page" "public" {
  slug = "acme"
}

resource "devhelm_status_page_component" "api" {
  status_page_id = data.devhelm_status_page.public.id
  name           = "Public API"
  type           = "MONITOR"
  monitor_id     = devhelm_monitor.api.id
}

output "page_url" {
  description = "Public URL where customers see this status page."
  value       = data.devhelm_status_page.public.page_url
}
