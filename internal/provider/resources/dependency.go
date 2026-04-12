package resources

import (
	"context"
	"fmt"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &DependencyResource{}
	_ resource.ResourceWithImportState = &DependencyResource{}
)

type DependencyResource struct {
	client *api.Client
}

type DependencyResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Service          types.String `tfsdk:"service"`
	ServiceName      types.String `tfsdk:"service_name"`
	AlertSensitivity types.String `tfsdk:"alert_sensitivity"`
	ComponentID      types.String `tfsdk:"component_id"`
}

func NewDependencyResource() resource.Resource {
	return &DependencyResource{}
}

func (r *DependencyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dependency"
}

func (r *DependencyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version:     0,
		Description: "Manages a DevHelm service dependency subscription for tracking third-party service health.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, Description: "Subscription identifier",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"service": schema.StringAttribute{
				Required: true, Description: "Service slug to subscribe to",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"service_name": schema.StringAttribute{
				Computed: true, Description: "Human-readable service name",
			},
			"alert_sensitivity": schema.StringAttribute{
				Optional:    true,
				Description: "Alert sensitivity: ALL, INCIDENTS_ONLY, or MAJOR_ONLY",
				Validators: []validator.String{
					stringvalidator.OneOf("ALL", "INCIDENTS_ONLY", "MAJOR_ONLY"),
				},
			},
			"component_id": schema.StringAttribute{
				Optional:    true,
				Description: "Specific component ID to monitor within the service",
			},
		},
	}
}

func (r *DependencyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*api.Client)
}

func (r *DependencyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan DependencyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := generated.ServiceSubscribeRequest{
		AlertSensitivity: stringPtrOrNil(plan.AlertSensitivity),
		ComponentID:      stringPtrOrNil(plan.ComponentID),
	}

	sub, err := api.Create[generated.ServiceSubscriptionDto](
		ctx, r.client,
		"/api/v1/service-subscriptions/"+api.PathEscape(plan.Service.ValueString()),
		body,
	)
	if err != nil {
		resp.Diagnostics.AddError("Error creating dependency", err.Error())
		return
	}

	plan.ID = types.StringValue(sub.SubscriptionID)
	plan.ServiceName = types.StringValue(sub.Name)
	plan.AlertSensitivity = stringValue(sub.AlertSensitivity)
	plan.ComponentID = stringValue(sub.ComponentID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *DependencyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state DependencyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	sub, err := api.Get[generated.ServiceSubscriptionDto](ctx, r.client, "/api/v1/service-subscriptions/"+state.ID.ValueString())
	if err != nil {
		if api.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading dependency", err.Error())
		return
	}

	state.Service = types.StringValue(sub.Slug)
	state.ServiceName = types.StringValue(sub.Name)
	state.AlertSensitivity = stringValue(sub.AlertSensitivity)
	state.ComponentID = stringValue(sub.ComponentID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *DependencyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan DependencyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state DependencyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !plan.AlertSensitivity.Equal(state.AlertSensitivity) {
		body := generated.UpdateAlertSensitivityRequest{
			AlertSensitivity: plan.AlertSensitivity.ValueString(),
		}
		_, err := api.Patch[generated.ServiceSubscriptionDto](
			ctx, r.client,
			"/api/v1/service-subscriptions/"+state.ID.ValueString()+"/alert-sensitivity",
			body,
		)
		if err != nil {
			resp.Diagnostics.AddError("Error updating alert sensitivity", err.Error())
			return
		}
	}

	plan.ID = state.ID
	plan.ServiceName = state.ServiceName
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *DependencyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state DependencyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := api.Delete(ctx, r.client, "/api/v1/service-subscriptions/"+state.ID.ValueString())
	if err != nil && !api.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting dependency", err.Error())
	}
}

func (r *DependencyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	subs, err := api.List[generated.ServiceSubscriptionDto](ctx, r.client, "/api/v1/service-subscriptions")
	if err != nil {
		resp.Diagnostics.AddError("Error listing service subscriptions for import", err.Error())
		return
	}

	for _, s := range subs {
		if s.Slug == req.ID {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), s.SubscriptionID)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("service"), s.Slug)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("service_name"), s.Name)...)
			return
		}
	}

	resp.Diagnostics.AddError("Dependency not found", fmt.Sprintf("No service subscription found with slug %q", req.ID))
}
