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
		forbidden: []string{"mention_role_id", "recipients", "routing_key", "severity_override", "api_key", "region", "url", "custom_headers", "signing_secret", "bot_token", "chat_id", "user_key", "app_token", "priority", "sound", "channel", "icon_url", "access_token", "device_iden", "team_id", "label_id", "severity_id", "visibility", "severity", "site", "tags", "domain", "email", "api_token", "project_key", "issue_type", "endpoint_url", "authorization_key"},
	},
	"discord": {
		required:  []string{"webhook_url"},
		optional:  []string{"mention_role_id"},
		forbidden: []string{"mention_text", "recipients", "routing_key", "severity_override", "api_key", "region", "url", "custom_headers", "signing_secret", "bot_token", "chat_id", "user_key", "app_token", "priority", "sound", "channel", "icon_url", "access_token", "device_iden", "team_id", "label_id", "severity_id", "visibility", "severity", "site", "tags", "domain", "email", "api_token", "project_key", "issue_type", "endpoint_url", "authorization_key"},
	},
	"teams": {
		required:  []string{"webhook_url"},
		optional:  []string{},
		forbidden: []string{"mention_text", "mention_role_id", "recipients", "routing_key", "severity_override", "api_key", "region", "url", "custom_headers", "signing_secret", "bot_token", "chat_id", "user_key", "app_token", "priority", "sound", "channel", "icon_url", "access_token", "device_iden", "team_id", "label_id", "severity_id", "visibility", "severity", "site", "tags", "domain", "email", "api_token", "project_key", "issue_type", "endpoint_url", "authorization_key"},
	},
	"email": {
		required:  []string{"recipients"},
		optional:  []string{},
		forbidden: []string{"webhook_url", "mention_text", "mention_role_id", "routing_key", "severity_override", "api_key", "region", "url", "custom_headers", "signing_secret", "bot_token", "chat_id", "user_key", "app_token", "priority", "sound", "channel", "icon_url", "access_token", "device_iden", "team_id", "label_id", "severity_id", "visibility", "severity", "site", "tags", "domain", "email", "api_token", "project_key", "issue_type", "endpoint_url", "authorization_key"},
	},
	"pagerduty": {
		required:  []string{"routing_key"},
		optional:  []string{"severity_override"},
		forbidden: []string{"webhook_url", "mention_text", "mention_role_id", "recipients", "api_key", "region", "url", "custom_headers", "signing_secret", "bot_token", "chat_id", "user_key", "app_token", "priority", "sound", "channel", "icon_url", "access_token", "device_iden", "team_id", "label_id", "severity_id", "visibility", "severity", "site", "tags", "domain", "email", "api_token", "project_key", "issue_type", "endpoint_url", "authorization_key"},
	},
	"opsgenie": {
		required:  []string{"api_key"},
		optional:  []string{"region"},
		forbidden: []string{"webhook_url", "mention_text", "mention_role_id", "recipients", "routing_key", "severity_override", "url", "custom_headers", "signing_secret", "bot_token", "chat_id", "user_key", "app_token", "priority", "sound", "channel", "icon_url", "access_token", "device_iden", "team_id", "label_id", "severity_id", "visibility", "severity", "site", "tags", "domain", "email", "api_token", "project_key", "issue_type", "endpoint_url", "authorization_key"},
	},
	"webhook": {
		required:  []string{"url"},
		optional:  []string{"custom_headers", "signing_secret"},
		forbidden: []string{"webhook_url", "mention_text", "mention_role_id", "recipients", "routing_key", "severity_override", "api_key", "region", "bot_token", "chat_id", "user_key", "app_token", "priority", "sound", "channel", "icon_url", "access_token", "device_iden", "team_id", "label_id", "severity_id", "visibility", "severity", "site", "tags", "domain", "email", "api_token", "project_key", "issue_type", "endpoint_url", "authorization_key"},
	},
	"telegram": {
		required:  []string{"bot_token", "chat_id"},
		optional:  []string{},
		forbidden: []string{"webhook_url", "mention_text", "mention_role_id", "recipients", "routing_key", "severity_override", "api_key", "region", "url", "custom_headers", "signing_secret", "user_key", "app_token", "priority", "sound", "channel", "icon_url", "access_token", "device_iden", "team_id", "label_id", "severity_id", "visibility", "severity", "site", "tags", "domain", "email", "api_token", "project_key", "issue_type", "endpoint_url", "authorization_key"},
	},
	"google_chat": {
		required:  []string{"webhook_url"},
		optional:  []string{},
		forbidden: []string{"mention_text", "mention_role_id", "recipients", "routing_key", "severity_override", "api_key", "region", "url", "custom_headers", "signing_secret", "bot_token", "chat_id", "user_key", "app_token", "priority", "sound", "channel", "icon_url", "access_token", "device_iden", "team_id", "label_id", "severity_id", "visibility", "severity", "site", "tags", "domain", "email", "api_token", "project_key", "issue_type", "endpoint_url", "authorization_key"},
	},
	"pushover": {
		required:  []string{"user_key", "app_token"},
		optional:  []string{"priority", "sound"},
		forbidden: []string{"webhook_url", "mention_text", "mention_role_id", "recipients", "routing_key", "severity_override", "api_key", "region", "url", "custom_headers", "signing_secret", "bot_token", "chat_id", "channel", "icon_url", "access_token", "device_iden", "team_id", "label_id", "severity_id", "visibility", "severity", "site", "tags", "domain", "email", "api_token", "project_key", "issue_type", "endpoint_url", "authorization_key"},
	},
	"mattermost": {
		required:  []string{"webhook_url"},
		optional:  []string{"channel", "icon_url"},
		forbidden: []string{"mention_text", "mention_role_id", "recipients", "routing_key", "severity_override", "api_key", "region", "url", "custom_headers", "signing_secret", "bot_token", "chat_id", "user_key", "app_token", "priority", "sound", "access_token", "device_iden", "team_id", "label_id", "severity_id", "visibility", "severity", "site", "tags", "domain", "email", "api_token", "project_key", "issue_type", "endpoint_url", "authorization_key"},
	},
	"splunk_oncall": {
		required:  []string{"api_key", "routing_key"},
		optional:  []string{},
		forbidden: []string{"webhook_url", "mention_text", "mention_role_id", "recipients", "severity_override", "region", "url", "custom_headers", "signing_secret", "bot_token", "chat_id", "user_key", "app_token", "priority", "sound", "channel", "icon_url", "access_token", "device_iden", "team_id", "label_id", "severity_id", "visibility", "severity", "site", "tags", "domain", "email", "api_token", "project_key", "issue_type", "endpoint_url", "authorization_key"},
	},
	"pushbullet": {
		required:  []string{"access_token"},
		optional:  []string{"device_iden"},
		forbidden: []string{"webhook_url", "mention_text", "mention_role_id", "recipients", "routing_key", "severity_override", "api_key", "region", "url", "custom_headers", "signing_secret", "bot_token", "chat_id", "user_key", "app_token", "priority", "sound", "channel", "icon_url", "team_id", "label_id", "severity_id", "visibility", "severity", "site", "tags", "domain", "email", "api_token", "project_key", "issue_type", "endpoint_url", "authorization_key"},
	},
	"linear": {
		required:  []string{"api_key", "team_id"},
		optional:  []string{"label_id"},
		forbidden: []string{"webhook_url", "mention_text", "mention_role_id", "recipients", "routing_key", "severity_override", "region", "url", "custom_headers", "signing_secret", "bot_token", "chat_id", "user_key", "app_token", "priority", "sound", "channel", "icon_url", "access_token", "device_iden", "severity_id", "visibility", "severity", "site", "tags", "domain", "email", "api_token", "project_key", "issue_type", "endpoint_url", "authorization_key"},
	},
	"incident_io": {
		required:  []string{"api_key"},
		optional:  []string{"severity_id", "visibility"},
		forbidden: []string{"webhook_url", "mention_text", "mention_role_id", "recipients", "routing_key", "severity_override", "region", "url", "custom_headers", "signing_secret", "bot_token", "chat_id", "user_key", "app_token", "priority", "sound", "channel", "icon_url", "access_token", "device_iden", "team_id", "label_id", "severity", "site", "tags", "domain", "email", "api_token", "project_key", "issue_type", "endpoint_url", "authorization_key"},
	},
	"rootly": {
		required:  []string{"api_key"},
		optional:  []string{"severity"},
		forbidden: []string{"webhook_url", "mention_text", "mention_role_id", "recipients", "routing_key", "severity_override", "region", "url", "custom_headers", "signing_secret", "bot_token", "chat_id", "user_key", "app_token", "priority", "sound", "channel", "icon_url", "access_token", "device_iden", "team_id", "label_id", "severity_id", "visibility", "site", "tags", "domain", "email", "api_token", "project_key", "issue_type", "endpoint_url", "authorization_key"},
	},
	"zapier": {
		required:  []string{"webhook_url"},
		optional:  []string{},
		forbidden: []string{"mention_text", "mention_role_id", "recipients", "routing_key", "severity_override", "api_key", "region", "url", "custom_headers", "signing_secret", "bot_token", "chat_id", "user_key", "app_token", "priority", "sound", "channel", "icon_url", "access_token", "device_iden", "team_id", "label_id", "severity_id", "visibility", "severity", "site", "tags", "domain", "email", "api_token", "project_key", "issue_type", "endpoint_url", "authorization_key"},
	},
	"datadog": {
		required:  []string{"api_key"},
		optional:  []string{"site", "tags"},
		forbidden: []string{"webhook_url", "mention_text", "mention_role_id", "recipients", "routing_key", "severity_override", "region", "url", "custom_headers", "signing_secret", "bot_token", "chat_id", "user_key", "app_token", "priority", "sound", "channel", "icon_url", "access_token", "device_iden", "team_id", "label_id", "severity_id", "visibility", "severity", "domain", "email", "api_token", "project_key", "issue_type", "endpoint_url", "authorization_key"},
	},
	"jira": {
		required:  []string{"domain", "email", "api_token", "project_key"},
		optional:  []string{"issue_type"},
		forbidden: []string{"webhook_url", "mention_text", "mention_role_id", "recipients", "routing_key", "severity_override", "api_key", "region", "url", "custom_headers", "signing_secret", "bot_token", "chat_id", "user_key", "app_token", "priority", "sound", "channel", "icon_url", "access_token", "device_iden", "team_id", "label_id", "severity_id", "visibility", "severity", "site", "tags", "endpoint_url", "authorization_key"},
	},
	"gitlab": {
		required:  []string{"endpoint_url", "authorization_key"},
		optional:  []string{},
		forbidden: []string{"webhook_url", "mention_text", "mention_role_id", "recipients", "routing_key", "severity_override", "api_key", "region", "url", "custom_headers", "signing_secret", "bot_token", "chat_id", "user_key", "app_token", "priority", "sound", "channel", "icon_url", "access_token", "device_iden", "team_id", "label_id", "severity_id", "visibility", "severity", "site", "tags", "domain", "email", "api_token", "project_key", "issue_type"},
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
	"bot_token",
	"chat_id",
	"user_key",
	"app_token",
	"priority",
	"sound",
	"channel",
	"icon_url",
	"access_token",
	"device_iden",
	"team_id",
	"label_id",
	"severity_id",
	"visibility",
	"severity",
	"site",
	"tags",
	"domain",
	"email",
	"api_token",
	"project_key",
	"issue_type",
	"endpoint_url",
	"authorization_key",
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
	case "bot_token":
		return m.BotToken
	case "chat_id":
		return m.ChatID
	case "user_key":
		return m.UserKey
	case "app_token":
		return m.AppToken
	case "priority":
		return m.Priority
	case "sound":
		return m.Sound
	case "channel":
		return m.Channel
	case "icon_url":
		return m.IconURL
	case "access_token":
		return m.AccessToken
	case "device_iden":
		return m.DeviceIden
	case "team_id":
		return m.TeamID
	case "label_id":
		return m.LabelID
	case "severity_id":
		return m.SeverityID
	case "visibility":
		return m.Visibility
	case "severity":
		return m.Severity
	case "site":
		return m.Site
	case "tags":
		return m.Tags
	case "domain":
		return m.Domain
	case "email":
		return m.Email
	case "api_token":
		return m.APIToken
	case "project_key":
		return m.ProjectKey
	case "issue_type":
		return m.IssueType
	case "endpoint_url":
		return m.EndpointURL
	case "authorization_key":
		return m.AuthorizationKey
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
