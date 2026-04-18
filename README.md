# Terraform Provider for DevHelm

[![Terraform Registry](https://img.shields.io/badge/terraform-registry-purple.svg)](https://registry.terraform.io/providers/devhelmhq/devhelm/latest)
[![Go Reference](https://pkg.go.dev/badge/github.com/devhelmhq/terraform-provider-devhelm.svg)](https://pkg.go.dev/github.com/devhelmhq/terraform-provider-devhelm)
[![License](https://img.shields.io/badge/license-MPL--2.0-blue.svg)](LICENSE)

Manage [DevHelm](https://devhelm.io) monitoring, alerting, status pages, and
incident management as code.

The provider is the recommended way to manage DevHelm at scale: it gives you
a stable Terraform identity for every resource (so renames are non-destructive
via the built-in `moved {}` block), composes naturally with `for_each` and
modules, and integrates cleanly with DNS providers, secret stores, and other
parts of your platform stack.

> **Status:** the schema is stable and the resource set is feature-complete
> against the DevHelm v1 API. We're tagging the first non-alpha release as
> `0.1.0` and following [semver](https://semver.org/) thereafter — schema
> changes that would break existing state will go through a deprecation
> cycle.

## Quick start

```hcl
terraform {
  required_version = ">= 1.5.0"
  required_providers {
    devhelm = {
      source  = "devhelmhq/devhelm"
      version = "~> 0.1"
    }
  }
}

provider "devhelm" {}

resource "devhelm_alert_channel" "ops" {
  name         = "Ops Email"
  channel_type = "email"
  recipients   = ["ops@example.com"]
}

resource "devhelm_monitor" "api" {
  name              = "Public API"
  type              = "HTTP"
  frequency_seconds = 60
  config            = jsonencode({ url = "https://api.example.com/health", method = "GET" })
  alert_channel_ids = [devhelm_alert_channel.ops.id]

  assertions {
    type   = "status_code"
    config = jsonencode({ expected = 200 })
  }
}
```

```bash
export DEVHELM_API_TOKEN=…  # create at https://app.devhelm.io/settings/tokens
terraform init
terraform plan
terraform apply
```

A full quickstart, plus end-to-end examples for status pages, custom domains,
notification policies, and resource groups lives at the [Getting Started with
Terraform guide](https://devhelm.io/docs/cli/terraform).

## What you can manage

| Category               | Resources / Data sources                                                                                                  |
|------------------------|---------------------------------------------------------------------------------------------------------------------------|
| **Monitoring**         | `devhelm_monitor`, `devhelm_environment`, `devhelm_secret`, `devhelm_tag`                                                 |
| **Alerting**           | `devhelm_alert_channel`, `devhelm_notification_policy`, `devhelm_webhook`                                                 |
| **Fleet management**   | `devhelm_resource_group`, `devhelm_resource_group_membership`                                                             |
| **Third-party deps**   | `devhelm_dependency`                                                                                                      |
| **Status pages**       | `devhelm_status_page`, `devhelm_status_page_component_group`, `devhelm_status_page_component`                             |
| **Custom domains**     | `devhelm_status_page_custom_domain`, `devhelm_status_page_custom_domain_verification`                                     |
| **Look-up data sources** | `devhelm_monitor`, `devhelm_alert_channel`, `devhelm_environment`, `devhelm_resource_group`, `devhelm_status_page`, `devhelm_tag` |

Per-resource documentation, schemas, and copy-paste examples are published
on the [Terraform Registry](https://registry.terraform.io/providers/devhelmhq/devhelm/latest/docs).

## Configuration

All four provider attributes have environment-variable equivalents. The
recommended pattern is to leave the provider block empty and supply
credentials through the environment so the same config works locally, in
CI, and in Terraform Cloud without modification.

| Attribute      | Env var                | Default                     | Required |
|----------------|------------------------|-----------------------------|----------|
| `token`        | `DEVHELM_API_TOKEN`    | —                           | yes      |
| `base_url`     | `DEVHELM_API_URL`      | `https://api.devhelm.io`    | no       |
| `org_id`       | `DEVHELM_ORG_ID`       | `1`                         | no       |
| `workspace_id` | `DEVHELM_WORKSPACE_ID` | `1`                         | no       |

Create an API token at <https://app.devhelm.io/settings/tokens>. The token
should be scoped to the workspace you intend to manage from Terraform.

## Examples

The [`examples/`](./examples) directory holds runnable, copy-paste-ready
configurations for every resource and data source the provider exposes:

```
examples/
├── provider/                 # provider {} block + env-var docs
├── resources/
│   ├── devhelm_monitor/         # HTTP, heartbeat, authenticated
│   ├── devhelm_alert_channel/   # all 7 channel types
│   ├── devhelm_notification_policy/
│   ├── devhelm_status_page/
│   ├── devhelm_status_page_custom_domain/  # end-to-end Cloudflare wiring
│   └── …
└── data-sources/
    └── …
```

Each `<resource>/resource.tf` is what gets embedded into the Registry
documentation page for that resource via `tfplugindocs`.

## Common workflows

### Renaming resources without recreating

The status-page family (`devhelm_status_page`, `devhelm_status_page_component_group`,
`devhelm_status_page_component`) all assign stable UUIDs server-side. Renaming
the Terraform address with a `moved {}` block preserves the underlying UUID,
incident history, and subscriber list — no destructive delete/recreate.

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

### Importing existing infrastructure

Every resource implements `ImportState`. Most resources accept either a UUID
or a human-friendly identifier (name / slug / key); see the import command
example in each resource's docs page.

```bash
# Single-segment imports
terraform import devhelm_monitor.api "Public API"
terraform import devhelm_status_page.public acme

# Compound imports (parent/child resources)
terraform import devhelm_status_page_component.api \
  7f819203-…/9182b3c4-…
```

### Custom domain verification

The `devhelm_status_page_custom_domain` resource exposes the DNS records you
need to create at your DNS provider, and the companion
`devhelm_status_page_custom_domain_verification` resource blocks
`terraform apply` until the API confirms verification — modeled on the
well-known `aws_acm_certificate_validation` pattern. See the
[full example](./examples/resources/devhelm_status_page_custom_domain/resource.tf).

## Contributing

Local development:

```bash
git clone https://github.com/devhelmhq/terraform-provider-devhelm
cd terraform-provider-devhelm

# Build + install into ~/.terraform.d/plugins so dev_overrides resolves it
make install

# Run the unit + framework acceptance tests (the latter requires Terraform on PATH)
make test
TF_ACC=1 make testacc

# Regenerate docs/ after editing schema descriptions or examples/
make docs
```

The Go acceptance tests use an in-process mock API server so they run
in <1s per scenario — see [`internal/provider/framework_test.go`](./internal/provider/framework_test.go).

End-to-end surface tests that drive a real `terraform apply` against the
DevHelm API live in the monorepo at
[`tests/surfaces/terraform_provider_devhelm/`](https://github.com/devhelmhq/mono/tree/main/tests/surfaces/terraform_provider_devhelm)
and are run on every PR via the `surface_release` integration test workflow.

## License

[MPL-2.0](LICENSE)
