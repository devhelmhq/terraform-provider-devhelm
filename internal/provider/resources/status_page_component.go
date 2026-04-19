package resources

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// componentStartDateLayout is the wire format the API expects for
// StartDate (ISO-8601 calendar date). We surface the same layout in HCL
// so users can write `start_date = "2024-01-15"` without time-of-day
// noise leaking through.
const componentStartDateLayout = "2006-01-02"

var (
	_ resource.Resource                   = &StatusPageComponentResource{}
	_ resource.ResourceWithImportState    = &StatusPageComponentResource{}
	_ resource.ResourceWithValidateConfig = &StatusPageComponentResource{}
)

// StatusPageComponentResource manages a single component on a status page as
// a first-class Terraform resource (rather than an inline block on the
// parent page). See StatusPageComponentGroupResource for the architectural
// rationale; the same logic applies here.
type StatusPageComponentResource struct {
	client *api.Client
}

type StatusPageComponentResourceModel struct {
	ID                 types.String `tfsdk:"id"`
	StatusPageID       types.String `tfsdk:"status_page_id"`
	Name               types.String `tfsdk:"name"`
	Description        types.String `tfsdk:"description"`
	Type               types.String `tfsdk:"type"`
	GroupID            types.String `tfsdk:"group_id"`
	MonitorID          types.String `tfsdk:"monitor_id"`
	ResourceGroupID    types.String `tfsdk:"resource_group_id"`
	DisplayOrder       types.Int64  `tfsdk:"display_order"`
	ExcludeFromOverall types.Bool   `tfsdk:"exclude_from_overall"`
	ShowUptime         types.Bool   `tfsdk:"show_uptime"`
	StartDate          types.String `tfsdk:"start_date"`
}

func NewStatusPageComponentResource() resource.Resource {
	return &StatusPageComponentResource{}
}

func (r *StatusPageComponentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_status_page_component"
}

func (r *StatusPageComponentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version: 0,
		Description: "Manages a single component on a DevHelm status page. " +
			"Renames preserve the component UUID (use the built-in moved {} block). " +
			"Use group_id to attach to a devhelm_status_page_component_group; omit for ungrouped.",
		MarkdownDescription: "Manages a single component on a DevHelm status page.\n\n" +
			"Each component is a first-class Terraform resource with a stable address and " +
			"server-side UUID; renames preserve identity via the built-in `moved {}` block. " +
			"Set `group_id` to attach the component to a " +
			"[`devhelm_status_page_component_group`](./status_page_component_group); omit it to leave the " +
			"component ungrouped.\n\n" +
			"## Example\n\n" +
			"```hcl\n" +
			"resource \"devhelm_status_page_component\" \"api\" {\n" +
			"  status_page_id = devhelm_status_page.public.id\n" +
			"  group_id       = devhelm_status_page_component_group.infra.id\n" +
			"  name           = \"Public API\"\n" +
			"  type           = \"MONITOR\"\n" +
			"  monitor_id     = devhelm_monitor.api.id\n" +
			"}\n" +
			"```\n\n" +
			"### Bulk creation with `for_each`\n\n" +
			"```hcl\n" +
			"locals {\n" +
			"  services = {\n" +
			"    api     = { name = \"Public API\",        type = \"MONITOR\", monitor_id = devhelm_monitor.api.id }\n" +
			"    website = { name = \"Marketing Site\",    type = \"STATIC\" }\n" +
			"    workers = { name = \"Background Workers\", type = \"GROUP\",   resource_group_id = devhelm_resource_group.workers.id }\n" +
			"  }\n" +
			"}\n" +
			"\n" +
			"resource \"devhelm_status_page_component\" \"svc\" {\n" +
			"  for_each          = local.services\n" +
			"  status_page_id    = devhelm_status_page.public.id\n" +
			"  group_id          = devhelm_status_page_component_group.infra.id\n" +
			"  name              = each.value.name\n" +
			"  type              = each.value.type\n" +
			"  monitor_id        = try(each.value.monitor_id, null)\n" +
			"  resource_group_id = try(each.value.resource_group_id, null)\n" +
			"}\n" +
			"```\n",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Component UUID assigned by the API",
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
				Description: "Component display name shown on the status page",
			},
			"description": schema.StringAttribute{
				Optional: true,
				Description: "Optional description shown when the component is expanded. " +
					"Omit to clear an existing description; empty string is rejected.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"type": schema.StringAttribute{
				Required: true,
				Description: "Component type: STATIC (text-only), MONITOR (driven by a monitor's " +
					"check status), or GROUP (rolls up a resource group). Changing forces replacement.",
				Validators: []validator.String{
					stringvalidator.OneOf(
					string(generated.CreateStatusPageComponentRequestTypeSTATIC),
					string(generated.CreateStatusPageComponentRequestTypeMONITOR),
					string(generated.CreateStatusPageComponentRequestTypeGROUP),
				),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"group_id": schema.StringAttribute{
				Optional:    true,
				Description: "ID of the `devhelm_status_page_component_group` to place this component under; omit to leave ungrouped",
			},
			"monitor_id": schema.StringAttribute{
				Optional:    true,
				Description: "Monitor UUID (required when type=MONITOR). Changing forces replacement.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"resource_group_id": schema.StringAttribute{
				Optional:    true,
				Description: "Resource group UUID (required when type=GROUP). Changing forces replacement.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"display_order": schema.Int64Attribute{
				Optional: true, Computed: true,
				Description: "Position in the component list (lower = earlier). Server-assigned if omitted.",
			},
			"exclude_from_overall": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "Exclude from the overall page status calculation (default: false)",
			},
			"show_uptime": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				Description: "Whether the uptime bar is shown for this component (default: true)",
			},
			"start_date": schema.StringAttribute{
				Optional: true, Computed: true,
				Description: "Date (ISO 8601, YYYY-MM-DD) from which uptime data should be displayed. " +
					"Useful when migrating an existing service onto a status page so historical uptime " +
					"is not shown back to the component's creation. Server-assigned if omitted.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.String{
					componentStartDateValidator{},
				},
			},
		},
	}
}

// componentStartDateValidator enforces the YYYY-MM-DD wire format at
// plan time so users get a clear error before any API call is made,
// rather than a confusing 400 on apply.
type componentStartDateValidator struct{}

func (componentStartDateValidator) Description(_ context.Context) string {
	return "value must be a date in YYYY-MM-DD format (ISO 8601)"
}

func (v componentStartDateValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (componentStartDateValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if _, err := time.Parse(componentStartDateLayout, req.ConfigValue.ValueString()); err != nil {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid start_date format",
			fmt.Sprintf("Expected ISO 8601 date (YYYY-MM-DD), got %q: %s", req.ConfigValue.ValueString(), err),
		)
	}
}

func (r *StatusPageComponentResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model StatusPageComponentResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if model.Type.IsNull() || model.Type.IsUnknown() {
		return
	}

	compType := generated.CreateStatusPageComponentRequestType(model.Type.ValueString())

	switch compType {
	case generated.CreateStatusPageComponentRequestTypeMONITOR:
		if model.MonitorID.IsNull() || model.MonitorID.IsUnknown() {
			resp.Diagnostics.AddAttributeError(
				path.Root("monitor_id"),
				"Missing required attribute",
				"monitor_id is required when component type is MONITOR",
			)
		}
	case generated.CreateStatusPageComponentRequestTypeGROUP:
		if model.ResourceGroupID.IsNull() || model.ResourceGroupID.IsUnknown() {
			resp.Diagnostics.AddAttributeError(
				path.Root("resource_group_id"),
				"Missing required attribute",
				"resource_group_id is required when component type is GROUP",
			)
		}
	}

	if compType != generated.CreateStatusPageComponentRequestTypeMONITOR && !model.MonitorID.IsNull() && !model.MonitorID.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("monitor_id"),
			"Conflicting attribute",
			fmt.Sprintf("monitor_id should not be set when component type is %s", string(compType)),
		)
	}

	if compType != generated.CreateStatusPageComponentRequestTypeGROUP && !model.ResourceGroupID.IsNull() && !model.ResourceGroupID.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("resource_group_id"),
			"Conflicting attribute",
			fmt.Sprintf("resource_group_id should not be set when component type is %s", string(compType)),
		)
	}
}

func (r *StatusPageComponentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// validateTypeRefs enforces the discriminated-union constraint between
// `type` and the corresponding reference attribute. Without this guard a
// MONITOR-typed component with a missing monitor_id would fail server-side
// with a confusing 400; surfacing it during plan gives a clean error.
func (r *StatusPageComponentResource) validateTypeRefs(plan *StatusPageComponentResourceModel, diags *[]string) {
	t := generated.CreateStatusPageComponentRequestType(plan.Type.ValueString())
	switch t {
	case generated.CreateStatusPageComponentRequestTypeMONITOR:
		if plan.MonitorID.IsNull() || plan.MonitorID.ValueString() == "" {
			*diags = append(*diags, "type=MONITOR requires monitor_id to be set")
		}
		if !plan.ResourceGroupID.IsNull() && plan.ResourceGroupID.ValueString() != "" {
			*diags = append(*diags, "type=MONITOR forbids resource_group_id; remove it or change type to GROUP")
		}
	case generated.CreateStatusPageComponentRequestTypeGROUP:
		if plan.ResourceGroupID.IsNull() || plan.ResourceGroupID.ValueString() == "" {
			*diags = append(*diags, "type=GROUP requires resource_group_id to be set")
		}
		if !plan.MonitorID.IsNull() && plan.MonitorID.ValueString() != "" {
			*diags = append(*diags, "type=GROUP forbids monitor_id; remove it or change type to MONITOR")
		}
	case generated.CreateStatusPageComponentRequestTypeSTATIC:
		if !plan.MonitorID.IsNull() && plan.MonitorID.ValueString() != "" {
			*diags = append(*diags, "type=STATIC forbids monitor_id")
		}
		if !plan.ResourceGroupID.IsNull() && plan.ResourceGroupID.ValueString() != "" {
			*diags = append(*diags, "type=STATIC forbids resource_group_id")
		}
	}
}

func (r *StatusPageComponentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan StatusPageComponentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var refErrs []string
	r.validateTypeRefs(&plan, &refErrs)
	if len(refErrs) > 0 {
		resp.Diagnostics.AddError(
			"Invalid component type/reference combination",
			strings.Join(refErrs, "; "),
		)
		return
	}

	startDate, err := componentStartDatePtr(plan.StartDate)
	if err != nil {
		resp.Diagnostics.AddError("Invalid start_date", err.Error())
		return
	}
	body := generated.CreateStatusPageComponentRequest{
		Name:               plan.Name.ValueString(),
		Type:               generated.CreateStatusPageComponentRequestType(plan.Type.ValueString()),
		Description:        stringPtrOrNil(plan.Description),
		ExcludeFromOverall: boolPtrOrNil(plan.ExcludeFromOverall),
		ShowUptime:         boolPtrOrNil(plan.ShowUptime),
		DisplayOrder:       int32PtrOrNil(plan.DisplayOrder),
		StartDate:          startDate,
	}

	groupID, err := parseUUIDPtrChecked(plan.GroupID, "group_id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid group_id", err.Error())
		return
	}
	body.GroupId = groupID

	monitorID, err := parseUUIDPtrChecked(plan.MonitorID, "monitor_id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid monitor_id", err.Error())
		return
	}
	body.MonitorId = monitorID

	resourceGroupID, err := parseUUIDPtrChecked(plan.ResourceGroupID, "resource_group_id")
	if err != nil {
		resp.Diagnostics.AddError("Invalid resource_group_id", err.Error())
		return
	}
	body.ResourceGroupId = resourceGroupID

	created, err := api.Create[generated.StatusPageComponentDto](
		ctx, r.client,
		fmt.Sprintf("/api/v1/status-pages/%s/components", plan.StatusPageID.ValueString()),
		body,
	)
	if err != nil {
		resp.Diagnostics.AddError("Error creating component", err.Error())
		return
	}

	r.mapToState(&plan, created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *StatusPageComponentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state StatusPageComponentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	components, err := api.List[generated.StatusPageComponentDto](
		ctx, r.client,
		fmt.Sprintf("/api/v1/status-pages/%s/components", state.StatusPageID.ValueString()),
	)
	if err != nil {
		if api.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading components", err.Error())
		return
	}

	for i := range components {
		if components[i].Id.String() == state.ID.ValueString() {
			r.mapToState(&state, &components[i])
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *StatusPageComponentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan StatusPageComponentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state StatusPageComponentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var refErrs []string
	r.validateTypeRefs(&plan, &refErrs)
	if len(refErrs) > 0 {
		resp.Diagnostics.AddError(
			"Invalid component type/reference combination",
			strings.Join(refErrs, "; "),
		)
		return
	}

	startDate, err := componentStartDatePtr(plan.StartDate)
	if err != nil {
		resp.Diagnostics.AddError("Invalid start_date", err.Error())
		return
	}
	name := plan.Name.ValueString()
	body := generated.UpdateStatusPageComponentRequest{
		Name:               &name,
		Description:        descriptionPtrForClear(plan.Description),
		ExcludeFromOverall: boolPtrOrNil(plan.ExcludeFromOverall),
		ShowUptime:         boolPtrOrNil(plan.ShowUptime),
		DisplayOrder:       int32PtrOrNil(plan.DisplayOrder),
		StartDate:          startDate,
	}

	// Group movement: explicit `remove_from_group` flag is required to
	// detach (the API distinguishes "leave group field unchanged" from
	// "remove this component from its group" and we have to encode that
	// difference faithfully).
	switch {
	case !plan.GroupID.IsNull() && plan.GroupID.ValueString() != "":
		gid, err := parseUUIDPtrChecked(plan.GroupID, "group_id")
		if err != nil {
			resp.Diagnostics.AddError("Invalid group_id", err.Error())
			return
		}
		body.GroupId = gid
	case !state.GroupID.IsNull() && state.GroupID.ValueString() != "":
		// Plan dropped group_id and state had one → tell the API to detach.
		remove := true
		body.RemoveFromGroup = &remove
	}

	updated, err := api.Update[generated.StatusPageComponentDto](
		ctx, r.client,
		fmt.Sprintf("/api/v1/status-pages/%s/components/%s",
			state.StatusPageID.ValueString(), state.ID.ValueString()),
		body,
	)
	if err != nil {
		resp.Diagnostics.AddError("Error updating component", err.Error())
		return
	}

	r.mapToState(&plan, updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *StatusPageComponentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state StatusPageComponentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := api.Delete(ctx, r.client,
		fmt.Sprintf("/api/v1/status-pages/%s/components/%s",
			state.StatusPageID.ValueString(), state.ID.ValueString()),
	)
	if err != nil && !api.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting component", err.Error())
	}
}

func (r *StatusPageComponentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected '<status_page_id>/<component_id>', got %q", req.ID),
		)
		return
	}
	pageID, componentID := parts[0], parts[1]

	components, err := api.List[generated.StatusPageComponentDto](
		ctx, r.client,
		fmt.Sprintf("/api/v1/status-pages/%s/components", pageID),
	)
	if err != nil {
		resp.Diagnostics.AddError("Error listing components for import", err.Error())
		return
	}

	for i := range components {
		if components[i].Id.String() == componentID {
			model := StatusPageComponentResourceModel{
				StatusPageID: types.StringValue(pageID),
			}
			r.mapToState(&model, &components[i])
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), model.ID)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("status_page_id"), model.StatusPageID)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), model.Name)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("description"), model.Description)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("type"), model.Type)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("group_id"), model.GroupID)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("monitor_id"), model.MonitorID)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("resource_group_id"), model.ResourceGroupID)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("display_order"), model.DisplayOrder)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("exclude_from_overall"), model.ExcludeFromOverall)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("show_uptime"), model.ShowUptime)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("start_date"), model.StartDate)...)
			return
		}
	}

	resp.Diagnostics.AddError(
		"Component not found",
		fmt.Sprintf("No component with ID %q on status page %q", componentID, pageID),
	)
}

func (r *StatusPageComponentResource) mapToState(model *StatusPageComponentResourceModel, dto *generated.StatusPageComponentDto) {
	model.ID = types.StringValue(dto.Id.String())
	model.Name = types.StringValue(dto.Name)
	model.Description = stringValueClearable(dto.Description)
	model.Type = types.StringValue(string(dto.Type))
	model.DisplayOrder = types.Int64Value(int64(dto.DisplayOrder))
	model.ExcludeFromOverall = types.BoolValue(dto.ExcludeFromOverall)
	model.ShowUptime = types.BoolValue(dto.ShowUptime)
	if dto.GroupId != nil {
		model.GroupID = types.StringValue(dto.GroupId.String())
	} else {
		model.GroupID = types.StringNull()
	}
	if dto.MonitorId != nil {
		model.MonitorID = types.StringValue(dto.MonitorId.String())
	} else {
		model.MonitorID = types.StringNull()
	}
	if dto.ResourceGroupId != nil {
		model.ResourceGroupID = types.StringValue(dto.ResourceGroupId.String())
	} else {
		model.ResourceGroupID = types.StringNull()
	}
	if dto.StartDate != nil {
		model.StartDate = types.StringValue(dto.StartDate.Format(componentStartDateLayout))
	} else {
		model.StartDate = types.StringNull()
	}
}

// componentStartDatePtr converts an HCL string ("2024-01-15") into the
// API's openapi_types.Date pointer. Returns (nil, nil) when the value
// is null/unknown so omitting `start_date` preserves the server-side
// value (consistent with the schema's Computed semantics).
func componentStartDatePtr(v types.String) (*openapi_types.Date, error) {
	if v.IsNull() || v.IsUnknown() {
		return nil, nil
	}
	t, err := time.Parse(componentStartDateLayout, v.ValueString())
	if err != nil {
		return nil, fmt.Errorf("expected ISO 8601 date (YYYY-MM-DD), got %q: %w", v.ValueString(), err)
	}
	return &openapi_types.Date{Time: t}, nil
}
