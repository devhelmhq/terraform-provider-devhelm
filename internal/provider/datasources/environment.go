package datasources

import (
	"context"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &EnvironmentDataSource{}

type EnvironmentDataSource struct {
	client *api.Client
}

type EnvironmentDataSourceModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Slug      types.String `tfsdk:"slug"`
	IsDefault types.Bool   `tfsdk:"is_default"`
}

func NewEnvironmentDataSource() datasource.DataSource {
	return &EnvironmentDataSource{}
}

func (d *EnvironmentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environment"
}

func (d *EnvironmentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a DevHelm environment by slug.",
		Attributes: map[string]schema.Attribute{
			"id":         schema.StringAttribute{Computed: true, Description: "Unique identifier"},
			"name":       schema.StringAttribute{Computed: true, Description: "Environment name"},
			"slug":       schema.StringAttribute{Required: true, Description: "Environment slug to look up"},
			"is_default": schema.BoolAttribute{Computed: true, Description: "Whether this is the default environment"},
		},
	}
}

func (d *EnvironmentDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *EnvironmentDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model EnvironmentDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	env, err := api.Get[generated.EnvironmentDto](ctx, d.client, "/api/v1/environments/"+model.Slug.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading environment", err.Error())
		return
	}

	mapEnvironmentToState(&model, env)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func mapEnvironmentToState(model *EnvironmentDataSourceModel, env *generated.EnvironmentDto) {
	model.ID = types.StringValue(env.Id.String())
	model.Name = types.StringValue(env.Name)
	model.Slug = types.StringValue(env.Slug)
	if env.IsDefault != nil {
		model.IsDefault = types.BoolValue(*env.IsDefault)
	} else {
		model.IsDefault = types.BoolNull()
	}
}
