package resources

import (
	"context"
	"fmt"
	"strings"

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
	_ resource.Resource                = &StatusPageComponentGroupResource{}
	_ resource.ResourceWithImportState = &StatusPageComponentGroupResource{}
)

// StatusPageComponentGroupResource manages a single component group on a
// status page as a first-class Terraform resource.
//
// **Why this is a separate resource (not an inline block).**
// Component groups have their own server-side identity (UUID), independent
// lifecycle (create/update/delete), and may be referenced by other resources
// (e.g. `devhelm_status_page_component.group_id`). The canonical Terraform
// pattern for entities matching all three criteria is a standalone resource:
// the TF resource address becomes the stable identity, which lets users
// rename the HCL identifier via the built-in `moved {}` block while
// preserving the server UUID — no `name`-keyed reconciliation tricks
// needed.
type StatusPageComponentGroupResource struct {
	client *api.Client
}

type StatusPageComponentGroupResourceModel struct {
	ID           types.String `tfsdk:"id"`
	StatusPageID types.String `tfsdk:"status_page_id"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	DefaultOpen  types.Bool   `tfsdk:"default_open"`
	DisplayOrder types.Int64  `tfsdk:"display_order"`
}

func NewStatusPageComponentGroupResource() resource.Resource {
	return &StatusPageComponentGroupResource{}
}

func (r *StatusPageComponentGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_status_page_component_group"
}

func (r *StatusPageComponentGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version: 0,
		Description: "Manages a single component group on a DevHelm status page. " +
			"Renames preserve the group UUID (use the built-in moved {} block to rename the resource address).",
		MarkdownDescription: "Manages a single component group on a DevHelm status page.\n\n" +
			"Renames are safe: changing the HCL address preserves the underlying group UUID, " +
			"so subscribers, incident history, and `devhelm_status_page_component.group_id` references stay intact. " +
			"Use Terraform's built-in `moved {}` block to rename the resource address.\n\n" +
			"## Example\n\n" +
			"```hcl\n" +
			"resource \"devhelm_status_page_component_group\" \"infra\" {\n" +
			"  status_page_id = devhelm_status_page.public.id\n" +
			"  name           = \"Infrastructure\"\n" +
			"  description    = \"Core platform services.\"\n" +
			"}\n" +
			"```\n\n" +
			"### Bulk creation with `for_each`\n\n" +
			"```hcl\n" +
			"locals {\n" +
			"  groups = {\n" +
			"    infra = { name = \"Infrastructure\" }\n" +
			"    apps  = { name = \"Applications\" }\n" +
			"  }\n" +
			"}\n" +
			"\n" +
			"resource \"devhelm_status_page_component_group\" \"g\" {\n" +
			"  for_each       = local.groups\n" +
			"  status_page_id = devhelm_status_page.public.id\n" +
			"  name           = each.value.name\n" +
			"}\n" +
			"```\n",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Group UUID assigned by the API",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"status_page_id": schema.StringAttribute{
				Required:    true,
				Description: "ID of the parent status page. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Group display name shown above its components",
			},
			"description": schema.StringAttribute{
				Optional: true,
				Description: "Optional description shown below the group title. " +
					"Omit the attribute to clear an existing description; empty string is rejected " +
					"(use omission instead, since the API normalizes \"\" → null on write).",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"default_open": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				Description: "Initial expand/collapse state on first page load (default: true). " +
					"The renderer may auto-expand a collapsed group when an active incident affects it.",
			},
			"display_order": schema.Int64Attribute{
				Optional: true, Computed: true,
				Description: "Position in the group list (lower = earlier). Server-assigned if omitted.",
			},
		},
	}
}

func (r *StatusPageComponentGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *StatusPageComponentGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan StatusPageComponentGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := generated.CreateStatusPageComponentGroupRequest{
		Name:         plan.Name.ValueString(),
		Description:  stringPtrOrNil(plan.Description),
		DefaultOpen:  boolPtrOrNil(plan.DefaultOpen),
		DisplayOrder: int32PtrOrNil(plan.DisplayOrder),
	}

	created, err := api.Create[generated.StatusPageComponentGroupDto](
		ctx, r.client,
		api.StatusPageGroupsPath(plan.StatusPageID.ValueString()),
		body,
	)
	if err != nil {
		api.AddAPIError(&resp.Diagnostics, "create component group", err, path.Root("name"))
		return
	}

	r.mapToState(&plan, created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *StatusPageComponentGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state StatusPageComponentGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The API exposes individual groups via list-then-filter. We could
	// alternatively add a per-group GET endpoint, but listing is cheap
	// (groups per page are small) and avoids a new endpoint.
	groups, err := api.List[generated.StatusPageComponentGroupDto](
		ctx, r.client,
		api.StatusPageGroupsPath(state.StatusPageID.ValueString()),
	)
	if err != nil {
		if api.IsNotFound(err) {
			// Parent status page is gone; this resource is also gone.
			resp.State.RemoveResource(ctx)
			return
		}
		api.AddAPIError(&resp.Diagnostics, "read component groups", err, path.Root("id"))
		return
	}

	for i := range groups {
		if groups[i].Id.String() == state.ID.ValueString() {
			r.mapToState(&state, &groups[i])
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}
	// Group has been deleted out of band.
	resp.State.RemoveResource(ctx)
}

func (r *StatusPageComponentGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan StatusPageComponentGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state StatusPageComponentGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	body := generated.UpdateStatusPageComponentGroupRequest{
		Name:         &name,
		Description:  descriptionPtrForClear(plan.Description),
		DefaultOpen:  boolPtrOrNil(plan.DefaultOpen),
		DisplayOrder: int32PtrOrNil(plan.DisplayOrder),
	}

	updated, err := api.Update[generated.StatusPageComponentGroupDto](
		ctx, r.client,
		api.StatusPageGroupPath(state.StatusPageID.ValueString(), state.ID.ValueString()),
		body,
	)
	if err != nil {
		api.AddAPIError(&resp.Diagnostics, "update component group", err, path.Root("name"))
		return
	}

	r.mapToState(&plan, updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *StatusPageComponentGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state StatusPageComponentGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := api.Delete(ctx, r.client,
		api.StatusPageGroupPath(state.StatusPageID.ValueString(), state.ID.ValueString()),
	)
	if err != nil && !api.IsNotFound(err) {
		api.AddAPIError(&resp.Diagnostics, "delete component group", err, path.Root("id"))
	}
}

func (r *StatusPageComponentGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import format: "<status_page_id>/<group_id>". The compound ID is
	// required because group UUIDs alone don't tell us which page they
	// belong to (and the API enforces the parent in the URL path).
	parts := strings.SplitN(req.ID, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected '<status_page_id>/<group_id>', got %q", req.ID),
		)
		return
	}
	pageID, groupID := parts[0], parts[1]

	groups, err := api.List[generated.StatusPageComponentGroupDto](
		ctx, r.client,
		api.StatusPageGroupsPath(pageID),
	)
	if err != nil {
		resp.Diagnostics.AddError("Error listing component groups for import", err.Error())
		return
	}

	for i := range groups {
		if groups[i].Id.String() == groupID {
			model := StatusPageComponentGroupResourceModel{
				StatusPageID: types.StringValue(pageID),
			}
			r.mapToState(&model, &groups[i])
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), model.ID)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("status_page_id"), model.StatusPageID)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), model.Name)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("description"), model.Description)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("default_open"), model.DefaultOpen)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("display_order"), model.DisplayOrder)...)
			return
		}
	}

	resp.Diagnostics.AddError(
		"Component group not found",
		fmt.Sprintf("No component group with ID %q on status page %q", groupID, pageID),
	)
}

func (r *StatusPageComponentGroupResource) mapToState(model *StatusPageComponentGroupResourceModel, dto *generated.StatusPageComponentGroupDto) {
	model.ID = types.StringValue(dto.Id.String())
	model.Name = types.StringValue(dto.Name)
	model.Description = stringValueClearable(dto.Description)
	model.DefaultOpen = types.BoolValue(dto.DefaultOpen)
	model.DisplayOrder = types.Int64Value(int64(dto.DisplayOrder))
}
