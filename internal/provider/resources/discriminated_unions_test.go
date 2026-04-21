package resources

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ───────────────────────────────────────────────────────────────────────
// Discriminated-union assembly tests
//
// All major resources negotiate a oneOf-shaped wire format with the API:
//   - monitor.config       (HTTP / DNS / TCP / ICMP / HEARTBEAT / MCP_SERVER)
//   - monitor.auth         (bearer / basic / header / api_key)
//   - monitor.assertions[] (status_code / response_time / header_value / …)
//   - alert_channel.config (slack / discord / email / pagerduty / opsgenie /
//                           teams / webhook)
//
// Each variant requires a `type` (or `channelType`) discriminator field; the
// API rejects bodies where the discriminator is missing or doesn't match a
// known variant. The provider builds these unions in three different ways:
//
//   1. The user supplies the JSON directly (monitor config, monitor auth)
//      → we just UnmarshalJSON into the union.
//   2. The provider merges a discriminator into a user-supplied config
//      (assertions: type from a sibling HCL attribute is injected into the
//      JSON blob).
//   3. The provider constructs the variant struct from typed HCL attributes
//      and marshals to JSON (alert_channel.config).
//
// Tests below cover each of those three flows for every documented variant.
// A regression here would silently surface as the API returning
// `400 Bad Request: unknown variant` at apply time.
// ───────────────────────────────────────────────────────────────────────

// ── Flow 1: Monitor auth (typed discriminated-union round-trip) ─────────
//
// The OpenAPI spec defines `MonitorAuthConfig` as a discriminated `oneOf`
// over BearerAuthConfig / BasicAuthConfig / HeaderAuthConfig /
// ApiKeyAuthConfig. The codegen emits proper From*/As* methods that
// preserve every variant field; the provider exposes this union as a
// nested `auth { bearer = {...} | basic = {...} | header = {...} |
// api_key = {...} }` block with exactly-one validation at plan time.
//
// Since the API never accepts raw secret material on the wire (credentials
// live in vault, referenced by `vault_secret_id`), the typed roundtrip is
// lossless and we no longer need a raw-JSON workaround. These tests pin
// down that:
//
//  1. buildMonitorAuthConfig produces the expected wire JSON for every
//     variant (discriminator + non-secret fields).
//  2. The wire JSON deserializes back through the same variant via
//     mapMonitorAuthToTF, with every field round-tripping byte-for-byte.

func TestMonitorAuth_TypedUnionRoundTripsEveryVariant(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name             string
		variantKey       string // "bearer" | "basic" | "header" | "api_key"
		headerName       string
		vaultSecretID    string
		wantWireContains []string
	}{
		{
			name:             "bearer",
			variantKey:       "bearer",
			vaultSecretID:    "00000000-0000-0000-0000-000000000001",
			wantWireContains: []string{`"type":"bearer"`, `"vaultSecretId":"00000000-0000-0000-0000-000000000001"`},
		},
		{
			name:             "basic",
			variantKey:       "basic",
			vaultSecretID:    "00000000-0000-0000-0000-000000000002",
			wantWireContains: []string{`"type":"basic"`, `"vaultSecretId":"00000000-0000-0000-0000-000000000002"`},
		},
		{
			name:             "header",
			variantKey:       "header",
			headerName:       "X-API",
			vaultSecretID:    "00000000-0000-0000-0000-000000000003",
			wantWireContains: []string{`"type":"header"`, `"headerName":"X-API"`, `"vaultSecretId":"00000000-0000-0000-0000-000000000003"`},
		},
		{
			name:             "api_key",
			variantKey:       "api_key",
			headerName:       "X-Key",
			vaultSecretID:    "00000000-0000-0000-0000-000000000004",
			wantWireContains: []string{`"type":"api_key"`, `"headerName":"X-Key"`, `"vaultSecretId":"00000000-0000-0000-0000-000000000004"`},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			authObj := mustAuthObject(t, tc.variantKey, tc.headerName, tc.vaultSecretID)
			union, err := buildMonitorAuthConfig(ctx, authObj)
			if err != nil {
				t.Fatalf("buildMonitorAuthConfig: %v", err)
			}
			if union == nil {
				t.Fatalf("buildMonitorAuthConfig returned nil for non-null auth")
			}
			wire, err := union.MarshalJSON()
			if err != nil {
				t.Fatalf("marshal union: %v", err)
			}
			s := string(wire)
			for _, want := range tc.wantWireContains {
				if !strings.Contains(s, want) {
					t.Errorf("wire body missing %q\nbody=%s", want, s)
				}
			}

			// Round-trip back into a TF object and verify every field
			// survives the typed mapping.
			roundTrip, diags := mapMonitorAuthToTF(ctx, union)
			if diags.HasError() {
				t.Fatalf("mapMonitorAuthToTF: %v", diags)
			}
			rtBytes, _ := json.Marshal(roundTrip.String())
			origBytes, _ := json.Marshal(authObj.String())
			if string(rtBytes) != string(origBytes) {
				t.Errorf("round-trip mismatch:\n  before=%s\n  after =%s", origBytes, rtBytes)
			}
		})
	}
}

// mustAuthObject builds a populated auth nested object for the given
// variant; helper for the discriminated-union round-trip table tests.
func mustAuthObject(t *testing.T, variant, headerName, vaultID string) types.Object {
	t.Helper()
	bearer := types.ObjectNull(authBearerObjectType().AttrTypes)
	basic := types.ObjectNull(authBasicObjectType().AttrTypes)
	header := types.ObjectNull(authHeaderObjectType().AttrTypes)
	apikey := types.ObjectNull(authApiKeyObjectType().AttrTypes)

	switch variant {
	case "bearer":
		obj, _ := types.ObjectValue(authBearerObjectType().AttrTypes, map[string]attr.Value{
			"vault_secret_id": types.StringValue(vaultID),
		})
		bearer = obj
	case "basic":
		obj, _ := types.ObjectValue(authBasicObjectType().AttrTypes, map[string]attr.Value{
			"vault_secret_id": types.StringValue(vaultID),
		})
		basic = obj
	case "header":
		obj, _ := types.ObjectValue(authHeaderObjectType().AttrTypes, map[string]attr.Value{
			"header_name":     types.StringValue(headerName),
			"vault_secret_id": types.StringValue(vaultID),
		})
		header = obj
	case "api_key":
		obj, _ := types.ObjectValue(authApiKeyObjectType().AttrTypes, map[string]attr.Value{
			"header_name":     types.StringValue(headerName),
			"vault_secret_id": types.StringValue(vaultID),
		})
		apikey = obj
	default:
		t.Fatalf("unknown variant %q", variant)
	}
	out, _ := types.ObjectValue(monitorAuthObjectType().AttrTypes, map[string]attr.Value{
		"bearer":  bearer,
		"basic":   basic,
		"header":  header,
		"api_key": apikey,
	})
	return out
}

func TestMonitorAuth_BuildReturnsNilWhenAuthIsNull(t *testing.T) {
	ctx := context.Background()
	union, err := buildMonitorAuthConfig(ctx, types.ObjectNull(monitorAuthObjectType().AttrTypes))
	if err != nil {
		t.Fatalf("buildMonitorAuthConfig: %v", err)
	}
	if union != nil {
		t.Errorf("expected nil union for null auth, got %v", union)
	}
}

func TestMonitorAuth_BuildRejectsMultipleVariants(t *testing.T) {
	ctx := context.Background()
	bearerObj, _ := types.ObjectValue(authBearerObjectType().AttrTypes, map[string]attr.Value{
		"vault_secret_id": types.StringValue("00000000-0000-0000-0000-000000000001"),
	})
	basicObj, _ := types.ObjectValue(authBasicObjectType().AttrTypes, map[string]attr.Value{
		"vault_secret_id": types.StringValue("00000000-0000-0000-0000-000000000002"),
	})
	authObj, _ := types.ObjectValue(monitorAuthObjectType().AttrTypes, map[string]attr.Value{
		"bearer":  bearerObj,
		"basic":   basicObj,
		"header":  types.ObjectNull(authHeaderObjectType().AttrTypes),
		"api_key": types.ObjectNull(authApiKeyObjectType().AttrTypes),
	})
	if _, err := buildMonitorAuthConfig(ctx, authObj); err == nil {
		t.Fatal("expected error for multi-variant auth, got nil")
	}
}

func TestMonitorAuth_BuildRejectsHeaderVariantMissingHeaderName(t *testing.T) {
	ctx := context.Background()
	headerObj, _ := types.ObjectValue(authHeaderObjectType().AttrTypes, map[string]attr.Value{
		"header_name":     types.StringNull(),
		"vault_secret_id": types.StringValue("00000000-0000-0000-0000-000000000001"),
	})
	authObj, _ := types.ObjectValue(monitorAuthObjectType().AttrTypes, map[string]attr.Value{
		"bearer":  types.ObjectNull(authBearerObjectType().AttrTypes),
		"basic":   types.ObjectNull(authBasicObjectType().AttrTypes),
		"header":  headerObj,
		"api_key": types.ObjectNull(authApiKeyObjectType().AttrTypes),
	})
	if _, err := buildMonitorAuthConfig(ctx, authObj); err == nil {
		t.Fatal("expected error when auth.header.header_name is missing, got nil")
	}
}

// ── Flow 1: Monitor config (user-supplied JSON) ─────────────────────────
//
// Unlike monitor.auth, monitor.config has NO inline `type` discriminator
// in the variant structs — the API discriminates on the OUTER
// CreateMonitorRequest.Type enum (HTTP/DNS/TCP/...). The provider blindly
// stuffs the user-supplied JSON into the union; the codegen union accepts
// any shape. These tests therefore prove that:
//   1. The user-supplied JSON parses into the union without error.
//   2. The union round-trips into the expected variant struct preserving
//      the variant-specific fields (which is what the API actually keys on).
// A regression here (e.g. a typo in a generated alias) would surface as
// the variant-specific data being silently dropped on the wire.

func TestMonitorConfig_UnmarshalEveryVariant(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		assert func(*testing.T, *generated.CreateMonitorRequest_Config)
	}{
		{
			name: "http",
			in:   `{"url":"https://example.com","method":"GET"}`,
			assert: func(t *testing.T, u *generated.CreateMonitorRequest_Config) {
				v, err := u.AsHttpMonitorConfig()
				if err != nil {
					t.Fatalf("AsHttp: %v", err)
				}
				if v.Url != "https://example.com" {
					t.Errorf("url not preserved: %q", v.Url)
				}
				if string(v.Method) != "GET" {
					t.Errorf("method not preserved: %q", v.Method)
				}
			},
		},
		{
			name: "dns",
			in:   `{"hostname":"example.com","recordType":"A"}`,
			assert: func(t *testing.T, u *generated.CreateMonitorRequest_Config) {
				v, err := u.AsDnsMonitorConfig()
				if err != nil {
					t.Fatalf("AsDns: %v", err)
				}
				if v.Hostname != "example.com" {
					t.Errorf("hostname not preserved: %q", v.Hostname)
				}
			},
		},
		{
			name: "tcp",
			in:   `{"host":"example.com","port":443}`,
			assert: func(t *testing.T, u *generated.CreateMonitorRequest_Config) {
				v, err := u.AsTcpMonitorConfig()
				if err != nil {
					t.Fatalf("AsTcp: %v", err)
				}
				if v.Host != "example.com" {
					t.Errorf("host not preserved: %q", v.Host)
				}
				if v.Port != 443 {
					t.Errorf("port not preserved: %d", v.Port)
				}
			},
		},
		{
			name: "icmp",
			in:   `{"host":"example.com"}`,
			assert: func(t *testing.T, u *generated.CreateMonitorRequest_Config) {
				v, err := u.AsIcmpMonitorConfig()
				if err != nil {
					t.Fatalf("AsIcmp: %v", err)
				}
				if v.Host != "example.com" {
					t.Errorf("host not preserved: %q", v.Host)
				}
			},
		},
		{
			name: "heartbeat",
			in:   `{"expectedInterval":300,"gracePeriod":60}`,
			assert: func(t *testing.T, u *generated.CreateMonitorRequest_Config) {
				v, err := u.AsHeartbeatMonitorConfig()
				if err != nil {
					t.Fatalf("AsHeartbeat: %v", err)
				}
				if v.ExpectedInterval != 300 {
					t.Errorf("expected interval not preserved: %d", v.ExpectedInterval)
				}
				if v.GracePeriod != 60 {
					t.Errorf("grace period not preserved: %d", v.GracePeriod)
				}
			},
		},
		{
			name: "mcp_server",
			in:   `{"command":"node","args":["server.js"]}`,
			assert: func(t *testing.T, u *generated.CreateMonitorRequest_Config) {
				v, err := u.AsMcpServerMonitorConfig()
				if err != nil {
					t.Fatalf("AsMcp: %v", err)
				}
				if v.Command != "node" {
					t.Errorf("command not preserved: %q", v.Command)
				}
				if v.Args == nil || len(*v.Args) != 1 || *(*v.Args)[0] != "server.js" {
					t.Errorf("args not preserved: %+v", v.Args)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var u generated.CreateMonitorRequest_Config
			if err := u.UnmarshalJSON([]byte(tc.in)); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			tc.assert(t, &u)
		})
	}
}

// ── Flow 2: Assertions (provider merges discriminator) ──────────────────

// The user supplies `type` as a sibling HCL attribute and a `config` JSON
// blob WITHOUT the `type` field. buildAssertions must merge the two so the
// API receives `{"type": "<x>", ...config}`. A bug here (e.g. failing to
// merge, or putting `type` next to `config` instead of inside it) would
// fail the API's union discriminator check at apply time.
func TestBuildAssertions_MergesTypeIntoEachVariantConfig(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name        string
		assertType  string
		configJSON  string
		variantType string // expected discriminator after merge
		// After buildAssertions, marshal the union back to JSON and assert
		// that this substring appears (i.e. the variant-specific data
		// survived the round-trip into the union).
		wantSubstring string
	}{
		{
			name:          "status_code",
			assertType:    "status_code",
			configJSON:    `{"expected":"200","operator":"equals"}`,
			variantType:   "status_code",
			wantSubstring: `"expected":"200"`,
		},
		{
			name:          "response_time",
			assertType:    "response_time",
			configJSON:    `{"thresholdMs":1500}`,
			variantType:   "response_time",
			wantSubstring: `"thresholdMs":1500`,
		},
		{
			name:          "header_value",
			assertType:    "header_value",
			configJSON:    `{"headerName":"Content-Type","expected":"application/json","operator":"equals"}`,
			variantType:   "header_value",
			wantSubstring: `"headerName":"Content-Type"`,
		},
		{
			name:          "regex_body",
			assertType:    "regex_body",
			configJSON:    `{"pattern":"^OK$"}`,
			variantType:   "regex_body",
			wantSubstring: `"pattern":"^OK$"`,
		},
		{
			name:          "ssl_expiry",
			assertType:    "ssl_expiry",
			configJSON:    `{"minDaysRemaining":30}`,
			variantType:   "ssl_expiry",
			wantSubstring: `"minDaysRemaining":30`,
		},
		{
			name:          "tcp_connects",
			assertType:    "tcp_connects",
			configJSON:    `{}`,
			variantType:   "tcp_connects",
			wantSubstring: `"type":"tcp_connects"`,
		},
		{
			name:          "dns_resolves",
			assertType:    "dns_resolves",
			configJSON:    `{}`,
			variantType:   "dns_resolves",
			wantSubstring: `"type":"dns_resolves"`,
		},
		{
			name:          "json_path",
			assertType:    "json_path",
			configJSON:    `{"path":"$.status","expected":"ok","operator":"equals"}`,
			variantType:   "json_path",
			wantSubstring: `"path":"$.status"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Build a single-element types.List of assertionModel.
			elem, diags := types.ObjectValue(
				assertionObjectType().AttrTypes,
				map[string]attr.Value{
					"type":     types.StringValue(tc.assertType),
					"config":   types.StringValue(tc.configJSON),
					"severity": types.StringNull(),
				},
			)
			if diags.HasError() {
				t.Fatalf("ObjectValue: %v", diags)
			}
			list, diags := types.ListValue(assertionObjectType(), []attr.Value{elem})
			if diags.HasError() {
				t.Fatalf("ListValue: %v", diags)
			}

			got, err := buildAssertions(ctx, list)
			if err != nil {
				t.Fatalf("buildAssertions: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("len(got) = %d, want 1", len(got))
			}

			// Round-trip the union back through JSON to verify both that
			// the type discriminator is present and that the rest of the
			// caller-provided config survived.
			raw, err := got[0].Config.MarshalJSON()
			if err != nil {
				t.Fatalf("union marshal: %v", err)
			}

			var probe map[string]any
			if err := json.Unmarshal(raw, &probe); err != nil {
				t.Fatalf("probe unmarshal: %v", err)
			}
			if got, ok := probe["type"].(string); !ok || got != tc.variantType {
				t.Errorf("merged JSON missing/wrong type discriminator: %s (want %q)", raw, tc.variantType)
			}
			if !strings.Contains(string(raw), tc.wantSubstring) {
				t.Errorf("merged JSON dropped variant data: %s (want %q)", raw, tc.wantSubstring)
			}
		})
	}
}

// TestBuildAssertions_RejectsInvalidConfigJSON guards the user-error path:
// supplying a non-object JSON (e.g. an array or a primitive) should surface
// a parse error, not silently ship a malformed body to the API.
func TestBuildAssertions_RejectsInvalidConfigJSON(t *testing.T) {
	ctx := context.Background()
	elem, _ := types.ObjectValue(
		assertionObjectType().AttrTypes,
		map[string]attr.Value{
			"type":     types.StringValue("status_code"),
			"config":   types.StringValue("[\"not an object\"]"),
			"severity": types.StringNull(),
		},
	)
	list, _ := types.ListValue(assertionObjectType(), []attr.Value{elem})
	if _, err := buildAssertions(ctx, list); err == nil {
		t.Fatal("expected error for non-object config JSON")
	}
}

// TestBuildAssertions_NullListReturnsNilNoError mirrors how the schema
// renders an absent assertions block: an empty/null List must produce no
// API body, not a misleading empty array (which the API would interpret
// as "clear all assertions").
func TestBuildAssertions_NullListReturnsNilNoError(t *testing.T) {
	ctx := context.Background()
	got, err := buildAssertions(ctx, types.ListNull(assertionObjectType()))
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// ── Flow 3: Alert channel config (provider builds typed struct) ─────────

// buildConfig switches on channel_type and constructs the corresponding
// generated.<X>ChannelConfig struct, then marshals to JSON. The test
// verifies that for each variant:
//   - the resulting JSON sets channelType to the expected discriminator
//   - the union UnmarshalJSON + As<Variant>() round-trip succeeds
//   - the variant-specific data survives the round-trip (a bug like
//     "buildConfig forgets to populate WebhookUrl" would surface here)
func TestAlertChannel_BuildConfig_AllVariants(t *testing.T) {
	ctx := context.Background()
	r := &AlertChannelResource{}

	cases := []struct {
		name         string
		model        AlertChannelResourceModel
		wantChannel  string
		wantContains string
		// asFn extracts the variant struct so we can check its fields.
		asFn func(*generated.CreateAlertChannelRequest_Config) (string, error)
	}{
		{
			name: "slack",
			model: AlertChannelResourceModel{
				ChannelType: types.StringValue("slack"),
				WebhookURL:  types.StringValue("https://hooks.slack.com/services/AAA/BBB/CCC"),
				MentionText: types.StringValue("@oncall"),
			},
			wantChannel:  "slack",
			wantContains: "hooks.slack.com",
			asFn: func(u *generated.CreateAlertChannelRequest_Config) (string, error) {
				v, err := u.AsSlackChannelConfig()
				return string(v.ChannelType), err
			},
		},
		{
			name: "discord",
			model: AlertChannelResourceModel{
				ChannelType:   types.StringValue("discord"),
				WebhookURL:    types.StringValue("https://discord.com/api/webhooks/x"),
				MentionRoleID: types.StringValue("999"),
			},
			wantChannel:  "discord",
			wantContains: "discord.com",
			asFn: func(u *generated.CreateAlertChannelRequest_Config) (string, error) {
				v, err := u.AsDiscordChannelConfig()
				return string(v.ChannelType), err
			},
		},
		{
			name: "email",
			model: AlertChannelResourceModel{
				ChannelType: types.StringValue("email"),
				Recipients: types.ListValueMust(types.StringType, []attr.Value{
					types.StringValue("a@example.com"),
					types.StringValue("b@example.com"),
				}),
			},
			wantChannel:  "email",
			wantContains: "a@example.com",
			asFn: func(u *generated.CreateAlertChannelRequest_Config) (string, error) {
				v, err := u.AsEmailChannelConfig()
				return string(v.ChannelType), err
			},
		},
		{
			name: "pagerduty",
			model: AlertChannelResourceModel{
				ChannelType:      types.StringValue("pagerduty"),
				RoutingKey:       types.StringValue("rk-1234"),
				SeverityOverride: types.StringValue("critical"),
			},
			wantChannel:  "pagerduty",
			wantContains: "rk-1234",
			asFn: func(u *generated.CreateAlertChannelRequest_Config) (string, error) {
				v, err := u.AsPagerDutyChannelConfig()
				return string(v.ChannelType), err
			},
		},
		{
			name: "opsgenie",
			model: AlertChannelResourceModel{
				ChannelType: types.StringValue("opsgenie"),
				APIKey:      types.StringValue("key-xyz"),
				Region:      types.StringValue("us"),
			},
			wantChannel:  "opsgenie",
			wantContains: "key-xyz",
			asFn: func(u *generated.CreateAlertChannelRequest_Config) (string, error) {
				v, err := u.AsOpsGenieChannelConfig()
				return string(v.ChannelType), err
			},
		},
		{
			name: "teams",
			model: AlertChannelResourceModel{
				ChannelType: types.StringValue("teams"),
				WebhookURL:  types.StringValue("https://outlook.office.com/webhook/x"),
			},
			wantChannel:  "teams",
			wantContains: "outlook.office.com",
			asFn: func(u *generated.CreateAlertChannelRequest_Config) (string, error) {
				v, err := u.AsTeamsChannelConfig()
				return string(v.ChannelType), err
			},
		},
		{
			name: "webhook",
			model: AlertChannelResourceModel{
				ChannelType: types.StringValue("webhook"),
				URL:         types.StringValue("https://example.com/hook"),
				CustomHeaders: types.MapValueMust(types.StringType, map[string]attr.Value{
					"X-Token": types.StringValue("secret"),
				}),
				SigningSecret: types.StringValue("hmac-secret"),
			},
			wantChannel:  "webhook",
			wantContains: "X-Token",
			asFn: func(u *generated.CreateAlertChannelRequest_Config) (string, error) {
				v, err := u.AsWebhookChannelConfig()
				return string(v.ChannelType), err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := r.buildConfig(&tc.model)
			if err != nil {
				t.Fatalf("buildConfig: %v", err)
			}
			if !strings.Contains(string(raw), `"channelType":"`+tc.wantChannel+`"`) {
				t.Errorf("missing/wrong channelType discriminator: %s", raw)
			}
			if !strings.Contains(string(raw), tc.wantContains) {
				t.Errorf("variant-specific field dropped: %s (want %q)", raw, tc.wantContains)
			}

			var u generated.CreateAlertChannelRequest_Config
			if err := u.UnmarshalJSON(raw); err != nil {
				t.Fatalf("union UnmarshalJSON: %v", err)
			}
			gotChannel, err := tc.asFn(&u)
			if err != nil {
				t.Fatalf("As%s: %v", tc.name, err)
			}
			if gotChannel != tc.wantChannel {
				t.Errorf("As%s ChannelType = %q, want %q", tc.name, gotChannel, tc.wantChannel)
			}
		})
	}
	_ = ctx
}

// TestAlertChannel_BuildConfig_UnknownChannelTypeErrors guards the user
// error path: an unsupported channel_type must surface a parse-time error
// rather than silently shipping `null` to the API.
func TestAlertChannel_BuildConfig_UnknownChannelTypeErrors(t *testing.T) {
	r := &AlertChannelResource{}
	model := AlertChannelResourceModel{
		ChannelType: types.StringValue("not-a-real-type"),
	}
	if _, err := r.buildConfig(&model); err == nil {
		t.Fatal("expected error for unsupported channel type")
	}
}
