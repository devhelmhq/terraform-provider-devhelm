package resources

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ───────────────────────────────────────────────────────────────────────
// Matrix self-audit
// ───────────────────────────────────────────────────────────────────────

// TestAlertChannelMatrixCoversEveryDiscriminator asserts that
// `alertChannelMatrix` enumerates every wire-format channel type the
// generated package knows about. New spec values land in
// `api.AlertChannelTypes` automatically (via `TestEnumSliceCoverage`),
// and this test forces the matrix to keep up.
func TestAlertChannelMatrixCoversEveryDiscriminator(t *testing.T) {
	matrixKeys := alertChannelKnownDiscriminators()
	sort.Strings(matrixKeys)

	known := append([]string(nil), api.AlertChannelTypes...)
	sort.Strings(known)

	if len(matrixKeys) != len(known) {
		t.Fatalf(
			"alertChannelMatrix has %d discriminators, api.AlertChannelTypes has %d. "+
				"Matrix: %v\nKnown: %v",
			len(matrixKeys), len(known), matrixKeys, known,
		)
	}
	for i := range matrixKeys {
		if matrixKeys[i] != known[i] {
			t.Errorf("discriminator drift at index %d: matrix=%q, known=%q", i, matrixKeys[i], known[i])
		}
	}
}

// TestAlertChannelMatrixIsExhaustive asserts that every entry in
// `alertChannelMatrix` partitions `alertChannelAllVariantFields`
// into required ∪ optional ∪ forbidden with no overlap and no
// missing field.
func TestAlertChannelMatrixIsExhaustive(t *testing.T) {
	universe := map[string]struct{}{}
	for _, name := range alertChannelAllVariantFields {
		universe[name] = struct{}{}
	}
	for channelType, shape := range alertChannelMatrix {
		seen := map[string]string{}
		check := func(bucket string, names []string) {
			for _, n := range names {
				if _, ok := universe[n]; !ok {
					t.Errorf("%s: bucket %s lists %q which is not in alertChannelAllVariantFields", channelType, bucket, n)
				}
				if prev, dup := seen[n]; dup {
					t.Errorf("%s: %q appears in both %s and %s", channelType, n, prev, bucket)
				}
				seen[n] = bucket
			}
		}
		check("required", shape.required)
		check("optional", shape.optional)
		check("forbidden", shape.forbidden)

		for n := range universe {
			if _, ok := seen[n]; !ok {
				t.Errorf("%s: %q is in alertChannelAllVariantFields but missing from required/optional/forbidden", channelType, n)
			}
		}
	}
}

// TestAlertChannelMatrixMatchesGeneratedFieldShape cross-checks the
// matrix against the generated `*ChannelConfig` struct fields. The
// generated structs are the canonical wire contract; if the matrix
// disagrees, either the spec changed (matrix needs updating) or the
// matrix has a typo. We intentionally don't try to derive the matrix
// from the generated structs at runtime — the required/optional split
// in the matrix is a richer signal than the JSON `omitempty` tag
// (e.g. for `recipients` the wire field is always required even when
// the spec marks it as a slice that could be empty).
//
// This test only spot-checks that every field the matrix marks as
// required for a channel type IS a non-pointer field on the
// corresponding `*ChannelConfig` struct, and every field the matrix
// marks as optional IS a pointer field. The TestEnumSliceCoverage in
// the api package provides the orthogonal guarantee that no channel
// type went missing.
func TestAlertChannelMatrixMatchesGeneratedFieldShape(t *testing.T) {
	// Mapping from matrix attr name → wire JSON field name on the
	// corresponding generated struct. (Same key in every struct;
	// only the struct varies by channel_type.)
	jsonFieldByAttr := map[string]string{
		"webhook_url":       "webhookUrl",
		"mention_text":      "mentionText",
		"mention_role_id":   "mentionRoleId",
		"recipients":        "recipients",
		"routing_key":       "routingKey",
		"severity_override": "severityOverride",
		"api_key":           "apiKey",
		"region":            "region",
		"url":               "url",
		"custom_headers":    "customHeaders",
		"signing_secret":    "signingSecret",
	}

	cases := []struct {
		channelType string
		zero        any // a zero-value of the corresponding *ChannelConfig struct
	}{
		{"slack", generated.SlackChannelConfig{}},
		{"discord", generated.DiscordChannelConfig{}},
		{"teams", generated.TeamsChannelConfig{}},
		{"email", generated.EmailChannelConfig{}},
		{"pagerduty", generated.PagerDutyChannelConfig{}},
		{"opsgenie", generated.OpsGenieChannelConfig{}},
		{"webhook", generated.WebhookChannelConfig{}},
	}

	for _, c := range cases {
		c := c
		t.Run(c.channelType, func(t *testing.T) {
			shape := alertChannelMatrix[c.channelType]
			fields := jsonFieldsByName(c.zero)
			for _, attrName := range shape.required {
				jsonName, ok := jsonFieldByAttr[attrName]
				if !ok {
					t.Errorf("required attr %q has no jsonFieldByAttr mapping", attrName)
					continue
				}
				info, present := fields[jsonName]
				if !present {
					t.Errorf("matrix marks %s.%s required, but generated struct has no %q field", c.channelType, attrName, jsonName)
					continue
				}
				if info.isPointer {
					t.Errorf("matrix marks %s.%s required, but generated struct has it as *T (would be optional)", c.channelType, attrName)
				}
			}
			for _, attrName := range shape.optional {
				jsonName, ok := jsonFieldByAttr[attrName]
				if !ok {
					t.Errorf("optional attr %q has no jsonFieldByAttr mapping", attrName)
					continue
				}
				info, present := fields[jsonName]
				if !present {
					t.Errorf("matrix marks %s.%s optional, but generated struct has no %q field", c.channelType, attrName, jsonName)
					continue
				}
				if !info.isPointer {
					t.Errorf("matrix marks %s.%s optional, but generated struct has it as T (would be required)", c.channelType, attrName)
				}
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// Validator behaviour
// ───────────────────────────────────────────────────────────────────────

func TestValidateAlertChannelModel_RequiredMissing(t *testing.T) {
	// Each row drops the required attribute for its channel type and
	// asserts the validator errors on the right path with the right
	// summary.
	cases := []struct {
		name         string
		model        *AlertChannelResourceModel
		expectedPath string
	}{
		{
			name: "slack_missing_webhook_url",
			model: &AlertChannelResourceModel{
				ChannelType:   types.StringValue("slack"),
				WebhookURL:    types.StringNull(),
				MentionText:   types.StringNull(),
				MentionRoleID: types.StringNull(),
				Recipients:    types.ListNull(types.StringType),
				RoutingKey:    types.StringNull(),
				APIKey:        types.StringNull(),
				URL:           types.StringNull(),
				CustomHeaders: types.MapNull(types.StringType),
			},
			expectedPath: "webhook_url",
		},
		{
			name: "email_missing_recipients",
			model: &AlertChannelResourceModel{
				ChannelType: types.StringValue("email"),
				Recipients:  types.ListNull(types.StringType),
			},
			expectedPath: "recipients",
		},
		{
			name: "pagerduty_missing_routing_key",
			model: &AlertChannelResourceModel{
				ChannelType: types.StringValue("pagerduty"),
				RoutingKey:  types.StringNull(),
			},
			expectedPath: "routing_key",
		},
		{
			name: "opsgenie_missing_api_key",
			model: &AlertChannelResourceModel{
				ChannelType: types.StringValue("opsgenie"),
				APIKey:      types.StringNull(),
			},
			expectedPath: "api_key",
		},
		{
			name: "webhook_missing_url",
			model: &AlertChannelResourceModel{
				ChannelType: types.StringValue("webhook"),
				URL:         types.StringNull(),
			},
			expectedPath: "url",
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			diags := diag.Diagnostics{}
			validateAlertChannelModel(c.model, &diags)
			assertHasAttributeError(t, diags, c.expectedPath, "Missing required attribute")
		})
	}
}

func TestValidateAlertChannelModel_ForbiddenSet(t *testing.T) {
	cases := []struct {
		name         string
		model        *AlertChannelResourceModel
		expectedPath string
	}{
		{
			name: "slack_with_routing_key",
			model: &AlertChannelResourceModel{
				ChannelType: types.StringValue("slack"),
				WebhookURL:  types.StringValue("https://hooks.slack.com/services/x/y/z"),
				RoutingKey:  types.StringValue("pd-key-leaked-into-slack"),
			},
			expectedPath: "routing_key",
		},
		{
			name: "email_with_webhook_url",
			model: &AlertChannelResourceModel{
				ChannelType: types.StringValue("email"),
				Recipients: types.ListValueMust(types.StringType, []attr.Value{
					types.StringValue("ops@example.com"),
				}),
				WebhookURL: types.StringValue("https://example.com/hook"),
			},
			expectedPath: "webhook_url",
		},
		{
			name: "pagerduty_with_api_key",
			model: &AlertChannelResourceModel{
				ChannelType: types.StringValue("pagerduty"),
				RoutingKey:  types.StringValue("pd-routing-key"),
				APIKey:      types.StringValue("opsgenie-api-key"),
			},
			expectedPath: "api_key",
		},
		{
			name: "webhook_with_recipients",
			model: &AlertChannelResourceModel{
				ChannelType: types.StringValue("webhook"),
				URL:         types.StringValue("https://example.com/hook"),
				Recipients: types.ListValueMust(types.StringType, []attr.Value{
					types.StringValue("ops@example.com"),
				}),
			},
			expectedPath: "recipients",
		},
		{
			name: "discord_with_mention_text",
			model: &AlertChannelResourceModel{
				ChannelType: types.StringValue("discord"),
				WebhookURL:  types.StringValue("https://discord.com/api/webhooks/1/x"),
				MentionText: types.StringValue("@channel"),
			},
			expectedPath: "mention_text",
		},
		{
			name: "teams_with_mention_role_id",
			model: &AlertChannelResourceModel{
				ChannelType:   types.StringValue("teams"),
				WebhookURL:    types.StringValue("https://outlook.office.com/webhook/..."),
				MentionRoleID: types.StringValue("role-1"),
			},
			expectedPath: "mention_role_id",
		},
		{
			name: "opsgenie_with_severity_override",
			model: &AlertChannelResourceModel{
				ChannelType:      types.StringValue("opsgenie"),
				APIKey:           types.StringValue("og-key"),
				SeverityOverride: types.StringValue("critical"),
			},
			expectedPath: "severity_override",
		},
		{
			name: "slack_with_empty_list_for_recipients_still_errors",
			model: &AlertChannelResourceModel{
				ChannelType: types.StringValue("slack"),
				WebhookURL:  types.StringValue("https://hooks.slack.com/x"),
				Recipients:  types.ListValueMust(types.StringType, []attr.Value{}),
			},
			expectedPath: "recipients",
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			diags := diag.Diagnostics{}
			validateAlertChannelModel(c.model, &diags)
			assertHasAttributeError(t, diags, c.expectedPath, "Unsupported attribute for channel type")
		})
	}
}

func TestValidateAlertChannelModel_HappyPaths(t *testing.T) {
	cases := []struct {
		name  string
		model *AlertChannelResourceModel
	}{
		{
			name: "slack_minimal",
			model: &AlertChannelResourceModel{
				ChannelType: types.StringValue("slack"),
				WebhookURL:  types.StringValue("https://hooks.slack.com/services/x/y/z"),
			},
		},
		{
			name: "slack_with_optional_mention_text",
			model: &AlertChannelResourceModel{
				ChannelType: types.StringValue("slack"),
				WebhookURL:  types.StringValue("https://hooks.slack.com/services/x/y/z"),
				MentionText: types.StringValue("<!channel>"),
			},
		},
		{
			name: "email_minimal",
			model: &AlertChannelResourceModel{
				ChannelType: types.StringValue("email"),
				Recipients: types.ListValueMust(types.StringType, []attr.Value{
					types.StringValue("ops@example.com"),
				}),
			},
		},
		{
			name: "pagerduty_minimal",
			model: &AlertChannelResourceModel{
				ChannelType: types.StringValue("pagerduty"),
				RoutingKey:  types.StringValue("pd-key"),
			},
		},
		{
			name: "pagerduty_with_severity_override",
			model: &AlertChannelResourceModel{
				ChannelType:      types.StringValue("pagerduty"),
				RoutingKey:       types.StringValue("pd-key"),
				SeverityOverride: types.StringValue("critical"),
			},
		},
		{
			name: "opsgenie_with_region",
			model: &AlertChannelResourceModel{
				ChannelType: types.StringValue("opsgenie"),
				APIKey:      types.StringValue("og-key"),
				Region:      types.StringValue("eu"),
			},
		},
		{
			name: "webhook_with_custom_headers_and_signing_secret",
			model: &AlertChannelResourceModel{
				ChannelType: types.StringValue("webhook"),
				URL:         types.StringValue("https://example.com/hook"),
				CustomHeaders: types.MapValueMust(types.StringType, map[string]attr.Value{
					"X-Trace": types.StringValue("abc"),
				}),
				SigningSecret: types.StringValue("hmac-secret"),
			},
		},
		{
			name: "discord_with_mention_role_id",
			model: &AlertChannelResourceModel{
				ChannelType:   types.StringValue("discord"),
				WebhookURL:    types.StringValue("https://discord.com/api/webhooks/1/x"),
				MentionRoleID: types.StringValue("role-1"),
			},
		},
		{
			name: "teams_minimal",
			model: &AlertChannelResourceModel{
				ChannelType: types.StringValue("teams"),
				WebhookURL:  types.StringValue("https://outlook.office.com/webhook/..."),
			},
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			diags := diag.Diagnostics{}
			validateAlertChannelModel(c.model, &diags)
			if diags.HasError() {
				t.Errorf("expected no errors for happy path; got: %v", diagsToStrings(diags))
			}
		})
	}
}

func TestValidateAlertChannelModel_BailsOnNullOrUnknownDiscriminator(t *testing.T) {
	// Null discriminator → OneOf validator on the attribute itself
	// is the canonical error site, not us. Verify we don't double-report.
	t.Run("null_channel_type", func(t *testing.T) {
		diags := diag.Diagnostics{}
		validateAlertChannelModel(&AlertChannelResourceModel{
			ChannelType: types.StringNull(),
			RoutingKey:  types.StringValue("set-but-discriminator-is-null"),
		}, &diags)
		if diags.HasError() {
			t.Errorf("expected no errors for null channel_type; got: %v", diagsToStrings(diags))
		}
	})
	t.Run("unknown_channel_type", func(t *testing.T) {
		diags := diag.Diagnostics{}
		validateAlertChannelModel(&AlertChannelResourceModel{
			ChannelType: types.StringUnknown(),
			RoutingKey:  types.StringValue("set-but-discriminator-is-unknown"),
		}, &diags)
		if diags.HasError() {
			t.Errorf("expected no errors for unknown channel_type; got: %v", diagsToStrings(diags))
		}
	})
}

func TestValidateAlertChannelModel_UnknownForbiddenAttributeIsTolerated(t *testing.T) {
	// `for_each`-bound or computed attributes resolve to Unknown at
	// validate time. We cannot tell whether they will be null or set
	// at apply, so we suppress the forbidden-field error to avoid
	// false positives on dynamic configurations.
	diags := diag.Diagnostics{}
	validateAlertChannelModel(&AlertChannelResourceModel{
		ChannelType: types.StringValue("slack"),
		WebhookURL:  types.StringValue("https://hooks.slack.com/services/x/y/z"),
		RoutingKey:  types.StringUnknown(),
	}, &diags)
	if diags.HasError() {
		t.Errorf("expected no errors when forbidden attribute is unknown; got: %v", diagsToStrings(diags))
	}
}

// ───────────────────────────────────────────────────────────────────────
// Helpers
// ───────────────────────────────────────────────────────────────────────

func assertHasAttributeError(t *testing.T, diags diag.Diagnostics, attrName, summarySubstring string) {
	t.Helper()
	if !diags.HasError() {
		t.Fatalf("expected an error on `%s`; got no errors", attrName)
	}
	want := path.Root(attrName)
	for _, d := range diags.Errors() {
		// `diag.DiagnosticWithPath` is implemented by all
		// `*AttributeErrorDiagnostic` constructors in the
		// framework (NewAttributeErrorDiagnostic et al.). The
		// `AddAttributeError` method on `diag.Diagnostics` always
		// produces one of these, so this assertion is exhaustive
		// for our validator's outputs.
		dwp, ok := d.(diag.DiagnosticWithPath)
		if !ok {
			continue
		}
		if dwp.Path().Equal(want) && strings.Contains(d.Summary(), summarySubstring) {
			return
		}
	}
	t.Errorf("no error matched path=%q summary~=%q. all diagnostics: %v",
		attrName, summarySubstring, diagsToStrings(diags))
}

// jsonFieldsByName reflects over a struct's exported fields and returns
// a map keyed by the JSON tag (stripped of `,omitempty` suffixes) →
// pointer-ness of the field type. Used by
// `TestAlertChannelMatrixMatchesGeneratedFieldShape` to cross-check
// the matrix against the generated wire-format structs.
type fieldShape struct {
	isPointer bool
}

func jsonFieldsByName(zero any) map[string]fieldShape {
	out := map[string]fieldShape{}
	rt := reflect.TypeOf(zero)
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name
		}
		out[name] = fieldShape{isPointer: f.Type.Kind() == reflect.Ptr}
	}
	return out
}

func diagsToStrings(diags diag.Diagnostics) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Severity().String()+": "+d.Summary()+" — "+d.Detail())
	}
	return out
}
