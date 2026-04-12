package resources

import (
	"context"
	"fmt"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &ResourceGroupResource{}
	_ resource.ResourceWithImportState = &ResourceGroupResource{}
)

type ResourceGroupResource struct {
	client *api.Client
}

type ResourceGroupModel struct {
	ID                       types.String  `tfsdk:"id"`
	Name                     types.String  `tfsdk:"name"`
	Slug                     types.String  `tfsdk:"slug"`
	Description              types.String  `tfsdk:"description"`
	AlertPolicyID            types.String  `tfsdk:"alert_policy_id"`
	DefaultFrequency         types.Int64   `tfsdk:"default_frequency"`
	DefaultRegions           types.List    `tfsdk:"default_regions"`
	DefaultAlertChannels     types.List    `tfsdk:"default_alert_channels"`
	DefaultEnvironmentID     types.String  `tfsdk:"default_environment_id"`
	HealthThresholdType      types.String  `tfsdk:"health_threshold_type"`
	HealthThresholdValue     types.Float64 `tfsdk:"health_threshold_value"`
	SuppressMemberAlerts     types.Bool    `tfsdk:"suppress_member_alerts"`
	ConfirmationDelaySeconds types.Int64   `tfsdk:"confirmation_delay_seconds"`
	RecoveryCooldownMinutes  types.Int64   `tfsdk:"recovery_cooldown_minutes"`
}

func NewResourceGroupResource() resource.Resource {
	return &ResourceGroupResource{}
}

func (r *ResourceGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_resource_group"
}

func (r *ResourceGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version:     0,
		Description: "Manages a DevHelm resource group for organizing monitors with shared defaults.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, Description: "Unique identifier",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required: true, Description: "Human-readable name for this resource group",
			},
			"slug": schema.StringAttribute{
				Computed: true, Description: "URL-safe slug (auto-generated from name)",
			},
			"description": schema.StringAttribute{
				Optional: true, Description: "Description of this resource group",
			},
			"alert_policy_id": schema.StringAttribute{
				Optional: true, Description: "Notification policy ID for group-level alerts",
			},
			"default_frequency": schema.Int64Attribute{
				Optional: true, Description: "Default check frequency in seconds for group members",
			},
			"default_regions": schema.ListAttribute{
				Optional: true, ElementType: types.StringType,
				Description: "Default probe regions for group members",
			},
			"default_alert_channels": schema.ListAttribute{
				Optional: true, ElementType: types.StringType,
				Description: "Default alert channel IDs for group members",
			},
			"default_environment_id": schema.StringAttribute{
				Optional: true, Description: "Default environment ID for group members",
			},
			"health_threshold_type": schema.StringAttribute{
				Optional: true, Description: "Health threshold type: COUNT or PERCENTAGE",
			},
			"health_threshold_value": schema.Float64Attribute{
				Optional: true, Description: "Health threshold value",
			},
			"suppress_member_alerts": schema.BoolAttribute{
				Optional: true, Description: "Suppress individual member alerts when group-level policy handles them",
			},
			"confirmation_delay_seconds": schema.Int64Attribute{
				Optional: true, Description: "Seconds to wait before confirming a group incident",
			},
			"recovery_cooldown_minutes": schema.Int64Attribute{
				Optional: true, Description: "Minutes to wait before auto-resolving a group incident",
			},
		},
	}
}

func (r *ResourceGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*api.Client)
}

func (r *ResourceGroupResource) buildRequest(plan *ResourceGroupModel) generated.CreateResourceGroupRequest {
	return generated.CreateResourceGroupRequest{
		Name:                     plan.Name.ValueString(),
		Description:              stringPtrOrNil(plan.Description),
		AlertPolicyID:            stringPtrOrNil(plan.AlertPolicyID),
		DefaultFrequency:         intPtrOrNil(plan.DefaultFrequency),
		DefaultRegions:           stringListToSlice(plan.DefaultRegions),
		DefaultAlertChannels:     stringListToSlice(plan.DefaultAlertChannels),
		DefaultEnvironmentID:     stringPtrOrNil(plan.DefaultEnvironmentID),
		HealthThresholdType:      stringPtrOrNil(plan.HealthThresholdType),
		HealthThresholdValue:     float64PtrOrNil(plan.HealthThresholdValue),
		SuppressMemberAlerts:     boolPtrOrNil(plan.SuppressMemberAlerts),
		ConfirmationDelaySeconds: intPtrOrNil(plan.ConfirmationDelaySeconds),
		RecoveryCooldownMinutes:  intPtrOrNil(plan.RecoveryCooldownMinutes),
	}
}

func (r *ResourceGroupResource) mapToState(model *ResourceGroupModel, dto *generated.ResourceGroupDto) {
	model.ID = types.StringValue(dto.ID)
	model.Name = types.StringValue(dto.Name)
	model.Slug = types.StringValue(dto.Slug)
	model.Description = stringValue(dto.Description)
	model.AlertPolicyID = stringValue(dto.AlertPolicyID)
	model.DefaultFrequency = intValue(dto.DefaultFrequency)
	model.DefaultEnvironmentID = stringValue(dto.DefaultEnvironmentID)
	model.HealthThresholdType = stringValue(dto.HealthThresholdType)
	model.HealthThresholdValue = float64Value(dto.HealthThresholdValue)
	model.SuppressMemberAlerts = boolValue(dto.SuppressMemberAlerts)
	model.ConfirmationDelaySeconds = intValue(dto.ConfirmationDelaySeconds)
	model.RecoveryCooldownMinutes = intValue(dto.RecoveryCooldownMinutes)
}

func (r *ResourceGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ResourceGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := r.buildRequest(&plan)
	group, err := api.Create[generated.ResourceGroupDto](ctx, r.client, "/api/v1/resource-groups", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating resource group", err.Error())
		return
	}

	r.mapToState(&plan, group)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ResourceGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ResourceGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	group, err := api.Get[generated.ResourceGroupDto](ctx, r.client, "/api/v1/resource-groups/"+state.ID.ValueString())
	if err != nil {
		if api.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading resource group", err.Error())
		return
	}

	r.mapToState(&state, group)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ResourceGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ResourceGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state ResourceGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := r.buildRequest(&plan)
	group, err := api.Update[generated.ResourceGroupDto](ctx, r.client, "/api/v1/resource-groups/"+state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating resource group", err.Error())
		return
	}

	r.mapToState(&plan, group)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ResourceGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ResourceGroupModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := api.Delete(ctx, r.client, "/api/v1/resource-groups/"+state.ID.ValueString())
	if err != nil && !api.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting resource group", err.Error())
	}
}

func (r *ResourceGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	groups, err := api.List[generated.ResourceGroupDto](ctx, r.client, "/api/v1/resource-groups")
	if err != nil {
		resp.Diagnostics.AddError("Error listing resource groups for import", err.Error())
		return
	}

	for _, g := range groups {
		if g.Name == req.ID {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), g.ID)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), g.Name)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("slug"), g.Slug)...)
			return
		}
	}

	resp.Diagnostics.AddError("Resource group not found", fmt.Sprintf("No resource group found with name %q", req.ID))
}
