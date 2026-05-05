---
page_title: "Getting started with Terraform"
subcategory: "Guides"
description: |-
  End-to-end walkthrough — provision your first DevHelm monitor, alert
  channel, notification policy, and public status page in under five
  minutes.
---

# Getting started with Terraform

This guide walks you through setting up the DevHelm Terraform provider and
shipping a fully wired monitoring stack — one HTTP monitor, one alert
channel, a tiered notification policy, and a public status page — in a
single `terraform apply`.

By the end you'll have a working pattern you can scale to your real fleet
with `for_each`, modules, and the rest of Terraform's normal toolbox.

## Prerequisites

* **Terraform** ≥ 1.5 — install via [tfenv](https://github.com/tfutils/tfenv)
  or [the official downloads](https://developer.hashicorp.com/terraform/install).
* **A DevHelm account** — sign up at [app.devhelm.io](https://app.devhelm.io).
* **An API token** — create one at
  [Settings → Tokens](https://app.devhelm.io/settings/tokens). Scope it to
  the workspace you intend to manage.

## 1. Configure the provider

Create a new directory for your DevHelm config and add a `versions.tf`:

```hcl
# versions.tf
terraform {
  required_version = ">= 1.5.0"
  required_providers {
    devhelm = {
      source  = "devhelmhq/devhelm"
      version = "0.2.0-beta.1"
    }
  }
}
```

~> **Pre-release.** The DevHelm provider currently ships only as
pre-release versions. Terraform's `~>` operator never selects
pre-releases, so `~> 0.2` matches *zero* published versions today.
Pin the exact version above and bump it explicitly when the next
release ships. Once the GA `1.0.0` cut lands, you can switch to
`version = "~> 1.0"`.

The provider reads its credentials from the environment by default — keep
the `provider "devhelm" {}` block empty so the same config works on your
laptop, in CI, and in Terraform Cloud:

```hcl
# main.tf
provider "devhelm" {}
```

Export your token:

```bash
export DEVHELM_API_TOKEN="dh_live_…"
```

| Variable               | Purpose                                                                 |
|------------------------|-------------------------------------------------------------------------|
| `DEVHELM_API_TOKEN`    | API token (required).                                                   |
| `DEVHELM_API_URL`      | API base URL. Defaults to `https://api.devhelm.io`.                     |
| `DEVHELM_ORG_ID`       | Organization ID. Defaults to `1`. Override for multi-org tokens.        |
| `DEVHELM_WORKSPACE_ID` | Workspace ID. Defaults to `1`. Override for multi-workspace tokens.     |

## 2. Initialize

```bash
terraform init
```

You should see:

```text
Terraform has been successfully initialized!
```

## 3. Add an alert channel

Channels are *where* alerts go. Start with email — it requires no third-party
setup.

```hcl
# main.tf
resource "devhelm_alert_channel" "ops_email" {
  name         = "Ops Email"
  channel_type = "email"
  recipients   = ["ops@example.com"]
}
```

Need Slack, PagerDuty, OpsGenie, Discord, Teams, or a generic webhook
instead? Each is a one-line `channel_type` change — see the
[`devhelm_alert_channel` reference](../resources/alert_channel) for the full
matrix.

## 4. Add a monitor

Monitors are *what* you check. The HTTP type is the most common; here we
verify both reachability and a 200 OK response.

```hcl
resource "devhelm_monitor" "api" {
  name              = "Public API"
  type              = "HTTP"
  frequency_seconds = 60
  regions           = ["us-east", "eu-west"]

  config = jsonencode({
    url             = "https://api.example.com/health"
    method          = "GET"
    timeout_seconds = 10
  })

  assertions {
    type   = "status_code"
    config = jsonencode({ expected = 200, operator = "equals" })
  }

  assertions {
    type = "response_time"
    # API field names are camelCase inside `config`. Use `thresholdMs`,
    # not `threshold_ms`.
    config = jsonencode({ thresholdMs = 500 })
  }

  alert_channel_ids = [devhelm_alert_channel.ops_email.id]
}
```

Heartbeat, DNS, TCP, ICMP, and MCP-server monitors follow the same shape;
only `type` and the inner `config` payload change.

## 5. Wire up a notification policy

Channels deliver notifications, but a *policy* decides which incidents
trigger what. This minimal policy pages `ops_email` the moment any monitor
goes DOWN:

```hcl
resource "devhelm_notification_policy" "prod_down" {
  name = "Production - Down"

  match_rule {
    type  = "severity_gte"
    value = "DOWN"
  }

  escalation_step {
    channel_ids = [devhelm_alert_channel.ops_email.id]
  }
}
```

For multi-step escalation (Slack → PagerDuty after 10 min, repeat every
5 min), add a second `escalation_step` block — see the
[`devhelm_notification_policy` examples](../resources/notification_policy).

## 6. Publish a status page

A public status page is the customer-facing surface for everything you've
built so far.

```hcl
resource "devhelm_status_page" "public" {
  name        = "Acme Status"
  slug        = "acme"
  description = "Live status of Acme services."
}

resource "devhelm_status_page_component" "api" {
  status_page_id = devhelm_status_page.public.id
  name           = "Public API"
  type           = "MONITOR"
  monitor_id     = devhelm_monitor.api.id
}
```

`page_url` is computed by the API. After apply, see where the page is
served:

```hcl
output "status_page_url" {
  value = devhelm_status_page.public.page_url
}
```

To wire a custom hostname like `status.acme.com`, see
[Custom domain verification](#custom-domain-verification) below.

## 7. Apply

```bash
terraform plan
terraform apply
```

You'll see the URL of your live status page in the apply output. Open it
in a browser — your monitor, channel, and policy are now live.

```bash
terraform output status_page_url
# https://acme.devhelmstatus.com
```

## Where to go next

### Scale to a real fleet with `for_each`

Once you have one of each resource working, the natural next step is to
manage many monitors at once. The most common idiom is a local list driven
through `for_each`:

```hcl
locals {
  endpoints = {
    api      = "https://api.example.com/health"
    cart     = "https://cart.example.com/health"
    checkout = "https://checkout.example.com/health"
  }
}

resource "devhelm_monitor" "service" {
  for_each          = local.endpoints
  name              = each.key
  type              = "HTTP"
  frequency_seconds = 60
  config            = jsonencode({ url = each.value, method = "GET" })
  alert_channel_ids = [devhelm_alert_channel.ops_email.id]
}
```

### Group fleets with `devhelm_resource_group`

A resource group defines defaults (retry strategy, regions, alert channels)
that apply to every member. Use it when ten monitors all belong to the same
service and should be paged together:

```hcl
resource "devhelm_resource_group" "checkout" {
  name = "Checkout"

  default_retry_strategy = {
    type        = "fixed"
    interval    = 30
    max_retries = 3
  }
}

resource "devhelm_resource_group_membership" "members" {
  for_each    = devhelm_monitor.service
  group_id    = devhelm_resource_group.checkout.id
  member_type = "monitor"
  member_id   = each.value.id
}
```

### Custom domain verification

Use [`devhelm_status_page_custom_domain`](../resources/status_page_custom_domain)
to reserve a hostname and surface the DNS records you need to create. Pair
it with [`devhelm_status_page_custom_domain_verification`](../resources/status_page_custom_domain_verification)
to block `terraform apply` until the API confirms verification — modeled
on `aws_acm_certificate_validation`.

End-to-end Cloudflare wiring lives in the
[`devhelm_status_page_custom_domain` example](../resources/status_page_custom_domain).

### Refactor with `moved {}` blocks

Renaming a Terraform address normally destroys and re-creates the resource.
The DevHelm provider preserves server-side UUIDs across renames, so you can
combine `moved {}` with a rename to keep history, subscribers, and
references intact:

```hcl
moved {
  from = devhelm_status_page_component.api
  to   = devhelm_status_page_component.public_api
}

resource "devhelm_status_page_component" "public_api" {
  status_page_id = devhelm_status_page.public.id
  name           = "Public API"
  type           = "MONITOR"
  monitor_id     = devhelm_monitor.api.id
}
```

### Import existing infrastructure

If you've been managing DevHelm via the dashboard, every resource supports
`terraform import`. Most accept either a UUID or a human-friendly name /
slug / key:

```bash
terraform import devhelm_monitor.api "Public API"
terraform import devhelm_status_page.public acme
terraform import devhelm_status_page_component.api 7f8…/918…
```

See the **Import** section on each resource's reference page for the exact
syntax.

## Troubleshooting

**`Error: Missing API Token`** — set `DEVHELM_API_TOKEN`, or pass `token =
var.token` in the `provider "devhelm" {}` block.

**`401 Unauthorized`** — the token is invalid or scoped to a different
workspace. Double-check `DEVHELM_ORG_ID` and `DEVHELM_WORKSPACE_ID` if you
have multi-workspace tokens.

**Plan shows perpetual diff on `branding`, `regions`, or `alert_channel_ids`**
— these attributes have explicit "preserve vs clear" semantics. Omit the
attribute (or set it to `null`) to preserve the current value; set it to
`[]` / `{}` to clear. See the per-attribute description in the resource
reference for details.

**`terraform apply` for a custom domain hangs** — the verification resource
polls the API for up to 10 minutes for DNS to propagate. If it times out,
double-check the DNS records exposed by the
`devhelm_status_page_custom_domain` resource match what your DNS provider
actually serves.

## Reference

- [Provider configuration](../index)
- [Full resource list](../resources)
- [Data sources](../data-sources)
- [`examples/` directory in the GitHub repo](https://github.com/devhelmhq/terraform-provider-devhelm/tree/main/examples) — every resource has a runnable example
- [DevHelm dashboard](https://app.devhelm.io)
