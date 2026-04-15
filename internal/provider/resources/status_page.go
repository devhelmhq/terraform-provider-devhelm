package resources

import (
	"context"
	"fmt"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
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

	ComponentGroups []StatusPageComponentGroupModel `tfsdk:"component_group"`
	Components      []StatusPageComponentModel      `tfsdk:"component"`
}

type StatusPageComponentGroupModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	Collapsed    types.Bool   `tfsdk:"collapsed"`
	DisplayOrder types.Int64  `tfsdk:"display_order"`
}

type StatusPageComponentModel struct {
	ID                 types.String `tfsdk:"id"`
	Name               types.String `tfsdk:"name"`
	Description        types.String `tfsdk:"description"`
	Type               types.String `tfsdk:"type"`
	GroupName          types.String `tfsdk:"group_name"`
	MonitorID          types.String `tfsdk:"monitor_id"`
	ResourceGroupID    types.String `tfsdk:"resource_group_id"`
	DisplayOrder       types.Int64  `tfsdk:"display_order"`
	ExcludeFromOverall types.Bool   `tfsdk:"exclude_from_overall"`
	ShowUptime         types.Bool   `tfsdk:"show_uptime"`
}

func NewStatusPageResource() resource.Resource {
	return &StatusPageResource{}
}

func (r *StatusPageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_status_page"
}

func (r *StatusPageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version:     0,
		Description: "Manages a DevHelm status page with component groups and components.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, Description: "Unique identifier for this status page",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required: true, Description: "Human-readable name for this status page",
			},
			"slug": schema.StringAttribute{
				Required: true, Description: "URL slug (lowercase, hyphens, globally unique)",
			},
			"description": schema.StringAttribute{
				Optional: true, Description: "Description shown below the page header",
			},
			"visibility": schema.StringAttribute{
				Optional: true, Computed: true, Description: "Page visibility: PUBLIC (default: PUBLIC)",
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
		Blocks: map[string]schema.Block{
			"component_group": schema.ListNestedBlock{
				Description: "Component groups for visual organization. Components reference groups by name.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed: true, Description: "Group ID (set after creation)",
							PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
						},
						"name": schema.StringAttribute{
							Required: true, Description: "Group display name",
						},
						"description": schema.StringAttribute{
							Optional: true, Description: "Optional group description",
						},
						"collapsed": schema.BoolAttribute{
							Optional: true, Computed: true, Default: booldefault.StaticBool(true),
							Description: "Whether the group is collapsed by default (default: true)",
						},
						"display_order": schema.Int64Attribute{
							Optional: true, Computed: true, Description: "Position in the group list",
						},
					},
				},
			},
			"component": schema.ListNestedBlock{
				Description: "Components that appear on the status page.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed: true, Description: "Component ID (set after creation)",
							PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
						},
						"name": schema.StringAttribute{
							Required: true, Description: "Component display name",
						},
						"description": schema.StringAttribute{
							Optional: true, Description: "Optional description shown on expand",
						},
						"type": schema.StringAttribute{
							Required: true, Description: "Component type: STATIC, MONITOR, or GROUP",
							Validators: []validator.String{
								stringvalidator.OneOf("STATIC", "MONITOR", "GROUP"),
							},
						},
						"group_name": schema.StringAttribute{
							Optional: true, Description: "Name of the component_group to place this component in",
						},
						"monitor_id": schema.StringAttribute{
							Optional: true, Description: "Monitor ID (required when type=MONITOR)",
						},
						"resource_group_id": schema.StringAttribute{
							Optional: true, Description: "Resource group ID (required when type=GROUP)",
						},
						"display_order": schema.Int64Attribute{
							Optional: true, Computed: true, Description: "Position in the component list",
						},
						"exclude_from_overall": schema.BoolAttribute{
							Optional: true, Computed: true, Default: booldefault.StaticBool(false),
							Description: "Exclude from overall status calculation (default: false)",
						},
						"show_uptime": schema.BoolAttribute{
							Optional: true, Computed: true, Default: booldefault.StaticBool(true),
							Description: "Whether to show the uptime bar (default: true)",
						},
					},
				},
			},
		},
	}
}

func (r *StatusPageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*api.Client)
}

// ── CRUD ────────────────────────────────────────────────────────────────

func (r *StatusPageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan StatusPageResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := r.buildCreateRequest(&plan)
	page, err := api.Create[generated.StatusPageDto](ctx, r.client, "/api/v1/status-pages", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating status page", err.Error())
		return
	}

	pageID := page.Id.String()

	groupNameToID := make(map[string]string)
	for i, g := range plan.ComponentGroups {
		gBody := generated.CreateStatusPageComponentGroupRequest{
			Name:        g.Name.ValueString(),
			Description: stringPtrOrNil(g.Description),
			Collapsed:   boolPtrOrNil(g.Collapsed),
			DisplayOrder: func() *int32 {
				if g.DisplayOrder.IsNull() || g.DisplayOrder.IsUnknown() {
					return nil
				}
				v := int32(g.DisplayOrder.ValueInt64())
				return &v
			}(),
		}
		created, err := api.Create[generated.StatusPageComponentGroupDto](
			ctx, r.client, fmt.Sprintf("/api/v1/status-pages/%s/groups", pageID), gBody,
		)
		if err != nil {
			resp.Diagnostics.AddError("Error creating component group", err.Error())
			return
		}
		plan.ComponentGroups[i].ID = types.StringValue(created.Id.String())
		plan.ComponentGroups[i].DisplayOrder = types.Int64Value(int64(created.DisplayOrder))
		groupNameToID[created.Name] = created.Id.String()
	}

	for i, c := range plan.Components {
		cBody := r.buildComponentRequest(&c, groupNameToID)
		created, err := api.Create[generated.StatusPageComponentDto](
			ctx, r.client, fmt.Sprintf("/api/v1/status-pages/%s/components", pageID), cBody,
		)
		if err != nil {
			resp.Diagnostics.AddError("Error creating component", err.Error())
			return
		}
		plan.Components[i].ID = types.StringValue(created.Id.String())
		plan.Components[i].DisplayOrder = types.Int64Value(int64(created.DisplayOrder))
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

	pageID := state.ID.ValueString()

	groups, err := api.List[generated.StatusPageComponentGroupDto](ctx, r.client, fmt.Sprintf("/api/v1/status-pages/%s/groups", pageID))
	if err != nil {
		resp.Diagnostics.AddError("Error listing component groups", err.Error())
		return
	}

	components, err := api.List[generated.StatusPageComponentDto](ctx, r.client, fmt.Sprintf("/api/v1/status-pages/%s/components", pageID))
	if err != nil {
		resp.Diagnostics.AddError("Error listing components", err.Error())
		return
	}

	r.mapToState(&state, page)
	r.mapGroupsToState(&state, groups)
	r.mapComponentsToState(&state, components)
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

	pageID := state.ID.ValueString()
	body := r.buildUpdateRequest(&plan)
	page, err := api.Update[generated.StatusPageDto](ctx, r.client, "/api/v1/status-pages/"+pageID, body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating status page", err.Error())
		return
	}

	// Reconcile sub-resources: delete all existing, then recreate from plan.
	existingComponents, err := api.List[generated.StatusPageComponentDto](ctx, r.client, fmt.Sprintf("/api/v1/status-pages/%s/components", pageID))
	if err != nil {
		resp.Diagnostics.AddError("Error listing components for update", err.Error())
		return
	}
	for _, c := range existingComponents {
		if err := api.Delete(ctx, r.client, fmt.Sprintf("/api/v1/status-pages/%s/components/%s", pageID, c.Id.String())); err != nil {
			resp.Diagnostics.AddError("Error deleting component during update", err.Error())
			return
		}
	}

	existingGroups, err := api.List[generated.StatusPageComponentGroupDto](ctx, r.client, fmt.Sprintf("/api/v1/status-pages/%s/groups", pageID))
	if err != nil {
		resp.Diagnostics.AddError("Error listing groups for update", err.Error())
		return
	}
	for _, g := range existingGroups {
		if err := api.Delete(ctx, r.client, fmt.Sprintf("/api/v1/status-pages/%s/groups/%s", pageID, g.Id.String())); err != nil {
			resp.Diagnostics.AddError("Error deleting group during update", err.Error())
			return
		}
	}

	groupNameToID := make(map[string]string)
	for i, g := range plan.ComponentGroups {
		gBody := generated.CreateStatusPageComponentGroupRequest{
			Name:        g.Name.ValueString(),
			Description: stringPtrOrNil(g.Description),
			Collapsed:   boolPtrOrNil(g.Collapsed),
			DisplayOrder: func() *int32 {
				if g.DisplayOrder.IsNull() || g.DisplayOrder.IsUnknown() {
					return nil
				}
				v := int32(g.DisplayOrder.ValueInt64())
				return &v
			}(),
		}
		created, err := api.Create[generated.StatusPageComponentGroupDto](
			ctx, r.client, fmt.Sprintf("/api/v1/status-pages/%s/groups", pageID), gBody,
		)
		if err != nil {
			resp.Diagnostics.AddError("Error creating component group during update", err.Error())
			return
		}
		plan.ComponentGroups[i].ID = types.StringValue(created.Id.String())
		plan.ComponentGroups[i].DisplayOrder = types.Int64Value(int64(created.DisplayOrder))
		groupNameToID[created.Name] = created.Id.String()
	}

	for i, c := range plan.Components {
		cBody := r.buildComponentRequest(&c, groupNameToID)
		created, err := api.Create[generated.StatusPageComponentDto](
			ctx, r.client, fmt.Sprintf("/api/v1/status-pages/%s/components", pageID), cBody,
		)
		if err != nil {
			resp.Diagnostics.AddError("Error creating component during update", err.Error())
			return
		}
		plan.Components[i].ID = types.StringValue(created.Id.String())
		plan.Components[i].DisplayOrder = types.Int64Value(int64(created.DisplayOrder))
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
	pages, err := api.List[generated.StatusPageDto](ctx, r.client, "/api/v1/status-pages")
	if err != nil {
		resp.Diagnostics.AddError("Error listing status pages for import", err.Error())
		return
	}

	for _, p := range pages {
		if p.Slug == req.ID {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), p.Id.String())...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), p.Name)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("slug"), p.Slug)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("enabled"), p.Enabled)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("visibility"), string(p.Visibility))...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("incident_mode"), string(p.IncidentMode))...)
			return
		}
	}

	resp.Diagnostics.AddError("Status page not found", fmt.Sprintf("No status page found with slug %q", req.ID))
}

// ── Request builders ────────────────────────────────────────────────────

func (r *StatusPageResource) buildCreateRequest(plan *StatusPageResourceModel) generated.CreateStatusPageRequest {
	req := generated.CreateStatusPageRequest{
		Name: plan.Name.ValueString(),
		Slug: plan.Slug.ValueString(),
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		d := plan.Description.ValueString()
		req.Description = &d
	}
	if !plan.Visibility.IsNull() && !plan.Visibility.IsUnknown() {
		v := generated.CreateStatusPageRequestVisibility(plan.Visibility.ValueString())
		req.Visibility = &v
	}
	req.Enabled = boolPtrOrNil(plan.Enabled)
	if !plan.IncidentMode.IsNull() && !plan.IncidentMode.IsUnknown() {
		m := generated.CreateStatusPageRequestIncidentMode(plan.IncidentMode.ValueString())
		req.IncidentMode = &m
	}
	return req
}

func (r *StatusPageResource) buildUpdateRequest(plan *StatusPageResourceModel) generated.UpdateStatusPageRequest {
	name := plan.Name.ValueString()
	req := generated.UpdateStatusPageRequest{
		Name: &name,
	}
	if !plan.Description.IsNull() && !plan.Description.IsUnknown() {
		d := plan.Description.ValueString()
		req.Description = &d
	}
	req.Enabled = boolPtrOrNil(plan.Enabled)
	if !plan.Visibility.IsNull() && !plan.Visibility.IsUnknown() {
		v := generated.UpdateStatusPageRequestVisibility(plan.Visibility.ValueString())
		req.Visibility = &v
	}
	if !plan.IncidentMode.IsNull() && !plan.IncidentMode.IsUnknown() {
		m := generated.UpdateStatusPageRequestIncidentMode(plan.IncidentMode.ValueString())
		req.IncidentMode = &m
	}
	return req
}

func (r *StatusPageResource) buildComponentRequest(c *StatusPageComponentModel, groupNameToID map[string]string) generated.CreateStatusPageComponentRequest {
	cType := generated.CreateStatusPageComponentRequestType(c.Type.ValueString())
	req := generated.CreateStatusPageComponentRequest{
		Name:               c.Name.ValueString(),
		Type:               cType,
		Description:        stringPtrOrNil(c.Description),
		ExcludeFromOverall: boolPtrOrNil(c.ExcludeFromOverall),
		ShowUptime:         boolPtrOrNil(c.ShowUptime),
		DisplayOrder: func() *int32 {
			if c.DisplayOrder.IsNull() || c.DisplayOrder.IsUnknown() {
				return nil
			}
			v := int32(c.DisplayOrder.ValueInt64())
			return &v
		}(),
	}

	if !c.GroupName.IsNull() && !c.GroupName.IsUnknown() {
		if gid, ok := groupNameToID[c.GroupName.ValueString()]; ok {
			uid := uuidFromString(gid)
			req.GroupId = &uid
		}
	}
	if !c.MonitorID.IsNull() && !c.MonitorID.IsUnknown() {
		uid := uuidFromString(c.MonitorID.ValueString())
		req.MonitorId = &uid
	}
	if !c.ResourceGroupID.IsNull() && !c.ResourceGroupID.IsUnknown() {
		uid := uuidFromString(c.ResourceGroupID.ValueString())
		req.ResourceGroupId = &uid
	}

	return req
}

// ── State mapping ───────────────────────────────────────────────────────

func (r *StatusPageResource) mapToState(model *StatusPageResourceModel, dto *generated.StatusPageDto) {
	model.ID = types.StringValue(dto.Id.String())
	model.Name = types.StringValue(dto.Name)
	model.Slug = types.StringValue(dto.Slug)
	model.Description = stringValue(dto.Description)
	model.Visibility = types.StringValue(string(dto.Visibility))
	model.Enabled = types.BoolValue(dto.Enabled)
	model.IncidentMode = types.StringValue(string(dto.IncidentMode))
	model.PageURL = types.StringValue(fmt.Sprintf("https://%s.devhelm.page", dto.Slug))
}

func (r *StatusPageResource) mapGroupsToState(model *StatusPageResourceModel, groups []generated.StatusPageComponentGroupDto) {
	if len(model.ComponentGroups) == 0 && len(groups) == 0 {
		return
	}

	nameToGroup := make(map[string]generated.StatusPageComponentGroupDto)
	for _, g := range groups {
		nameToGroup[g.Name] = g
	}

	for i, mg := range model.ComponentGroups {
		if g, ok := nameToGroup[mg.Name.ValueString()]; ok {
			model.ComponentGroups[i].ID = types.StringValue(g.Id.String())
			model.ComponentGroups[i].DisplayOrder = types.Int64Value(int64(g.DisplayOrder))
			model.ComponentGroups[i].Collapsed = types.BoolValue(g.Collapsed)
			model.ComponentGroups[i].Description = stringValue(g.Description)
		}
	}
}

func (r *StatusPageResource) mapComponentsToState(model *StatusPageResourceModel, components []generated.StatusPageComponentDto) {
	if len(model.Components) == 0 && len(components) == 0 {
		return
	}

	nameToComp := make(map[string]generated.StatusPageComponentDto)
	for _, c := range components {
		nameToComp[c.Name] = c
	}

	for i, mc := range model.Components {
		if c, ok := nameToComp[mc.Name.ValueString()]; ok {
			model.Components[i].ID = types.StringValue(c.Id.String())
			model.Components[i].DisplayOrder = types.Int64Value(int64(c.DisplayOrder))
			model.Components[i].ExcludeFromOverall = types.BoolValue(c.ExcludeFromOverall)
			model.Components[i].ShowUptime = types.BoolValue(c.ShowUptime)
		}
	}
}

// ── Helpers ─────────────────────────────────────────────────────────────

func uuidFromString(s string) uuid.UUID {
	u, _ := uuid.Parse(s)
	return u
}
