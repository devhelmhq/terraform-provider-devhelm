package resources

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// ───────────────────────────────────────────────────────────────────────
// Schema-vs-DTO completeness audit (Class I)
//
// The generated DTOs are the source of truth for what the API accepts.
// Any field a customer can send to the API but cannot express in the TF
// schema is a silent feature gap. This test walks each resource's Schema
// alongside the corresponding generated request DTOs and asserts every
// DTO field is either:
//   1. mapped to a schema attribute (matched by name), or
//   2. on an explicit allow-list of intentionally hidden fields with a
//      one-line justification for why TF doesn't expose it.
//
// When a new field lands in a generated DTO, this test will fail until
// the developer either wires it into the schema or adds it to the
// allow-list. The goal is "no silent gaps".
// ───────────────────────────────────────────────────────────────────────

// camelToSnake mirrors the convention codegen uses for JSON tags
// (camelCase) vs Terraform attribute names (snake_case).
func camelToSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			r = r - 'A' + 'a'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// dtoFieldNames returns the snake_cased JSON tag names of every field in
// the supplied struct. Fields without json tags or with json:"-" are
// skipped. Embedded structs are not recursed into; the union/embedded
// types we care about for this audit are flat structs.
func dtoFieldNames(t reflect.Type) []string {
	t = derefStructType(t)
	if t == nil || t.Kind() != reflect.Struct {
		return nil
	}
	out := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.SplitN(tag, ",", 2)[0]
		if name == "" {
			name = f.Name
		}
		out = append(out, camelToSnake(name))
	}
	return out
}

func derefStructType(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

// schemaAttributeNames returns the set of attribute names declared on a
// resource's schema, including nested-block attributes.
func schemaAttributeNames(t *testing.T, r resource.Resource) map[string]struct{} {
	t.Helper()
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, resp)

	names := map[string]struct{}{}
	for k := range resp.Schema.Attributes {
		names[k] = struct{}{}
	}
	for k := range resp.Schema.Blocks {
		names[k] = struct{}{}
	}
	// Collect aliases: if an attribute is a SingleNestedAttribute, its
	// sub-fields count as supported even though they're nested.
	for _, attr := range resp.Schema.Attributes {
		if nested, ok := attr.(schema.SingleNestedAttribute); ok {
			for k := range nested.Attributes {
				names[k] = struct{}{}
			}
		}
	}
	return names
}

// auditCase pins a request DTO type to the resource that owns it and
// declares which JSON fields are intentionally not exposed via the
// schema (with a justification for each).
type auditCase struct {
	name       string
	resource   resource.Resource
	dto        any
	dtoAliases map[string]string // dto field name → schema attribute name (for renames)
	allowed    map[string]string // dto field name → reason for omission
}

func TestSchemaVsDTO_Audit(t *testing.T) {
	cases := []auditCase{
		{
			name:     "monitor_create",
			resource: &MonitorResource{},
			dto:      generated.CreateMonitorRequest{},
			dtoAliases: map[string]string{
				"tags": "tag_ids",
			},
			allowed: map[string]string{
				"managed_by": "Hardcoded to TERRAFORM by the provider; not a user-facing knob.",
			},
		},
		{
			name:     "monitor_update",
			resource: &MonitorResource{},
			dto:      generated.UpdateMonitorRequest{},
			dtoAliases: map[string]string{
				"tags": "tag_ids",
			},
			allowed: map[string]string{
				"managed_by":           "Hardcoded to TERRAFORM by the provider; not a user-facing knob.",
				"clear_auth":           "Internal flag derived from null Auth attribute, not user-facing.",
				"clear_environment_id": "Internal flag derived from null environment_id attribute.",
			},
		},
		{
			name:     "alert_channel_create",
			resource: &AlertChannelResource{},
			dto:      generated.CreateAlertChannelRequest{},
			dtoAliases: map[string]string{
				"channel_type": "type",
			},
			allowed: map[string]string{
				"config": "Discriminated union spread across per-channel-type schema blocks (slack, discord, email, …); the union is built by the provider's buildConfig().",
			},
		},
		{
			name:     "alert_channel_update",
			resource: &AlertChannelResource{},
			dto:      generated.UpdateAlertChannelRequest{},
			allowed: map[string]string{
				"config": "See alert_channel_create note: discriminated union exploded into per-type blocks.",
			},
		},
		{
			name:     "environment_create",
			resource: &EnvironmentResource{},
			dto:      generated.CreateEnvironmentRequest{},
			allowed:  map[string]string{},
		},
		{
			name:     "environment_update",
			resource: &EnvironmentResource{},
			dto:      generated.UpdateEnvironmentRequest{},
			allowed: map[string]string{
				"slug": "Slug is RequiresReplace on the schema; the Update DTO accepts it for completeness but TF treats slug changes as destroy/create.",
			},
		},
		{
			name:     "tag_create",
			resource: &TagResource{},
			dto:      generated.CreateTagRequest{},
			allowed:  map[string]string{},
		},
		{
			name:     "tag_update",
			resource: &TagResource{},
			dto:      generated.UpdateTagRequest{},
			allowed:  map[string]string{},
		},
		{
			name:     "secret_create",
			resource: &SecretResource{},
			dto:      generated.CreateSecretRequest{},
			allowed:  map[string]string{},
		},
		{
			name:     "secret_update",
			resource: &SecretResource{},
			dto:      generated.UpdateSecretRequest{},
			allowed:  map[string]string{},
		},
		{
			name:     "webhook_create",
			resource: &WebhookResource{},
			dto:      generated.CreateWebhookEndpointRequest{},
			allowed:  map[string]string{},
		},
		{
			name:     "webhook_update",
			resource: &WebhookResource{},
			dto:      generated.UpdateWebhookEndpointRequest{},
			allowed:  map[string]string{},
		},
		{
			name:     "resource_group_create",
			resource: &ResourceGroupResource{},
			dto:      generated.CreateResourceGroupRequest{},
			allowed:  map[string]string{},
		},
		{
			name:     "resource_group_update",
			resource: &ResourceGroupResource{},
			dto:      generated.UpdateResourceGroupRequest{},
			allowed:  map[string]string{},
		},
		{
			name:     "resource_group_membership_create",
			resource: &ResourceGroupMembershipResource{},
			dto:      generated.AddResourceGroupMemberRequest{},
			allowed:  map[string]string{},
		},
		{
			name:     "notification_policy_create",
			resource: &NotificationPolicyResource{},
			dto:      generated.CreateNotificationPolicyRequest{},
			dtoAliases: map[string]string{
				"escalation":  "escalation_step",
				"match_rules": "match_rule",
			},
			allowed: map[string]string{},
		},
		{
			name:     "notification_policy_update",
			resource: &NotificationPolicyResource{},
			dto:      generated.UpdateNotificationPolicyRequest{},
			dtoAliases: map[string]string{
				"escalation":  "escalation_step",
				"match_rules": "match_rule",
			},
			allowed: map[string]string{},
		},
		{
			name:     "dependency_create",
			resource: &DependencyResource{},
			dto:      generated.ServiceSubscribeRequest{},
			allowed:  map[string]string{},
		},
		{
			name:     "status_page_create",
			resource: &StatusPageResource{},
			dto:      generated.CreateStatusPageRequest{},
			allowed:  map[string]string{},
		},
		{
			name:     "status_page_update",
			resource: &StatusPageResource{},
			dto:      generated.UpdateStatusPageRequest{},
			allowed:  map[string]string{},
		},
		{
			name:     "status_page_component_create",
			resource: &StatusPageComponentResource{},
			dto:      generated.CreateStatusPageComponentRequest{},
			allowed:  map[string]string{},
		},
		{
			name:     "status_page_component_update",
			resource: &StatusPageComponentResource{},
			dto:      generated.UpdateStatusPageComponentRequest{},
			allowed: map[string]string{
				"remove_from_group": "Synthesised by the provider when group_id is dropped from HCL.",
			},
		},
		{
			name:     "status_page_component_group_create",
			resource: &StatusPageComponentGroupResource{},
			dto:      generated.CreateStatusPageComponentGroupRequest{},
			allowed:  map[string]string{},
		},
		{
			name:     "status_page_component_group_update",
			resource: &StatusPageComponentGroupResource{},
			dto:      generated.UpdateStatusPageComponentGroupRequest{},
			allowed:  map[string]string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schemaAttrs := schemaAttributeNames(t, tc.resource)
			dtoFields := dtoFieldNames(reflect.TypeOf(tc.dto))
			if len(dtoFields) == 0 {
				t.Fatalf("no DTO fields discovered for %T", tc.dto)
			}
			for _, fieldName := range dtoFields {
				if reason, ok := tc.allowed[fieldName]; ok {
					if reason == "" {
						t.Errorf("allow-listed field %q must include a reason", fieldName)
					}
					continue
				}
				attrName := fieldName
				if alias, ok := tc.dtoAliases[fieldName]; ok {
					attrName = alias
				}
				if _, ok := schemaAttrs[attrName]; !ok {
					t.Errorf(
						"DTO field %q is not exposed on the %T schema (no attribute named %q). "+
							"Add a schema attribute, or add it to the allow-list with a reason.",
						fieldName, tc.resource, attrName,
					)
				}
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// Validator presence audit (Class V)
//
// Verifies that schema attributes corresponding to known enum fields in
// the generated DTOs have Validators configured (typically OneOf). This
// catches silent drift: if a new enum field appears in the spec but no
// validator is wired in, the provider would accept arbitrary strings
// and let the API reject them at apply time.
// ───────────────────────────────────────────────────────────────────────

type enumValidatorCase struct {
	resource    resource.Resource
	attrPath    string // dot-separated path, e.g. "type" or "incident_policy.trigger_rules.type"
	enumValues  []string
	description string
}

func hasStringValidators(t *testing.T, r resource.Resource, attrPath string) bool {
	t.Helper()
	resp := &resource.SchemaResponse{}
	r.Schema(context.Background(), resource.SchemaRequest{}, resp)

	parts := strings.Split(attrPath, ".")
	attrs := resp.Schema.Attributes
	blocks := resp.Schema.Blocks

	for i, part := range parts {
		isLast := i == len(parts)-1

		if attr, ok := attrs[part]; ok {
			if isLast {
				switch a := attr.(type) {
				case schema.StringAttribute:
					return len(a.Validators) > 0
				case schema.Int64Attribute:
					return len(a.Validators) > 0
				default:
					return false
				}
			}
			if nested, ok := attr.(schema.SingleNestedAttribute); ok {
				attrs = nested.Attributes
				blocks = nil
				continue
			}
			if nested, ok := attr.(schema.ListNestedAttribute); ok {
				attrs = nested.NestedObject.Attributes
				blocks = nil
				continue
			}
			return false
		}
		if block, ok := blocks[part]; ok {
			if nested, ok := block.(schema.ListNestedBlock); ok {
				attrs = nested.NestedObject.Attributes
				blocks = nil
				continue
			}
			return false
		}

		return false
	}
	return false
}

func TestSchemaValidatorAudit(t *testing.T) {
	cases := []enumValidatorCase{
		// Monitor enums
		{&MonitorResource{}, "type", []string{"HTTP", "DNS", "TCP", "ICMP", "HEARTBEAT", "MCP_SERVER"}, "monitor type"},
		{&MonitorResource{}, "frequency_seconds", nil, "monitor frequency range"},
		{&MonitorResource{}, "incident_policy.trigger_rules.type", []string{"consecutive_failures", "failures_in_window", "response_time"}, "trigger rule type"},
		{&MonitorResource{}, "incident_policy.trigger_rules.severity", []string{"down", "degraded"}, "trigger severity"},
		{&MonitorResource{}, "incident_policy.trigger_rules.scope", []string{"per_region", "any_region"}, "trigger scope"},
		{&MonitorResource{}, "incident_policy.trigger_rules.aggregation_type", []string{"all_exceed", "average", "p95", "max"}, "trigger aggregation"},
		{&MonitorResource{}, "incident_policy.trigger_rules.count", nil, "trigger count range"},
		{&MonitorResource{}, "assertions.severity", nil, "assertion severity"},
		// Alert channel enums (TF attribute is "channel_type", not "type")
		{&AlertChannelResource{}, "channel_type", []string{"slack", "email", "pagerduty", "opsgenie", "discord", "teams", "webhook"}, "channel type"},
		// Status page enums
		{&StatusPageResource{}, "visibility", []string{"PUBLIC"}, "visibility"},
		{&StatusPageResource{}, "incident_mode", []string{"MANUAL", "REVIEW", "AUTOMATIC"}, "incident mode"},
		// Status page component enums
		{&StatusPageComponentResource{}, "type", []string{"STATIC", "MONITOR", "GROUP"}, "component type"},
		// Dependency enums
		{&DependencyResource{}, "alert_sensitivity", []string{"ALL", "INCIDENTS_ONLY", "MAJOR_ONLY"}, "alert sensitivity"},
		// Resource group enums
		{&ResourceGroupResource{}, "health_threshold_type", []string{"COUNT", "PERCENTAGE"}, "health threshold type"},
		{&ResourceGroupResource{}, "default_frequency", nil, "resource group default frequency range"},
	}

	for _, tc := range cases {
		name := fmt.Sprintf("%T/%s", tc.resource, tc.attrPath)
		t.Run(name, func(t *testing.T) {
			if !hasStringValidators(t, tc.resource, tc.attrPath) {
				t.Errorf(
					"attribute %q on %T has no Validators. "+
						"Add a stringvalidator.OneOf(%v) or int64validator for %s.",
					tc.attrPath, tc.resource, tc.enumValues, tc.description,
				)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// ValidateConfig audit (Class VC)
//
// Verifies that key resources implement resource.ResourceWithValidateConfig.
// ───────────────────────────────────────────────────────────────────────

func TestValidateConfigImplemented(t *testing.T) {
	resources := []struct {
		name     string
		resource resource.Resource
	}{
		{"monitor", &MonitorResource{}},
		{"status_page_component", &StatusPageComponentResource{}},
	}

	for _, tc := range resources {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := tc.resource.(resource.ResourceWithValidateConfig); !ok {
				t.Errorf("%T does not implement ResourceWithValidateConfig", tc.resource)
			}
		})
	}
}
