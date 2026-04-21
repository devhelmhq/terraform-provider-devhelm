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
	_ resource.Resource                = &TagResource{}
	_ resource.ResourceWithImportState = &TagResource{}
)

type TagResource struct {
	client *api.Client
}

type TagResourceModel struct {
	ID    types.String `tfsdk:"id"`
	Name  types.String `tfsdk:"name"`
	Color types.String `tfsdk:"color"`
}

func NewTagResource() resource.Resource {
	return &TagResource{}
}

func (r *TagResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tag"
}

func (r *TagResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version:     0,
		Description: "Manages a DevHelm tag for organizing monitors.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Unique identifier for this tag",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable name for this tag",
			},
			"color": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Hex color code (e.g. #6B7280). Defaults to grey if omitted",
			},
		},
	}
}

func (r *TagResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *TagResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan TagResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := generated.CreateTagRequest{
		Name:  plan.Name.ValueString(),
		Color: stringPtrOrNil(plan.Color),
	}

	tag, err := api.Create[generated.TagDto](ctx, r.client, api.PathTags, body)
	if err != nil {
		api.AddAPIError(&resp.Diagnostics, "create tag", err, path.Root("name"))
		return
	}

	plan.ID = types.StringValue(tag.Id.String())
	plan.Name = types.StringValue(tag.Name)
	plan.Color = types.StringValue(tag.Color)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *TagResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state TagResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tag, err := api.Get[generated.TagDto](ctx, r.client, api.TagPath(state.ID.ValueString()))
	if err != nil {
		if api.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		api.AddAPIError(&resp.Diagnostics, "read tag", err, path.Root("id"))
		return
	}

	state.Name = types.StringValue(tag.Name)
	state.Color = types.StringValue(tag.Color)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *TagResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan TagResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state TagResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	nameStr := plan.Name.ValueString()
	body := generated.UpdateTagRequest{
		Name:  &nameStr,
		Color: stringPtrOrNil(plan.Color),
	}

	tag, err := api.Update[generated.TagDto](ctx, r.client, api.TagPath(state.ID.ValueString()), body)
	if err != nil {
		api.AddAPIError(&resp.Diagnostics, "update tag", err, path.Root("name"))
		return
	}

	plan.ID = state.ID
	plan.Name = types.StringValue(tag.Name)
	plan.Color = types.StringValue(tag.Color)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *TagResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state TagResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := api.Delete(ctx, r.client, api.TagPath(state.ID.ValueString()))
	if err != nil && !api.IsNotFound(err) {
		api.AddAPIError(&resp.Diagnostics, "delete tag", err, path.Root("id"))
	}
}

func (r *TagResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	tags, err := api.List[generated.TagDto](ctx, r.client, api.PathTags)
	if err != nil {
		api.AddAPIError(&resp.Diagnostics, "list tags for import", err, path.Root("id"))
		return
	}

	// Accept UUID or name. Tag names should be unique within an org but the
	// API does not enforce this for legacy tenants, so guard explicitly.
	var matched *generated.TagDto
	var matchedByName []*generated.TagDto
	for i := range tags {
		t := &tags[i]
		if t.Id.String() == req.ID {
			matched = t
			matchedByName = nil
			break
		}
		if t.Name == req.ID {
			matchedByName = append(matchedByName, t)
		}
	}
	if matched == nil {
		switch len(matchedByName) {
		case 0:
			api.AddNotFoundError(&resp.Diagnostics, "Tag", req.ID)
			return
		case 1:
			matched = matchedByName[0]
		default:
			ids := make([]string, len(matchedByName))
			for i, t := range matchedByName {
				ids[i] = t.Id.String()
			}
			resp.Diagnostics.AddAttributeError(
				path.Root("id"),
				"Ambiguous tag import",
				fmt.Sprintf("%d tags share the name %q (ids: %v). Import by UUID instead.", len(matchedByName), req.ID, ids),
			)
			return
		}
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), matched.Id.String())...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), matched.Name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("color"), matched.Color)...)
}
