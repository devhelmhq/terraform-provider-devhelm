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

var _ datasource.DataSource = &SecretDataSource{}

type SecretDataSource struct {
	client *api.Client
}

type SecretDataSourceModel struct {
	ID        types.String `tfsdk:"id"`
	Key       types.String `tfsdk:"key"`
	ValueHash types.String `tfsdk:"value_hash"`
}

func NewSecretDataSource() datasource.DataSource {
	return &SecretDataSource{}
}

func (d *SecretDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret"
}

func (d *SecretDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Look up a DevHelm secret by key. The plaintext value is never " +
			"returned by the API; use `value_hash` for change detection.",
		Attributes: map[string]schema.Attribute{
			"id":  schema.StringAttribute{Computed: true, Description: "Unique secret identifier"},
			"key": schema.StringAttribute{Required: true, Description: "Secret key to look up"},
			"value_hash": schema.StringAttribute{
				Computed:    true,
				Description: "SHA-256 hex digest of the current plaintext; use for change detection",
			},
		},
	}
}

func (d *SecretDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *SecretDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var model SecretDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	secrets, err := api.List[generated.SecretDto](ctx, d.client, api.PathSecrets)
	if err != nil {
		resp.Diagnostics.AddError("Error listing secrets", err.Error())
		return
	}

	// Secret keys are unique within a workspace (per SecretDto.key), so a
	// match is unambiguous — but we still surface a not-found error rather
	// than silently returning empty state.
	matches := matchByName(secrets, model.Key.ValueString(), func(s generated.SecretDto) string { return s.Key })
	switch len(matches) {
	case 0:
		resp.Diagnostics.AddError("Secret not found", fmt.Sprintf("No secret found with key %q", model.Key.ValueString()))
	case 1:
		mapSecretToState(&model, &matches[0])
		resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
	default:
		ids := make([]string, len(matches))
		for i, m := range matches {
			ids[i] = m.Id.String()
		}
		resp.Diagnostics.AddError(
			"Ambiguous secret lookup",
			fmt.Sprintf("%d secrets share the key %q (ids: %v). Reference the secret by UUID instead of using this data source.", len(matches), model.Key.ValueString(), ids),
		)
	}
}

func mapSecretToState(model *SecretDataSourceModel, s *generated.SecretDto) {
	model.ID = types.StringValue(s.Id.String())
	model.Key = types.StringValue(s.Key)
	model.ValueHash = types.StringValue(s.ValueHash)
}
