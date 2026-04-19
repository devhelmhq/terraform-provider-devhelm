package resources

import (
	"context"
	"fmt"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
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
		Variables: stringMapToPtr(plan.Variables),
		IsDefault: plan.IsDefault.ValueBool(),
	}

	env, err := api.Create[generated.EnvironmentDto](ctx, r.client, "/api/v1/environments", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating environment", err.Error())
		return
	}

	r.mapToState(ctx, &plan, env)
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

	r.mapToState(ctx, &state, env)
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
		Variables: stringMapToPtr(plan.Variables),
		IsDefault: boolPtrOrNil(plan.IsDefault),
	}

	env, err := api.Update[generated.EnvironmentDto](ctx, r.client, "/api/v1/environments/"+plan.Slug.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating environment", err.Error())
		return
	}

	r.mapToState(ctx, &plan, env)
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
	// The API exposes environments by slug only (`/environments/{slug}`).
	// We optimistically try a direct GET first (covers the common case where
	// the user supplies a slug), and fall back to a list scan if that 404s
	// so users can also import by UUID for round-tripping with dashboard URLs.
	env, err := api.Get[generated.EnvironmentDto](ctx, r.client, "/api/v1/environments/"+api.PathEscape(req.ID))
	if err != nil {
		if !api.IsNotFound(err) {
			resp.Diagnostics.AddError("Error importing environment", err.Error())
			return
		}
		envs, listErr := api.List[generated.EnvironmentDto](ctx, r.client, "/api/v1/environments")
		if listErr != nil {
			resp.Diagnostics.AddError("Error importing environment", listErr.Error())
			return
		}
		for i := range envs {
			e := &envs[i]
			if e.Id.String() == req.ID {
				env = e
				break
			}
		}
		if env == nil {
			resp.Diagnostics.AddError(
				"Environment not found",
				fmt.Sprintf("No environment found with slug or ID %q", req.ID),
			)
			return
		}
	}

	model := EnvironmentResourceModel{}
	// Pre-initialize Variables so mapToState writes a typed value even when
	// the environment has no variables (avoids unknown/null inconsistencies).
	model.Variables, _ = types.MapValueFrom(ctx, types.StringType, map[string]string{})
	r.mapToState(ctx, &model, env)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func (r *EnvironmentResource) mapToState(ctx context.Context, model *EnvironmentResourceModel, dto *generated.EnvironmentDto) {
	model.ID = types.StringValue(dto.Id.String())
	model.Name = types.StringValue(dto.Name)
	model.Slug = types.StringValue(dto.Slug)
	model.IsDefault = types.BoolValue(dto.IsDefault)
	if len(dto.Variables) > 0 {
		elements := make(map[string]string, len(dto.Variables))
		for k, v := range dto.Variables {
			elements[k] = v
		}
		model.Variables, _ = types.MapValueFrom(ctx, types.StringType, elements)
	} else if !model.Variables.IsNull() {
		model.Variables, _ = types.MapValueFrom(ctx, types.StringType, map[string]string{})
	}
}
