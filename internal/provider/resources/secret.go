package resources

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &SecretResource{}
	_ resource.ResourceWithImportState = &SecretResource{}
)

type SecretResource struct {
	client *api.Client
}

type SecretResourceModel struct {
	ID        types.String `tfsdk:"id"`
	Key       types.String `tfsdk:"key"`
	Value     types.String `tfsdk:"value"`
	ValueHash types.String `tfsdk:"value_hash"`
}

func NewSecretResource() resource.Resource {
	return &SecretResource{}
}

func (r *SecretResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret"
}

func (r *SecretResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version:     0,
		Description: "Manages a DevHelm vault secret for use in monitor authentication.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Unique identifier for this secret",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"key": schema.StringAttribute{
				Required:    true,
				Description: "Secret key (used to reference this secret in auth configs)",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"value": schema.StringAttribute{
				Required:    true,
				Sensitive:   true,
				Description: "Secret value (write-only, never returned by the API)",
			},
			"value_hash": schema.StringAttribute{
				Computed:    true,
				Description: "SHA-256 hash of the secret value (used for drift detection)",
			},
		},
	}
}

func (r *SecretResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *SecretResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SecretResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := generated.CreateSecretRequest{
		Key:   plan.Key.ValueString(),
		Value: plan.Value.ValueString(),
	}

	secret, err := api.Create[generated.SecretDto](ctx, r.client, "/api/v1/secrets", body)
	if err != nil {
		resp.Diagnostics.AddError("Error creating secret", err.Error())
		return
	}

	plan.ID = types.StringValue(secret.Id.String())
	plan.ValueHash = types.StringValue(sha256Hex(plan.Value.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SecretResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SecretResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	secrets, err := api.List[generated.SecretDto](ctx, r.client, "/api/v1/secrets")
	if err != nil {
		resp.Diagnostics.AddError("Error reading secrets", err.Error())
		return
	}

	var found *generated.SecretDto
	for _, s := range secrets {
		if s.Id.String() == state.ID.ValueString() {
			found = &s
			break
		}
	}

	if found == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	state.Key = types.StringValue(found.Key)
	state.ValueHash = types.StringValue(found.ValueHash)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *SecretResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan SecretResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := generated.UpdateSecretRequest{
		Value: plan.Value.ValueString(),
	}

	_, err := api.Update[generated.SecretDto](ctx, r.client, "/api/v1/secrets/"+api.PathEscape(plan.Key.ValueString()), body)
	if err != nil {
		resp.Diagnostics.AddError("Error updating secret", err.Error())
		return
	}

	plan.ValueHash = types.StringValue(sha256Hex(plan.Value.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SecretResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SecretResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := api.Delete(ctx, r.client, "/api/v1/secrets/"+state.ID.ValueString())
	if err != nil && !api.IsNotFound(err) {
		resp.Diagnostics.AddError("Error deleting secret", err.Error())
	}
}

func (r *SecretResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	secrets, err := api.List[generated.SecretDto](ctx, r.client, "/api/v1/secrets")
	if err != nil {
		resp.Diagnostics.AddError("Error listing secrets for import", err.Error())
		return
	}

	for _, s := range secrets {
		if s.Key == req.ID {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), s.Id.String())...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("key"), s.Key)...)
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("value_hash"), s.ValueHash)...)
			// value is write-only; after import, user must set it in config
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("value"), "IMPORTED_PLACEHOLDER")...)
			return
		}
	}

	resp.Diagnostics.AddError("Secret not found", fmt.Sprintf("No secret found with key %q", req.ID))
}

func sha256Hex(input string) string {
	h := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", h)
}
