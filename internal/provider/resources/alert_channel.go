package resources

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                   = &AlertChannelResource{}
	_ resource.ResourceWithImportState    = &AlertChannelResource{}
	_ resource.ResourceWithValidateConfig = &AlertChannelResource{}
)

type AlertChannelResource struct {
	client *api.Client
}

type AlertChannelResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	ChannelType types.String `tfsdk:"channel_type"`
	ConfigHash  types.String `tfsdk:"config_hash"`

	// Slack / Discord / Teams / Google Chat / Mattermost / Zapier
	WebhookURL    types.String `tfsdk:"webhook_url"`
	MentionText   types.String `tfsdk:"mention_text"`
	MentionRoleID types.String `tfsdk:"mention_role_id"`

	// Email
	Recipients types.List `tfsdk:"recipients"`

	// PagerDuty / Splunk On-Call
	RoutingKey       types.String `tfsdk:"routing_key"`
	SeverityOverride types.String `tfsdk:"severity_override"`

	// OpsGenie / Linear / Incident.io / Rootly / Datadog / Splunk On-Call
	APIKey types.String `tfsdk:"api_key"`
	Region types.String `tfsdk:"region"`

	// Webhook channel
	URL           types.String `tfsdk:"url"`
	CustomHeaders types.Map    `tfsdk:"custom_headers"`
	SigningSecret types.String `tfsdk:"signing_secret"`

	// Telegram
	BotToken types.String `tfsdk:"bot_token"`
	ChatID   types.String `tfsdk:"chat_id"`

	// Pushover
	UserKey  types.String `tfsdk:"user_key"`
	AppToken types.String `tfsdk:"app_token"`
	Priority types.String `tfsdk:"priority"`
	Sound    types.String `tfsdk:"sound"`

	// Mattermost
	Channel types.String `tfsdk:"channel"`
	IconURL types.String `tfsdk:"icon_url"`

	// Pushbullet
	AccessToken types.String `tfsdk:"access_token"`
	DeviceIden  types.String `tfsdk:"device_iden"`

	// Linear
	TeamID  types.String `tfsdk:"team_id"`
	LabelID types.String `tfsdk:"label_id"`

	// Incident.io
	SeverityID types.String `tfsdk:"severity_id"`
	Visibility types.String `tfsdk:"visibility"`

	// Rootly
	Severity types.String `tfsdk:"severity"`

	// Datadog
	Site types.String `tfsdk:"site"`
	Tags types.String `tfsdk:"tags"`

	// Jira
	Domain     types.String `tfsdk:"domain"`
	Email      types.String `tfsdk:"email"`
	APIToken   types.String `tfsdk:"api_token"`
	ProjectKey types.String `tfsdk:"project_key"`
	IssueType  types.String `tfsdk:"issue_type"`

	// GitLab
	EndpointURL      types.String `tfsdk:"endpoint_url"`
	AuthorizationKey types.String `tfsdk:"authorization_key"`
}

func NewAlertChannelResource() resource.Resource {
	return &AlertChannelResource{}
}

func (r *AlertChannelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_alert_channel"
}

func (r *AlertChannelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version:     0,
		Description: "Manages a DevHelm alert channel for delivering incident notifications.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, Description: "Unique identifier",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required: true, Description: "Human-readable name for this alert channel",
			},
			"channel_type": schema.StringAttribute{
				Required: true,
				Description: "Channel type discriminator. " +
					"Spec source of truth: `AlertChannelDto.channelType` enum. " +
					"Each value gates a specific subset of optional attributes; " +
					"see ValidateConfig in `alert_channel_validate.go` for the " +
					"per-type required + forbidden field matrix.",
				Validators: []validator.String{
					stringvalidator.OneOf(api.AlertChannelTypes...),
				},
			},
			"config_hash": schema.StringAttribute{
				Computed: true, Description: "Content-addressed hash of the channel configuration",
			},

			// Slack / Discord / Teams / Google Chat / Mattermost / Zapier
			"webhook_url": schema.StringAttribute{
				Optional: true, Description: "Incoming webhook URL",
			},
			"mention_text": schema.StringAttribute{
				Optional: true, Description: "Mention text for Slack notifications",
			},
			"mention_role_id": schema.StringAttribute{
				Optional: true, Description: "Role ID to mention for Discord notifications",
			},

			// Email
			"recipients": schema.ListAttribute{
				Optional: true, ElementType: types.StringType,
				Description: "Email recipients (required for email type)",
			},

			// PagerDuty / Splunk On-Call
			"routing_key": schema.StringAttribute{
				Optional: true, Sensitive: true, Description: "Routing or integration key",
			},
			"severity_override": schema.StringAttribute{
				Optional: true, Description: "PagerDuty severity override",
			},

			// OpsGenie / Linear / Incident.io / Rootly / Datadog / Splunk On-Call
			"api_key": schema.StringAttribute{
				Optional: true, Sensitive: true, Description: "Service API key",
			},
			"region": schema.StringAttribute{
				Optional: true, Description: "OpsGenie API region (us or eu)",
			},

			// Webhook
			"url": schema.StringAttribute{
				Optional: true, Description: "Webhook endpoint URL (required for webhook type)",
			},
			"custom_headers": schema.MapAttribute{
				Optional: true, ElementType: types.StringType,
				Description: "Custom HTTP headers for webhook delivery",
			},
			"signing_secret": schema.StringAttribute{
				Optional: true, Sensitive: true, Description: "HMAC signing secret for webhook payloads",
			},

			// Telegram
			"bot_token": schema.StringAttribute{
				Optional: true, Sensitive: true, Description: "Telegram bot token from @BotFather",
			},
			"chat_id": schema.StringAttribute{
				Optional: true, Description: "Telegram chat, group, or channel ID",
			},

			// Pushover
			"user_key": schema.StringAttribute{
				Optional: true, Sensitive: true, Description: "Pushover user or group key",
			},
			"app_token": schema.StringAttribute{
				Optional: true, Sensitive: true, Description: "Pushover application API token",
			},
			"priority": schema.StringAttribute{
				Optional: true, Description: "Notification priority override (-2 to 2)",
			},
			"sound": schema.StringAttribute{
				Optional: true, Description: "Notification sound override",
			},

			// Mattermost
			"channel": schema.StringAttribute{
				Optional: true, Description: "Override channel (if webhook allows)",
			},
			"icon_url": schema.StringAttribute{
				Optional: true, Description: "Custom bot icon URL for Mattermost",
			},

			// Pushbullet
			"access_token": schema.StringAttribute{
				Optional: true, Sensitive: true, Description: "Pushbullet access token",
			},
			"device_iden": schema.StringAttribute{
				Optional: true, Description: "Target device identifier (broadcasts to all if empty)",
			},

			// Linear
			"team_id": schema.StringAttribute{
				Optional: true, Description: "Linear team ID to create issues in",
			},
			"label_id": schema.StringAttribute{
				Optional: true, Description: "Linear label ID to attach to created issues",
			},

			// Incident.io
			"severity_id": schema.StringAttribute{
				Optional: true, Description: "Incident.io severity ID for created incidents",
			},
			"visibility": schema.StringAttribute{
				Optional: true, Description: "Incident visibility: public or private",
			},

			// Rootly
			"severity": schema.StringAttribute{
				Optional: true, Description: "Severity slug override (e.g. sev0, sev1)",
			},

			// Datadog
			"site": schema.StringAttribute{
				Optional: true, Description: "Datadog site region (e.g. datadoghq.com, datadoghq.eu)",
			},
			"tags": schema.StringAttribute{
				Optional: true, Description: "Comma-separated tags to attach to events",
			},

			// Jira
			"domain": schema.StringAttribute{
				Optional: true, Description: "Atlassian instance domain (e.g. yourteam.atlassian.net)",
			},
			"email": schema.StringAttribute{
				Optional: true, Description: "Atlassian account email for API authentication",
			},
			"api_token": schema.StringAttribute{
				Optional: true, Sensitive: true, Description: "Atlassian API token",
			},
			"project_key": schema.StringAttribute{
				Optional: true, Description: "Jira project key where issues are created (e.g. OPS)",
			},
			"issue_type": schema.StringAttribute{
				Optional: true, Description: "Issue type name (e.g. Bug, Task, Incident)",
			},

			// GitLab
			"endpoint_url": schema.StringAttribute{
				Optional: true, Description: "GitLab alert integration endpoint URL",
			},
			"authorization_key": schema.StringAttribute{
				Optional: true, Sensitive: true, Description: "GitLab alert integration authorization key",
			},
		},
	}
}

func (r *AlertChannelResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*api.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *api.Client")
		return
	}
	r.client = client
}

func (r *AlertChannelResource) buildConfig(model *AlertChannelResourceModel) (json.RawMessage, error) {
	channelType := model.ChannelType.ValueString()

	// Each subtype carries its own per-discriminator enum (e.g.
	// SlackChannelConfigChannelType with one value `Slack`). The
	// discriminator inlining in the upstream OpenAPI preprocessor
	// produces tagged unions — codegens emit one type per subtype rather
	// than a shared enum, so each struct literal needs its own typed
	// constant. The switch dispatches on the raw `channelType` string
	// because the parent `AlertChannelDto.channelType` typed alias was
	// dropped by the spec-level Postel's-Law relaxation
	// (`mini/runbooks/api-contract.md` § 3); response DTOs decode the
	// channel type as plain `string`. We compare against the canonical
	// subtype constants (cast to string) so a wire-format rename here
	// will trip the Go compiler at the right line.
	var cfg any
	switch channelType {
	case string(generated.SlackChannelConfigChannelTypeSlack):
		cfg = generated.SlackChannelConfig{
			ChannelType: generated.SlackChannelConfigChannelTypeSlack,
			WebhookUrl:  model.WebhookURL.ValueString(),
			MentionText: stringPtrOrNil(model.MentionText),
		}
	case string(generated.DiscordChannelConfigChannelTypeDiscord):
		cfg = generated.DiscordChannelConfig{
			ChannelType:   generated.DiscordChannelConfigChannelTypeDiscord,
			WebhookUrl:    model.WebhookURL.ValueString(),
			MentionRoleId: stringPtrOrNil(model.MentionRoleID),
		}
	case string(generated.EmailChannelConfigChannelTypeEmail):
		cfg = generated.EmailChannelConfig{
			ChannelType: generated.EmailChannelConfigChannelTypeEmail,
			Recipients:  emailsFromStringList(model.Recipients),
		}
	case string(generated.PagerDutyChannelConfigChannelTypePagerduty):
		cfg = generated.PagerDutyChannelConfig{
			ChannelType:      generated.PagerDutyChannelConfigChannelTypePagerduty,
			RoutingKey:       model.RoutingKey.ValueString(),
			SeverityOverride: stringPtrOrNil(model.SeverityOverride),
		}
	case string(generated.OpsGenieChannelConfigChannelTypeOpsgenie):
		cfg = generated.OpsGenieChannelConfig{
			ChannelType: generated.OpsGenieChannelConfigChannelTypeOpsgenie,
			ApiKey:      model.APIKey.ValueString(),
			Region:      stringPtrOrNil(model.Region),
		}
	case string(generated.TeamsChannelConfigChannelTypeTeams):
		cfg = generated.TeamsChannelConfig{
			ChannelType: generated.TeamsChannelConfigChannelTypeTeams,
			WebhookUrl:  model.WebhookURL.ValueString(),
		}
	case string(generated.WebhookChannelConfigChannelTypeWebhook):
		cfg = generated.WebhookChannelConfig{
			ChannelType:   generated.WebhookChannelConfigChannelTypeWebhook,
			Url:           model.URL.ValueString(),
			CustomHeaders: stringMapToPtr(model.CustomHeaders),
			SigningSecret: stringPtrOrNil(model.SigningSecret),
		}
	case string(generated.TelegramChannelConfigChannelTypeTelegram):
		cfg = generated.TelegramChannelConfig{
			ChannelType: generated.TelegramChannelConfigChannelTypeTelegram,
			BotToken:    model.BotToken.ValueString(),
			ChatId:      model.ChatID.ValueString(),
		}
	case string(generated.GoogleChatChannelConfigChannelTypeGoogleChat):
		cfg = generated.GoogleChatChannelConfig{
			ChannelType: generated.GoogleChatChannelConfigChannelTypeGoogleChat,
			WebhookUrl:  model.WebhookURL.ValueString(),
		}
	case string(generated.PushoverChannelConfigChannelTypePushover):
		cfg = generated.PushoverChannelConfig{
			ChannelType: generated.PushoverChannelConfigChannelTypePushover,
			UserKey:     model.UserKey.ValueString(),
			AppToken:    model.AppToken.ValueString(),
			Priority:    stringPtrOrNil(model.Priority),
			Sound:       stringPtrOrNil(model.Sound),
		}
	case string(generated.MattermostChannelConfigChannelTypeMattermost):
		cfg = generated.MattermostChannelConfig{
			ChannelType: generated.MattermostChannelConfigChannelTypeMattermost,
			WebhookUrl:  model.WebhookURL.ValueString(),
			Channel:     stringPtrOrNil(model.Channel),
			IconUrl:     stringPtrOrNil(model.IconURL),
		}
	case string(generated.SplunkOnCallChannelConfigChannelTypeSplunkOncall):
		cfg = generated.SplunkOnCallChannelConfig{
			ChannelType: generated.SplunkOnCallChannelConfigChannelTypeSplunkOncall,
			ApiKey:      model.APIKey.ValueString(),
			RoutingKey:  model.RoutingKey.ValueString(),
		}
	case string(generated.PushbulletChannelConfigChannelTypePushbullet):
		cfg = generated.PushbulletChannelConfig{
			ChannelType: generated.PushbulletChannelConfigChannelTypePushbullet,
			AccessToken: model.AccessToken.ValueString(),
			DeviceIden:  stringPtrOrNil(model.DeviceIden),
		}
	case string(generated.LinearChannelConfigChannelTypeLinear):
		cfg = generated.LinearChannelConfig{
			ChannelType: generated.LinearChannelConfigChannelTypeLinear,
			ApiKey:      model.APIKey.ValueString(),
			TeamId:      model.TeamID.ValueString(),
			LabelId:     stringPtrOrNil(model.LabelID),
		}
	case string(generated.IncidentIoChannelConfigChannelTypeIncidentIo):
		cfg = generated.IncidentIoChannelConfig{
			ChannelType: generated.IncidentIoChannelConfigChannelTypeIncidentIo,
			ApiKey:      model.APIKey.ValueString(),
			SeverityId:  stringPtrOrNil(model.SeverityID),
			Visibility:  stringPtrOrNil(model.Visibility),
		}
	case string(generated.RootlyChannelConfigChannelTypeRootly):
		cfg = generated.RootlyChannelConfig{
			ChannelType: generated.RootlyChannelConfigChannelTypeRootly,
			ApiKey:      model.APIKey.ValueString(),
			Severity:    stringPtrOrNil(model.Severity),
		}
	case string(generated.ZapierChannelConfigChannelTypeZapier):
		cfg = generated.ZapierChannelConfig{
			ChannelType: generated.ZapierChannelConfigChannelTypeZapier,
			WebhookUrl:  model.WebhookURL.ValueString(),
		}
	case string(generated.DatadogChannelConfigChannelTypeDatadog):
		cfg = generated.DatadogChannelConfig{
			ChannelType: generated.DatadogChannelConfigChannelTypeDatadog,
			ApiKey:      model.APIKey.ValueString(),
			Site:        stringPtrOrNil(model.Site),
			Tags:        stringPtrOrNil(model.Tags),
		}
	case string(generated.JiraChannelConfigChannelTypeJira):
		cfg = generated.JiraChannelConfig{
			ChannelType: generated.JiraChannelConfigChannelTypeJira,
			Domain:      model.Domain.ValueString(),
			Email:       model.Email.ValueString(),
			ApiToken:    model.APIToken.ValueString(),
			ProjectKey:  model.ProjectKey.ValueString(),
			IssueType:   stringPtrOrNil(model.IssueType),
		}
	case string(generated.GitLabChannelConfigChannelTypeGitlab):
		cfg = generated.GitLabChannelConfig{
			ChannelType:      generated.GitLabChannelConfigChannelTypeGitlab,
			EndpointUrl:      model.EndpointURL.ValueString(),
			AuthorizationKey: model.AuthorizationKey.ValueString(),
		}
	default:
		return nil, fmt.Errorf("unsupported channel type: %s", channelType)
	}

	return json.Marshal(cfg)
}

func (r *AlertChannelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan AlertChannelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	config, err := r.buildConfig(&plan)
	if err != nil {
		resp.Diagnostics.AddError("Error building channel config", err.Error())
		return
	}

	var configUnion generated.CreateAlertChannelRequest_Config
	if err := configUnion.UnmarshalJSON(config); err != nil {
		resp.Diagnostics.AddError("Error marshaling channel config", err.Error())
		return
	}

	managedByTF := generated.CreateAlertChannelRequestManagedByTERRAFORM
	body := generated.CreateAlertChannelRequest{
		Name:      plan.Name.ValueString(),
		Config:    configUnion,
		ManagedBy: &managedByTF,
	}

	ch, err := api.Create[generated.AlertChannelDto](ctx, r.client, api.PathAlertChannels, body)
	if err != nil {
		api.AddAPIError(&resp.Diagnostics, "create alert channel", err, path.Root("name"))
		return
	}

	plan.ID = types.StringValue(ch.Id.String())
	plan.ConfigHash = stringValue(ch.ConfigHash)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *AlertChannelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state AlertChannelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	channels, err := api.List[generated.AlertChannelDto](ctx, r.client, api.PathAlertChannels)
	if err != nil {
		api.AddAPIError(&resp.Diagnostics, "read alert channels", err, path.Root("id"))
		return
	}

	var found *generated.AlertChannelDto
	for _, ch := range channels {
		if ch.Id.String() == state.ID.ValueString() {
			found = &ch
			break
		}
	}

	if found == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	state.Name = types.StringValue(found.Name)
	state.ChannelType = types.StringValue(string(found.ChannelType))

	// Config fields are write-only (secrets). The API only returns a display
	// config with non-sensitive hints plus a `configHash` for change
	// detection. We preserve current state values for all config attributes
	// so Terraform doesn't detect phantom diffs on sensitive fields.
	//
	// Drift detection: compare the server's `configHash` against the hash
	// stored in state. When they diverge the channel config was changed
	// out-of-band (dashboard, raw API, another tool). We deliberately do NOT
	// overwrite `state.ConfigHash` with the server value — doing so would
	// mask the divergence and the next plan would look clean. Instead we keep
	// the stored hash (the divergence signal) and NULL the write-only config
	// attributes so the next plan surfaces a diff; the subsequent Update
	// re-pushes the HCL-declared config and reconciles the channel.
	serverHash := stringValue(found.ConfigHash)
	if !state.ConfigHash.IsNull() && !state.ConfigHash.IsUnknown() && !serverHash.Equal(state.ConfigHash) {
		clearAlertChannelConfigAttrs(&state)
	} else {
		state.ConfigHash = serverHash
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// clearAlertChannelConfigAttrs sets every type-specific config attribute on
// the model to its typed null. Called when out-of-band drift is detected so
// the write-only config (which the API never echoes back) reads as null in
// state, forcing the next `terraform plan` to show a diff against the
// HCL-declared config. The identity attributes (id, name, channel_type) and
// config_hash are intentionally left untouched.
func clearAlertChannelConfigAttrs(m *AlertChannelResourceModel) {
	// Slack / Discord / Teams / Google Chat / Mattermost / Zapier
	m.WebhookURL = types.StringNull()
	m.MentionText = types.StringNull()
	m.MentionRoleID = types.StringNull()

	// Email
	m.Recipients = types.ListNull(types.StringType)

	// PagerDuty / Splunk On-Call
	m.RoutingKey = types.StringNull()
	m.SeverityOverride = types.StringNull()

	// OpsGenie / Linear / Incident.io / Rootly / Datadog / Splunk On-Call
	m.APIKey = types.StringNull()
	m.Region = types.StringNull()

	// Webhook
	m.URL = types.StringNull()
	m.CustomHeaders = types.MapNull(types.StringType)
	m.SigningSecret = types.StringNull()

	// Telegram
	m.BotToken = types.StringNull()
	m.ChatID = types.StringNull()

	// Pushover
	m.UserKey = types.StringNull()
	m.AppToken = types.StringNull()
	m.Priority = types.StringNull()
	m.Sound = types.StringNull()

	// Mattermost
	m.Channel = types.StringNull()
	m.IconURL = types.StringNull()

	// Pushbullet
	m.AccessToken = types.StringNull()
	m.DeviceIden = types.StringNull()

	// Linear
	m.TeamID = types.StringNull()
	m.LabelID = types.StringNull()

	// Incident.io
	m.SeverityID = types.StringNull()
	m.Visibility = types.StringNull()

	// Rootly
	m.Severity = types.StringNull()

	// Datadog
	m.Site = types.StringNull()
	m.Tags = types.StringNull()

	// Jira
	m.Domain = types.StringNull()
	m.Email = types.StringNull()
	m.APIToken = types.StringNull()
	m.ProjectKey = types.StringNull()
	m.IssueType = types.StringNull()

	// GitLab
	m.EndpointURL = types.StringNull()
	m.AuthorizationKey = types.StringNull()
}

func (r *AlertChannelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan AlertChannelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state AlertChannelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	config, err := r.buildConfig(&plan)
	if err != nil {
		resp.Diagnostics.AddError("Error building channel config", err.Error())
		return
	}

	var configUnion generated.UpdateAlertChannelRequest_Config
	if err := configUnion.UnmarshalJSON(config); err != nil {
		resp.Diagnostics.AddError("Error marshaling channel config", err.Error())
		return
	}

	managedByTF := generated.UpdateAlertChannelRequestManagedByTERRAFORM
	body := generated.UpdateAlertChannelRequest{
		Name:      plan.Name.ValueString(),
		Config:    configUnion,
		ManagedBy: &managedByTF,
	}

	ch, err := api.Update[generated.AlertChannelDto](ctx, r.client, api.AlertChannelPath(state.ID.ValueString()), body)
	if err != nil {
		api.AddAPIError(&resp.Diagnostics, "update alert channel", err, path.Root("name"))
		return
	}

	plan.ID = state.ID
	plan.ConfigHash = stringValue(ch.ConfigHash)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *AlertChannelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state AlertChannelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := api.Delete(ctx, r.client, api.AlertChannelPath(state.ID.ValueString()))
	if err != nil && !api.IsNotFound(err) {
		api.AddAPIError(&resp.Diagnostics, "delete alert channel", err, path.Root("id"))
	}
}

func (r *AlertChannelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	channels, err := api.List[generated.AlertChannelDto](ctx, r.client, api.PathAlertChannels)
	if err != nil {
		resp.Diagnostics.AddError("Error listing alert channels for import", err.Error())
		return
	}

	// Accept either a UUID (always unambiguous) or a name. Channel names are
	// not unique within an org, so when the import ID matches multiple
	// channels by name we surface ambiguity rather than silently picking one.
	var matched *generated.AlertChannelDto
	var matchedByName []*generated.AlertChannelDto
	for i := range channels {
		ch := &channels[i]
		if ch.Id.String() == req.ID {
			matched = ch
			matchedByName = nil
			break
		}
		if ch.Name == req.ID {
			matchedByName = append(matchedByName, ch)
		}
	}
	if matched == nil {
		switch len(matchedByName) {
		case 0:
			resp.Diagnostics.AddError("Alert channel not found", fmt.Sprintf("No alert channel found with name or ID %q", req.ID))
			return
		case 1:
			matched = matchedByName[0]
		default:
			ids := make([]string, len(matchedByName))
			for i, c := range matchedByName {
				ids[i] = c.Id.String()
			}
			resp.Diagnostics.AddError(
				"Ambiguous alert channel import",
				fmt.Sprintf("%d alert channels share the name %q (ids: %v). Import by UUID instead.", len(matchedByName), req.ID, ids),
			)
			return
		}
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), matched.Id.String())...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), matched.Name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("channel_type"), string(matched.ChannelType))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("config_hash"), stringValue(matched.ConfigHash))...)
}
