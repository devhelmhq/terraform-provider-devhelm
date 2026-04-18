terraform {
  required_version = ">= 1.5.0"
  required_providers {
    devhelm = {
      source  = "devhelmhq/devhelm"
      version = "~> 0.1"
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
