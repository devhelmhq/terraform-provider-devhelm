package resources

// alert_channel_validate.go encapsulates the per-channel-type field shape
// contract for `devhelm_alert_channel`.
//
// Why this exists
// ───────────────
//
// `AlertChannelResourceModel` is a flat struct with one Terraform attribute
// per leaf field across every channel kind (`webhook_url`, `routing_key`,
// `api_key`, `recipients`, etc.). The `channel_type` discriminator decides
// which subset is meaningful. Without a `ValidateConfig` implementation,
// Terraform happily accepts any combination — a user writing
//
//	resource "devhelm_alert_channel" "ops_slack" {
//	  channel_type = "slack"
//	  webhook_url  = var.slack_webhook_url
//	  routing_key  = var.pagerduty_routing_key  # silently ignored!
//	}
//
// would have `routing_key` quietly dropped at `buildConfig` time (it is
// never read on the slack branch of the switch), defeating their explicit
// intent. This is the same class of silent-data-loss bug the typed `auth`
// refactor closed for monitor authentication. We close it here at the
// schema layer instead of the marshalling layer because alert_channel
// already has typed top-level attributes — we just need plan-time
// enforcement that the right ones are set.
//
// Two assertions per channel type
// ───────────────────────────────
//
//  1. Required attributes for the chosen `channel_type` MUST be set
//     (non-null, non-unknown). Surfaced as a "Missing required attribute"
//     error on the offending attribute path.
//  2. Attributes that don't belong to the chosen `channel_type` MUST NOT
//     be set. Surfaced as a "Unsupported attribute for channel type"
//     error on the offending attribute path. The matrix below comes
//     directly from the generated `*ChannelConfig` structs in
//     `internal/generated/types.go`; if a new attribute lands in the
//     spec, the test `TestAlertChannelValidateConfig_MatrixCovers...`
//     forces an update here.
//
// Unknown values
// ──────────────
//
// `IsUnknown()` values come from `for_each`, `count`, computed
// references, etc. — we cannot tell at plan time whether they will
// resolve to null or a value. To stay HCL-idiomatic (no false
// positives on dynamic configurations) we treat unknown values as
// "may or may not be set" and skip the forbidden-field check for
// them; the required-field check still fires (an unknown is
// considered "set" for the required side, since the value flowing
// through to apply will be a real string).

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// alertChannelFieldShape declares which schema attributes are required
// vs forbidden for one channel_type discriminator value. Attribute
// labels here MUST match the `tfsdk` tag on `AlertChannelResourceModel`.
type alertChannelFieldShape struct {
	required  []string // must be set when channel_type matches
	optional  []string // may be set when channel_type matches
	forbidden []string // must NOT be set when channel_type matches
}

// alertChannelMatrix mirrors the generated `*ChannelConfig` structs
// in `internal/generated/types.go`. Keep in sync — see the explanatory
// comment at the top of this file.
//
// Universe of variant attributes (every key referenced below MUST appear
// in this list exactly once across required+optional+forbidden for every
// channel type, enforced by `TestAlertChannelMatrixIsExhaustive`):
//
//	webhook_url, mention_text, mention_role_id, recipients,
//	routing_key, severity_override, api_key, region,
//	url, custom_headers, signing_secret.
var alertChannelMatrix = map[string]alertChannelFieldShape{
	"slack": {
		required:  []string{"webhook_url"},
		optional:  []string{"mention_text"},
		forbidden: []string{"mention_role_id", "recipients", "routing_key", "severity_override", "api_key", "region", "url", "custom_headers", "signing_secret"},
	},
	"discord": {
		required:  []string{"webhook_url"},
		optional:  []string{"mention_role_id"},
		forbidden: []string{"mention_text", "recipients", "routing_key", "severity_override", "api_key", "region", "url", "custom_headers", "signing_secret"},
	},
	"teams": {
		required:  []string{"webhook_url"},
		optional:  []string{},
		forbidden: []string{"mention_text", "mention_role_id", "recipients", "routing_key", "severity_override", "api_key", "region", "url", "custom_headers", "signing_secret"},
	},
	"email": {
		required:  []string{"recipients"},
		optional:  []string{},
		forbidden: []string{"webhook_url", "mention_text", "mention_role_id", "routing_key", "severity_override", "api_key", "region", "url", "custom_headers", "signing_secret"},
	},
	"pagerduty": {
		required:  []string{"routing_key"},
		optional:  []string{"severity_override"},
		forbidden: []string{"webhook_url", "mention_text", "mention_role_id", "recipients", "api_key", "region", "url", "custom_headers", "signing_secret"},
	},
	"opsgenie": {
		required:  []string{"api_key"},
		optional:  []string{"region"},
		forbidden: []string{"webhook_url", "mention_text", "mention_role_id", "recipients", "routing_key", "severity_override", "url", "custom_headers", "signing_secret"},
	},
	"webhook": {
		required:  []string{"url"},
		optional:  []string{"custom_headers", "signing_secret"},
		forbidden: []string{"webhook_url", "mention_text", "mention_role_id", "recipients", "routing_key", "severity_override", "api_key", "region"},
	},
}

// alertChannelAllVariantFields enumerates every variant attribute the
// matrix mentions. Used both by the audit test (universe coverage) and
// by `attrSetByName` (constant-time name → attr.Value lookup).
var alertChannelAllVariantFields = []string{
	"webhook_url",
	"mention_text",
	"mention_role_id",
	"recipients",
	"routing_key",
	"severity_override",
	"api_key",
	"region",
	"url",
	"custom_headers",
	"signing_secret",
}

// attrSetByName returns the `attr.Value` for one of the variant
// attributes by its `tfsdk` tag. Centralised so the validator and the
// tests share one source of truth for the model→name mapping.
func (m *AlertChannelResourceModel) attrSetByName(name string) attr.Value {
	switch name {
	case "webhook_url":
		return m.WebhookURL
	case "mention_text":
		return m.MentionText
	case "mention_role_id":
		return m.MentionRoleID
	case "recipients":
		return m.Recipients
	case "routing_key":
		return m.RoutingKey
	case "severity_override":
		return m.SeverityOverride
	case "api_key":
		return m.APIKey
	case "region":
		return m.Region
	case "url":
		return m.URL
	case "custom_headers":
		return m.CustomHeaders
	case "signing_secret":
		return m.SigningSecret
	default:
		return nil
	}
}

// ValidateConfig implements `resource.ResourceWithValidateConfig`. It
// delegates the actual matrix enforcement to `validateAlertChannelModel`
// so the same logic is unit-testable without constructing a full
// `tfsdk.Config` value.
func (r *AlertChannelResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg AlertChannelResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	validateAlertChannelModel(&cfg, &resp.Diagnostics)
}

// validateAlertChannelModel enforces the per-channel-type field shape
// matrix in `alertChannelMatrix`. Surfaces:
//
//   - "Missing required attribute" on the required attribute's path
//     when the chosen `channel_type` requires it but the model has
//     a null value.
//   - "Unsupported attribute for channel type" on the forbidden
//     attribute's path when the chosen `channel_type` does not use
//     it but the model has a non-null/non-unknown value.
//
// Bails out early for null/unknown discriminators (the OneOf validator
// on `channel_type` is the canonical error site for those cases) and
// for unknown discriminator values (oneOf would already have errored).
func validateAlertChannelModel(cfg *AlertChannelResourceModel, diags *diag.Diagnostics) {
	if cfg.ChannelType.IsNull() || cfg.ChannelType.IsUnknown() {
		return
	}
	channelType := cfg.ChannelType.ValueString()
	shape, ok := alertChannelMatrix[channelType]
	if !ok {
		return
	}

	for _, name := range shape.required {
		v := cfg.attrSetByName(name)
		if v == nil || v.IsNull() {
			diags.AddAttributeError(
				path.Root(name),
				"Missing required attribute for channel type",
				"`"+name+"` is required when `channel_type = \""+channelType+"\"` "+
					"(maps to the wire-format required field on the corresponding "+
					"*ChannelConfig struct).",
			)
		}
	}

	for _, name := range shape.forbidden {
		v := cfg.attrSetByName(name)
		if v == nil {
			continue
		}
		// Skip unknown values: a `for_each`-bound or computed
		// reference may resolve to null at apply time, in which
		// case it is genuinely unset and not a forbidden value.
		// We don't want plan-time false positives on dynamic
		// configurations.
		if v.IsUnknown() {
			continue
		}
		if v.IsNull() {
			continue
		}
		// Treat explicitly-empty collections (`recipients = []`,
		// `custom_headers = {}`) as still-set: bypassing the
		// check via empty literals would only delay the API's
		// "unexpected field" rejection to apply time. Better to
		// fail at plan time with a clear path.
		diags.AddAttributeError(
			path.Root(name),
			"Unsupported attribute for channel type",
			"`"+name+"` is not used by `channel_type = \""+channelType+"\"` "+
				"and would be silently dropped when the request is built. "+
				"Remove it, or set `channel_type` to a kind that uses this "+
				"attribute. See the per-type field shape matrix in "+
				"`alert_channel_validate.go`.",
		)
	}
}

// alertChannelKnownDiscriminators returns the discriminator values
// covered by the matrix. Exposed for the audit test to cross-check
// against `api.AlertChannelTypes`.
func alertChannelKnownDiscriminators() []string {
	out := make([]string, 0, len(alertChannelMatrix))
	for k := range alertChannelMatrix {
		out = append(out, k)
	}
	return out
}
