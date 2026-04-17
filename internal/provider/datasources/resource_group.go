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

var _ datasource.DataSource = &ResourceGroupDataSource{}

type ResourceGroupDataSource struct {
	client *api.Client
}

type ResourceGroupDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Slug        types.String `tfsdk:"slug"`
	Description types.String `tfsdk:"description"`
}

func NewResourceGroupDataSource() datasource.DataSource {
	return &ResourceGroupDataSource{}
}

func (d *ResourceGroupDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_resource_group"
}

func (d *ResourceGroupDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a DevHelm resource group by name.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Computed: true, Description: "Unique identifier"},
			"name":        schema.StringAttribute{Required: true, Description: "Resource group name to look up"},
			"slug":        schema.StringAttribute{Computed: true, Description: "URL-safe slug"},
			"description": schema.StringAttribute{Computed: true, Description: "Group description"},
		},
	}
}

func (d *ResourceGroupDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *ResourceGroupDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model ResourceGroupDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	groups, err := api.List[generated.ResourceGroupDto](ctx, d.client, "/api/v1/resource-groups")
	if err != nil {
		resp.Diagnostics.AddError("Error listing resource groups", err.Error())
		return
	}

	for _, g := range groups {
		if g.Name == model.Name.ValueString() {
			model.ID = types.StringValue(g.Id.String())
			model.Slug = types.StringValue(g.Slug)
			if g.Description != nil {
				model.Description = types.StringValue(*g.Description)
			} else {
				model.Description = types.StringNull()
			}
			resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
			return
		}
	}

	resp.Diagnostics.AddError("Resource group not found", fmt.Sprintf("No resource group found with name %q", model.Name.ValueString()))
}
