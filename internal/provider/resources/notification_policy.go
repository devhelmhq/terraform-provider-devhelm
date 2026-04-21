package resources

import (
	"context"
	"fmt"
	"strings"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &NotificationPolicyResource{}
	_ resource.ResourceWithImportState = &NotificationPolicyResource{}
)

type NotificationPolicyResource struct {
	client *api.Client
}

type NotificationPolicyModel struct {
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	Enabled    types.Bool   `tfsdk:"enabled"`
	Priority   types.Int64  `tfsdk:"priority"`
	MatchRules types.List   `tfsdk:"match_rule"`
	Escalation types.List   `tfsdk:"escalation_step"`
	OnResolve  types.String `tfsdk:"on_resolve"`
	OnReopen   types.String `tfsdk:"on_reopen"`
}

func NewNotificationPolicyResource() resource.Resource {
	return &NotificationPolicyResource{}
}

func (r *NotificationPolicyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_notification_policy"
}

func (r *NotificationPolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version:     0,
		Description: "Manages a DevHelm notification policy with escalation chains for incident alerting.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, Description: "Unique identifier",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required: true, Description: "Human-readable name for this notification policy",
			},
			"enabled": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				Description: "Whether this policy is active (default: true)",
			},
			"priority": schema.Int64Attribute{
				Optional: true, Computed: true, Default: int64default.StaticInt64(0),
				Description: "Policy priority (higher = evaluated first)",
			},
			"on_resolve": schema.StringAttribute{
				Optional:    true,
				Description: "Action when incident resolves (e.g. notify channel)",
			},
			"on_reopen": schema.StringAttribute{
				Optional:    true,
				Description: "Action when incident reopens",
			},
		},
		Blocks: map[string]schema.Block{
			"escalation_step": schema.ListNestedBlock{
				Description: "Ordered escalation steps. Each step defines channels and delays.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"channel_ids": schema.ListAttribute{
							Required: true, ElementType: types.StringType,
							Description: "Alert channel IDs to notify in this step",
						},
						"delay_minutes": schema.Int64Attribute{
							Optional:    true,
							Description: "Minutes to wait before escalating to next step",
						},
						"require_ack": schema.BoolAttribute{
							Optional:    true,
							Description: "Whether acknowledgement is required before escalating",
						},
						"repeat_interval_seconds": schema.Int64Attribute{
							Optional:    true,
							Description: "Seconds between repeated notifications for this step",
						},
					},
				},
			},
			"match_rule": schema.ListNestedBlock{
				Description: "Rules to match which incidents trigger this policy.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"type": schema.StringAttribute{
							Required: true,
							Description: "Rule type discriminator. One of: " +
								"component_name_in, incident_status, monitor_id_in, " +
								"monitor_type_in, region_in, resource_group_id_in, " +
								"service_id_in, severity_gte. Spec source of truth: " +
								"`MatchRuleType` enum.",
							Validators: []validator.String{
								stringvalidator.OneOf(api.MatchRuleTypes...),
							},
						},
						"value": schema.StringAttribute{
							Optional: true, Description: "Single match value",
						},
						"values": schema.ListAttribute{
							Optional: true, ElementType: types.StringType,
							Description: "Multiple match values",
						},
						"monitor_ids": schema.ListAttribute{
							Optional: true, ElementType: types.StringType,
							Description: "Monitor IDs to match",
						},
						"regions": schema.ListAttribute{
							Optional: true, ElementType: types.StringType,
							Description: "Regions to match",
						},
					},
				},
			},
		},
	}
}

func (r *NotificationPolicyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

type escalationStepModel struct {
	ChannelIDs            types.List  `tfsdk:"channel_ids"`
	DelayMinutes          types.Int64 `tfsdk:"delay_minutes"`
	RequireAck            types.Bool  `tfsdk:"require_ack"`
	RepeatIntervalSeconds types.Int64 `tfsdk:"repeat_interval_seconds"`
}

type matchRuleModel struct {
	Type       types.String `tfsdk:"type"`
	Value      types.String `tfsdk:"value"`
	Values     types.List   `tfsdk:"values"`
	MonitorIDs types.List   `tfsdk:"monitor_ids"`
	Regions    types.List   `tfsdk:"regions"`
}

func (r *NotificationPolicyResource) buildRequest(ctx context.Context, plan *NotificationPolicyModel) (*generated.CreateNotificationPolicyRequest, error) {
	var steps []escalationStepModel
	diags := plan.Escalation.ElementsAs(ctx, &steps, false)
	if diags.HasError() {
		return nil, fmt.Errorf("parsing escalation steps: %s", diags.Errors()[0].Detail())
	}

	var apiSteps []generated.EscalationStep
	for i, s := range steps {
		channelIDs, err := uuidListToSliceChecked(s.ChannelIDs, fmt.Sprintf("escalation[%d].channel_ids", i))
		if err != nil {
			return nil, err
		}
		apiSteps = append(apiSteps, generated.EscalationStep{
			ChannelIds:            channelIDs,
			DelayMinutes:          int32OrZero(s.DelayMinutes),
			RequireAck:            boolPtrOrNil(s.RequireAck),
			RepeatIntervalSeconds: int32PtrOrNil(s.RepeatIntervalSeconds),
		})
	}

	var apiRules []generated.MatchRule
	var rules []matchRuleModel
	if ruleDiags := plan.MatchRules.ElementsAs(ctx, &rules, false); ruleDiags.HasError() {
		return nil, fmt.Errorf("parsing match rules: %s", ruleDiags.Errors()[0].Detail())
	}
	for i, mr := range rules {
		monitorIDs, err := uuidSliceFromStringListChecked(mr.MonitorIDs, fmt.Sprintf("match_rule[%d].monitor_ids", i))
		if err != nil {
			return nil, err
		}
		apiRules = append(apiRules, generated.MatchRule{
			Type:       generated.MatchRuleType(mr.Type.ValueString()),
			Value:      stringPtrOrNil(mr.Value),
			Values:     stringSliceToPtr(mr.Values),
			MonitorIds: monitorIDs,
			Regions:    stringSliceToPtr(mr.Regions),
		})
	}

	createEnabled := true
	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
		createEnabled = plan.Enabled.ValueBool()
	}
	createPriority := int32(0)
	if !plan.Priority.IsNull() && !plan.Priority.IsUnknown() {
		createPriority = int32(plan.Priority.ValueInt64())
	}

	req := &generated.CreateNotificationPolicyRequest{
		Name:     plan.Name.ValueString(),
		Enabled:  &createEnabled,
		Priority: &createPriority,
		Escalation: generated.EscalationChain{
			Steps:     apiSteps,
			OnResolve: stringPtrOrNil(plan.OnResolve),
			OnReopen:  stringPtrOrNil(plan.OnReopen),
		},
		MatchRules: &apiRules,
	}
	return req, nil
}

// buildUpdateRequest mirrors buildRequest but targets the
// UpdateNotificationPolicyRequest DTO (which uses non-pointer fields; the API
// treats missing JSON fields as "preserve current").
func (r *NotificationPolicyResource) buildUpdateRequest(ctx context.Context, plan *NotificationPolicyModel) (*generated.UpdateNotificationPolicyRequest, error) {
	var steps []escalationStepModel
	diags := plan.Escalation.ElementsAs(ctx, &steps, false)
	if diags.HasError() {
		return nil, fmt.Errorf("parsing escalation steps: %s", diags.Errors()[0].Detail())
	}

	var apiSteps []generated.EscalationStep
	for i, s := range steps {
		channelIDs, err := uuidListToSliceChecked(s.ChannelIDs, fmt.Sprintf("escalation[%d].channel_ids", i))
		if err != nil {
			return nil, err
		}
		apiSteps = append(apiSteps, generated.EscalationStep{
			ChannelIds:            channelIDs,
			DelayMinutes:          int32OrZero(s.DelayMinutes),
			RequireAck:            boolPtrOrNil(s.RequireAck),
			RepeatIntervalSeconds: int32PtrOrNil(s.RepeatIntervalSeconds),
		})
	}

	var apiRules []generated.MatchRule
	var rules []matchRuleModel
	if ruleDiags := plan.MatchRules.ElementsAs(ctx, &rules, false); ruleDiags.HasError() {
		return nil, fmt.Errorf("parsing match rules: %s", ruleDiags.Errors()[0].Detail())
	}
	for i, mr := range rules {
		monitorIDs, err := uuidSliceFromStringListChecked(mr.MonitorIDs, fmt.Sprintf("match_rule[%d].monitor_ids", i))
		if err != nil {
			return nil, err
		}
		apiRules = append(apiRules, generated.MatchRule{
			Type:       generated.MatchRuleType(mr.Type.ValueString()),
			Value:      stringPtrOrNil(mr.Value),
			Values:     stringSliceToPtr(mr.Values),
			MonitorIds: monitorIDs,
			Regions:    stringSliceToPtr(mr.Regions),
		})
	}

	priority := int32(0)
	if !plan.Priority.IsNull() && !plan.Priority.IsUnknown() {
		priority = int32(plan.Priority.ValueInt64())
	}
	enabled := true
	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() {
		enabled = plan.Enabled.ValueBool()
	}

	return &generated.UpdateNotificationPolicyRequest{
		Name:     stringPtrOrNil(plan.Name),
		Enabled:  &enabled,
		Priority: &priority,
		Escalation: &generated.EscalationChain{
			Steps:     apiSteps,
			OnResolve: stringPtrOrNil(plan.OnResolve),
			OnReopen:  stringPtrOrNil(plan.OnReopen),
		},
		MatchRules: &apiRules,
	}, nil
}

func (r *NotificationPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan NotificationPolicyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, err := r.buildRequest(ctx, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Error building request", err.Error())
		return
	}

	policy, err := api.Create[generated.NotificationPolicyDto](ctx, r.client, api.PathNotificationPolicies, body)
	if err != nil {
		api.AddAPIError(&resp.Diagnostics, "create notification policy", err, path.Root("name"))
		return
	}

	resp.Diagnostics.Append(r.mapToState(ctx, &plan, policy)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func escalationStepObjectType() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"channel_ids":             types.ListType{ElemType: types.StringType},
			"delay_minutes":           types.Int64Type,
			"require_ack":             types.BoolType,
			"repeat_interval_seconds": types.Int64Type,
		},
	}
}

func matchRuleObjectType() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"type":        types.StringType,
			"value":       types.StringType,
			"values":      types.ListType{ElemType: types.StringType},
			"monitor_ids": types.ListType{ElemType: types.StringType},
			"regions":     types.ListType{ElemType: types.StringType},
		},
	}
}

// mapToState mirrors a NotificationPolicyDto onto the Terraform model.
// Returns any framework diagnostics (END-1141: previously these were
// silently dropped, hiding spec drift between the API DTO and the schema).
func (r *NotificationPolicyResource) mapToState(ctx context.Context, model *NotificationPolicyModel, dto *generated.NotificationPolicyDto) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(dto.Id.String())
	model.Name = types.StringValue(dto.Name)
	model.Enabled = types.BoolValue(dto.Enabled)
	model.Priority = types.Int64Value(int64(dto.Priority))
	model.OnResolve = stringValue(dto.Escalation.OnResolve)
	model.OnReopen = stringValue(dto.Escalation.OnReopen)

	// Escalation steps
	if len(dto.Escalation.Steps) > 0 {
		var priorSteps []escalationStepModel
		if !model.Escalation.IsNull() {
			diags.Append(model.Escalation.ElementsAs(ctx, &priorSteps, false)...)
		}
		var stepModels []escalationStepModel
		for i, s := range dto.Escalation.Steps {
			sm := escalationStepModel{
				RepeatIntervalSeconds: int32Value(s.RepeatIntervalSeconds),
			}
			// Preserve null for optional fields not set by user
			if i < len(priorSteps) {
				if priorSteps[i].DelayMinutes.IsNull() {
					sm.DelayMinutes = types.Int64Null()
				} else {
					sm.DelayMinutes = types.Int64Value(int64(s.DelayMinutes))
				}
				if priorSteps[i].RequireAck.IsNull() {
					sm.RequireAck = types.BoolNull()
				} else {
					sm.RequireAck = boolValue(s.RequireAck)
				}
				if priorSteps[i].RepeatIntervalSeconds.IsNull() {
					sm.RepeatIntervalSeconds = types.Int64Null()
				}
			} else {
				sm.DelayMinutes = types.Int64Value(int64(s.DelayMinutes))
				sm.RequireAck = boolValue(s.RequireAck)
			}
			if len(s.ChannelIds) > 0 {
				chElems := make([]types.String, len(s.ChannelIds))
				for j, id := range s.ChannelIds {
					chElems[j] = types.StringValue(id.String())
				}
				var d diag.Diagnostics
				sm.ChannelIDs, d = types.ListValueFrom(ctx, types.StringType, chElems)
				diags.Append(d...)
			} else {
				var d diag.Diagnostics
				sm.ChannelIDs, d = types.ListValueFrom(ctx, types.StringType, []types.String{})
				diags.Append(d...)
			}
			stepModels = append(stepModels, sm)
		}
		var d diag.Diagnostics
		model.Escalation, d = types.ListValueFrom(ctx, escalationStepObjectType(), stepModels)
		diags.Append(d...)
	} else {
		model.Escalation = types.ListNull(escalationStepObjectType())
	}

	// Match rules
	if len(dto.MatchRules) > 0 {
		var priorRules []matchRuleModel
		if !model.MatchRules.IsNull() {
			diags.Append(model.MatchRules.ElementsAs(ctx, &priorRules, false)...)
		}
		var ruleModels []matchRuleModel
		for i, mr := range dto.MatchRules {
			val := stringValue(mr.Value)
			// Preserve user-provided casing when it matches case-insensitively
			if i < len(priorRules) && !priorRules[i].Value.IsNull() {
				priorVal := priorRules[i].Value.ValueString()
				if strings.EqualFold(priorVal, val.ValueString()) {
					val = priorRules[i].Value
				}
			}
			rm := matchRuleModel{
				Type:  types.StringValue(string(mr.Type)),
				Value: val,
			}
			rm.Values = ptrStringSliceToList(ctx, mr.Values)
			rm.MonitorIDs = ptrUUIDSliceToList(ctx, mr.MonitorIds)
			rm.Regions = ptrStringSliceToList(ctx, mr.Regions)
			ruleModels = append(ruleModels, rm)
		}
		var d diag.Diagnostics
		model.MatchRules, d = types.ListValueFrom(ctx, matchRuleObjectType(), ruleModels)
		diags.Append(d...)
	} else {
		model.MatchRules = types.ListNull(matchRuleObjectType())
	}

	return diags
}

func (r *NotificationPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state NotificationPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	policy, err := api.Get[generated.NotificationPolicyDto](ctx, r.client, api.NotificationPolicyPath(state.ID.ValueString()))
	if err != nil {
		if api.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		api.AddAPIError(&resp.Diagnostics, "read notification policy", err, path.Root("id"))
		return
	}

	resp.Diagnostics.Append(r.mapToState(ctx, &state, policy)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *NotificationPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan NotificationPolicyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state NotificationPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, err := r.buildUpdateRequest(ctx, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Error building request", err.Error())
		return
	}

	policy, err := api.Update[generated.NotificationPolicyDto](ctx, r.client, api.NotificationPolicyPath(state.ID.ValueString()), body)
	if err != nil {
		api.AddAPIError(&resp.Diagnostics, "update notification policy", err, path.Root("name"))
		return
	}

	resp.Diagnostics.Append(r.mapToState(ctx, &plan, policy)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *NotificationPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state NotificationPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := api.Delete(ctx, r.client, api.NotificationPolicyPath(state.ID.ValueString()))
	if err != nil && !api.IsNotFound(err) {
		api.AddAPIError(&resp.Diagnostics, "delete notification policy", err, path.Root("id"))
	}
}

func (r *NotificationPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	policies, err := api.List[generated.NotificationPolicyDto](ctx, r.client, api.PathNotificationPolicies)
	if err != nil {
		resp.Diagnostics.AddError("Error listing notification policies for import", err.Error())
		return
	}

	// UUID matches are unique. Name matches must be unique within the
	// org or we refuse the import — silently picking the first match
	// would produce a stale or arbitrary state for users who happen to
	// share a policy name across teams or environments.
	var policyID string
	var matchedByName []string
	for _, p := range policies {
		if p.Id.String() == req.ID {
			policyID = p.Id.String()
			matchedByName = nil
			break
		}
		if p.Name == req.ID {
			matchedByName = append(matchedByName, p.Id.String())
		}
	}
	if policyID == "" {
		switch len(matchedByName) {
		case 0:
			resp.Diagnostics.AddError("Notification policy not found", fmt.Sprintf("No notification policy found with name or ID %q", req.ID))
			return
		case 1:
			policyID = matchedByName[0]
		default:
			resp.Diagnostics.AddError(
				"Ambiguous notification policy import",
				fmt.Sprintf("%d notification policies share the name %q (ids: %v). Import by UUID instead.", len(matchedByName), req.ID, matchedByName),
			)
			return
		}
	}

	policy, err := api.Get[generated.NotificationPolicyDto](ctx, r.client, api.NotificationPolicyPath(policyID))
	if err != nil {
		api.AddAPIError(&resp.Diagnostics, "read notification policy for import", err, path.Root("id"))
		return
	}

	// Pre-initialize nested block lists so mapToState populates them.
	model := NotificationPolicyModel{}
	{
		var d diag.Diagnostics
		model.Escalation, d = types.ListValueFrom(ctx, escalationStepObjectType(), []escalationStepModel{})
		resp.Diagnostics.Append(d...)
		model.MatchRules, d = types.ListValueFrom(ctx, matchRuleObjectType(), []matchRuleModel{})
		resp.Diagnostics.Append(d...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	resp.Diagnostics.Append(r.mapToState(ctx, &model, policy)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
