# Subscribe to GitHub status — when GitHub reports an incident, DevHelm
# surfaces it alongside your own monitors. Browse the catalog at
# https://app.devhelm.io/dependencies for the full list of `service` slugs.
resource "devhelm_dependency" "github" {
  service           = "github"
  alert_sensitivity = "INCIDENTS_ONLY"
}

resource "devhelm_dependency" "cloudflare" {
  service = "cloudflare"
}

# A more focused subscription scoped to a specific component (e.g. AWS
# EC2 in us-east-1).
resource "devhelm_dependency" "aws_us_east_ec2" {
  service           = "aws"
  component_id      = "us-east-1-ec2"
  alert_sensitivity = "MAJOR_ONLY"
}
