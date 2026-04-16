package resources

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
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
				Description: "Authentication configuration as JSON (BearerAuthConfig, BasicAuthConfig, ApiKeyAuthConfig, HeaderAuthConfig)",
			},
		},
		Blocks: map[string]schema.Block{
			"assertions": schema.ListNestedBlock{
				Description: "Monitor assertions that define pass/fail criteria.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"type": schema.StringAttribute{
							Required:    true,
							Description: "Assertion type discriminator (e.g. StatusCodeAssertion, ResponseTimeAssertion)",
						},
						"config": schema.StringAttribute{
							Required:    true,
							Description: "Assertion configuration as JSON (shape depends on type)",
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
	r.client = req.ProviderData.(*api.Client)
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
	m.TriggerRules.ElementsAs(ctx, &ruleModels, false)
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

	req := &generated.CreateMonitorRequest{
		Name:             plan.Name.ValueString(),
		Type:             generated.CreateMonitorRequestType(plan.Type.ValueString()),
		Config:           configUnion,
		ManagedBy:        generated.CreateMonitorRequestManagedBy("TERRAFORM"),
		FrequencySeconds: int32PtrOrNil(plan.FrequencySeconds),
		Enabled:          boolPtrOrNil(plan.Enabled),
		Regions:          stringSliceToPtr(plan.Regions),
		EnvironmentId:    parseUUIDPtr(plan.EnvironmentID),
		Assertions:       &assertions,
		AlertChannelIds:  uuidSliceFromStringList(plan.AlertChannelIds),
		IncidentPolicy:   incidentPolicy,
	}

	if !plan.Auth.IsNull() && !plan.Auth.IsUnknown() {
		var authUnion generated.CreateMonitorRequest_Auth
		if err := authUnion.UnmarshalJSON(json.RawMessage(plan.Auth.ValueString())); err != nil {
			return nil, fmt.Errorf("monitor auth: %w", err)
		}
		req.Auth = &authUnion
	}

	if tagUUIDs := uuidSliceFromStringList(plan.TagIds); tagUUIDs != nil && len(*tagUUIDs) > 0 {
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

	var ip generated.UpdateIncidentPolicyRequest
	if incidentPolicy != nil {
		ip = *incidentPolicy
	}

	req := &generated.UpdateMonitorRequest{
		Name:             &name,
		Config:           &configUnion,
		ManagedBy:        &managedBy,
		FrequencySeconds: int32PtrOrNil(plan.FrequencySeconds),
		Enabled:          boolPtrOrNil(plan.Enabled),
		Regions:          stringSliceToPtr(plan.Regions),
		EnvironmentId:    parseUUIDPtr(plan.EnvironmentID),
		Assertions:       &assertions,
		AlertChannelIds:  uuidSliceFromStringList(plan.AlertChannelIds),
		IncidentPolicy:   ip,
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

	if tagUUIDs := uuidSliceFromStringList(plan.TagIds); tagUUIDs != nil && len(*tagUUIDs) > 0 {
		req.Tags = generated.AddMonitorTagsRequest{
			TagIds: tagUUIDs,
		}
	}

	return req, nil
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
			model.Auth = types.StringValue(string(authBytes))
		}
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

	monitor, err := api.Update[generated.MonitorDto](ctx, r.client, "/api/v1/monitors/"+state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating monitor", err.Error())
		return
	}

	r.mapToState(ctx, &plan, monitor)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
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

	for _, m := range monitors {
		if m.Name == req.ID {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), m.Id.String())...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), m.Name)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("type"), string(m.Type))...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("frequency_seconds"), int64(m.FrequencySeconds))...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("enabled"), m.Enabled)...)
			if m.Config != nil {
				if configBytes, err := m.Config.MarshalJSON(); err == nil {
					resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("config"), string(configBytes))...)
				}
			}
			return
		}
	}

	resp.Diagnostics.AddError("Monitor not found", fmt.Sprintf("No monitor found with name %q", req.ID))
}
