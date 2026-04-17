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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &StatusPageResource{}
	_ resource.ResourceWithImportState = &StatusPageResource{}
)

// StatusPageResource manages a DevHelm status page.
//
// **Children are separate resources.** Component groups and components live
// in their own resources (`devhelm_status_page_component_group` and
// `devhelm_status_page_component`). This mirrors the API's resource model
// — each child has its own UUID, lifecycle, and `/components/{id}` endpoint
// — and lets users rename child entries via Terraform's built-in `moved {}`
// block without losing identity, or use `for_each` to attach components in
// bulk. The previous inline-block design has been removed.
type StatusPageResource struct {
	client *api.Client
}

type StatusPageResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Slug         types.String `tfsdk:"slug"`
	Description  types.String `tfsdk:"description"`
	Visibility   types.String `tfsdk:"visibility"`
	Enabled      types.Bool   `tfsdk:"enabled"`
	IncidentMode types.String `tfsdk:"incident_mode"`
	PageURL      types.String `tfsdk:"page_url"`
}

func NewStatusPageResource() resource.Resource {
	return &StatusPageResource{}
}

func (r *StatusPageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_status_page"
}

func (r *StatusPageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version: 0,
		Description: "Manages a DevHelm status page. " +
			"Component groups and components are managed as separate resources " +
			"(devhelm_status_page_component_group, devhelm_status_page_component); " +
			"this resource only owns the page-level configuration.",
		MarkdownDescription: "Manages a DevHelm status page.\n\n" +
			"This resource owns **page-level configuration only** (name, slug, visibility, " +
			"incident mode, branding). Children are first-class resources:\n\n" +
			"- [`devhelm_status_page_component_group`](./status_page_component_group) " +
			"— groupings shown on the page\n" +
			"- [`devhelm_status_page_component`](./status_page_component) " +
			"— individual components (static, monitor-backed, or resource-group-backed)\n\n" +
			"Splitting children into their own resources gives them stable Terraform addresses, " +
			"so renames preserve the underlying API UUID via the built-in `moved {}` block " +
			"(no destructive delete/recreate, no loss of incident history or subscribers). " +
			"It also unlocks `for_each`, `-target`, and cross-resource references.\n\n" +
			"## Example\n\n" +
			"```hcl\n" +
			"resource \"devhelm_status_page\" \"public\" {\n" +
			"  name          = \"Acme Status\"\n" +
			"  slug          = \"acme\"\n" +
			"  description   = \"Live status of Acme services.\"\n" +
			"  visibility    = \"PUBLIC\"\n" +
			"  incident_mode = \"AUTOMATIC\"\n" +
			"}\n" +
			"\n" +
			"resource \"devhelm_status_page_component_group\" \"infra\" {\n" +
			"  status_page_id = devhelm_status_page.public.id\n" +
			"  name           = \"Infrastructure\"\n" +
			"}\n" +
			"```\n",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, Description: "Unique identifier for this status page",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required: true, Description: "Human-readable name for this status page",
			},
			"slug": schema.StringAttribute{
				Required:    true,
				Description: "URL slug (lowercase, hyphens, globally unique). Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description": schema.StringAttribute{
				Optional: true,
				Description: "Description shown below the page header. " +
					"Omit to clear an existing description; empty string is rejected.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"visibility": schema.StringAttribute{
				Optional: true, Computed: true,
				Description: "Page visibility. Only PUBLIC is currently supported (default: PUBLIC)",
				Validators: []validator.String{
					stringvalidator.OneOf("PUBLIC"),
				},
			},
			"enabled": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				Description: "Whether the page is enabled (default: true)",
			},
			"incident_mode": schema.StringAttribute{
				Optional: true, Computed: true, Description: "Incident mode: MANUAL, REVIEW, or AUTOMATIC (default: AUTOMATIC)",
				Validators: []validator.String{
					stringvalidator.OneOf("MANUAL", "REVIEW", "AUTOMATIC"),
				},
			},
			"page_url": schema.StringAttribute{
				Computed: true, Description: "Public URL of the status page (https://<slug>.devhelm.page)",
			},
		},
	}
}

func (r *StatusPageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *StatusPageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan StatusPageResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := generated.CreateStatusPageRequest{
		Name:         plan.Name.ValueString(),
		Slug:         plan.Slug.ValueString(),
		Description:  stringPtrOrNil(plan.Description),
		Visibility:   visibilityCreatePtr(plan.Visibility),
		Enabled:      boolPtrOrNil(plan.Enabled),
		IncidentMode: incidentModeCreatePtr(plan.IncidentMode),
	}

	page, err := api.Create[generated.StatusPageDto](ctx, r.client, "/api/v1/status-pages", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating status page", err.Error())
		return
	}

	r.mapToState(&plan, page)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *StatusPageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state StatusPageResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	page, err := api.Get[generated.StatusPageDto](ctx, r.client, "/api/v1/status-pages/"+state.ID.ValueString())
	if err != nil {
		if api.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading status page", err.Error())
		return
	}

	r.mapToState(&state, page)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *StatusPageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan StatusPageResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state StatusPageResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	body := generated.UpdateStatusPageRequest{
		Name: &name,
		// Description follows the API's "null preserves, empty string clears"
		// contract. We always emit so removing the attribute from HCL clears
		// the server-side value (the Terraform contract is "config is the
		// source of truth"). See helpers.go::descriptionPtrForClear.
		Description:  descriptionPtrForClear(plan.Description),
		Visibility:   visibilityUpdatePtr(plan.Visibility),
		Enabled:      boolPtrOrNil(plan.Enabled),
		IncidentMode: incidentModeUpdatePtr(plan.IncidentMode),
	}

	page, err := api.Update[generated.StatusPageDto](ctx, r.client, "/api/v1/status-pages/"+state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating status page", err.Error())
		return
	}

	r.mapToState(&plan, page)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *StatusPageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state StatusPageResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := api.Delete(ctx, r.client, "/api/v1/status-pages/"+state.ID.ValueString())
	if err != nil && !api.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting status page", err.Error())
	}
}

func (r *StatusPageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Try direct ID lookup first; fall back to slug list scan if that 404s,
	// so users can import either way.
	page, err := api.Get[generated.StatusPageDto](ctx, r.client, "/api/v1/status-pages/"+req.ID)
	if err != nil {
		if !api.IsNotFound(err) {
			resp.Diagnostics.AddError("Error importing status page", err.Error())
			return
		}
		pages, listErr := api.List[generated.StatusPageDto](ctx, r.client, "/api/v1/status-pages")
		if listErr != nil {
			resp.Diagnostics.AddError("Error importing status page", listErr.Error())
			return
		}
		for i := range pages {
			if pages[i].Slug == req.ID {
				page = &pages[i]
				break
			}
		}
		if page == nil {
			resp.Diagnostics.AddError(
				"Status page not found",
				fmt.Sprintf("No status page with ID or slug %q", req.ID),
			)
			return
		}
	}

	model := StatusPageResourceModel{}
	r.mapToState(&model, page)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), model.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), model.Name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("slug"), model.Slug)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("description"), model.Description)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("visibility"), model.Visibility)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("enabled"), model.Enabled)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("incident_mode"), model.IncidentMode)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("page_url"), model.PageURL)...)
}

func (r *StatusPageResource) mapToState(model *StatusPageResourceModel, dto *generated.StatusPageDto) {
	model.ID = types.StringValue(dto.Id.String())
	model.Name = types.StringValue(dto.Name)
	model.Slug = types.StringValue(dto.Slug)
	model.Description = stringValueClearable(dto.Description)
	model.Visibility = types.StringValue(string(dto.Visibility))
	model.Enabled = types.BoolValue(dto.Enabled)
	model.IncidentMode = types.StringValue(string(dto.IncidentMode))
	// page_url is derived client-side from the slug. The API doesn't return
	// it because the host is dictated by the deployment (devhelm.page) and
	// would otherwise drift across environments.
	model.PageURL = types.StringValue(fmt.Sprintf("https://%s.devhelm.page", dto.Slug))
}

// ── Enum conversion helpers (visibility, incident mode) ─────────────────

func visibilityCreatePtr(v types.String) *generated.CreateStatusPageRequestVisibility {
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		return nil
	}
	out := generated.CreateStatusPageRequestVisibility(v.ValueString())
	return &out
}

func visibilityUpdatePtr(v types.String) *generated.UpdateStatusPageRequestVisibility {
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		return nil
	}
	out := generated.UpdateStatusPageRequestVisibility(v.ValueString())
	return &out
}

func incidentModeCreatePtr(v types.String) *generated.CreateStatusPageRequestIncidentMode {
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		return nil
	}
	out := generated.CreateStatusPageRequestIncidentMode(v.ValueString())
	return &out
}

func incidentModeUpdatePtr(v types.String) *generated.UpdateStatusPageRequestIncidentMode {
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		return nil
	}
	out := generated.UpdateStatusPageRequestIncidentMode(v.ValueString())
	return &out
}
