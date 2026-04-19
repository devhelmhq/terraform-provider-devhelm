package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

var (
	_ resource.Resource                   = &MonitorResource{}
	_ resource.ResourceWithImportState    = &MonitorResource{}
	_ resource.ResourceWithValidateConfig = &MonitorResource{}
)

type MonitorResource struct {
	client *api.Client
}

type MonitorResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Type             types.String `tfsdk:"type"`
	FrequencySeconds types.Int64  `tfsdk:"frequency_seconds"`
	Enabled          types.Bool   `tfsdk:"enabled"`
	Regions          types.List   `tfsdk:"regions"`
	EnvironmentID    types.String `tfsdk:"environment_id"`
	AlertChannelIds  types.List   `tfsdk:"alert_channel_ids"`
	TagIds           types.List   `tfsdk:"tag_ids"`
	PingUrl          types.String `tfsdk:"ping_url"`

	Config         types.String `tfsdk:"config"`
	Auth           types.String `tfsdk:"auth"`
	Assertions     types.List   `tfsdk:"assertions"`
	IncidentPolicy types.Object `tfsdk:"incident_policy"`
}

func NewMonitorResource() resource.Resource {
	return &MonitorResource{}
}

func (r *MonitorResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitor"
}

func (r *MonitorResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version:     0,
		Description: "Manages a DevHelm monitor. Supports HTTP, DNS, TCP, ICMP, Heartbeat, and MCP Server monitor types.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, Description: "Unique identifier for this monitor",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required: true, Description: "Human-readable name for this monitor",
			},
			"type": schema.StringAttribute{
				Required: true, Description: "Monitor type: HTTP, DNS, TCP, ICMP, HEARTBEAT, or MCP_SERVER",
				Validators: []validator.String{
					stringvalidator.OneOf(
						string(generated.CreateMonitorRequestTypeHTTP),
						string(generated.CreateMonitorRequestTypeDNS),
						string(generated.CreateMonitorRequestTypeTCP),
						string(generated.CreateMonitorRequestTypeICMP),
						string(generated.CreateMonitorRequestTypeHEARTBEAT),
						string(generated.CreateMonitorRequestTypeMCPSERVER),
					),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"frequency_seconds": schema.Int64Attribute{
				Optional: true, Computed: true,
				Description: "Check frequency in seconds (30\u201386400). " +
					"Server applies its default (60s) when omitted on create. " +
					"Omit on update to preserve the current value; the API has no way to clear this field.",
				Validators: []validator.Int64{
					int64validator.Between(30, 86400),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"enabled": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				Description: "Whether the monitor is active (default: true)",
			},
			"regions": schema.ListAttribute{
				Optional: true, Computed: true, ElementType: types.StringType,
				Description: "Probe regions (e.g. us-east, eu-west). " +
					"Omit to preserve the server's current regions; explicitly set to `[]` to clear.",
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"environment_id": schema.StringAttribute{
				Optional: true, Description: "Environment ID for variable substitution",
			},
			"alert_channel_ids": schema.ListAttribute{
				Optional: true, Computed: true, ElementType: types.StringType,
				Description: "Alert channel IDs to notify on incidents. " +
					"Omit to preserve the current list; explicitly set to `[]` to clear all channels.",
				PlanModifiers: []planmodifier.List{
					UseStateForUnknownAlwaysList(),
				},
			},
			"tag_ids": schema.ListAttribute{
				Optional: true, ElementType: types.StringType,
				Description: "Tag IDs to attach to this monitor",
			},
			"ping_url": schema.StringAttribute{
				Computed: true, Description: "Heartbeat ping URL (only set for HEARTBEAT monitors)",
				PlanModifiers: []planmodifier.String{UseStateForUnknownAlwaysString()},
			},
			"config": schema.StringAttribute{
				Required:    true,
				Description: "Monitor configuration as JSON. Shape depends on type (HttpMonitorConfig, DnsMonitorConfig, etc.)",
			},
			"auth": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Authentication configuration as JSON. Discriminator field `type` selects the auth scheme: `bearer`, `basic`, `header`, or `api_key`. Example: `jsonencode({type = \"bearer\", token = var.api_token})`",
			},
			"incident_policy": schema.SingleNestedAttribute{
				Optional: true, Computed: true,
				Description: "Incident policy controlling when failures escalate to incidents. " +
					"The API auto-creates a default policy on monitor creation, so omitting this attribute " +
					"adopts the server defaults; supplying any field overrides the policy in full.",
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"confirmation_type": schema.StringAttribute{
						Optional: true, Computed: true,
						Description:   "Confirmation strategy type (e.g. multi_region)",
						PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
					},
					"min_regions_failing": schema.Int64Attribute{
						Optional: true, Computed: true,
						Description:   "Minimum regions that must fail to confirm an incident",
						PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
					},
					"max_wait_seconds": schema.Int64Attribute{
						Optional: true, Computed: true,
						Description:   "Maximum seconds to wait for multi-region confirmation",
						PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
					},
					"consecutive_successes": schema.Int64Attribute{
						Optional: true, Computed: true,
						Description:   "Consecutive successes required for recovery",
						PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
					},
					"min_regions_passing": schema.Int64Attribute{
						Optional: true, Computed: true,
						Description:   "Minimum regions passing for recovery",
						PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
					},
					"cooldown_minutes": schema.Int64Attribute{
						Optional: true, Computed: true,
						Description:   "Minutes to wait before auto-resolving",
						PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
					},
					"trigger_rules": schema.ListNestedAttribute{
						Optional: true, Computed: true,
						Description:   "Rules that determine when failures escalate to incidents.",
						PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
						NestedObject: schema.NestedAttributeObject{
							Attributes: map[string]schema.Attribute{
								"type": schema.StringAttribute{
									Required:    true,
									Description: "Rule type: consecutive_failures, failures_in_window, or response_time",
									Validators: []validator.String{
										stringvalidator.OneOf(
											string(generated.ConsecutiveFailures),
											string(generated.FailuresInWindow),
											string(generated.ResponseTime),
										),
									},
								},
								"severity": schema.StringAttribute{
									Required:    true,
									Description: "Incident severity: down or degraded",
									Validators: []validator.String{
										stringvalidator.OneOf(
											string(generated.Down),
											string(generated.Degraded),
										),
									},
								},
								"scope": schema.StringAttribute{
									Optional:    true,
									Description: "Rule scope: per_region or any_region",
									Validators: []validator.String{
										stringvalidator.OneOf(
											string(generated.PerRegion),
											string(generated.AnyRegion),
										),
									},
								},
								"count": schema.Int64Attribute{
									Optional:    true,
									Description: "Failure count threshold (1–10)",
									Validators: []validator.Int64{
										int64validator.Between(1, 10),
									},
								},
								"window_minutes": schema.Int64Attribute{
									Optional:    true,
									Description: "Time window in minutes (for failures_in_window)",
									Validators: []validator.Int64{
										int64validator.AtLeast(1),
									},
								},
								"threshold_ms": schema.Int64Attribute{
									Optional:    true,
									Description: "Response time threshold in ms (for response_time)",
									Validators: []validator.Int64{
										int64validator.AtLeast(1),
									},
								},
								"aggregation_type": schema.StringAttribute{
									Optional:    true,
									Description: "Aggregation type: all_exceed, average, p95, max",
									Validators: []validator.String{
										stringvalidator.OneOf(
											string(generated.AllExceed),
											string(generated.Average),
											string(generated.P95),
											string(generated.Max),
										),
									},
								},
							},
						},
					},
				},
			},
		},
		Blocks: map[string]schema.Block{
			"assertions": schema.ListNestedBlock{
				Description: "Monitor assertions that define pass/fail criteria.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"type": schema.StringAttribute{
							Required:    true,
							Description: "Assertion type discriminator in snake_case wire format (e.g. `status_code`, `response_time`, `body_contains`, `header_value`, `dns_resolves`, `ssl_expiry`, `tcp_connects`). Must match an AssertionType enum value as serialized by the API.",
						},
						"config": schema.StringAttribute{
							Required:    true,
							Description: "Assertion configuration as JSON; the inner `type` field is omitted (set via the sibling `type` attribute) and the rest of the shape depends on the assertion kind. Field names inside the JSON are camelCase (the API wire format), e.g. `jsonencode({expected = 200, operator = \"equals\"})` for `status_code` or `jsonencode({thresholdMs = 500})` for `response_time`.",
						},
						"severity": schema.StringAttribute{
							Optional:    true,
							Description: "Assertion severity: fail or warn (default: fail)",
							Validators: []validator.String{
								stringvalidator.OneOf(
									string(generated.CreateAssertionRequestSeverityFail),
									string(generated.CreateAssertionRequestSeverityWarn),
								),
							},
						},
					},
				},
			},
		},
	}
}

func (r *MonitorResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model MonitorResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Config JSON validation: verify it's parseable as JSON.
	if !model.Config.IsNull() && !model.Config.IsUnknown() {
		configJSON := model.Config.ValueString()
		var parsed map[string]json.RawMessage
		if err := json.Unmarshal([]byte(configJSON), &parsed); err != nil {
			resp.Diagnostics.AddAttributeError(
				path.Root("config"),
				"Invalid JSON in config",
				fmt.Sprintf("The config attribute must be valid JSON: %s", err),
			)
		}
	}

	// Auth JSON validation: verify it's parseable as JSON with a valid type.
	if !model.Auth.IsNull() && !model.Auth.IsUnknown() {
		authJSON := model.Auth.ValueString()
		if authJSON != "" {
			var parsed map[string]json.RawMessage
			if err := json.Unmarshal([]byte(authJSON), &parsed); err != nil {
				resp.Diagnostics.AddAttributeError(
					path.Root("auth"),
					"Invalid JSON in auth",
					fmt.Sprintf("The auth attribute must be valid JSON: %s", err),
				)
			} else if typeRaw, ok := parsed["type"]; ok {
				var authType string
				if err := json.Unmarshal(typeRaw, &authType); err == nil {
					if !generated.MonitorAuthDtoAuthType(authType).Valid() {
						resp.Diagnostics.AddAttributeError(
							path.Root("auth"),
							"Invalid auth type",
							fmt.Sprintf("Auth type %q is not valid. Must be one of: bearer, basic, header, api_key", authType),
						)
					}
				}
			}
		}
	}

	// Trigger rule conditional validation: count is required for all rule types.
	if !model.IncidentPolicy.IsNull() && !model.IncidentPolicy.IsUnknown() {
		var policy incidentPolicyModel
		diags := model.IncidentPolicy.As(ctx, &policy, basetypes.ObjectAsOptions{
			UnhandledNullAsEmpty:    true,
			UnhandledUnknownAsEmpty: true,
		})
		if !diags.HasError() && !policy.TriggerRules.IsNull() && !policy.TriggerRules.IsUnknown() {
			var rules []triggerRuleModel
			if ruleDiags := policy.TriggerRules.ElementsAs(ctx, &rules, false); !ruleDiags.HasError() {
				for i, rule := range rules {
					rulePath := path.Root("incident_policy").AtName("trigger_rules").AtListIndex(i)

					if rule.Count.IsNull() || rule.Count.IsUnknown() {
						resp.Diagnostics.AddAttributeError(
							rulePath.AtName("count"),
							"Missing required attribute",
							"Trigger rule count is required for all rule types",
						)
					}

					ruleType := generated.TriggerRuleType(rule.Type.ValueString())
					if ruleType == generated.FailuresInWindow && (rule.WindowMinutes.IsNull() || rule.WindowMinutes.IsUnknown()) {
						resp.Diagnostics.AddAttributeError(
							rulePath.AtName("window_minutes"),
							"Missing required attribute",
							"window_minutes is required when trigger rule type is failures_in_window",
						)
					}

					if ruleType == generated.ResponseTime && (rule.ThresholdMs.IsNull() || rule.ThresholdMs.IsUnknown()) {
						resp.Diagnostics.AddAttributeError(
							rulePath.AtName("threshold_ms"),
							"Missing required attribute",
							"threshold_ms is required when trigger rule type is response_time",
						)
					}
				}
			}
		}
	}
}

func (r *MonitorResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*api.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *api.Client")
		return
	}
	r.client = client
}

// ── TF → API mapping ───────────────────────────────────────────────────

type assertionModel struct {
	Type     types.String `tfsdk:"type"`
	Config   types.String `tfsdk:"config"`
	Severity types.String `tfsdk:"severity"`
}

type triggerRuleModel struct {
	Type            types.String `tfsdk:"type"`
	Severity        types.String `tfsdk:"severity"`
	Scope           types.String `tfsdk:"scope"`
	Count           types.Int64  `tfsdk:"count"`
	WindowMinutes   types.Int64  `tfsdk:"window_minutes"`
	ThresholdMs     types.Int64  `tfsdk:"threshold_ms"`
	AggregationType types.String `tfsdk:"aggregation_type"`
}

type incidentPolicyModel struct {
	ConfirmationType     types.String `tfsdk:"confirmation_type"`
	MinRegionsFailing    types.Int64  `tfsdk:"min_regions_failing"`
	MaxWaitSeconds       types.Int64  `tfsdk:"max_wait_seconds"`
	ConsecutiveSuccesses types.Int64  `tfsdk:"consecutive_successes"`
	MinRegionsPassing    types.Int64  `tfsdk:"min_regions_passing"`
	CooldownMinutes      types.Int64  `tfsdk:"cooldown_minutes"`
	TriggerRules         types.List   `tfsdk:"trigger_rules"`
}

func buildAssertions(ctx context.Context, list types.List) ([]generated.CreateAssertionRequest, error) {
	if list.IsNull() || list.IsUnknown() || len(list.Elements()) == 0 {
		return nil, nil
	}

	var models []assertionModel
	diags := list.ElementsAs(ctx, &models, false)
	if diags.HasError() {
		return nil, fmt.Errorf("parsing assertions: %s", diags.Errors())
	}

	var result []generated.CreateAssertionRequest
	for _, m := range models {
		configJSON := json.RawMessage(m.Config.ValueString())

		typedConfig := map[string]json.RawMessage{
			"type": json.RawMessage(fmt.Sprintf("%q", m.Type.ValueString())),
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(configJSON, &raw); err != nil {
			return nil, fmt.Errorf("assertion config is not valid JSON: %w", err)
		}
		for k, v := range raw {
			typedConfig[k] = v
		}
		wrappedConfig, _ := json.Marshal(typedConfig)

		var configUnion generated.CreateAssertionRequest_Config
		if err := configUnion.UnmarshalJSON(wrappedConfig); err != nil {
			return nil, fmt.Errorf("assertion config unmarshal: %w", err)
		}

		req := generated.CreateAssertionRequest{
			Config:   configUnion,
			Severity: typedStringPtrOrNil[generated.CreateAssertionRequestSeverity](m.Severity),
		}
		result = append(result, req)
	}
	return result, nil
}

func buildIncidentPolicy(ctx context.Context, obj types.Object) (*generated.UpdateIncidentPolicyRequest, error) {
	// Null/unknown means "do not override the API's default policy". Empty
	// attributes inside a present object are treated the same as omitting
	// only that attribute (server-side defaults are kept).
	if obj.IsNull() || obj.IsUnknown() {
		return nil, nil
	}

	var m incidentPolicyModel
	diags := obj.As(ctx, &m, basetypes.ObjectAsOptions{
		UnhandledNullAsEmpty:    true,
		UnhandledUnknownAsEmpty: true,
	})
	if diags.HasError() {
		return nil, fmt.Errorf("parsing incident policy: %s", diags.Errors())
	}

	var triggerRules []generated.TriggerRule
	var ruleModels []triggerRuleModel
	if !m.TriggerRules.IsNull() && !m.TriggerRules.IsUnknown() {
		if ruleDiags := m.TriggerRules.ElementsAs(ctx, &ruleModels, false); ruleDiags.HasError() {
			return nil, fmt.Errorf("parsing trigger rules: %s", ruleDiags.Errors())
		}
	}
	for i, rm := range ruleModels {
		ruleType := generated.TriggerRuleType(rm.Type.ValueString())
		if !ruleType.Valid() {
			return nil, fmt.Errorf("trigger_rules[%d].type: invalid value %q", i, rm.Type.ValueString())
		}
		ruleSeverity := generated.TriggerRuleSeverity(rm.Severity.ValueString())
		if !ruleSeverity.Valid() {
			return nil, fmt.Errorf("trigger_rules[%d].severity: invalid value %q", i, rm.Severity.ValueString())
		}
		triggerRules = append(triggerRules, generated.TriggerRule{
			Type:            ruleType,
			Severity:        ruleSeverity,
			Scope:           typedStringPtrOrNil[generated.TriggerRuleScope](rm.Scope),
			Count:           int32PtrOrNil(rm.Count),
			WindowMinutes:   int32PtrOrNil(rm.WindowMinutes),
			ThresholdMs:     int32PtrOrNil(rm.ThresholdMs),
			AggregationType: typedStringPtrOrNil[generated.TriggerRuleAggregationType](rm.AggregationType),
		})
	}

	return &generated.UpdateIncidentPolicyRequest{
		TriggerRules: triggerRules,
		Confirmation: generated.ConfirmationPolicy{
			Type:              generated.ConfirmationPolicyType(m.ConfirmationType.ValueString()),
			MinRegionsFailing: int32OrZero(m.MinRegionsFailing),
			MaxWaitSeconds:    int32OrZero(m.MaxWaitSeconds),
		},
		Recovery: generated.RecoveryPolicy{
			ConsecutiveSuccesses: int32OrZero(m.ConsecutiveSuccesses),
			MinRegionsPassing:    int32OrZero(m.MinRegionsPassing),
			CooldownMinutes:      int32OrZero(m.CooldownMinutes),
		},
	}, nil
}

func (r *MonitorResource) buildCreateRequest(ctx context.Context, plan *MonitorResourceModel) (*generated.CreateMonitorRequest, error) {
	assertions, err := buildAssertions(ctx, plan.Assertions)
	if err != nil {
		return nil, err
	}

	incidentPolicy, err := buildIncidentPolicy(ctx, plan.IncidentPolicy)
	if err != nil {
		return nil, err
	}

	var configUnion generated.CreateMonitorRequest_Config
	if err := configUnion.UnmarshalJSON(json.RawMessage(plan.Config.ValueString())); err != nil {
		return nil, fmt.Errorf("monitor config: %w", err)
	}

	envID, err := parseUUIDPtrChecked(plan.EnvironmentID, "environment_id")
	if err != nil {
		return nil, err
	}
	alertChannels, err := uuidSliceFromStringListChecked(plan.AlertChannelIds, "alert_channel_ids")
	if err != nil {
		return nil, err
	}

	monitorType := generated.CreateMonitorRequestType(plan.Type.ValueString())
	if !monitorType.Valid() {
		return nil, fmt.Errorf("type: invalid monitor type %q", plan.Type.ValueString())
	}

	req := &generated.CreateMonitorRequest{
		Name:             plan.Name.ValueString(),
		Type:             monitorType,
		Config:           configUnion,
		ManagedBy:        generated.CreateMonitorRequestManagedByTERRAFORM,
		FrequencySeconds: int32PtrOrNil(plan.FrequencySeconds),
		Enabled:          boolPtrOrNil(plan.Enabled),
		Regions:          stringSliceToPtr(plan.Regions),
		EnvironmentId:    envID,
		Assertions:       &assertions,
		AlertChannelIds:  alertChannels,
		IncidentPolicy:   incidentPolicy,
	}

	// NOTE: req.Auth is intentionally left nil here. The generated
	// `MonitorAuthConfig` collapsed the polymorphic oneOf into a flat
	// {type: string} struct (the OpenAPI generator does not synthesize a
	// proper union type for a discriminator-only base schema), so writing
	// the typed field would drop every credential beyond `type`. The auth
	// blob is injected as raw JSON in (*MonitorResource).Create — see
	// marshalWithRawAuth.

	tagUUIDs, err := uuidSliceFromStringListChecked(plan.TagIds, "tag_ids")
	if err != nil {
		return nil, err
	}
	if tagUUIDs != nil && len(*tagUUIDs) > 0 {
		req.Tags = &generated.AddMonitorTagsRequest{
			TagIds: tagUUIDs,
		}
	}

	return req, nil
}

func (r *MonitorResource) buildUpdateRequest(ctx context.Context, plan *MonitorResourceModel) (*generated.UpdateMonitorRequest, error) {
	assertions, err := buildAssertions(ctx, plan.Assertions)
	if err != nil {
		return nil, err
	}

	incidentPolicy, err := buildIncidentPolicy(ctx, plan.IncidentPolicy)
	if err != nil {
		return nil, err
	}

	name := plan.Name.ValueString()
	managedBy := generated.UpdateMonitorRequestManagedByTERRAFORM

	// MonitorConfig is a `map[string]interface{}` alias post-spec-sync
	// (the spec dropped the polymorphic oneOf for the update path), so we
	// parse the user-supplied JSON into a plain map.
	var configMap generated.MonitorConfig
	if err := json.Unmarshal([]byte(plan.Config.ValueString()), &configMap); err != nil {
		return nil, fmt.Errorf("monitor config: %w", err)
	}

	envID, err := parseUUIDPtrChecked(plan.EnvironmentID, "environment_id")
	if err != nil {
		return nil, err
	}
	alertChannels, err := uuidSliceFromStringListChecked(plan.AlertChannelIds, "alert_channel_ids")
	if err != nil {
		return nil, err
	}

	req := &generated.UpdateMonitorRequest{
		Name:             &name,
		Config:           &configMap,
		ManagedBy:        &managedBy,
		FrequencySeconds: int32PtrOrNil(plan.FrequencySeconds),
		Enabled:          boolPtrOrNil(plan.Enabled),
		Regions:          stringSliceToPtr(plan.Regions),
		EnvironmentId:    envID,
		Assertions:       &assertions,
		AlertChannelIds:  alertChannels,
		IncidentPolicy:   incidentPolicy,
	}

	if plan.EnvironmentID.IsNull() {
		clearEnv := true
		req.ClearEnvironmentId = &clearEnv
	}

	// req.Auth left nil; raw auth JSON is merged into the request body in
	// (*MonitorResource).Update via marshalWithRawAuth. ClearAuth is also
	// set there so we never send `auth: null` and `clearAuth: true` together.

	// NOTE: Tag add/remove reconciliation is handled outside the PUT body
	// in (*MonitorResource).reconcileTags. PUT /monitors/{id} only supports
	// adding tags via the embedded Tags request; removing tags (including
	// clearing the list entirely) requires a separate DELETE call.

	return req, nil
}

// ── Object type helpers for nested blocks ───────────────────────────────

func assertionObjectType() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"type":     types.StringType,
			"config":   types.StringType,
			"severity": types.StringType,
		},
	}
}

func triggerRuleObjectType() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"type":             types.StringType,
			"severity":         types.StringType,
			"scope":            types.StringType,
			"count":            types.Int64Type,
			"window_minutes":   types.Int64Type,
			"threshold_ms":     types.Int64Type,
			"aggregation_type": types.StringType,
		},
	}
}

func incidentPolicyObjectType() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"confirmation_type":     types.StringType,
			"min_regions_failing":   types.Int64Type,
			"max_wait_seconds":      types.Int64Type,
			"consecutive_successes": types.Int64Type,
			"min_regions_passing":   types.Int64Type,
			"cooldown_minutes":      types.Int64Type,
			"trigger_rules":         types.ListType{ElemType: triggerRuleObjectType()},
		},
	}
}

// ── API → TF mapping ───────────────────────────────────────────────────

// mapToState mirrors a freshly-fetched MonitorDto onto the Terraform model.
// rawAuth is the raw JSON blob for the `auth` field as returned by the API
// (extracted from the response body via extractRawField) — see the comment in
// buildCreateRequest for why we cannot rely on the typed dto.Auth field.
//
// Returns any diagnostics produced while marshaling collection-valued
// attributes (e.g. types.ListValueFrom). Callers should Append the
// diagnostics to their response so framework-level errors are surfaced
// instead of being silently swallowed (END-1141).
func (r *MonitorResource) mapToState(ctx context.Context, model *MonitorResourceModel, dto *generated.MonitorDto, rawAuth string) diag.Diagnostics {
	var diags diag.Diagnostics
	model.ID = types.StringValue(dto.Id.String())
	model.Name = types.StringValue(dto.Name)
	model.Type = types.StringValue(string(dto.Type))
	model.FrequencySeconds = types.Int64Value(int64(dto.FrequencySeconds))
	model.Enabled = types.BoolValue(dto.Enabled)
	model.PingUrl = stringValue(dto.PingUrl)

	if configBytes, err := dto.Config.MarshalJSON(); err == nil && unionHasData(configBytes) {
		// The API strips the `type` discriminator from the config object
		// in its response (it's already represented at the top level via
		// dto.Type). Users frequently include it in their HCL config,
		// though — both because it's a natural way to reason about the
		// shape of the union and because some config fields require the
		// discriminator to disambiguate (e.g. ICMP packetCount). Echo
		// the user's `type` choice back into state if they originally
		// supplied one, so the round-trip is identity-preserving.
		normalized := normalizeJSON(configBytes)
		if priorHasConfigType(model.Config) {
			normalized = injectConfigType(normalized, string(dto.Type))
		}
		model.Config = types.StringValue(normalized)
	}

	if dto.Environment != nil && dto.Environment.Id.String() != "00000000-0000-0000-0000-000000000000" {
		model.EnvironmentID = types.StringValue(dto.Environment.Id.String())
	} else {
		model.EnvironmentID = types.StringNull()
	}

	// Auth: the API stores credential fields server-side (in a vault) and
	// only echoes back the public discriminator/handle (e.g. `{type, vaultSecretId}`),
	// stripping the actual `token`/`password`/etc. that the user supplied.
	// If we overwrote model.Auth with the API echo we'd lose those fields
	// from state, producing both a plan diff and a "Provider produced
	// inconsistent result after apply: .auth: inconsistent values for
	// sensitive attribute" error from Terraform on the very first Create.
	//
	// Strategy: when the user already has an auth value in plan/state we
	// preserve it verbatim. We only adopt the API echo when our local
	// value is null (e.g. import flow), and we clear local state only when
	// the API explicitly returned an empty/missing auth.
	switch {
	case rawAuth == "":
		// API returned no auth — only clear if state currently has one and
		// the resource genuinely has no auth attached server-side.
		if !model.Auth.IsNull() && !model.Auth.IsUnknown() {
			model.Auth = types.StringNull()
		}
	case model.Auth.IsNull() || model.Auth.IsUnknown():
		// Initial state has no auth (Read after import, or refresh of a
		// resource we don't yet have credential bytes for) — adopt the API
		// echo, even though it only contains the public handle.
		model.Auth = types.StringValue(normalizeJSON(json.RawMessage(rawAuth)))
		// Otherwise: keep the user-supplied value untouched. The token/secret
		// in plan is the source of truth; the API echo is a redacted handle.
	}

	// Regions
	if len(dto.Regions) > 0 {
		regionElems := make([]types.String, len(dto.Regions))
		for i, r := range dto.Regions {
			regionElems[i] = types.StringValue(r)
		}
		var d diag.Diagnostics
		model.Regions, d = types.ListValueFrom(ctx, types.StringType, regionElems)
		diags.Append(d...)
	} else if !model.Regions.IsNull() {
		var d diag.Diagnostics
		model.Regions, d = types.ListValueFrom(ctx, types.StringType, []types.String{})
		diags.Append(d...)
	}

	// Tag IDs
	// The API treats tags as an unordered set and may return them in a
	// different order than the user supplied (e.g. by primary key). If the
	// returned *set* matches the plan's set we preserve the plan's order
	// to avoid spurious diffs and "Provider produced inconsistent result
	// after apply" errors. Otherwise (genuine drift), we adopt the API's
	// order as the new source of truth.
	if dto.Tags != nil && len(*dto.Tags) > 0 {
		apiIDs := make([]string, len(*dto.Tags))
		for i, t := range *dto.Tags {
			apiIDs[i] = t.Id.String()
		}
		var d diag.Diagnostics
		model.TagIds, d = preserveListOrder(ctx, model.TagIds, apiIDs)
		diags.Append(d...)
	} else if !model.TagIds.IsNull() {
		var d diag.Diagnostics
		model.TagIds, d = types.ListValueFrom(ctx, types.StringType, []types.String{})
		diags.Append(d...)
	}

	// Alert Channel IDs — same set semantics as Tag IDs above.
	if dto.AlertChannelIds != nil && len(*dto.AlertChannelIds) > 0 {
		apiIDs := make([]string, len(*dto.AlertChannelIds))
		for i, id := range *dto.AlertChannelIds {
			apiIDs[i] = id.String()
		}
		var d diag.Diagnostics
		model.AlertChannelIds, d = preserveListOrder(ctx, model.AlertChannelIds, apiIDs)
		diags.Append(d...)
	} else if !model.AlertChannelIds.IsNull() {
		var d diag.Diagnostics
		model.AlertChannelIds, d = types.ListValueFrom(ctx, types.StringType, []types.String{})
		diags.Append(d...)
	}

	// Assertions
	if dto.Assertions != nil && len(*dto.Assertions) > 0 {
		// Build a content-keyed lookup of the user's prior assertions so we
		// can preserve their severity casing (e.g. "Fail" vs "fail") and
		// their decision to leave severity null even when the API order
		// differs from the HCL order. Matching by index is fragile because
		// the API does not promise a stable ordering across responses, and
		// users can reorder blocks freely in their HCL — both cases would
		// otherwise leak server-side casing into state and produce noisy
		// (or worse, incorrect) plans on the next run.
		var priorAssertions []assertionModel
		if !model.Assertions.IsNull() {
			diags.Append(model.Assertions.ElementsAs(ctx, &priorAssertions, false)...)
		}

		// Key = "<type>|<normalized-config-json>". The config is normalized
		// the same way as the API → state mapping below so the keys produced
		// from prior state and from the freshly-read DTO are comparable.
		// Multiple identical assertions (same type+config) are stored in a
		// FIFO slice so the first DTO match consumes the first prior entry.
		priorByKey := map[string][]int{}
		assertionKey := func(typ, cfg string) string { return typ + "|" + cfg }
		for idx, p := range priorAssertions {
			k := assertionKey(p.Type.ValueString(), p.Config.ValueString())
			priorByKey[k] = append(priorByKey[k], idx)
		}

		var assertionModels []assertionModel
		for _, a := range *dto.Assertions {
			am := assertionModel{
				Type: types.StringValue(string(a.AssertionType)),
			}

			// Compute normalized config first so we can use it as a lookup key.
			cfgStr := ""
			if cfgBytes, err := a.Config.MarshalJSON(); err == nil && unionHasData(cfgBytes) {
				var raw map[string]json.RawMessage
				if err := json.Unmarshal(cfgBytes, &raw); err == nil {
					delete(raw, "type")
					if stripped, err := json.Marshal(raw); err == nil {
						// Strip null-valued keys so the API echoing optional
						// fields back as `null` does not show as a diff
						// against a user-supplied config that omits them.
						cfgStr = normalizeJSON(stripped)
						am.Config = types.StringValue(cfgStr)
					}
				}
			}

			// Find a content-matched prior entry (FIFO) and consume it so
			// duplicate assertions are paired one-for-one.
			var matched *assertionModel
			k := assertionKey(string(a.AssertionType), cfgStr)
			if idxs := priorByKey[k]; len(idxs) > 0 {
				matched = &priorAssertions[idxs[0]]
				priorByKey[k] = idxs[1:]
			}

			sev := string(a.Severity)
			switch {
			case matched != nil && !matched.Severity.IsNull():
				// Preserve user-supplied casing when it matches the API value.
				priorSev := matched.Severity.ValueString()
				if strings.EqualFold(priorSev, sev) {
					sev = priorSev
				}
				am.Severity = types.StringValue(sev)
			case matched != nil && matched.Severity.IsNull():
				// User omitted severity in HCL → keep state null to match the
				// plan and avoid spurious null→value diffs.
			default:
				// New / imported assertion with no prior content match —
				// populate severity so import flows produce a complete state.
				am.Severity = types.StringValue(sev)
			}

			assertionModels = append(assertionModels, am)
		}
		var d diag.Diagnostics
		model.Assertions, d = types.ListValueFrom(ctx, types.ObjectType{
			AttrTypes: assertionObjectType().AttrTypes,
		}, assertionModels)
		diags.Append(d...)
	}

	// Incident Policy — schema is a SingleNestedAttribute (Optional+Computed
	// with UseStateForUnknown), so we always populate it from the DTO when
	// the API has one. The plan modifier ensures Terraform sees the prior
	// state when the user omits the attribute, eliminating the 0↔1 block
	// count drift that the previous ListNestedBlock design produced.
	if dto.IncidentPolicy != nil && dto.IncidentPolicy.Id.String() != "00000000-0000-0000-0000-000000000000" {
		policyModel := incidentPolicyModel{
			ConfirmationType:     types.StringValue(string(dto.IncidentPolicy.Confirmation.Type)),
			MinRegionsFailing:    types.Int64Value(int64(dto.IncidentPolicy.Confirmation.MinRegionsFailing)),
			MaxWaitSeconds:       types.Int64Value(int64(dto.IncidentPolicy.Confirmation.MaxWaitSeconds)),
			ConsecutiveSuccesses: types.Int64Value(int64(dto.IncidentPolicy.Recovery.ConsecutiveSuccesses)),
			MinRegionsPassing:    types.Int64Value(int64(dto.IncidentPolicy.Recovery.MinRegionsPassing)),
			CooldownMinutes:      types.Int64Value(int64(dto.IncidentPolicy.Recovery.CooldownMinutes)),
		}
		if len(dto.IncidentPolicy.TriggerRules) > 0 {
			var ruleModels []triggerRuleModel
			for _, tr := range dto.IncidentPolicy.TriggerRules {
				ruleModels = append(ruleModels, triggerRuleModel{
					Type:            types.StringValue(string(tr.Type)),
					Severity:        types.StringValue(string(tr.Severity)),
					Scope:           typedStringPtrValue(tr.Scope),
					Count:           int32Value(tr.Count),
					WindowMinutes:   int32Value(tr.WindowMinutes),
					ThresholdMs:     int32Value(tr.ThresholdMs),
					AggregationType: typedStringPtrValue(tr.AggregationType),
				})
			}
			var d diag.Diagnostics
			policyModel.TriggerRules, d = types.ListValueFrom(ctx, triggerRuleObjectType(), ruleModels)
			diags.Append(d...)
		} else {
			policyModel.TriggerRules = types.ListNull(triggerRuleObjectType())
		}
		obj, d := types.ObjectValueFrom(ctx, incidentPolicyObjectType().AttrTypes, policyModel)
		diags.Append(d...)
		model.IncidentPolicy = obj
	} else {
		model.IncidentPolicy = types.ObjectNull(incidentPolicyObjectType().AttrTypes)
	}

	return diags
}

// ── CRUD ────────────────────────────────────────────────────────────────

func (r *MonitorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan MonitorResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, err := r.buildCreateRequest(ctx, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Error building monitor request", err.Error())
		return
	}

	bodyJSON, err := marshalWithRawAuth(body, plan.Auth)
	if err != nil {
		resp.Diagnostics.AddError("Error encoding monitor request body", err.Error())
		return
	}

	monitor, rawResp, err := api.CreateRaw[generated.MonitorDto](ctx, r.client, "/api/v1/monitors", bodyJSON)
	if err != nil {
		resp.Diagnostics.AddError("Error creating monitor", err.Error())
		return
	}

	resp.Diagnostics.Append(r.mapToState(ctx, &plan, monitor, extractDataField(rawResp, "auth"))...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *MonitorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state MonitorResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	monitor, rawResp, err := api.GetRaw[generated.MonitorDto](ctx, r.client, "/api/v1/monitors/"+state.ID.ValueString())
	if err != nil {
		if api.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading monitor", err.Error())
		return
	}

	resp.Diagnostics.Append(r.mapToState(ctx, &state, monitor, extractDataField(rawResp, "auth"))...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *MonitorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan MonitorResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state MonitorResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, err := r.buildUpdateRequest(ctx, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Error building monitor request", err.Error())
		return
	}

	// Auth handling: when the user removes auth in HCL we set clearAuth: true
	// on the typed body; when the user supplies auth we inject the raw JSON
	// blob (the typed MonitorAuthConfig field would discard everything beyond
	// `type`). We never send both, which would be ambiguous.
	if plan.Auth.IsNull() || plan.Auth.IsUnknown() {
		clearAuth := true
		body.ClearAuth = &clearAuth
	}

	bodyJSON, err := marshalWithRawAuth(body, plan.Auth)
	if err != nil {
		resp.Diagnostics.AddError("Error encoding monitor request body", err.Error())
		return
	}

	// IMPORTANT: use `state.ID` (not `plan.ID`) for the URL — `plan.ID` is
	// `Unknown` during Update because the schema marks `id` as Computed; the
	// only authoritative source for the existing monitor's identifier is the
	// prior state. `mapToState` at the end of this function will overwrite
	// `plan.ID` with the value returned by the API, which keeps state stable
	// even if the backend ever decides to issue a new ID (currently it does
	// not, but the contract makes the read path resilient either way).
	// We deliberately discard the PUT response body: its DTO omits the tag
	// list entirely (so `monitor.Tags` would be misleading), and we re-GET
	// below to capture the post-reconciliation state authoritatively.
	if _, _, err := api.UpdateRaw[generated.MonitorDto](ctx, r.client, "/api/v1/monitors/"+state.ID.ValueString(), bodyJSON); err != nil {
		resp.Diagnostics.AddError("Error updating monitor", err.Error())
		return
	}

	// The PUT /monitors/{id} response does not echo back the tag set, so we
	// cannot trust monitor.Tags here as the "current" set when computing the
	// add/remove delta. The last-persisted tag list in `state.TagIds` is the
	// authoritative "before" view from Terraform's perspective.
	if err := r.reconcileTags(ctx, state.ID.ValueString(), plan.TagIds, state.TagIds); err != nil {
		resp.Diagnostics.AddError("Error reconciling monitor tags", err.Error())
		return
	}

	// reconcileTags issues out-of-band POST/DELETE calls to the tags
	// sub-resource; those mutations are NOT reflected in the DTO returned by
	// the previous PUT. We must re-GET to capture the post-reconciliation
	// tag set before calling mapToState, otherwise the persisted state would
	// describe the monitor as it existed *before* the tag delta was applied.
	monitor, rawResp, err := api.GetRaw[generated.MonitorDto](ctx, r.client, "/api/v1/monitors/"+state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error re-reading monitor after tag sync", err.Error())
		return
	}

	resp.Diagnostics.Append(r.mapToState(ctx, &plan, monitor, extractDataField(rawResp, "auth"))...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// reconcileTags brings the monitor's tag set in line with the plan:
//   - plan has tag IDs not in the prior state → POST to add
//   - prior state has tag IDs absent from the plan → DELETE to remove
//
// `currentTags` is the prior Terraform state value for `tag_ids`, used as the
// authoritative "before" view of the monitor's tag set. We deliberately do
// NOT use the API's PUT /monitors/{id} response: that endpoint omits the tag
// list from its DTO entirely, so a freshly-PUT monitor surfaces an empty
// `Tags` field even when the underlying record still has tags attached. If we
// computed the delta against that empty set we'd never DELETE the tags the
// user is trying to clear.
//
// When `planTags` is null/unknown we do not touch tags (preserves existing).
// When `planTags` is an empty list we remove everything currently attached.
func (r *MonitorResource) reconcileTags(ctx context.Context, monitorID string, planTags types.List, currentTags types.List) error {
	if planTags.IsNull() || planTags.IsUnknown() {
		return nil
	}

	desired := make(map[string]bool)
	for _, el := range planTags.Elements() {
		if s, ok := el.(types.String); ok && !s.IsNull() && !s.IsUnknown() {
			desired[s.ValueString()] = true
		}
	}

	existing := make(map[string]bool)
	if !currentTags.IsNull() && !currentTags.IsUnknown() {
		for _, el := range currentTags.Elements() {
			if s, ok := el.(types.String); ok && !s.IsNull() && !s.IsUnknown() {
				existing[s.ValueString()] = true
			}
		}
	}

	var toAdd []openapi_types.UUID
	for id := range desired {
		if !existing[id] {
			u, err := uuid.Parse(id)
			if err != nil {
				return fmt.Errorf("invalid tag id %q: %w", id, err)
			}
			toAdd = append(toAdd, openapi_types.UUID(u))
		}
	}

	var toRemove []openapi_types.UUID
	for id := range existing {
		if !desired[id] {
			u, err := uuid.Parse(id)
			if err != nil {
				return fmt.Errorf("invalid existing tag id %q: %w", id, err)
			}
			toRemove = append(toRemove, u)
		}
	}

	if len(toAdd) > 0 {
		addBody := generated.AddMonitorTagsRequest{TagIds: &toAdd}
		if _, err := api.CreateList[generated.TagDto](ctx, r.client, "/api/v1/monitors/"+monitorID+"/tags", addBody); err != nil {
			return fmt.Errorf("adding tags: %w", err)
		}
	}

	if len(toRemove) > 0 {
		removeBody := generated.RemoveMonitorTagsRequest{TagIds: toRemove}
		if err := api.DeleteWithBody(ctx, r.client, "/api/v1/monitors/"+monitorID+"/tags", removeBody); err != nil {
			return fmt.Errorf("removing tags: %w", err)
		}
	}

	return nil
}

func (r *MonitorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state MonitorResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := api.Delete(ctx, r.client, "/api/v1/monitors/"+state.ID.ValueString())
	if err != nil && !api.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting monitor", err.Error())
	}
}

func (r *MonitorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	monitors, err := api.List[generated.MonitorDto](ctx, r.client, "/api/v1/monitors")
	if err != nil {
		resp.Diagnostics.AddError("Error listing monitors for import", err.Error())
		return
	}

	// Accept both name and UUID as the import ID. If the import ID matches a
	// UUID we take it directly — UUIDs are unique. If it matches by name, we
	// require the match to be unique: monitor names are not unique across an
	// org, and silently picking the first match would produce a stale or
	// arbitrary import.
	var monitorID string
	var matchedByName []string
	for _, m := range monitors {
		if m.Id.String() == req.ID {
			monitorID = m.Id.String()
			matchedByName = nil
			break
		}
		if m.Name == req.ID {
			matchedByName = append(matchedByName, m.Id.String())
		}
	}
	if monitorID == "" {
		switch len(matchedByName) {
		case 0:
			resp.Diagnostics.AddError("Monitor not found", fmt.Sprintf("No monitor found with name or ID %q", req.ID))
			return
		case 1:
			monitorID = matchedByName[0]
		default:
			resp.Diagnostics.AddError(
				"Ambiguous monitor import",
				fmt.Sprintf("%d monitors share the name %q (ids: %v). Import by UUID instead.", len(matchedByName), req.ID, matchedByName),
			)
			return
		}
	}

	monitor, rawResp, err := api.GetRaw[generated.MonitorDto](ctx, r.client, "/api/v1/monitors/"+monitorID)
	if err != nil {
		resp.Diagnostics.AddError("Error fetching monitor for import", err.Error())
		return
	}

	var model MonitorResourceModel
	// All collection-valued attributes start as typed-null. mapToState
	// will populate them from the API response when present. Pre-seeding
	// to an empty list would force the post-import state to look like
	// the user wrote `tag_ids = []` even when the resource has none —
	// causing a guaranteed `[] -> null` diff against any HCL that omits
	// the attribute (the common case for greenfield imports).
	//
	// IncidentPolicy is a single nested attribute and mapToState writes
	// either a populated object or null directly, so no pre-init needed.
	model.Assertions = types.ListNull(types.ObjectType{
		AttrTypes: assertionObjectType().AttrTypes,
	})
	model.Regions = types.ListNull(types.StringType)
	model.TagIds = types.ListNull(types.StringType)
	model.AlertChannelIds = types.ListNull(types.StringType)
	resp.Diagnostics.Append(r.mapToState(ctx, &model, monitor, extractDataField(rawResp, "auth"))...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
