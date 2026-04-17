package datasources

import (
	"context"
	"fmt"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &StatusPageDataSource{}

type StatusPageDataSource struct {
	client *api.Client
}

type StatusPageDataSourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Slug         types.String `tfsdk:"slug"`
	Description  types.String `tfsdk:"description"`
	Visibility   types.String `tfsdk:"visibility"`
	Enabled      types.Bool   `tfsdk:"enabled"`
	IncidentMode types.String `tfsdk:"incident_mode"`
	PageURL      types.String `tfsdk:"page_url"`
}

func NewStatusPageDataSource() datasource.DataSource {
	return &StatusPageDataSource{}
}

func (d *StatusPageDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_status_page"
}

func (d *StatusPageDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a DevHelm status page by slug.",
		Attributes: map[string]schema.Attribute{
			"id":            schema.StringAttribute{Computed: true, Description: "Unique identifier"},
			"name":          schema.StringAttribute{Computed: true, Description: "Status page name"},
			"slug":          schema.StringAttribute{Required: true, Description: "URL slug to look up"},
			"description":   schema.StringAttribute{Computed: true, Description: "Status page description"},
			"visibility":    schema.StringAttribute{Computed: true, Description: "Page visibility"},
			"enabled":       schema.BoolAttribute{Computed: true, Description: "Whether the page is enabled"},
			"incident_mode": schema.StringAttribute{Computed: true, Description: "Incident mode"},
			"page_url":      schema.StringAttribute{Computed: true, Description: "Public URL of the status page"},
		},
	}
}

func (d *StatusPageDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*api.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Data Source Configure Type", "Expected *api.Client")
		return
	}
	d.client = client
}

func (d *StatusPageDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model StatusPageDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pages, err := api.List[generated.StatusPageDto](ctx, d.client, "/api/v1/status-pages")
	if err != nil {
		resp.Diagnostics.AddError("Error listing status pages", err.Error())
		return
	}

	slug := model.Slug.ValueString()
	for _, p := range pages {
		if p.Slug == slug {
			model.ID = types.StringValue(p.Id.String())
			model.Name = types.StringValue(p.Name)
			model.Slug = types.StringValue(p.Slug)
			if p.Description != nil {
				model.Description = types.StringValue(*p.Description)
			} else {
				model.Description = types.StringNull()
			}
			model.Visibility = types.StringValue(string(p.Visibility))
			model.Enabled = types.BoolValue(p.Enabled)
			model.IncidentMode = types.StringValue(string(p.IncidentMode))
			model.PageURL = types.StringValue(fmt.Sprintf("https://%s.devhelm.page", p.Slug))
			resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
			return
		}
	}

	resp.Diagnostics.AddError("Status page not found", fmt.Sprintf("No status page found with slug %q", slug))
}
