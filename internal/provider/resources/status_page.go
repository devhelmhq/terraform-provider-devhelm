package resources

import (
	"context"
	"fmt"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
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
	Branding     types.Object `tfsdk:"branding"`
	PageURL      types.String `tfsdk:"page_url"`
}

// statusPageBrandingModel mirrors generated.StatusPageBranding for use with
// types.Object.As(). All sub-fields are independently optional + computed
// (UseStateForUnknown), so a user can set just one knob in HCL without
// accidentally clearing the rest.
type statusPageBrandingModel struct {
	BrandColor     types.String `tfsdk:"brand_color"`
	TextColor      types.String `tfsdk:"text_color"`
	PageBackground types.String `tfsdk:"page_background"`
	CardBackground types.String `tfsdk:"card_background"`
	BorderColor    types.String `tfsdk:"border_color"`
	Theme          types.String `tfsdk:"theme"`
	HeaderStyle    types.String `tfsdk:"header_style"`
	LogoURL        types.String `tfsdk:"logo_url"`
	FaviconURL     types.String `tfsdk:"favicon_url"`
	ReportURL      types.String `tfsdk:"report_url"`
	CustomCSS      types.String `tfsdk:"custom_css"`
	CustomHeadHTML types.String `tfsdk:"custom_head_html"`
	HidePoweredBy  types.Bool   `tfsdk:"hide_powered_by"`
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
				Required:      true,
				Description:   "URL slug (lowercase, hyphens, globally unique). Changing this forces a new resource.",
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
					stringvalidator.OneOf(
						string(generated.CreateStatusPageRequestVisibilityPUBLIC),
					),
				},
			},
			"enabled": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				Description: "Whether the page is enabled (default: true)",
			},
			"incident_mode": schema.StringAttribute{
				Optional: true, Computed: true, Description: "Incident mode: MANUAL, REVIEW, or AUTOMATIC (default: AUTOMATIC)",
				Validators: []validator.String{
					stringvalidator.OneOf(
						string(generated.CreateStatusPageRequestIncidentModeMANUAL),
						string(generated.CreateStatusPageRequestIncidentModeREVIEW),
						string(generated.CreateStatusPageRequestIncidentModeAUTOMATIC),
					),
				},
			},
			"page_url": schema.StringAttribute{
				Computed: true, Description: "Public URL of the status page (https://<slug>.devhelmstatus.com)",
			},
			"branding": schema.SingleNestedAttribute{
				Optional: true, Computed: true,
				Description: "Visual branding for the public status page. " +
					"The block is **declarative** — every sub-field you list is upserted, " +
					"every sub-field you omit is cleared on the API. To leave branding " +
					"completely untouched, omit the entire `branding` attribute (or set " +
					"it to null — Terraform treats those identically). To reset all " +
					"branding back to defaults, set the attribute to an empty object: " +
					"`branding = {}`.",
				PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
				Attributes: map[string]schema.Attribute{
					"brand_color":      brandingHexAttr("Primary brand color as hex (e.g. #4F46E5); drives accent, links, and buttons"),
					"text_color":       brandingHexAttr("Primary text color as hex (e.g. #09090B)"),
					"page_background":  brandingHexAttr("Page body background color as hex (e.g. #FAFAFA)"),
					"card_background":  brandingHexAttr("Card / surface background color as hex (e.g. #FFFFFF)"),
					"border_color":     brandingHexAttr("Card border color as hex (e.g. #E4E4E7)"),
					"theme":            schema.StringAttribute{Optional: true, Description: "Color theme: 'light' or 'dark' (default: light)"},
					"header_style":     schema.StringAttribute{Optional: true, Description: "Header layout style (reserved for future use)"},
					"logo_url":         schema.StringAttribute{Optional: true, Description: "URL for the logo image displayed in the header"},
					"favicon_url":      schema.StringAttribute{Optional: true, Description: "URL for the browser tab favicon"},
					"report_url":       schema.StringAttribute{Optional: true, Description: "URL where visitors can report a problem"},
					"custom_css":       schema.StringAttribute{Optional: true, Description: "Custom CSS injected via <style> on the public page — grants full style control"},
					"custom_head_html": schema.StringAttribute{Optional: true, Description: "Custom HTML injected into <head> on the public page — grants full script control (analytics, pixels)"},
					"hide_powered_by": schema.BoolAttribute{
						Optional: true, Computed: true,
						Description:   "Whether to hide the 'Powered by DevHelm' footer badge (default: false)",
						PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
					},
				},
			},
		},
	}
}

// brandingHexAttr is a small factory for the many hex-color sub-fields on the
// branding object — they all share the same Optional shape and only their
// description differs. They are intentionally NOT Computed: each sub-field
// is declarative, so omitting it from the `branding` block clears it on the
// API (unless the parent `branding` attribute itself is omitted, in which
// case parent UseStateForUnknown preserves the entire prior object).
func brandingHexAttr(description string) schema.StringAttribute {
	return schema.StringAttribute{
		Optional:    true,
		Description: description,
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

	branding, diags := brandingForCreate(ctx, plan.Branding)
	resp.Diagnostics.Append(diags...)
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
		Branding:     branding,
	}

	page, err := api.Create[generated.StatusPageDto](ctx, r.client, api.PathStatusPages, body)
	if err != nil {
		api.AddAPIError(&resp.Diagnostics, "create status page", err, path.Root("name"))
		return
	}

	resp.Diagnostics.Append(r.mapToState(ctx, &plan, page)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *StatusPageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state StatusPageResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	page, err := api.Get[generated.StatusPageDto](ctx, r.client, api.StatusPagePath(state.ID.ValueString()))
	if err != nil {
		if api.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		api.AddAPIError(&resp.Diagnostics, "read status page", err, path.Root("id"))
		return
	}

	resp.Diagnostics.Append(r.mapToState(ctx, &state, page)...)
	if resp.Diagnostics.HasError() {
		return
	}
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

	branding, diags := brandingForUpdate(ctx, plan.Branding)
	resp.Diagnostics.Append(diags...)
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
		// Branding is non-pointer in the Update DTO so it must always be sent.
		// brandingForUpdate handles three cases:
		//   - plan.Branding fully populated (UseStateForUnknown filled blanks
		//     from prior state) → forward as-is, server upserts each field.
		//   - plan.Branding == null (user explicitly set `branding = null`) →
		//     send a zero-value StatusPageBranding, server clears all fields.
		//   - plan.Branding == unknown (rare, defensive) → also zero-value.
		Branding: &branding,
	}

	page, err := api.Update[generated.StatusPageDto](ctx, r.client, api.StatusPagePath(state.ID.ValueString()), body)
	if err != nil {
		api.AddAPIError(&resp.Diagnostics, "update status page", err, path.Root("name"))
		return
	}

	resp.Diagnostics.Append(r.mapToState(ctx, &plan, page)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *StatusPageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state StatusPageResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := api.Delete(ctx, r.client, api.StatusPagePath(state.ID.ValueString()))
	if err != nil && !api.IsNotFound(err) {
		api.AddAPIError(&resp.Diagnostics, "delete status page", err, path.Root("id"))
	}
}

func (r *StatusPageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Accept either a UUID or a slug as the import identifier.
	//
	// When the identifier parses as a UUID we go straight to the by-id GET.
	// Otherwise (or on 404 fallback) we list and scan by slug. We deliberately
	// do not POST a slug into the by-id endpoint, since the API rejects that
	// at the path-conversion layer with a 400 (not 404), which would otherwise
	// be surfaced as a confusing error.
	var page *generated.StatusPageDto
	if _, parseErr := uuid.Parse(req.ID); parseErr == nil {
		got, err := api.Get[generated.StatusPageDto](ctx, r.client, api.StatusPagePath(req.ID))
		if err != nil && !api.IsNotFound(err) {
			resp.Diagnostics.AddError("Error importing status page", err.Error())
			return
		}
		page = got
	}
	if page == nil {
		pages, listErr := api.List[generated.StatusPageDto](ctx, r.client, api.PathStatusPages)
		if listErr != nil {
			resp.Diagnostics.AddError("Error importing status page", listErr.Error())
			return
		}
		for i := range pages {
			if pages[i].Slug == req.ID || pages[i].Id.String() == req.ID {
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
	resp.Diagnostics.Append(r.mapToState(ctx, &model, page)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), model.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), model.Name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("slug"), model.Slug)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("description"), model.Description)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("visibility"), model.Visibility)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("enabled"), model.Enabled)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("incident_mode"), model.IncidentMode)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("branding"), model.Branding)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("page_url"), model.PageURL)...)
}

// mapToState mirrors a StatusPageDto onto the Terraform model.
// Returns framework diagnostics from object marshaling (END-1141).
func (r *StatusPageResource) mapToState(ctx context.Context, model *StatusPageResourceModel, dto *generated.StatusPageDto) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(dto.Id.String())
	model.Name = types.StringValue(dto.Name)
	model.Slug = types.StringValue(dto.Slug)
	model.Description = stringValueClearable(dto.Description)
	model.Visibility = types.StringValue(string(dto.Visibility))
	model.Enabled = types.BoolValue(dto.Enabled)
	model.IncidentMode = types.StringValue(string(dto.IncidentMode))
	var d diag.Diagnostics
	model.Branding, d = brandingObjectFromDto(ctx, dto.Branding)
	diags.Append(d...)
	// page_url is derived client-side from the slug. The API doesn't return
	// it because the host is dictated by the deployment (devhelmstatus.com)
	// and would otherwise drift across environments.
	model.PageURL = types.StringValue(fmt.Sprintf("https://%s.devhelmstatus.com", dto.Slug))
	return diags
}

// ── Branding conversion helpers ────────────────────────────────────────

func brandingObjectAttrTypes() map[string]attr.Type {
	return map[string]attr.Type{
		"brand_color":      types.StringType,
		"text_color":       types.StringType,
		"page_background":  types.StringType,
		"card_background":  types.StringType,
		"border_color":     types.StringType,
		"theme":            types.StringType,
		"header_style":     types.StringType,
		"logo_url":         types.StringType,
		"favicon_url":      types.StringType,
		"report_url":       types.StringType,
		"custom_css":       types.StringType,
		"custom_head_html": types.StringType,
		"hide_powered_by":  types.BoolType,
	}
}

// brandingObjectFromDto materializes the API's branding payload into a
// types.Object that matches the schema. *string nils round-trip to
// types.StringNull so an HCL omission stays stable across plans.
//
// Returns framework diagnostics so a marshaling failure surfaces to the
// caller's response instead of being silently swallowed (END-1141).
func brandingObjectFromDto(ctx context.Context, b generated.StatusPageBranding) (types.Object, diag.Diagnostics) {
	model := statusPageBrandingModel{
		BrandColor:     stringValue(b.BrandColor),
		TextColor:      stringValue(b.TextColor),
		PageBackground: stringValue(b.PageBackground),
		CardBackground: stringValue(b.CardBackground),
		BorderColor:    stringValue(b.BorderColor),
		Theme:          stringValue(b.Theme),
		HeaderStyle:    stringValue(b.HeaderStyle),
		LogoURL:        stringValue(b.LogoUrl),
		FaviconURL:     stringValue(b.FaviconUrl),
		ReportURL:      stringValue(b.ReportUrl),
		CustomCSS:      stringValue(b.CustomCss),
		CustomHeadHTML: stringValue(b.CustomHeadHtml),
		HidePoweredBy:  boolValue(b.HidePoweredBy),
	}
	return types.ObjectValueFrom(ctx, brandingObjectAttrTypes(), model)
}

// brandingForCreate returns the optional pointer used by CreateStatusPageRequest.
// A null/unknown plan attribute means "no branding overrides" → nil pointer →
// the server uses its built-in defaults for every sub-field.
func brandingForCreate(ctx context.Context, obj types.Object) (*generated.StatusPageBranding, diag.Diagnostics) {
	if obj.IsNull() || obj.IsUnknown() {
		return nil, nil
	}
	b, diags := brandingFromObject(ctx, obj)
	if diags.HasError() {
		return nil, diags
	}
	return &b, diags
}

// brandingForUpdate returns the always-sent branding payload for the Update
// DTO. A null/unknown plan attribute → zero-value StatusPageBranding, which
// instructs the server to clear every overrideable field. UseStateForUnknown
// on the parent attribute means the only path to a null plan value is an
// explicit `branding = null` in HCL.
func brandingForUpdate(ctx context.Context, obj types.Object) (generated.StatusPageBranding, diag.Diagnostics) {
	if obj.IsNull() || obj.IsUnknown() {
		return generated.StatusPageBranding{}, nil
	}
	return brandingFromObject(ctx, obj)
}

func brandingFromObject(ctx context.Context, obj types.Object) (generated.StatusPageBranding, diag.Diagnostics) {
	var model statusPageBrandingModel
	diags := obj.As(ctx, &model, basetypes.ObjectAsOptions{
		UnhandledNullAsEmpty:    false,
		UnhandledUnknownAsEmpty: true, // tolerate transient unknowns from partial updates
	})
	if diags.HasError() {
		return generated.StatusPageBranding{}, diags
	}
	return generated.StatusPageBranding{
		BrandColor:     stringPtrOrNil(model.BrandColor),
		TextColor:      stringPtrOrNil(model.TextColor),
		PageBackground: stringPtrOrNil(model.PageBackground),
		CardBackground: stringPtrOrNil(model.CardBackground),
		BorderColor:    stringPtrOrNil(model.BorderColor),
		Theme:          stringPtrOrNil(model.Theme),
		HeaderStyle:    stringPtrOrNil(model.HeaderStyle),
		LogoUrl:        stringPtrOrNil(model.LogoURL),
		FaviconUrl:     stringPtrOrNil(model.FaviconURL),
		ReportUrl:      stringPtrOrNil(model.ReportURL),
		CustomCss:      stringPtrOrNil(model.CustomCSS),
		CustomHeadHtml: stringPtrOrNil(model.CustomHeadHTML),
		HidePoweredBy:  boolPtrOrNil(model.HidePoweredBy),
	}, diags
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
