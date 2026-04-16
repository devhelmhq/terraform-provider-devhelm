package resources

import (
	"context"
	"fmt"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
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
							Required:    true,
							Description: "Rule type (e.g. monitor_name, tag, region)",
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
	r.client = req.ProviderData.(*api.Client)
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
	for _, s := range steps {
		apiSteps = append(apiSteps, generated.EscalationStep{
			ChannelIds:            uuidListToSlice(s.ChannelIDs),
			DelayMinutes:          int32PtrOrNil(s.DelayMinutes),
			RequireAck:            boolPtrOrNil(s.RequireAck),
			RepeatIntervalSeconds: int32PtrOrNil(s.RepeatIntervalSeconds),
		})
	}

	var apiRules []generated.MatchRule
	var rules []matchRuleModel
	plan.MatchRules.ElementsAs(ctx, &rules, false)
	for _, mr := range rules {
		apiRules = append(apiRules, generated.MatchRule{
			Type:       mr.Type.ValueString(),
			Value:      stringPtrOrNil(mr.Value),
			Values:     stringSliceToPtr(mr.Values),
			MonitorIds: uuidSliceFromStringList(mr.MonitorIDs),
			Regions:    stringSliceToPtr(mr.Regions),
		})
	}

	req := &generated.CreateNotificationPolicyRequest{
		Name:     plan.Name.ValueString(),
		Enabled:  boolPtrOrNil(plan.Enabled),
		Priority: int32PtrOrNil(plan.Priority),
		Escalation: generated.EscalationChain{
			Steps:     apiSteps,
			OnResolve: stringPtrOrNil(plan.OnResolve),
			OnReopen:  stringPtrOrNil(plan.OnReopen),
		},
		MatchRules: &apiRules,
	}
	return req, nil
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

	policy, err := api.Create[generated.NotificationPolicyDto](ctx, r.client, "/api/v1/notification-policies", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating notification policy", err.Error())
		return
	}

	plan.ID = types.StringValue(policy.Id.String())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *NotificationPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state NotificationPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	policy, err := api.Get[generated.NotificationPolicyDto](ctx, r.client, "/api/v1/notification-policies/"+state.ID.ValueString())
	if err != nil {
		if api.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading notification policy", err.Error())
		return
	}

	state.Name = types.StringValue(policy.Name)
	state.Enabled = types.BoolValue(policy.Enabled)
	state.Priority = types.Int64Value(int64(policy.Priority))
	state.OnResolve = stringValue(policy.Escalation.OnResolve)
	state.OnReopen = stringValue(policy.Escalation.OnReopen)
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

	body, err := r.buildRequest(ctx, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Error building request", err.Error())
		return
	}

	_, err = api.Update[generated.NotificationPolicyDto](ctx, r.client, "/api/v1/notification-policies/"+state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating notification policy", err.Error())
		return
	}

	plan.ID = state.ID
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *NotificationPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state NotificationPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := api.Delete(ctx, r.client, "/api/v1/notification-policies/"+state.ID.ValueString())
	if err != nil && !api.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting notification policy", err.Error())
	}
}

func (r *NotificationPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	policies, err := api.List[generated.NotificationPolicyDto](ctx, r.client, "/api/v1/notification-policies")
	if err != nil {
		resp.Diagnostics.AddError("Error listing notification policies for import", err.Error())
		return
	}

	for _, p := range policies {
		if p.Name == req.ID {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), p.Id.String())...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), p.Name)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("enabled"), p.Enabled)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("priority"), int64(p.Priority))...)
			return
		}
	}

	resp.Diagnostics.AddError("Notification policy not found", fmt.Sprintf("No notification policy found with name %q", req.ID))
}
