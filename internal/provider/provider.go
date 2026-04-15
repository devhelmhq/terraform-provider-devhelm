package provider

import (
	"context"
	"os"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/provider/datasources"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/provider/resources"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = &DevhelmProvider{}

type DevhelmProvider struct {
	version string
}

type DevhelmProviderModel struct {
	Token       types.String `tfsdk:"token"`
	BaseURL     types.String `tfsdk:"base_url"`
	OrgID       types.String `tfsdk:"org_id"`
	WorkspaceID types.String `tfsdk:"workspace_id"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &DevhelmProvider{version: version}
	}
}

func (p *DevhelmProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "devhelm"
	resp.Version = p.version
}

func (p *DevhelmProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Terraform provider for DevHelm — declarative monitoring, alerting, and incident management as code.",
		Attributes: map[string]schema.Attribute{
			"token": schema.StringAttribute{
				Description: "API token for authentication. Can also be set via DEVHELM_API_TOKEN env var.",
				Optional:    true,
				Sensitive:   true,
			},
			"base_url": schema.StringAttribute{
				Description: "Base URL for the DevHelm API. Defaults to https://api.devhelm.io",
				Optional:    true,
			},
			"org_id": schema.StringAttribute{
				Description: "Organization ID. Can also be set via DEVHELM_ORG_ID env var. Defaults to \"1\".",
				Optional:    true,
			},
			"workspace_id": schema.StringAttribute{
				Description: "Workspace ID. Can also be set via DEVHELM_WORKSPACE_ID env var. Defaults to \"1\".",
				Optional:    true,
			},
		},
	}
}

func (p *DevhelmProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config DevhelmProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	token := envOrDefault(config.Token, "DEVHELM_API_TOKEN", "")
	baseURL := envOrDefault(config.BaseURL, "DEVHELM_API_URL", "https://api.devhelm.io")
	orgID := envOrDefault(config.OrgID, "DEVHELM_ORG_ID", "1")
	workspaceID := envOrDefault(config.WorkspaceID, "DEVHELM_WORKSPACE_ID", "1")

	if token == "" {
		resp.Diagnostics.AddError(
			"Missing API Token",
			"Set the 'token' provider attribute or the DEVHELM_API_TOKEN environment variable.",
		)
		return
	}

	client := api.NewClient(baseURL, token, orgID, workspaceID, p.version)
	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *DevhelmProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		resources.NewTagResource,
		resources.NewEnvironmentResource,
		resources.NewSecretResource,
		resources.NewAlertChannelResource,
		resources.NewNotificationPolicyResource,
		resources.NewWebhookResource,
		resources.NewResourceGroupResource,
		resources.NewResourceGroupMembershipResource,
		resources.NewMonitorResource,
		resources.NewDependencyResource,
		resources.NewStatusPageResource,
		resources.NewStatusPageCustomDomainResource,
	}
}

func (p *DevhelmProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		datasources.NewTagDataSource,
		datasources.NewEnvironmentDataSource,
		datasources.NewAlertChannelDataSource,
		datasources.NewMonitorDataSource,
		datasources.NewResourceGroupDataSource,
		datasources.NewStatusPageDataSource,
	}
}

func envOrDefault(val types.String, envKey, defaultVal string) string {
	if !val.IsNull() && !val.IsUnknown() {
		return val.ValueString()
	}
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	return defaultVal
}
