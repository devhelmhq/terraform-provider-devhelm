package resources

import (
	"context"
	"fmt"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &WebhookResource{}
	_ resource.ResourceWithImportState = &WebhookResource{}
)

type WebhookResource struct {
	client *api.Client
}

type WebhookResourceModel struct {
	ID               types.String `tfsdk:"id"`
	URL              types.String `tfsdk:"url"`
	Description      types.String `tfsdk:"description"`
	Enabled          types.Bool   `tfsdk:"enabled"`
	SubscribedEvents types.Set    `tfsdk:"subscribed_events"`
}

func NewWebhookResource() resource.Resource {
	return &WebhookResource{}
}

func (r *WebhookResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhook"
}

func (r *WebhookResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version:     0,
		Description: "Manages a DevHelm outbound webhook endpoint that receives event payloads.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, Description: "Unique identifier",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"url": schema.StringAttribute{
				Required:    true,
				Description: "Webhook URL that receives event payloads",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Human-readable description of this webhook",
			},
			"enabled": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				Description: "Whether the webhook is enabled (default: true)",
			},
			"subscribed_events": schema.SetAttribute{
				Required: true, ElementType: types.StringType,
				Description: "Set of event types this webhook subscribes to",
			},
		},
	}
}

func (r *WebhookResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*api.Client)
}

func (r *WebhookResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan WebhookResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := generated.CreateWebhookEndpointRequest{
		Url:              plan.URL.ValueString(),
		SubscribedEvents: stringSetToSlice(plan.SubscribedEvents),
		Description:      stringPtrOrNil(plan.Description),
	}

	wh, err := api.Create[generated.WebhookEndpointDto](ctx, r.client, "/api/v1/webhooks", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating webhook", err.Error())
		return
	}

	plan.ID = types.StringValue(wh.Id.String())
	plan.Enabled = types.BoolValue(wh.Enabled)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WebhookResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WebhookResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	wh, err := api.Get[generated.WebhookEndpointDto](ctx, r.client, "/api/v1/webhooks/"+state.ID.ValueString())
	if err != nil {
		if api.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading webhook", err.Error())
		return
	}

	state.URL = types.StringValue(wh.Url)
	state.Description = stringValue(wh.Description)
	state.Enabled = types.BoolValue(wh.Enabled)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *WebhookResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan WebhookResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state WebhookResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	urlStr := plan.URL.ValueString()
	body := generated.UpdateWebhookEndpointRequest{
		Url:              &urlStr,
		Description:      stringPtrOrNil(plan.Description),
		Enabled:          boolPtrOrNil(plan.Enabled),
		SubscribedEvents: stringSliceToPtrFromSet(plan.SubscribedEvents),
	}

	wh, err := api.Update[generated.WebhookEndpointDto](ctx, r.client, "/api/v1/webhooks/"+state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating webhook", err.Error())
		return
	}

	plan.ID = state.ID
	plan.Enabled = types.BoolValue(wh.Enabled)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WebhookResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state WebhookResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := api.Delete(ctx, r.client, "/api/v1/webhooks/"+state.ID.ValueString())
	if err != nil && !api.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting webhook", err.Error())
	}
}

func (r *WebhookResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	webhooks, err := api.List[generated.WebhookEndpointDto](ctx, r.client, "/api/v1/webhooks")
	if err != nil {
		resp.Diagnostics.AddError("Error listing webhooks for import", err.Error())
		return
	}

	for _, wh := range webhooks {
		if wh.Url == req.ID {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), wh.Id.String())...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("url"), wh.Url)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("enabled"), wh.Enabled)...)
			return
		}
	}

	resp.Diagnostics.AddError("Webhook not found", fmt.Sprintf("No webhook found with URL %q", req.ID))
}
