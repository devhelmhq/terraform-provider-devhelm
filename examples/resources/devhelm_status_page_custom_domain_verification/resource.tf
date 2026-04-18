# Block `terraform apply` until the API confirms the custom domain is
# verified. Models the well-known `aws_acm_certificate_validation` pattern.
#
# The Create operation polls the DevHelm verify endpoint every 10s for up to
# 10 minutes; if the polling budget is exhausted the apply fails so CI can
# investigate the DNS record rather than silently succeeding with an
# unverified domain.
resource "devhelm_status_page_custom_domain_verification" "acme" {
  status_page_id   = devhelm_status_page.public.id
  custom_domain_id = devhelm_status_page_custom_domain.acme.id

  # IMPORTANT: depend on the DNS records that prove ownership / route
  # traffic — without `depends_on`, Terraform may attempt verification before
  # the record has propagated.
  depends_on = [cloudflare_record.verification, cloudflare_record.traffic]
}
