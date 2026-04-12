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
	_ resource.Resource                = &AlertChannelResource{}
	_ resource.ResourceWithImportState = &AlertChannelResource{}
)

type AlertChannelResource struct {
	client *api.Client
}

type AlertChannelResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	ChannelType types.String `tfsdk:"channel_type"`
	ConfigHash  types.String `tfsdk:"config_hash"`

	// Slack / Discord / Teams
	WebhookURL    types.String `tfsdk:"webhook_url"`
	MentionText   types.String `tfsdk:"mention_text"`
	MentionRoleID types.String `tfsdk:"mention_role_id"`

	// Email
	Recipients types.List `tfsdk:"recipients"`

	// PagerDuty
	RoutingKey       types.String `tfsdk:"routing_key"`
	SeverityOverride types.String `tfsdk:"severity_override"`

	// OpsGenie
	APIKey types.String `tfsdk:"api_key"`
	Region types.String `tfsdk:"region"`

	// Webhook channel
	URL            types.String `tfsdk:"url"`
	CustomHeaders  types.Map    `tfsdk:"custom_headers"`
	SigningSecret  types.String `tfsdk:"signing_secret"`
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
				Required:    true,
				Description: "Channel type: slack, email, pagerduty, opsgenie, discord, teams, webhook",
				Validators: []validator.String{
					stringvalidator.OneOf("slack", "email", "pagerduty", "opsgenie", "discord", "teams", "webhook"),
				},
			},
			"config_hash": schema.StringAttribute{
				Computed: true, Description: "Content-addressed hash of the channel configuration",
			},

			// Slack / Discord / Teams
			"webhook_url": schema.StringAttribute{
				Optional: true, Description: "Webhook URL (required for slack, discord, teams)",
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

			// PagerDuty
			"routing_key": schema.StringAttribute{
				Optional: true, Sensitive: true, Description: "PagerDuty routing key",
			},
			"severity_override": schema.StringAttribute{
				Optional: true, Description: "PagerDuty severity override",
			},

			// OpsGenie
			"api_key": schema.StringAttribute{
				Optional: true, Sensitive: true, Description: "OpsGenie API key",
			},
			"region": schema.StringAttribute{
				Optional: true, Description: "OpsGenie region",
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
		},
	}
}

func (r *AlertChannelResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*api.Client)
}

func (r *AlertChannelResource) buildConfig(model *AlertChannelResourceModel) (json.RawMessage, error) {
	channelType := model.ChannelType.ValueString()

	var cfg any
	switch channelType {
	case "slack":
		cfg = generated.SlackChannelConfig{
			ChannelType: "SlackChannelConfig",
			WebhookURL:  model.WebhookURL.ValueString(),
			MentionText: stringPtrOrNil(model.MentionText),
		}
	case "discord":
		cfg = generated.DiscordChannelConfig{
			ChannelType:   "DiscordChannelConfig",
			WebhookURL:    model.WebhookURL.ValueString(),
			MentionRoleID: stringPtrOrNil(model.MentionRoleID),
		}
	case "email":
		cfg = generated.EmailChannelConfig{
			ChannelType: "EmailChannelConfig",
			Recipients:  stringListToSlice(model.Recipients),
		}
	case "pagerduty":
		cfg = generated.PagerDutyChannelConfig{
			ChannelType:      "PagerDutyChannelConfig",
			RoutingKey:       model.RoutingKey.ValueString(),
			SeverityOverride: stringPtrOrNil(model.SeverityOverride),
		}
	case "opsgenie":
		cfg = generated.OpsGenieChannelConfig{
			ChannelType: "OpsGenieChannelConfig",
			APIKey:      model.APIKey.ValueString(),
			Region:      stringPtrOrNil(model.Region),
		}
	case "teams":
		cfg = generated.TeamsChannelConfig{
			ChannelType: "TeamsChannelConfig",
			WebhookURL:  model.WebhookURL.ValueString(),
		}
	case "webhook":
		cfg = generated.WebhookChannelConfig{
			ChannelType:   "WebhookChannelConfig",
			URL:           model.URL.ValueString(),
			CustomHeaders: mapToStringMap(model.CustomHeaders),
			SigningSecret: stringPtrOrNil(model.SigningSecret),
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

	body := generated.CreateAlertChannelRequest{
		Name:   plan.Name.ValueString(),
		Config: config,
	}

	ch, err := api.Create[generated.AlertChannelDto](ctx, r.client, "/api/v1/alert-channels", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating alert channel", err.Error())
		return
	}

	plan.ID = types.StringValue(ch.ID)
	plan.ConfigHash = types.StringValue(ch.ConfigHash)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *AlertChannelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state AlertChannelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	channels, err := api.List[generated.AlertChannelDto](ctx, r.client, "/api/v1/alert-channels")
	if err != nil {
		resp.Diagnostics.AddError("Error reading alert channels", err.Error())
		return
	}

	var found *generated.AlertChannelDto
	for _, ch := range channels {
		if ch.ID == state.ID.ValueString() {
			found = &ch
			break
		}
	}

	if found == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	state.Name = types.StringValue(found.Name)
	state.ChannelType = types.StringValue(found.ChannelType)
	state.ConfigHash = types.StringValue(found.ConfigHash)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
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

	body := generated.UpdateAlertChannelRequest{
		Name:   plan.Name.ValueString(),
		Config: config,
	}

	ch, err := api.Update[generated.AlertChannelDto](ctx, r.client, "/api/v1/alert-channels/"+state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating alert channel", err.Error())
		return
	}

	plan.ID = state.ID
	plan.ConfigHash = types.StringValue(ch.ConfigHash)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *AlertChannelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state AlertChannelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := api.Delete(ctx, r.client, "/api/v1/alert-channels/"+state.ID.ValueString())
	if err != nil && !api.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting alert channel", err.Error())
	}
}

func (r *AlertChannelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	channels, err := api.List[generated.AlertChannelDto](ctx, r.client, "/api/v1/alert-channels")
	if err != nil {
		resp.Diagnostics.AddError("Error listing alert channels for import", err.Error())
		return
	}

	for _, ch := range channels {
		if ch.Name == req.ID {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), ch.ID)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), ch.Name)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("channel_type"), ch.ChannelType)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("config_hash"), ch.ConfigHash)...)
			return
		}
	}

	resp.Diagnostics.AddError("Alert channel not found", fmt.Sprintf("No alert channel found with name %q", req.ID))
}
