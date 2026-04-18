resource "devhelm_status_page_component_group" "infra" {
  status_page_id = devhelm_status_page.public.id
  name           = "Infrastructure"
  description    = "Core platform services."
}

resource "devhelm_status_page_component_group" "apps" {
  status_page_id = devhelm_status_page.public.id
  name           = "Applications"
  display_order  = 1
  collapsed      = false
}
