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

var _ datasource.DataSource = &TagDataSource{}

type TagDataSource struct {
	client *api.Client
}

type TagDataSourceModel struct {
	ID    types.String `tfsdk:"id"`
	Name  types.String `tfsdk:"name"`
	Color types.String `tfsdk:"color"`
}

func NewTagDataSource() datasource.DataSource {
	return &TagDataSource{}
}

func (d *TagDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tag"
}

func (d *TagDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a DevHelm tag by name.",
		Attributes: map[string]schema.Attribute{
			"id":    schema.StringAttribute{Computed: true, Description: "Unique identifier"},
			"name":  schema.StringAttribute{Required: true, Description: "Tag name to look up"},
			"color": schema.StringAttribute{Computed: true, Description: "Hex color code"},
		},
	}
}

func (d *TagDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *TagDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model TagDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tags, err := api.List[generated.TagDto](ctx, d.client, api.PathTags)
	if err != nil {
		resp.Diagnostics.AddError("Error listing tags", err.Error())
		return
	}

	matches := matchByName(tags, model.Name.ValueString(), func(t generated.TagDto) string { return t.Name })
	switch len(matches) {
	case 0:
		resp.Diagnostics.AddError("Tag not found", fmt.Sprintf("No tag found with name %q", model.Name.ValueString()))
	case 1:
		mapTagToState(&model, &matches[0])
		resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
	default:
		ids := make([]string, len(matches))
		for i, m := range matches {
			ids[i] = m.Id.String()
		}
		resp.Diagnostics.AddError(
			"Ambiguous tag lookup",
			fmt.Sprintf("%d tags share the name %q (ids: %v). Reference the tag by UUID instead of using this data source.", len(matches), model.Name.ValueString(), ids),
		)
	}
}

func mapTagToState(model *TagDataSourceModel, t *generated.TagDto) {
	model.ID = types.StringValue(t.Id.String())
	model.Name = types.StringValue(t.Name)
	model.Color = types.StringValue(t.Color)
}
