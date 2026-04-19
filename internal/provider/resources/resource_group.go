package resources

import (
	"context"
	"fmt"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
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
	DefaultRetryStrategy     types.Object  `tfsdk:"default_retry_strategy"`
	HealthThresholdType      types.String  `tfsdk:"health_threshold_type"`
	HealthThresholdValue     types.Float64 `tfsdk:"health_threshold_value"`
	SuppressMemberAlerts     types.Bool    `tfsdk:"suppress_member_alerts"`
	ConfirmationDelaySeconds types.Int64   `tfsdk:"confirmation_delay_seconds"`
	RecoveryCooldownMinutes  types.Int64   `tfsdk:"recovery_cooldown_minutes"`
}

// retryStrategyModel mirrors generated.RetryStrategy for use with
// types.Object.As(). `type` is required (the API discriminator); interval
// and max_retries are optional integers.
type retryStrategyModel struct {
	Type       types.String `tfsdk:"type"`
	Interval   types.Int64  `tfsdk:"interval"`
	MaxRetries types.Int64  `tfsdk:"max_retries"`
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
				Optional: true, Description: "Default check frequency in seconds for group members (30–86400)",
				Validators: []validator.Int64{
					int64validator.Between(30, 86400),
				},
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
			"default_retry_strategy": schema.SingleNestedAttribute{
				Optional: true, Computed: true,
				Description: "Default retry strategy applied to monitor members of this group when they don't define their own. " +
					"Omit the block (or set to null) to leave the current value untouched (UseStateForUnknown). " +
					"Set to an empty object (`default_retry_strategy = {}`) to clear it back to defaults.",
				PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						Required:    true,
						Description: "Retry strategy kind (e.g. 'fixed' for fixed interval between attempts)",
						Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
					},
					"interval": schema.Int64Attribute{
						Optional:    true,
						Description: "Delay between retry attempts in seconds",
					},
					"max_retries": schema.Int64Attribute{
						Optional:    true,
						Description: "Maximum number of retries after a failed check",
					},
				},
			},
			"health_threshold_type": schema.StringAttribute{
				Optional: true, Description: "Health threshold type: COUNT or PERCENTAGE",
				Validators: []validator.String{
					stringvalidator.OneOf(
					string(generated.CreateResourceGroupRequestHealthThresholdTypeCOUNT),
					string(generated.CreateResourceGroupRequestHealthThresholdTypePERCENTAGE),
				),
				},
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

func (r *ResourceGroupResource) buildRequest(ctx context.Context, plan *ResourceGroupModel) (generated.CreateResourceGroupRequest, diag.Diagnostics) {
	alertPolicyID, err := parseUUIDPtrChecked(plan.AlertPolicyID, "alert_policy_id")
	if err != nil {
		return generated.CreateResourceGroupRequest{}, diagFromErr(err)
	}
	envID, err := parseUUIDPtrChecked(plan.DefaultEnvironmentID, "default_environment_id")
	if err != nil {
		return generated.CreateResourceGroupRequest{}, diagFromErr(err)
	}
	channels, err := uuidSliceFromStringListChecked(plan.DefaultAlertChannels, "default_alert_channels")
	if err != nil {
		return generated.CreateResourceGroupRequest{}, diagFromErr(err)
	}
	retry, diags := retryStrategyFromObject(ctx, plan.DefaultRetryStrategy)
	if diags.HasError() {
		return generated.CreateResourceGroupRequest{}, diags
	}
	return generated.CreateResourceGroupRequest{
		Name:                     plan.Name.ValueString(),
		Description:              stringPtrOrNil(plan.Description),
		AlertPolicyId:            alertPolicyID,
		DefaultFrequency:         int32PtrOrNil(plan.DefaultFrequency),
		DefaultRegions:           stringSliceToPtr(plan.DefaultRegions),
		DefaultAlertChannels:     channels,
		DefaultEnvironmentId:     envID,
		DefaultRetryStrategy:     retry,
		HealthThresholdType:      typedStringPtrOrNil[generated.CreateResourceGroupRequestHealthThresholdType](plan.HealthThresholdType),
		HealthThresholdValue:     float32PtrOrNil(plan.HealthThresholdValue),
		SuppressMemberAlerts:     boolPtrOrNil(plan.SuppressMemberAlerts),
		ConfirmationDelaySeconds: int32PtrOrNil(plan.ConfirmationDelaySeconds),
		RecoveryCooldownMinutes:  int32PtrOrNil(plan.RecoveryCooldownMinutes),
	}, diags
}

// buildUpdateRequest targets the UpdateResourceGroupRequest DTO which uses
// per-field null semantics ("null clears"). TF semantics align: an attribute
// that was previously set and is now removed from config becomes null in the
// plan, and we forward that to the API so the server clears the field.
func (r *ResourceGroupResource) buildUpdateRequest(ctx context.Context, plan *ResourceGroupModel) (generated.UpdateResourceGroupRequest, diag.Diagnostics) {
	alertPolicyID, err := parseUUIDPtrChecked(plan.AlertPolicyID, "alert_policy_id")
	if err != nil {
		return generated.UpdateResourceGroupRequest{}, diagFromErr(err)
	}
	envID, err := parseUUIDPtrChecked(plan.DefaultEnvironmentID, "default_environment_id")
	if err != nil {
		return generated.UpdateResourceGroupRequest{}, diagFromErr(err)
	}
	channels, err := uuidSliceFromStringListChecked(plan.DefaultAlertChannels, "default_alert_channels")
	if err != nil {
		return generated.UpdateResourceGroupRequest{}, diagFromErr(err)
	}
	retry, diags := retryStrategyFromObject(ctx, plan.DefaultRetryStrategy)
	if diags.HasError() {
		return generated.UpdateResourceGroupRequest{}, diags
	}
	return generated.UpdateResourceGroupRequest{
		Name:                 plan.Name.ValueString(),
		Description:          stringPtrOrNil(plan.Description),
		AlertPolicyId:        alertPolicyID,
		DefaultFrequency:     int32PtrOrNil(plan.DefaultFrequency),
		DefaultRegions:       stringSliceToPtr(plan.DefaultRegions),
		DefaultAlertChannels: channels,
		DefaultEnvironmentId: envID,
		// API contract: null clears, missing-from-payload preserves. We
		// always emit (config is the source of truth), so a removed-from-HCL
		// strategy will be cleared on the server.
		DefaultRetryStrategy:     retry,
		HealthThresholdType:      typedStringPtrOrNil[generated.UpdateResourceGroupRequestHealthThresholdType](plan.HealthThresholdType),
		HealthThresholdValue:     float32PtrOrNil(plan.HealthThresholdValue),
		SuppressMemberAlerts:     boolPtrOrNil(plan.SuppressMemberAlerts),
		ConfirmationDelaySeconds: int32PtrOrNil(plan.ConfirmationDelaySeconds),
		RecoveryCooldownMinutes:  int32PtrOrNil(plan.RecoveryCooldownMinutes),
	}, diags
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
	model.DefaultRetryStrategy = retryStrategyObjectFromDto(ctx, dto.DefaultRetryStrategy)
}

// ── Retry strategy conversion helpers ───────────────────────────────────

func retryStrategyObjectAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"type":        types.StringType,
		"interval":    types.Int64Type,
		"max_retries": types.Int64Type,
	}
}

// retryStrategyObjectFromDto converts the DTO's RetryStrategy into a TF object.
// An "empty" DTO (zero-value Type) is treated as "no strategy configured" and
// rendered as types.ObjectNull so HCL omission stays stable across plans.
func retryStrategyObjectFromDto(ctx context.Context, rs *generated.RetryStrategy) types.Object {
	if rs == nil || (rs.Type == "" && rs.Interval == 0 && rs.MaxRetries == 0) {
		return types.ObjectNull(retryStrategyObjectAttrTypes())
	}
	model := retryStrategyModel{
		Type:       types.StringValue(rs.Type),
		Interval:   types.Int64Value(int64(rs.Interval)),
		MaxRetries: types.Int64Value(int64(rs.MaxRetries)),
	}
	obj, _ := types.ObjectValueFrom(ctx, retryStrategyObjectAttrTypes(), model)
	return obj
}

// retryStrategyFromObject converts the TF object into the optional pointer used
// by both the Create and Update DTOs. A null/unknown plan value → nil pointer
// (omits the field on Create; instructs the API to clear on Update per the
// "null clears" contract).
func retryStrategyFromObject(ctx context.Context, obj types.Object) (*generated.RetryStrategy, diag.Diagnostics) {
	if obj.IsNull() || obj.IsUnknown() {
		return nil, nil
	}
	var model retryStrategyModel
	diags := obj.As(ctx, &model, basetypes.ObjectAsOptions{
		UnhandledNullAsEmpty:    false,
		UnhandledUnknownAsEmpty: true,
	})
	if diags.HasError() {
		return nil, diags
	}
	return &generated.RetryStrategy{
		Type:       model.Type.ValueString(),
		Interval:   int32OrZero(model.Interval),
		MaxRetries: int32OrZero(model.MaxRetries),
	}, diags
}

// diagFromErr lifts a plain Go error into the framework's diagnostics
// container so the request-builder helpers can return diagnostics uniformly.
func diagFromErr(err error) diag.Diagnostics {
	var d diag.Diagnostics
	d.AddError("Invalid resource group configuration", err.Error())
	return d
}

func (r *ResourceGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ResourceGroupModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := r.buildRequest(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
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

	body, diags := r.buildUpdateRequest(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
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
