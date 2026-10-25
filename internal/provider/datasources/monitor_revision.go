package datasources

import (
	"context"
	"fmt"
	"time"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework-validators/datasourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource                     = &MonitorRevisionDataSource{}
	_ datasource.DataSourceWithConfigValidators = &MonitorRevisionDataSource{}
)

// MonitorRevisionDataSource reads one code revision: by id
// (`GET /revisions/{id}`), or a monitor's live revision or revision number
// (`GET /monitors/{id}/revisions`).
type MonitorRevisionDataSource struct {
	client *api.Client
}

type MonitorRevisionDataSourceModel struct {
	ID            types.String `tfsdk:"id"`
	MonitorID     types.String `tfsdk:"monitor_id"`
	Number        types.Int64  `tfsdk:"number"`
	DefinitionID  types.String `tfsdk:"definition_id"`
	Digest        types.String `tfsdk:"digest"`
	Entrypoint    types.String `tfsdk:"entrypoint"`
	Files         types.List   `tfsdk:"files"`
	Keys          types.List   `tfsdk:"keys"`
	State         types.String `tfsdk:"state"`
	TestOutcome   types.String `tfsdk:"test_outcome"`
	TestRunID     types.String `tfsdk:"test_run_id"`
	CreatedAt     types.String `tfsdk:"created_at"`
	CreatedByKind types.String `tfsdk:"created_by_kind"`
	AuthoredBy    types.String `tfsdk:"authored_by"`
	SourceRepo    types.String `tfsdk:"source_repo"`
	SourceBranch  types.String `tfsdk:"source_branch"`
	SourceCommit  types.String `tfsdk:"source_commit"`
	SourceFile    types.String `tfsdk:"source_file"`
}

func NewMonitorRevisionDataSource() datasource.DataSource {
	return &MonitorRevisionDataSource{}
}

func (d *MonitorRevisionDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitor_revision"
}

func (d *MonitorRevisionDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	computed := func(desc string) schema.StringAttribute {
		return schema.StringAttribute{Computed: true, Description: desc}
	}
	resp.Schema = schema.Schema{
		Description: "Look up a code monitor revision by `id`, or a monitor's live revision (or revision " +
			"`number`) by `monitor_id`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional: true, Computed: true,
				Description: "Revision id",
				Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"monitor_id": schema.StringAttribute{
				Optional:    true,
				Description: "Monitor whose live revision (or revision `number`) to read",
			},
			"number": schema.Int64Attribute{
				Optional: true, Computed: true,
				Description: "Revision number within the definition; with `monitor_id`, omit for the live revision",
			},
			"definition_id":   computed("Definition this revision belongs to"),
			"digest":          computed("Closure digest (sha256:<hex>)"),
			"entrypoint":      computed("File that runs, relative to the bundle root"),
			"files":           schema.ListAttribute{Computed: true, ElementType: types.StringType, Description: "Files in the revision"},
			"keys":            schema.ListAttribute{Computed: true, ElementType: types.StringType, Description: "Secret keys the code reads"},
			"state":           computed("draft, tested_passed, tested_failed, live, previous, or discarded"),
			"test_outcome":    computed("Latest test outcome: passed or failed"),
			"test_run_id":     computed("Latest validation run for this revision"),
			"created_at":      computed("When the revision was created (RFC 3339)"),
			"created_by_kind": computed("Tool that created the revision: dashboard, git, terraform, api, or mcp"),
			"authored_by":     computed("Who authored the revision"),
			"source_repo":     computed("Repository that produced this code"),
			"source_branch":   computed("Branch that produced this code"),
			"source_commit":   computed("Commit that produced this code"),
			"source_file":     computed("Declaring file in that repository"),
		},
	}
}

func (d *MonitorRevisionDataSource) ConfigValidators(_ context.Context) []datasource.ConfigValidator {
	return []datasource.ConfigValidator{
		datasourcevalidator.ExactlyOneOf(path.MatchRoot("id"), path.MatchRoot("monitor_id")),
		datasourcevalidator.Conflicting(path.MatchRoot("id"), path.MatchRoot("number")),
	}
}

func (d *MonitorRevisionDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *MonitorRevisionDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model MonitorRevisionDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var rev *generated.RevisionDto
	if id := model.ID.ValueString(); id != "" {
		r, err := api.Get[generated.RevisionDto](ctx, d.client, api.RevisionPath(api.PathEscape(id)))
		if err != nil {
			api.AddAPIError(&resp.Diagnostics, "read revision "+id, err, path.Root("id"))
			return
		}
		rev = r
	} else {
		monitorID := model.MonitorID.ValueString()
		state := "live"
		if !model.Number.IsNull() {
			state = "all"
		}
		revs, err := api.List[generated.RevisionDto](ctx, d.client,
			api.MonitorRevisionsPath(api.PathEscape(monitorID))+"?state="+state)
		if err != nil {
			api.AddAPIError(&resp.Diagnostics, "list revisions of monitor "+monitorID, err, path.Root("monitor_id"))
			return
		}
		for i := range revs {
			if model.Number.IsNull() || int64(revs[i].Number) == model.Number.ValueInt64() {
				rev = &revs[i]
				break
			}
		}
		if rev == nil {
			what := "a live revision"
			if !model.Number.IsNull() {
				what = fmt.Sprintf("revision %d", model.Number.ValueInt64())
			}
			resp.Diagnostics.AddError("Revision not found", fmt.Sprintf("Monitor %s has no %s.", monitorID, what))
			return
		}
	}

	resp.Diagnostics.Append(mapRevision(ctx, &model, rev)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func mapRevision(ctx context.Context, m *MonitorRevisionDataSourceModel, r *generated.RevisionDto) diag.Diagnostics {
	var diags diag.Diagnostics
	str := func(s *string) types.String {
		if s == nil {
			return types.StringNull()
		}
		return types.StringValue(*s)
	}
	m.ID = types.StringValue(r.Id.String())
	m.Number = types.Int64Value(int64(r.Number))
	m.DefinitionID = types.StringValue(r.DefinitionId.String())
	m.Digest = types.StringValue(r.Digest)
	m.Entrypoint = types.StringValue(r.Entrypoint)
	m.State = str(r.State)
	m.TestOutcome = str(r.TestOutcome)
	m.TestRunID = types.StringNull()
	if r.TestRunId != nil {
		m.TestRunID = types.StringValue(r.TestRunId.String())
	}
	m.CreatedAt = types.StringValue(r.CreatedAt.UTC().Format(time.RFC3339))
	m.CreatedByKind = str(r.CreatedByKind)
	m.AuthoredBy = str(r.AuthoredBy)
	m.SourceRepo, m.SourceBranch, m.SourceCommit, m.SourceFile = types.StringNull(), types.StringNull(), types.StringNull(), types.StringNull()
	if r.Source != nil {
		m.SourceRepo, m.SourceBranch = str(r.Source.Repo), str(r.Source.Branch)
		m.SourceCommit, m.SourceFile = str(r.Source.Commit), str(r.Source.File)
	}
	var dd diag.Diagnostics
	m.Files, dd = types.ListValueFrom(ctx, types.StringType, r.Files)
	diags.Append(dd...)
	m.Keys, dd = types.ListValueFrom(ctx, types.StringType, r.Keys)
	diags.Append(dd...)
	return diags
}
