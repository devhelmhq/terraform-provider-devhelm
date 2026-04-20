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
				Optional: true,
				Description: "Human-readable description of this webhook. " +
					"Note: the API currently treats null as 'preserve current' and does NOT support clearing the description once set " +
					"(unlike status_page descriptions). Removing this attribute from HCL after a value has been set will leave the " +
					"existing description on the server unchanged. Track API parity here: https://devhelm.io/docs/api/webhook-description-clear.",
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
	client, ok := req.ProviderData.(*api.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *api.Client")
		return
	}
	r.client = client
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

	wh, err := api.Create[generated.WebhookEndpointDto](ctx, r.client, api.PathWebhooks, body)
	if err != nil {
		api.AddAPIError(&resp.Diagnostics, "create webhook", err, path.Root("name"))
		return
	}

	// CreateWebhookEndpointRequest has no `enabled` field — the API forces
	// new webhooks to enabled=true. If the user explicitly set
	// `enabled = false` in HCL we must follow up with an immediate Update
	// to honor their plan. Without this, `terraform apply` would silently
	// create the webhook in the wrong state and the next plan would show
	// a perpetual diff.
	if !plan.Enabled.IsNull() && !plan.Enabled.IsUnknown() && !plan.Enabled.ValueBool() && (wh.Enabled) {
		falseVal := false
		updateBody := generated.UpdateWebhookEndpointRequest{Enabled: &falseVal}
		updated, updateErr := api.Update[generated.WebhookEndpointDto](
			ctx, r.client, api.WebhookPath(wh.Id.String()), updateBody,
		)
		if updateErr != nil {
			resp.Diagnostics.AddError(
				"Error disabling webhook after create",
				fmt.Sprintf("Webhook was created (id=%s) but the follow-up disable request failed: %s. "+
					"Re-run `terraform apply` to retry, or set `enabled = true` in your config.", wh.Id, updateErr),
			)
			return
		}
		wh = updated
	}

	plan.ID = types.StringValue(wh.Id.String())
	plan.URL = types.StringValue(wh.Url)
	plan.Description = stringValue(wh.Description)
	plan.Enabled = types.BoolValue(wh.Enabled)
	plan.SubscribedEvents = stringSliceToSet(ctx, wh.SubscribedEvents)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WebhookResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state WebhookResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	wh, err := api.Get[generated.WebhookEndpointDto](ctx, r.client, api.WebhookPath(state.ID.ValueString()))
	if err != nil {
		if api.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		api.AddAPIError(&resp.Diagnostics, "read webhook", err, path.Root("id"))
		return
	}

	state.URL = types.StringValue(wh.Url)
	state.Description = stringValue(wh.Description)
	state.Enabled = types.BoolValue(wh.Enabled)
	state.SubscribedEvents = stringSliceToSet(ctx, wh.SubscribedEvents)
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

	wh, err := api.Update[generated.WebhookEndpointDto](ctx, r.client, api.WebhookPath(state.ID.ValueString()), body)
	if err != nil {
		api.AddAPIError(&resp.Diagnostics, "update webhook", err, path.Root("name"))
		return
	}

	plan.ID = state.ID
	plan.URL = types.StringValue(wh.Url)
	plan.Description = stringValue(wh.Description)
	plan.Enabled = types.BoolValue(wh.Enabled)
	plan.SubscribedEvents = stringSliceToSet(ctx, wh.SubscribedEvents)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *WebhookResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state WebhookResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := api.Delete(ctx, r.client, api.WebhookPath(state.ID.ValueString()))
	if err != nil && !api.IsNotFound(err) {
		api.AddAPIError(&resp.Diagnostics, "delete webhook", err, path.Root("id"))
	}
}

func (r *WebhookResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	webhooks, err := api.List[generated.WebhookEndpointDto](ctx, r.client, api.PathWebhooks)
	if err != nil {
		resp.Diagnostics.AddError("Error listing webhooks for import", err.Error())
		return
	}

	for _, wh := range webhooks {
		if wh.Url == req.ID || wh.Id.String() == req.ID {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), wh.Id.String())...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("url"), wh.Url)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("enabled"), wh.Enabled)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("description"), stringValue(wh.Description))...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("subscribed_events"), stringSliceToSet(ctx, wh.SubscribedEvents))...)
			return
		}
	}

	resp.Diagnostics.AddError("Webhook not found", fmt.Sprintf("No webhook found with URL or ID %q", req.ID))
}
