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

var _ datasource.DataSource = &ServiceDataSource{}

type ServiceDataSource struct {
	client *api.Client
}

type ServiceDataSourceModel struct {
	ID                types.String  `tfsdk:"id"`
	Slug              types.String  `tfsdk:"slug"`
	Name              types.String  `tfsdk:"name"`
	Category          types.String  `tfsdk:"category"`
	OfficialStatusURL types.String  `tfsdk:"official_status_url"`
	OverallStatus     types.String  `tfsdk:"overall_status"`
	ComponentCount    types.Int64   `tfsdk:"component_count"`
	Uptime30d         types.Float64 `tfsdk:"uptime_30d"`
}

func NewServiceDataSource() datasource.DataSource {
	return &ServiceDataSource{}
}

func (d *ServiceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service"
}

func (d *ServiceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a third-party service in the DevHelm status-data catalog by slug (or UUID). " +
			"Use the resulting `id` in notification-policy `service_id_in` match rules, or validate a slug before creating a `devhelm_dependency`.",
		Attributes: map[string]schema.Attribute{
			"id":                  schema.StringAttribute{Computed: true, Description: "Service UUID — referenceable from service_id_in notification policy match rules"},
			"slug":                schema.StringAttribute{Required: true, Description: "Catalog slug to look up (e.g. \"stripe\"); a service UUID is also accepted"},
			"name":                schema.StringAttribute{Computed: true, Description: "Human-readable service name"},
			"category":            schema.StringAttribute{Computed: true, Description: "Catalog category, e.g. payments or cloud"},
			"official_status_url": schema.StringAttribute{Computed: true, Description: "Vendor's official status page URL"},
			"overall_status":      schema.StringAttribute{Computed: true, Description: "Current overall status as last polled, e.g. operational; null when never polled"},
			"component_count":     schema.Int64Attribute{Computed: true, Description: "Number of components tracked for this service"},
			"uptime_30d":          schema.Float64Attribute{Computed: true, Description: "Uptime percentage over the last 30 days; null when no data"},
		},
	}
}

func (d *ServiceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *ServiceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model ServiceDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	slug := model.Slug.ValueString()
	svc, err := api.Get[generated.ServiceDetailDto](ctx, d.client, api.ServicePath(api.PathEscape(slug)))
	if err != nil {
		if api.IsNotFound(err) {
			resp.Diagnostics.AddError(
				"Service not found",
				fmt.Sprintf("No service found in the DevHelm catalog with slug or ID %q. "+
					"Browse available services at https://app.devhelm.io/dependencies, or check for typos in the slug.", slug),
			)
			return
		}
		resp.Diagnostics.AddError("Error reading service", err.Error())
		return
	}

	mapServiceToState(&model, svc)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

// mapServiceToState maps the catalog detail DTO onto the data source state.
// `slug` is deliberately NOT overwritten: the input also accepts a UUID, and
// the configured value must be preserved verbatim so Terraform's config/state
// consistency check never trips when a user looks a service up by ID.
func mapServiceToState(model *ServiceDataSourceModel, svc *generated.ServiceDetailDto) {
	model.ID = types.StringValue(svc.Id.String())
	model.Name = types.StringValue(svc.Name)
	if svc.Category != nil {
		model.Category = types.StringValue(*svc.Category)
	} else {
		model.Category = types.StringNull()
	}
	if svc.OfficialStatusUrl != nil {
		model.OfficialStatusURL = types.StringValue(*svc.OfficialStatusUrl)
	} else {
		model.OfficialStatusURL = types.StringNull()
	}
	if svc.CurrentStatus != nil {
		model.OverallStatus = types.StringValue(svc.CurrentStatus.OverallStatus)
	} else {
		model.OverallStatus = types.StringNull()
	}
	// componentsSummary is only present on summary-mode responses (large
	// vendors); when it exists its totalCount is authoritative because the
	// inline components list may be trimmed. Otherwise the inline list is
	// the full set.
	if svc.ComponentsSummary != nil {
		model.ComponentCount = types.Int64Value(int64(svc.ComponentsSummary.TotalCount))
	} else {
		model.ComponentCount = types.Int64Value(int64(len(svc.Components)))
	}
	if svc.Uptime != nil && svc.Uptime.Month != nil {
		model.Uptime30d = types.Float64Value(*svc.Uptime.Month)
	} else {
		model.Uptime30d = types.Float64Null()
	}
}
