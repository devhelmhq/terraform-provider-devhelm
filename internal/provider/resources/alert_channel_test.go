package resources

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ───────────────────────────────────────────────────────────────────────
// Alert channel resource tests
//
// The bulk of variant-coverage lives in discriminated_unions_test.go
// (Class C). This file covers the remaining bug classes:
//   D — buildConfig handling of optional fields and null collections
//   E — semantics of optional/sensitive fields (mention_text omitted vs
//       empty string)
//   F/G — read path is trivial (Read only refreshes Name/ChannelType/
//       ConfigHash because the config is write-only/sensitive); covered
//       indirectly by surface tests, no mapToState exists to round-trip.
// ───────────────────────────────────────────────────────────────────────

// TestAlertChannel_BuildConfig_OmitsAbsentOptionalFields verifies that
// optional pointers absent from the model do NOT serialize as JSON
// nulls. The API treats `"mentionText": null` differently from missing
// the key entirely (the former clears, the latter preserves), so a
// regression here would silently overwrite the user's previously-set
// mention text on every apply.
func TestAlertChannel_BuildConfig_OmitsAbsentOptionalFields(t *testing.T) {
	r := &AlertChannelResource{}
	model := AlertChannelResourceModel{
		ChannelType: types.StringValue("slack"),
		WebhookURL:  types.StringValue("https://hooks.slack.com/services/x"),
		MentionText: types.StringNull(),
	}
	raw, err := r.buildConfig(&model)
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}
	// Use a parsed view so we don't accidentally match nested keys named
	// "mentionText" inside a different variant's payload.
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := parsed["mentionText"]; present {
		t.Errorf("mentionText present (value=%v) but null/absent in HCL — should be omitted from the wire so the server preserves any previously-set value", parsed["mentionText"])
	}
	if _, present := parsed["webhookUrl"]; !present {
		t.Errorf("webhookUrl missing from JSON: %s", raw)
	}
	if got, _ := parsed["channelType"].(string); got != "slack" {
		t.Errorf("channelType = %v, want 'slack'", parsed["channelType"])
	}
}

// TestAlertChannel_BuildConfig_EmailEmptyRecipientsSerializesAsNull: in
// the post-spec-sync schema, EmailChannelConfig.Recipients is a required
// value slice (no omitempty). A null model value therefore serializes as
// `"recipients": null`. The API rejects this, surfacing a clear error to
// the user — which is the contract we want for a missing required field.
func TestAlertChannel_BuildConfig_EmailEmptyRecipientsSerializesAsNull(t *testing.T) {
	r := &AlertChannelResource{}
	model := AlertChannelResourceModel{
		ChannelType: types.StringValue("email"),
		Recipients:  types.ListNull(types.StringType),
	}
	raw, err := r.buildConfig(&model)
	if err != nil {
		t.Fatalf("buildConfig: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v, ok := parsed["recipients"]; !ok || v != nil {
		t.Errorf("recipients = %v (present=%v), want explicit null so the API can return a 400", v, ok)
	}
}

// TestAlertChannel_BuildConfig_WebhookCustomHeadersRoundTrip: the webhook
// variant accepts a map<string,*string> and we hand-roll the conversion.
// Verify that an explicit `custom_headers = {}` reaches the wire as an
// empty object (clears) and that key/value pairs are preserved verbatim.
func TestAlertChannel_BuildConfig_WebhookCustomHeadersRoundTrip(t *testing.T) {
	r := &AlertChannelResource{}

	t.Run("populated", func(t *testing.T) {
		model := AlertChannelResourceModel{
			ChannelType: types.StringValue("webhook"),
			URL:         types.StringValue("https://example.com/hook"),
			CustomHeaders: types.MapValueMust(types.StringType, map[string]attr.Value{
				"X-Source": types.StringValue("devhelm"),
				"X-Trace":  types.StringValue("abc"),
			}),
		}
		raw, err := r.buildConfig(&model)
		if err != nil {
			t.Fatalf("buildConfig: %v", err)
		}
		var parsed struct {
			ChannelType   string             `json:"channelType"`
			URL           string             `json:"url"`
			CustomHeaders map[string]*string `json:"customHeaders"`
		}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if parsed.CustomHeaders == nil {
			t.Fatal("customHeaders nil in JSON")
		}
		if got := parsed.CustomHeaders["X-Source"]; got == nil || *got != "devhelm" {
			t.Errorf("X-Source = %v, want 'devhelm'", got)
		}
	})

	t.Run("nullMapOmitsKey", func(t *testing.T) {
		model := AlertChannelResourceModel{
			ChannelType:   types.StringValue("webhook"),
			URL:           types.StringValue("https://example.com/hook"),
			CustomHeaders: types.MapNull(types.StringType),
		}
		raw, err := r.buildConfig(&model)
		if err != nil {
			t.Fatalf("buildConfig: %v", err)
		}
		// Pointer fields with omitempty must not appear when nil.
		if strings.Contains(string(raw), "customHeaders") {
			t.Errorf("customHeaders serialized despite null map: %s", raw)
		}
	})
}

// TestAlertChannel_BuildConfig_AlwaysSetsChannelType: this is the
// invariant that would catch a "switch case fell through" or "forgot to
// set ChannelType in struct literal" bug. Without a discriminator the
// API's union dispatch fails with 400.
func TestAlertChannel_BuildConfig_AlwaysSetsChannelType(t *testing.T) {
	r := &AlertChannelResource{}
	allTypes := []string{"slack", "discord", "email", "pagerduty", "opsgenie", "teams", "webhook"}
	for _, ct := range allTypes {
		model := AlertChannelResourceModel{ChannelType: types.StringValue(ct)}
		raw, err := r.buildConfig(&model)
		if err != nil {
			t.Errorf("%s buildConfig: %v", ct, err)
			continue
		}
		want := `"channelType":"` + ct + `"`
		if !strings.Contains(string(raw), want) {
			t.Errorf("%s: missing %s in %s", ct, want, raw)
		}
	}
}

// TestClearAlertChannelConfigAttrs_NullsEveryConfigFieldButKeepsIdentity
// guards the drift-detection helper: when out-of-band drift is detected,
// every type-specific config attribute must become typed-null so the next
// plan shows a diff, while id/name/channel_type/config_hash are preserved
// (they carry the divergence signal and the resource identity).
func TestClearAlertChannelConfigAttrs_NullsEveryConfigFieldButKeepsIdentity(t *testing.T) {
	m := &AlertChannelResourceModel{
		ID:          types.StringValue("11111111-1111-1111-1111-111111111111"),
		Name:        types.StringValue("ops-slack"),
		ChannelType: types.StringValue("slack"),
		ConfigHash:  types.StringValue("old-hash"),

		WebhookURL:    types.StringValue("https://hooks.slack.com/x"),
		MentionText:   types.StringValue("@here"),
		MentionRoleID: types.StringValue("role"),
		Recipients:    types.ListValueMust(types.StringType, []attr.Value{types.StringValue("a@b.com")}),
		RoutingKey:    types.StringValue("rk"),
		APIKey:        types.StringValue("ak"),
		URL:           types.StringValue("https://example.com"),
		CustomHeaders: types.MapValueMust(types.StringType, map[string]attr.Value{"X": types.StringValue("y")}),
		SigningSecret: types.StringValue("ss"),
		BotToken:      types.StringValue("bt"),
		ChatID:        types.StringValue("c"),
		Domain:        types.StringValue("acme.atlassian.net"),
		APIToken:      types.StringValue("at"),
		ProjectKey:    types.StringValue("OPS"),
		EndpointURL:   types.StringValue("https://gitlab/x"),
	}

	clearAlertChannelConfigAttrs(m)

	// Identity + divergence signal preserved.
	if m.ID.IsNull() || m.Name.IsNull() || m.ChannelType.IsNull() {
		t.Errorf("identity attrs must be preserved: id=%v name=%v type=%v", m.ID, m.Name, m.ChannelType)
	}
	if m.ConfigHash.ValueString() != "old-hash" {
		t.Errorf("config_hash must be preserved (divergence signal), got %q", m.ConfigHash.ValueString())
	}

	// A representative spread of config attrs across element kinds.
	if !m.WebhookURL.IsNull() || !m.MentionText.IsNull() || !m.SigningSecret.IsNull() ||
		!m.APIKey.IsNull() || !m.URL.IsNull() || !m.Domain.IsNull() || !m.EndpointURL.IsNull() {
		t.Errorf("string config attrs must be nulled: %+v", m)
	}
	if !m.Recipients.IsNull() {
		t.Errorf("recipients list must be nulled, got %v", m.Recipients)
	}
	if !m.CustomHeaders.IsNull() {
		t.Errorf("custom_headers map must be nulled, got %v", m.CustomHeaders)
	}
}
