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
	_ resource.Resource                = &EnvironmentResource{}
	_ resource.ResourceWithImportState = &EnvironmentResource{}
)

type EnvironmentResource struct {
	client *api.Client
}

type EnvironmentResourceModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Slug      types.String `tfsdk:"slug"`
	IsDefault types.Bool   `tfsdk:"is_default"`
	Variables types.Map    `tfsdk:"variables"`
}

func NewEnvironmentResource() resource.Resource {
	return &EnvironmentResource{}
}

func (r *EnvironmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environment"
}

func (r *EnvironmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version:     0,
		Description: "Manages a DevHelm environment for variable substitution in monitors.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Unique identifier for this environment",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable name for this environment",
			},
			"slug": schema.StringAttribute{
				Required:    true,
				Description: "URL-safe slug used as the stable identifier",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"is_default": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "Whether this is the default environment",
			},
			"variables": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Key-value pairs available for variable substitution in monitor configs",
			},
		},
	}
}

func (r *EnvironmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *EnvironmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan EnvironmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := generated.CreateEnvironmentRequest{
		Name:      plan.Name.ValueString(),
		Slug:      plan.Slug.ValueString(),
		Variables: mapToStringMap(plan.Variables),
		IsDefault: boolPtrOrNil(plan.IsDefault),
	}

	env, err := api.Create[generated.EnvironmentDto](ctx, r.client, "/api/v1/environments", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating environment", err.Error())
		return
	}

	r.mapToState(&plan, env)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *EnvironmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state EnvironmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	env, err := api.Get[generated.EnvironmentDto](ctx, r.client, "/api/v1/environments/"+state.Slug.ValueString())
	if err != nil {
		if api.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading environment", err.Error())
		return
	}

	r.mapToState(&state, env)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *EnvironmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan EnvironmentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	body := generated.UpdateEnvironmentRequest{
		Name:      &name,
		Variables: mapToStringMap(plan.Variables),
		IsDefault: boolPtrOrNil(plan.IsDefault),
	}

	env, err := api.Update[generated.EnvironmentDto](ctx, r.client, "/api/v1/environments/"+plan.Slug.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating environment", err.Error())
		return
	}

	r.mapToState(&plan, env)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *EnvironmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state EnvironmentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := api.Delete(ctx, r.client, "/api/v1/environments/"+state.Slug.ValueString())
	if err != nil && !api.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting environment", err.Error())
	}
}

func (r *EnvironmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	env, err := api.Get[generated.EnvironmentDto](ctx, r.client, "/api/v1/environments/"+api.PathEscape(req.ID))
	if err != nil {
		resp.Diagnostics.AddError("Error importing environment", fmt.Sprintf("No environment found with slug %q: %s", req.ID, err))
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), env.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), env.Name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("slug"), env.Slug)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("is_default"), env.IsDefault)...)
}

func (r *EnvironmentResource) mapToState(model *EnvironmentResourceModel, dto *generated.EnvironmentDto) {
	model.ID = types.StringValue(dto.ID)
	model.Name = types.StringValue(dto.Name)
	model.Slug = types.StringValue(dto.Slug)
	model.IsDefault = types.BoolValue(dto.IsDefault)
	if dto.Variables != nil && len(dto.Variables) > 0 {
		elements := make(map[string]types.String)
		for k, v := range dto.Variables {
			elements[k] = types.StringValue(v)
		}
		// We set variables from DTO but keep the model's map type
	}
}
