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
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "Suppress individual member alerts when group-level policy handles them",
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
	client, ok := req.ProviderData.(*api.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *api.Client")
		return
	}
	r.client = client
}

func (r *ResourceGroupResource) buildRequest(plan *ResourceGroupModel) (generated.CreateResourceGroupRequest, error) {
	alertPolicyID, err := parseUUIDPtrChecked(plan.AlertPolicyID, "alert_policy_id")
	if err != nil {
		return generated.CreateResourceGroupRequest{}, err
	}
	envID, err := parseUUIDPtrChecked(plan.DefaultEnvironmentID, "default_environment_id")
	if err != nil {
		return generated.CreateResourceGroupRequest{}, err
	}
	channels, err := uuidSliceFromStringListChecked(plan.DefaultAlertChannels, "default_alert_channels")
	if err != nil {
		return generated.CreateResourceGroupRequest{}, err
	}
	return generated.CreateResourceGroupRequest{
		Name:                     plan.Name.ValueString(),
		Description:              stringPtrOrNil(plan.Description),
		AlertPolicyId:            alertPolicyID,
		DefaultFrequency:         int32PtrOrNil(plan.DefaultFrequency),
		DefaultRegions:           stringSliceToPtr(plan.DefaultRegions),
		DefaultAlertChannels:     channels,
		DefaultEnvironmentId:     envID,
		HealthThresholdType:      typedStringPtrOrNil[generated.CreateResourceGroupRequestHealthThresholdType](plan.HealthThresholdType),
		HealthThresholdValue:     float32PtrOrNil(plan.HealthThresholdValue),
		SuppressMemberAlerts:     boolPtrOrNil(plan.SuppressMemberAlerts),
		ConfirmationDelaySeconds: int32PtrOrNil(plan.ConfirmationDelaySeconds),
		RecoveryCooldownMinutes:  int32PtrOrNil(plan.RecoveryCooldownMinutes),
	}, nil
}

// buildUpdateRequest targets the UpdateResourceGroupRequest DTO which uses
// per-field null semantics ("null clears"). TF semantics align: an attribute
// that was previously set and is now removed from config becomes null in the
// plan, and we forward that to the API so the server clears the field.
func (r *ResourceGroupResource) buildUpdateRequest(plan *ResourceGroupModel) (generated.UpdateResourceGroupRequest, error) {
	alertPolicyID, err := parseUUIDPtrChecked(plan.AlertPolicyID, "alert_policy_id")
	if err != nil {
		return generated.UpdateResourceGroupRequest{}, err
	}
	envID, err := parseUUIDPtrChecked(plan.DefaultEnvironmentID, "default_environment_id")
	if err != nil {
		return generated.UpdateResourceGroupRequest{}, err
	}
	channels, err := uuidSliceFromStringListChecked(plan.DefaultAlertChannels, "default_alert_channels")
	if err != nil {
		return generated.UpdateResourceGroupRequest{}, err
	}
	return generated.UpdateResourceGroupRequest{
		Name:                     plan.Name.ValueString(),
		Description:              stringPtrOrNil(plan.Description),
		AlertPolicyId:            alertPolicyID,
		DefaultFrequency:         int32PtrOrNil(plan.DefaultFrequency),
		DefaultRegions:           stringSliceToPtr(plan.DefaultRegions),
		DefaultAlertChannels:     channels,
		DefaultEnvironmentId:     envID,
		HealthThresholdType:      typedStringPtrOrNil[generated.UpdateResourceGroupRequestHealthThresholdType](plan.HealthThresholdType),
		HealthThresholdValue:     float32PtrOrNil(plan.HealthThresholdValue),
		SuppressMemberAlerts:     boolPtrOrNil(plan.SuppressMemberAlerts),
		ConfirmationDelaySeconds: int32PtrOrNil(plan.ConfirmationDelaySeconds),
		RecoveryCooldownMinutes:  int32PtrOrNil(plan.RecoveryCooldownMinutes),
	}, nil
}

func (r *ResourceGroupResource) mapToState(ctx context.Context, model *ResourceGroupModel, dto *generated.ResourceGroupDto) {
	model.ID = types.StringValue(dto.Id.String())
	model.Name = types.StringValue(dto.Name)
	model.Slug = types.StringValue(dto.Slug)
	model.Description = stringValue(dto.Description)
	model.AlertPolicyID = uuidPtrValue(dto.AlertPolicyId)
	model.DefaultFrequency = int32Value(dto.DefaultFrequency)
	model.DefaultEnvironmentID = uuidPtrValue(dto.DefaultEnvironmentId)
	model.HealthThresholdType = typedStringPtrValue(dto.HealthThresholdType)
	model.HealthThresholdValue = float32Value(dto.HealthThresholdValue)
	model.SuppressMemberAlerts = types.BoolValue(dto.SuppressMemberAlerts)
	model.ConfirmationDelaySeconds = int32Value(dto.ConfirmationDelaySeconds)
	model.RecoveryCooldownMinutes = int32Value(dto.RecoveryCooldownMinutes)

	model.DefaultRegions = ptrStringSliceToList(ctx, dto.DefaultRegions)
	model.DefaultAlertChannels = ptrUUIDSliceToList(ctx, dto.DefaultAlertChannels)
}

func (r *ResourceGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ResourceGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, err := r.buildRequest(&plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid resource group configuration", err.Error())
		return
	}
	group, err := api.Create[generated.ResourceGroupDto](ctx, r.client, "/api/v1/resource-groups", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating resource group", err.Error())
		return
	}

	r.mapToState(ctx, &plan, group)
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

	r.mapToState(ctx, &state, group)
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

	body, err := r.buildUpdateRequest(&plan)
	if err != nil {
		resp.Diagnostics.AddError("Invalid resource group configuration", err.Error())
		return
	}
	group, err := api.Update[generated.ResourceGroupDto](ctx, r.client, "/api/v1/resource-groups/"+state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating resource group", err.Error())
		return
	}

	r.mapToState(ctx, &plan, group)
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

	// Accept UUID, slug, or name. UUID and slug are unique per org; name may
	// repeat, so guard against ambiguity when matching by name.
	var matched *generated.ResourceGroupDto
	var matchedByName []*generated.ResourceGroupDto
	for i := range groups {
		g := &groups[i]
		if g.Id.String() == req.ID || g.Slug == req.ID {
			matched = g
			matchedByName = nil
			break
		}
		if g.Name == req.ID {
			matchedByName = append(matchedByName, g)
		}
	}
	if matched == nil {
		switch len(matchedByName) {
		case 0:
			resp.Diagnostics.AddError("Resource group not found", fmt.Sprintf("No resource group found with name, slug, or ID %q", req.ID))
			return
		case 1:
			matched = matchedByName[0]
		default:
			ids := make([]string, len(matchedByName))
			for i, g := range matchedByName {
				ids[i] = g.Id.String()
			}
			resp.Diagnostics.AddError(
				"Ambiguous resource group import",
				fmt.Sprintf("%d resource groups share the name %q (ids: %v). Import by slug or UUID instead.", len(matchedByName), req.ID, ids),
			)
			return
		}
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), matched.Id.String())...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), matched.Name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("slug"), matched.Slug)...)
}
