package datasources

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &MonitorDataSource{}

type MonitorDataSource struct {
	client *api.Client
}

type MonitorDataSourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Type             types.String `tfsdk:"type"`
	FrequencySeconds types.Int64  `tfsdk:"frequency_seconds"`
	Enabled          types.Bool   `tfsdk:"enabled"`
	Config           types.String `tfsdk:"config"`
	PingUrl          types.String `tfsdk:"ping_url"`
}

func NewMonitorDataSource() datasource.DataSource {
	return &MonitorDataSource{}
}

func (d *MonitorDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitor"
}

func (d *MonitorDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a DevHelm monitor by name.",
		Attributes: map[string]schema.Attribute{
			"id":                schema.StringAttribute{Computed: true, Description: "Unique identifier"},
			"name":              schema.StringAttribute{Required: true, Description: "Monitor name to look up"},
			"type":              schema.StringAttribute{Computed: true, Description: "Monitor type"},
			"frequency_seconds": schema.Int64Attribute{Computed: true, Description: "Check frequency in seconds"},
			"enabled":           schema.BoolAttribute{Computed: true, Description: "Whether the monitor is active"},
			"config":            schema.StringAttribute{Computed: true, Description: "Monitor configuration as JSON"},
			"ping_url":          schema.StringAttribute{Computed: true, Description: "Heartbeat ping URL"},
		},
	}
}

func (d *MonitorDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d.client = req.ProviderData.(*api.Client)
}

func (d *MonitorDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model MonitorDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	monitors, err := api.List[generated.MonitorDto](ctx, d.client, "/api/v1/monitors")
	if err != nil {
		resp.Diagnostics.AddError("Error listing monitors", err.Error())
		return
	}

	for _, m := range monitors {
		if m.Name == model.Name.ValueString() {
			model.ID = types.StringValue(m.ID)
			model.Type = types.StringValue(m.Type)
			model.FrequencySeconds = types.Int64Value(int64(m.FrequencySeconds))
			model.Enabled = types.BoolValue(m.Enabled)
			if m.Config != nil {
				cfgBytes, _ := json.Marshal(m.Config)
				model.Config = types.StringValue(string(cfgBytes))
			}
			if m.PingUrl != nil {
				model.PingUrl = types.StringValue(*m.PingUrl)
			} else {
				model.PingUrl = types.StringNull()
			}
			resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
			return
		}
	}

	resp.Diagnostics.AddError("Monitor not found", fmt.Sprintf("No monitor found with name %q", model.Name.ValueString()))
}
