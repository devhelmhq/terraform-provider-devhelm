package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &MonitorResource{}
	_ resource.ResourceWithImportState = &MonitorResource{}
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
	IncidentPolicy types.List   `tfsdk:"incident_policy"`
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
					stringvalidator.OneOf("HTTP", "DNS", "TCP", "ICMP", "HEARTBEAT", "MCP_SERVER"),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"frequency_seconds": schema.Int64Attribute{
				Optional: true, Description: "Check frequency in seconds (30\u201386400)",
			},
			"enabled": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				Description: "Whether the monitor is active (default: true)",
			},
			"regions": schema.ListAttribute{
				Optional: true, ElementType: types.StringType,
				Description: "Probe regions (e.g. us-east, eu-west)",
			},
			"environment_id": schema.StringAttribute{
				Optional: true, Description: "Environment ID for variable substitution",
			},
			"alert_channel_ids": schema.ListAttribute{
				Optional: true, ElementType: types.StringType,
				Description: "Alert channel IDs to notify on incidents",
			},
			"tag_ids": schema.ListAttribute{
				Optional: true, ElementType: types.StringType,
				Description: "Tag IDs to attach to this monitor",
			},
			"ping_url": schema.StringAttribute{
				Computed: true, Description: "Heartbeat ping URL (only set for HEARTBEAT monitors)",
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
							Description: "Assertion configuration as JSON; the inner `type` field is omitted (set via the sibling `type` attribute) and the rest of the shape depends on the assertion kind. Example for `status_code`: `jsonencode({expected = 200})`",
						},
						"severity": schema.StringAttribute{
							Optional:    true,
							Description: "Assertion severity: fail or warn (default: fail)",
						},
					},
				},
			},
			"incident_policy": schema.ListNestedBlock{
				Description: "Incident policy with trigger rules, confirmation, and recovery settings. At most one block.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"confirmation_type": schema.StringAttribute{
							Required:    true,
							Description: "Confirmation strategy type (e.g. multi_region)",
						},
						"min_regions_failing": schema.Int64Attribute{
							Optional:    true,
							Description: "Minimum regions that must fail to confirm incident",
						},
						"max_wait_seconds": schema.Int64Attribute{
							Optional:    true,
							Description: "Maximum seconds to wait for multi-region confirmation",
						},
						"consecutive_successes": schema.Int64Attribute{
							Optional:    true,
							Description: "Consecutive successes required for recovery",
						},
						"min_regions_passing": schema.Int64Attribute{
							Optional:    true,
							Description: "Minimum regions passing for recovery",
						},
						"cooldown_minutes": schema.Int64Attribute{
							Optional:    true,
							Description: "Minutes to wait before auto-resolving",
						},
					},
					Blocks: map[string]schema.Block{
						"trigger_rule": schema.ListNestedBlock{
							Description: "Rules that trigger incidents.",
							NestedObject: schema.NestedBlockObject{
								Attributes: map[string]schema.Attribute{
									"type": schema.StringAttribute{
										Required:    true,
										Description: "Rule type: consecutive_failures, failures_in_window, or response_time",
									},
									"severity": schema.StringAttribute{
										Required:    true,
										Description: "Incident severity: down or degraded",
									},
									"scope": schema.StringAttribute{
										Optional:    true,
										Description: "Rule scope: per_region or any_region",
									},
									"count": schema.Int64Attribute{
										Optional:    true,
										Description: "Failure count threshold",
									},
									"window_minutes": schema.Int64Attribute{
										Optional:    true,
										Description: "Time window in minutes (for failures_in_window)",
									},
									"threshold_ms": schema.Int64Attribute{
										Optional:    true,
										Description: "Response time threshold in ms (for response_time)",
									},
									"aggregation_type": schema.StringAttribute{
										Optional:    true,
										Description: "Aggregation type: all_exceed, average, p95, max",
									},
								},
							},
						},
					},
				},
			},
		},
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
	ConfirmationType    types.String `tfsdk:"confirmation_type"`
	MinRegionsFailing   types.Int64  `tfsdk:"min_regions_failing"`
	MaxWaitSeconds      types.Int64  `tfsdk:"max_wait_seconds"`
	ConsecutiveSuccesses types.Int64 `tfsdk:"consecutive_successes"`
	MinRegionsPassing   types.Int64  `tfsdk:"min_regions_passing"`
	CooldownMinutes     types.Int64  `tfsdk:"cooldown_minutes"`
	TriggerRules        types.List   `tfsdk:"trigger_rule"`
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

func buildIncidentPolicy(ctx context.Context, list types.List) (*generated.UpdateIncidentPolicyRequest, error) {
	if list.IsNull() || list.IsUnknown() || len(list.Elements()) == 0 {
		return nil, nil
	}

	var models []incidentPolicyModel
	diags := list.ElementsAs(ctx, &models, false)
	if diags.HasError() {
		return nil, fmt.Errorf("parsing incident policy: %s", diags.Errors())
	}
	if len(models) == 0 {
		return nil, nil
	}
	m := models[0]

	var triggerRules []generated.TriggerRule
	var ruleModels []triggerRuleModel
	if ruleDiags := m.TriggerRules.ElementsAs(ctx, &ruleModels, false); ruleDiags.HasError() {
		return nil, fmt.Errorf("parsing trigger rules: %s", ruleDiags.Errors())
	}
	for _, rm := range ruleModels {
		triggerRules = append(triggerRules, generated.TriggerRule{
			Type:            generated.TriggerRuleType(rm.Type.ValueString()),
			Severity:        generated.TriggerRuleSeverity(rm.Severity.ValueString()),
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
			MinRegionsFailing: int32PtrOrNil(m.MinRegionsFailing),
			MaxWaitSeconds:    int32PtrOrNil(m.MaxWaitSeconds),
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

	req := &generated.CreateMonitorRequest{
		Name:             plan.Name.ValueString(),
		Type:             generated.CreateMonitorRequestType(plan.Type.ValueString()),
		Config:           configUnion,
		ManagedBy:        generated.CreateMonitorRequestManagedBy("TERRAFORM"),
		FrequencySeconds: int32PtrOrNil(plan.FrequencySeconds),
		Enabled:          boolPtrOrNil(plan.Enabled),
		Regions:          stringSliceToPtr(plan.Regions),
		EnvironmentId:    envID,
		Assertions:       &assertions,
		AlertChannelIds:  alertChannels,
		IncidentPolicy:   incidentPolicy,
	}

	if !plan.Auth.IsNull() && !plan.Auth.IsUnknown() {
		var authUnion generated.CreateMonitorRequest_Auth
		if err := authUnion.UnmarshalJSON(json.RawMessage(plan.Auth.ValueString())); err != nil {
			return nil, fmt.Errorf("monitor auth: %w", err)
		}
		req.Auth = &authUnion
	}

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
	managedBy := generated.UpdateMonitorRequestManagedBy("TERRAFORM")

	var configUnion generated.UpdateMonitorRequest_Config
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

	req := &generated.UpdateMonitorRequest{
		Name:             &name,
		Config:           &configUnion,
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

	if !plan.Auth.IsNull() && !plan.Auth.IsUnknown() {
		var authUnion generated.UpdateMonitorRequest_Auth
		if err := authUnion.UnmarshalJSON(json.RawMessage(plan.Auth.ValueString())); err != nil {
			return nil, fmt.Errorf("monitor auth: %w", err)
		}
		req.Auth = &authUnion
	} else {
		clearAuth := true
		req.ClearAuth = &clearAuth
	}

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
			"confirmation_type":    types.StringType,
			"min_regions_failing":  types.Int64Type,
			"max_wait_seconds":     types.Int64Type,
			"consecutive_successes": types.Int64Type,
			"min_regions_passing":  types.Int64Type,
			"cooldown_minutes":     types.Int64Type,
			"trigger_rule":         types.ListType{ElemType: triggerRuleObjectType()},
		},
	}
}

// ── API → TF mapping ───────────────────────────────────────────────────

func (r *MonitorResource) mapToState(ctx context.Context, model *MonitorResourceModel, dto *generated.MonitorDto) {
	model.ID = types.StringValue(dto.Id.String())
	model.Name = types.StringValue(dto.Name)
	model.Type = types.StringValue(string(dto.Type))
	model.FrequencySeconds = types.Int64Value(int64(dto.FrequencySeconds))
	model.Enabled = types.BoolValue(dto.Enabled)
	model.PingUrl = stringValue(dto.PingUrl)

	if dto.Config != nil {
		if configBytes, err := dto.Config.MarshalJSON(); err == nil {
			model.Config = types.StringValue(normalizeJSON(configBytes))
		}
	}

	if dto.Environment.Id.String() != "00000000-0000-0000-0000-000000000000" {
		model.EnvironmentID = types.StringValue(dto.Environment.Id.String())
	} else {
		model.EnvironmentID = types.StringNull()
	}

	if dto.Auth != nil {
		if authBytes, err := dto.Auth.MarshalJSON(); err == nil {
			// Normalize the same way `config` is normalized so that the API echoing
			// optional fields back as `null` does not produce a perpetual diff
			// against a user-supplied auth blob that omits those keys.
			model.Auth = types.StringValue(normalizeJSON(authBytes))
		}
	}

	// Regions
	if len(dto.Regions) > 0 {
		regionElems := make([]types.String, len(dto.Regions))
		for i, r := range dto.Regions {
			regionElems[i] = types.StringValue(r)
		}
		model.Regions, _ = types.ListValueFrom(ctx, types.StringType, regionElems)
	} else if !model.Regions.IsNull() {
		model.Regions, _ = types.ListValueFrom(ctx, types.StringType, []types.String{})
	}

	// Tag IDs
	if dto.Tags != nil && len(*dto.Tags) > 0 {
		tagElems := make([]types.String, len(*dto.Tags))
		for i, t := range *dto.Tags {
			tagElems[i] = types.StringValue(t.Id.String())
		}
		model.TagIds, _ = types.ListValueFrom(ctx, types.StringType, tagElems)
	} else if !model.TagIds.IsNull() {
		model.TagIds, _ = types.ListValueFrom(ctx, types.StringType, []types.String{})
	}

	// Alert Channel IDs
	if dto.AlertChannelIds != nil && len(*dto.AlertChannelIds) > 0 {
		chElems := make([]types.String, len(*dto.AlertChannelIds))
		for i, id := range *dto.AlertChannelIds {
			chElems[i] = types.StringValue(id.String())
		}
		model.AlertChannelIds, _ = types.ListValueFrom(ctx, types.StringType, chElems)
	} else if !model.AlertChannelIds.IsNull() {
		model.AlertChannelIds, _ = types.ListValueFrom(ctx, types.StringType, []types.String{})
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
			_ = model.Assertions.ElementsAs(ctx, &priorAssertions, false)
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
			if a.Config != nil {
				if cfgBytes, err := a.Config.MarshalJSON(); err == nil {
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
		model.Assertions, _ = types.ListValueFrom(ctx, types.ObjectType{
			AttrTypes: assertionObjectType().AttrTypes,
		}, assertionModels)
	}

	// Incident Policy — populate when EITHER the prior state already has a
	// block (so we keep the regular plan/state in sync without producing a
	// 0↔1 block-count diff) OR the API actually returned a real policy and
	// the prior state has none, which happens during `terraform import` of
	// a monitor that has a policy attached. The Id-zero guard is the only
	// reliable wire-level signal: IncidentPolicyDto is a non-pointer struct
	// in the OpenAPI types, so a missing policy still arrives as a zero-
	// initialized struct — checking for a non-zero UUID separates "real
	// policy" from "default zero value".
	hasPriorPolicy := !model.IncidentPolicy.IsNull() && len(model.IncidentPolicy.Elements()) > 0
	apiHasPolicy := dto.IncidentPolicy.Id.String() != "00000000-0000-0000-0000-000000000000"
	if hasPriorPolicy || apiHasPolicy {
		policyModel := incidentPolicyModel{
			ConfirmationType:     types.StringValue(string(dto.IncidentPolicy.Confirmation.Type)),
			MinRegionsFailing:    int32Value(dto.IncidentPolicy.Confirmation.MinRegionsFailing),
			MaxWaitSeconds:       int32Value(dto.IncidentPolicy.Confirmation.MaxWaitSeconds),
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
			policyModel.TriggerRules, _ = types.ListValueFrom(ctx, types.ObjectType{
				AttrTypes: triggerRuleObjectType().AttrTypes,
			}, ruleModels)
		} else {
			policyModel.TriggerRules = types.ListNull(types.ObjectType{AttrTypes: triggerRuleObjectType().AttrTypes})
		}
		model.IncidentPolicy, _ = types.ListValueFrom(ctx, types.ObjectType{
			AttrTypes: incidentPolicyObjectType().AttrTypes,
		}, []incidentPolicyModel{policyModel})
	}
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

	monitor, err := api.Create[generated.MonitorDto](ctx, r.client, "/api/v1/monitors", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating monitor", err.Error())
		return
	}

	r.mapToState(ctx, &plan, monitor)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *MonitorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state MonitorResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	monitor, err := api.Get[generated.MonitorDto](ctx, r.client, "/api/v1/monitors/"+state.ID.ValueString())
	if err != nil {
		if api.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading monitor", err.Error())
		return
	}

	r.mapToState(ctx, &state, monitor)
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

	// IMPORTANT: use `state.ID` (not `plan.ID`) for the URL — `plan.ID` is
	// `Unknown` during Update because the schema marks `id` as Computed; the
	// only authoritative source for the existing monitor's identifier is the
	// prior state. `mapToState` at the end of this function will overwrite
	// `plan.ID` with the value returned by the API, which keeps state stable
	// even if the backend ever decides to issue a new ID (currently it does
	// not, but the contract makes the read path resilient either way).
	monitor, err := api.Update[generated.MonitorDto](ctx, r.client, "/api/v1/monitors/"+state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating monitor", err.Error())
		return
	}

	if err := r.reconcileTags(ctx, state.ID.ValueString(), plan.TagIds, monitor); err != nil {
		resp.Diagnostics.AddError("Error reconciling monitor tags", err.Error())
		return
	}

	// reconcileTags issues out-of-band POST/DELETE calls to the tags
	// sub-resource; those mutations are NOT reflected in the DTO returned by
	// the previous PUT. We must re-GET to capture the post-reconciliation
	// tag set before calling mapToState, otherwise the persisted state would
	// describe the monitor as it existed *before* the tag delta was applied.
	monitor, err = api.Get[generated.MonitorDto](ctx, r.client, "/api/v1/monitors/"+state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error re-reading monitor after tag sync", err.Error())
		return
	}

	r.mapToState(ctx, &plan, monitor)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// reconcileTags brings the monitor's tag set in line with the plan:
//   - plan has tag IDs not on the monitor → POST to add
//   - monitor has tag IDs absent from plan → DELETE to remove
//
// Called after the main PUT so we operate on the latest server state. When
// tag_ids is null in the plan we do not touch tags (preserves existing).
// When tag_ids is an empty list we remove everything.
func (r *MonitorResource) reconcileTags(ctx context.Context, monitorID string, planTags types.List, current *generated.MonitorDto) error {
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
	if current != nil && current.Tags != nil {
		for _, t := range *current.Tags {
			existing[t.Id.String()] = true
		}
	}

	var toAddPtrs []*openapi_types.UUID
	for id := range desired {
		if !existing[id] {
			u, err := uuid.Parse(id)
			if err != nil {
				return fmt.Errorf("invalid tag id %q: %w", id, err)
			}
			uu := u
			toAddPtrs = append(toAddPtrs, &uu)
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

	if len(toAddPtrs) > 0 {
		addBody := generated.AddMonitorTagsRequest{TagIds: &toAddPtrs}
		if _, err := api.Create[generated.MonitorDto](ctx, r.client, "/api/v1/monitors/"+monitorID+"/tags", addBody); err != nil {
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

	monitor, err := api.Get[generated.MonitorDto](ctx, r.client, "/api/v1/monitors/"+monitorID)
	if err != nil {
		resp.Diagnostics.AddError("Error fetching monitor for import", err.Error())
		return
	}

	var model MonitorResourceModel
	// Pre-initialize lists so mapToState populates them during import
	model.IncidentPolicy, _ = types.ListValueFrom(ctx, types.ObjectType{
		AttrTypes: incidentPolicyObjectType().AttrTypes,
	}, []incidentPolicyModel{})
	model.Assertions, _ = types.ListValueFrom(ctx, types.ObjectType{
		AttrTypes: assertionObjectType().AttrTypes,
	}, []assertionModel{})
	model.Regions, _ = types.ListValueFrom(ctx, types.StringType, []types.String{})
	model.TagIds, _ = types.ListValueFrom(ctx, types.StringType, []types.String{})
	model.AlertChannelIds, _ = types.ListValueFrom(ctx, types.StringType, []types.String{})
	r.mapToState(ctx, &model, monitor)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
