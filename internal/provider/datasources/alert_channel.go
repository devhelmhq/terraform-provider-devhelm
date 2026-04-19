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

var _ datasource.DataSource = &AlertChannelDataSource{}

type AlertChannelDataSource struct {
	client *api.Client
}

type AlertChannelDataSourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	ChannelType types.String `tfsdk:"channel_type"`
}

func NewAlertChannelDataSource() datasource.DataSource {
	return &AlertChannelDataSource{}
}

func (d *AlertChannelDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_alert_channel"
}

func (d *AlertChannelDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a DevHelm alert channel by name.",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{Computed: true, Description: "Unique identifier"},
			"name":         schema.StringAttribute{Required: true, Description: "Alert channel name to look up"},
			"channel_type": schema.StringAttribute{Computed: true, Description: "Channel type (slack, email, pagerduty, etc.)"},
		},
	}
}

func (d *AlertChannelDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *AlertChannelDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model AlertChannelDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	channels, err := api.List[generated.AlertChannelDto](ctx, d.client, api.PathAlertChannels)
	if err != nil {
		resp.Diagnostics.AddError("Error listing alert channels", err.Error())
		return
	}

	// Channel names are not unique within an org (e.g. multiple "ops"
	// Slack channels in different workspaces). matchByName surfaces
	// every collision so the operator can switch to a UUID reference.
	matches := matchByName(channels, model.Name.ValueString(), func(c generated.AlertChannelDto) string { return c.Name })
	switch len(matches) {
	case 0:
		resp.Diagnostics.AddError("Alert channel not found", fmt.Sprintf("No alert channel found with name %q", model.Name.ValueString()))
	case 1:
		mapAlertChannelToState(&model, &matches[0])
		resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
	default:
		ids := make([]string, len(matches))
		for i, m := range matches {
			ids[i] = m.Id.String()
		}
		resp.Diagnostics.AddError(
			"Ambiguous alert channel lookup",
			fmt.Sprintf("%d alert channels share the name %q (ids: %v). Reference the channel by UUID instead of using this data source.", len(matches), model.Name.ValueString(), ids),
		)
	}
}

// mapAlertChannelToState mirrors the resource's mapToState pattern so the
// data source has an isolated, testable state-population path.
func mapAlertChannelToState(model *AlertChannelDataSourceModel, dto *generated.AlertChannelDto) {
	model.ID = types.StringValue(dto.Id.String())
	model.Name = types.StringValue(dto.Name)
	model.ChannelType = types.StringValue(string(dto.ChannelType))
}
