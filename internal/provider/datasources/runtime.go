package datasources

import (
	"context"
	"sort"
	"time"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &RuntimeDataSource{}

// RuntimeDataSource reads one runtime profile from the synthetics runtime
// catalog (`GET /synthetics/runtime-profiles[/{id}]`). A code monitor pins
// a runtime with `config.runtimeId`.
type RuntimeDataSource struct {
	client *api.Client
}

type RuntimeDataSourceModel struct {
	ID                types.String `tfsdk:"id"`
	NodeVersion       types.String `tfsdk:"node_version"`
	PlaywrightVersion types.String `tfsdk:"playwright_version"`
	Status            types.String `tfsdk:"status"`
	AvailableAt       types.String `tfsdk:"available_at"`
	DeprecatedAt      types.String `tfsdk:"deprecated_at"`
	EolAt             types.String `tfsdk:"eol_at"`
	Packages          types.Map    `tfsdk:"packages"`
	NodeBuiltins      types.List   `tfsdk:"node_builtins"`
	ReservedBindings  types.List   `tfsdk:"reserved_bindings"`
}

func NewRuntimeDataSource() datasource.DataSource {
	return &RuntimeDataSource{}
}

func (d *RuntimeDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_runtime"
}

func (d *RuntimeDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a synthetics runtime (Node and Playwright versions, packages) by id, or the newest " +
			"available runtime when `id` is omitted. Pin it on a code monitor with `config.runtimeId`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional: true, Computed: true,
				Description: "Runtime id such as 2026.11; omit for the newest available runtime",
			},
			"node_version":       schema.StringAttribute{Computed: true, Description: "Node version available to checks"},
			"playwright_version": schema.StringAttribute{Computed: true, Description: "Playwright version available to checks"},
			"status":             schema.StringAttribute{Computed: true, Description: "baking, available, deprecated, or retired"},
			"available_at":       schema.StringAttribute{Computed: true, Description: "When this runtime became available (RFC 3339)"},
			"deprecated_at":      schema.StringAttribute{Computed: true, Description: "When this runtime was deprecated (RFC 3339)"},
			"eol_at":             schema.StringAttribute{Computed: true, Description: "When runtime support ends (RFC 3339)"},
			"packages": schema.MapAttribute{
				Computed: true, ElementType: types.StringType,
				Description: "Pinned npm packages available to checks, name to version",
			},
			"node_builtins": schema.ListAttribute{
				Computed: true, ElementType: types.StringType,
				Description: "Node built-in modules available to checks",
			},
			"reserved_bindings": schema.ListAttribute{
				Computed: true, ElementType: types.StringType,
				Description: "Environment bindings this runtime reads",
			},
		},
	}
}

func (d *RuntimeDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *RuntimeDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model RuntimeDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var profile *generated.RuntimeProfileDto
	if id := model.ID.ValueString(); id != "" {
		p, err := api.Get[generated.RuntimeProfileDto](ctx, d.client, api.RuntimeProfilePath(api.PathEscape(id)))
		if err != nil {
			api.AddAPIError(&resp.Diagnostics, "read runtime "+id, err, path.Empty())
			return
		}
		profile = p
	} else {
		all, err := api.List[generated.RuntimeProfileDto](ctx, d.client, api.PathRuntimeProfiles)
		if err != nil {
			api.AddAPIError(&resp.Diagnostics, "list runtimes", err, path.Empty())
			return
		}
		profile = newestAvailable(all)
		if profile == nil {
			resp.Diagnostics.AddError("No available runtime", "The runtime catalog has no runtime with status available.")
			return
		}
	}

	resp.Diagnostics.Append(mapRuntime(ctx, &model, profile)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

// newestAvailable picks the most recently available runtime with status
// `available`.
func newestAvailable(all []generated.RuntimeProfileDto) *generated.RuntimeProfileDto {
	var candidates []generated.RuntimeProfileDto
	for _, p := range all {
		if p.Status == "available" {
			candidates = append(candidates, p)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i].AvailableAt, candidates[j].AvailableAt
		switch {
		case a != nil && b != nil && !a.Equal(*b):
			return a.After(*b)
		case a != nil && b == nil:
			return true
		case a == nil && b != nil:
			return false
		default:
			return candidates[i].Id > candidates[j].Id
		}
	})
	return &candidates[0]
}

func mapRuntime(ctx context.Context, m *RuntimeDataSourceModel, p *generated.RuntimeProfileDto) diag.Diagnostics {
	var diags diag.Diagnostics
	m.ID = types.StringValue(p.Id)
	m.NodeVersion = types.StringValue(p.NodeVersion)
	m.PlaywrightVersion = types.StringValue(p.PlaywrightVersion)
	m.Status = types.StringValue(p.Status)
	m.AvailableAt = timeValue(p.AvailableAt)
	m.DeprecatedAt = timeValue(p.DeprecatedAt)
	m.EolAt = timeValue(p.EolAt)

	packages := map[string]string{}
	if p.Packages != nil {
		for name, version := range *p.Packages {
			if version != nil {
				packages[name] = *version
			}
		}
	}
	var dd diag.Diagnostics
	m.Packages, dd = types.MapValueFrom(ctx, types.StringType, packages)
	diags.Append(dd...)
	m.NodeBuiltins, dd = types.ListValueFrom(ctx, types.StringType, derefSlice(p.NodeBuiltins))
	diags.Append(dd...)
	m.ReservedBindings, dd = types.ListValueFrom(ctx, types.StringType, derefSlice(p.ReservedBindings))
	diags.Append(dd...)
	return diags
}

func timeValue(t *time.Time) types.String {
	if t == nil {
		return types.StringNull()
	}
	return types.StringValue(t.UTC().Format(time.RFC3339))
}

func derefSlice(s *[]string) []string {
	if s == nil {
		return []string{}
	}
	return *s
}
