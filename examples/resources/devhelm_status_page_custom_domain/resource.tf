# Reserve a custom hostname on a status page. The resource exposes the DNS
# records you need to create at your DNS provider for verification and
# traffic routing.
resource "devhelm_status_page_custom_domain" "acme" {
  status_page_id = devhelm_status_page.public.id
  hostname       = "status.acme.com"
}

# End-to-end automation: provision the verification + traffic CNAME at
# Cloudflare from the values exposed by the resource, then block apply
# until the API confirms the domain is verified.
resource "cloudflare_record" "verification" {
  zone_id = var.cloudflare_zone_id
  name    = devhelm_status_page_custom_domain.acme.verification_record.name
  type    = devhelm_status_page_custom_domain.acme.verification_record.type
  value   = devhelm_status_page_custom_domain.acme.verification_record.value
  ttl     = 300
  proxied = false
}

# When verification_method=CNAME (the default), verification_record and
# traffic_record are the same record — count=0 here avoids a duplicate.
resource "cloudflare_record" "traffic" {
  count = (
    devhelm_status_page_custom_domain.acme.verification_record.name ==
    devhelm_status_page_custom_domain.acme.traffic_record.name
  ) ? 0 : 1

  zone_id = var.cloudflare_zone_id
  name    = devhelm_status_page_custom_domain.acme.traffic_record.name
  type    = devhelm_status_page_custom_domain.acme.traffic_record.type
  value   = devhelm_status_page_custom_domain.acme.traffic_record.value
  ttl     = 300
  proxied = false
}

resource "devhelm_status_page_custom_domain_verification" "acme" {
  status_page_id   = devhelm_status_page.public.id
  custom_domain_id = devhelm_status_page_custom_domain.acme.id

  depends_on = [cloudflare_record.verification, cloudflare_record.traffic]
}
