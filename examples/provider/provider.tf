terraform {
  required_version = ">= 1.5.0"
  required_providers {
    devhelm = {
      source = "devhelmhq/devhelm"
      # Pre-release: the provider currently ships only as pre-release
      # versions and Terraform's `~>` operator never selects pre-releases.
      # Pin the exact version below — bump it explicitly when the next
      # version ships, or wait for the GA `1.0.0` cut to switch to a
      # range like `~> 1.0`.
      version = "0.2.0-beta.1"
    }
  }
}

# All four provider attributes have environment-variable equivalents and are
# optional in the block. The most common pattern is to leave the block empty
# and supply credentials through the environment so the same config works
# locally, in CI, and in Terraform Cloud without modification.
#
#   DEVHELM_API_TOKEN     — required; create one at https://app.devhelm.io/settings/tokens
#   DEVHELM_API_URL       — defaults to https://api.devhelm.io
#   DEVHELM_ORG_ID        — defaults to "1"; only required for multi-org tokens
#   DEVHELM_WORKSPACE_ID  — defaults to "1"; only required for multi-workspace tokens
provider "devhelm" {}
