# A bare-minimum status page.
resource "devhelm_status_page" "public" {
  name        = "Acme Status"
  slug        = "acme"
  description = "Live status of Acme services."
}

# A fully-branded page with components grouped by team. Each child
# (`component_group`, `component`, `custom_domain`) is a separate resource so
# renames preserve identity via the built-in `moved {}` block.
resource "devhelm_status_page" "branded" {
  name          = "Acme Cloud Status"
  slug          = "acme-cloud"
  description   = "Real-time status across Acme Cloud regions."
  visibility    = "PUBLIC"
  incident_mode = "AUTOMATIC" # MANUAL | REVIEW | AUTOMATIC

  branding = {
    brand_color     = "#4F46E5"
    text_color      = "#09090B"
    page_background = "#FAFAFA"
    card_background = "#FFFFFF"
    border_color    = "#E4E4E7"
    theme           = "light"
    logo_url        = "https://acme.com/static/logo.svg"
    favicon_url     = "https://acme.com/favicon.ico"
    hide_powered_by = true
  }
}

resource "devhelm_status_page_component_group" "infra" {
  status_page_id = devhelm_status_page.branded.id
  name           = "Infrastructure"
}

resource "devhelm_status_page_component" "api" {
  status_page_id = devhelm_status_page.branded.id
  group_id       = devhelm_status_page_component_group.infra.id
  name           = "Public API"
  type           = "MONITOR"
  monitor_id     = devhelm_monitor.api.id
}
