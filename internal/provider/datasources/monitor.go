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
	client, ok := req.ProviderData.(*api.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Data Source Configure Type", "Expected *api.Client")
		return
	}
	d.client = client
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

	// Names are not unique within an organization (two monitors can share
	// the same display name). Picking the first match would silently produce
	// non-deterministic lookups, so we collect all matches and surface
	// ambiguity as a hard error pointing the user at the matching IDs.
	var matches []generated.MonitorDto
	want := model.Name.ValueString()
	for _, m := range monitors {
		if m.Name == want {
			matches = append(matches, m)
		}
	}
	switch len(matches) {
	case 0:
		resp.Diagnostics.AddError("Monitor not found", fmt.Sprintf("No monitor found with name %q", want))
		return
	case 1:
		// fall through
	default:
		ids := make([]string, len(matches))
		for i, m := range matches {
			ids[i] = m.Id.String()
		}
		resp.Diagnostics.AddError(
			"Ambiguous monitor lookup",
			fmt.Sprintf("%d monitors share the name %q (ids: %v). Rename one in the dashboard, or reference the monitor by ID directly instead of via this data source.", len(matches), want, ids),
		)
		return
	}

	m := matches[0]
	model.ID = types.StringValue(m.Id.String())
	model.Type = types.StringValue(string(m.Type))
	model.FrequencySeconds = types.Int64Value(int64(m.FrequencySeconds))
	model.Enabled = types.BoolValue(m.Enabled)
	if m.Config != nil {
		cfgBytes, err := json.Marshal(m.Config)
		if err != nil {
			resp.Diagnostics.AddError("Error marshaling monitor config", err.Error())
			return
		}
		model.Config = types.StringValue(normalizeConfigJSON(cfgBytes))
	}
	if m.PingUrl != nil {
		model.PingUrl = types.StringValue(*m.PingUrl)
	} else {
		model.PingUrl = types.StringNull()
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
