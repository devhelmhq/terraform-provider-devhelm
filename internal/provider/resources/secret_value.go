package resources

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &SecretValueResource{}
	_ resource.ResourceWithImportState = &SecretValueResource{}
)

// SecretValueResource manages one secret's value in one environment
// (`PUT /secrets/{key}/environments/{environmentId}`). The value is
// write-only: it is sent on create and when `value_version` changes, and it
// never enters the plan or the state. State keeps the value stamp (when and
// by whom the value was last written), which is how out-of-band writes are
// noticed.
type SecretValueResource struct {
	client *api.Client
}

type SecretValueResourceModel struct {
	ID            types.String `tfsdk:"id"`
	Key           types.String `tfsdk:"key"`
	EnvironmentID types.String `tfsdk:"environment_id"`
	Value         types.String `tfsdk:"value"`
	ValueVersion  types.String `tfsdk:"value_version"`
	UpdatedAt     types.String `tfsdk:"updated_at"`
	UpdatedBy     types.String `tfsdk:"updated_by"`
}

func NewSecretValueResource() resource.Resource {
	return &SecretValueResource{}
}

func (r *SecretValueResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret_value"
}

func (r *SecretValueResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Sets a secret's value in one environment. The value is write-only (Terraform 1.11+): " +
			"it is never stored in the plan or the state. Change `value_version` to write a new value. " +
			"The secret key itself is a `devhelm_secret`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "`<key>/<environment_id>`",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"key": schema.StringAttribute{
				Required:      true,
				Description:   "Secret key, e.g. devhelm_secret.card_token.key",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"environment_id": schema.StringAttribute{
				Required:      true,
				Description:   "Environment that receives this value",
				Validators:    []validator.String{uuidStringValidator{}},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"value": schema.StringAttribute{
				Required:    true,
				Sensitive:   true,
				WriteOnly:   true,
				Description: "Secret value for this environment; write-only, never stored in state",
			},
			"value_version": schema.StringAttribute{
				Optional:    true,
				Description: "Any string; change it to write `value` again (write-only values aren't compared)",
			},
			"updated_at": schema.StringAttribute{
				Computed:    true,
				Description: "When this environment value was last written (RFC 3339)",
			},
			"updated_by": schema.StringAttribute{
				Computed:    true,
				Description: "Who last wrote this environment value",
			},
		},
	}
}

func (r *SecretValueResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// write sends the configured value (read from config: write-only values
// are never in the plan).
func (r *SecretValueResource) write(ctx context.Context, cfg types.String, model *SecretValueResourceModel) error {
	if cfg.IsNull() || cfg.IsUnknown() {
		return fmt.Errorf("value is unknown; write-only values must be known at apply")
	}
	p := api.SecretEnvironmentValuePath(api.PathEscape(model.Key.ValueString()), model.EnvironmentID.ValueString())
	return api.PutNoContent(ctx, r.client, p, generated.WriteSecretEnvironmentValueRequest{Value: cfg.ValueString()})
}

// stamp reads the value stamp for (key, environment) from
// `GET /secrets?environmentId=`. found is false when the environment has no
// value for the key.
func (r *SecretValueResource) stamp(ctx context.Context, key, envID string) (*generated.SecretValueStampDto, bool, error) {
	secrets, err := api.List[generated.SecretDto](ctx, r.client, api.PathSecrets+"?environmentId="+url.QueryEscape(envID))
	if err != nil {
		return nil, false, err
	}
	for _, s := range secrets {
		if s.Key != key {
			continue
		}
		if s.ValueStamp == nil || s.ValueStamp.UpdatedAt == nil || s.ValueStamp.Environment.Id.String() != envID {
			return nil, false, nil
		}
		return s.ValueStamp, true, nil
	}
	return nil, false, nil
}

func applyStamp(model *SecretValueResourceModel, st *generated.SecretValueStampDto) {
	model.UpdatedAt = types.StringValue(st.UpdatedAt.UTC().Format(time.RFC3339Nano))
	model.UpdatedBy = types.StringNull()
	if st.UpdatedBy != nil {
		model.UpdatedBy = types.StringValue(st.UpdatedBy.Identity)
	}
}

func (r *SecretValueResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SecretValueResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	var value types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("value"), &value)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.write(ctx, value, &plan); err != nil {
		api.AddAPIError(&resp.Diagnostics, "write the secret value", err, path.Root("key"))
		return
	}
	plan.ID = types.StringValue(plan.Key.ValueString() + "/" + plan.EnvironmentID.ValueString())
	plan.Value = types.StringNull()
	st, found, err := r.stamp(ctx, plan.Key.ValueString(), plan.EnvironmentID.ValueString())
	if err != nil {
		api.AddAPIError(&resp.Diagnostics, "read the secret value stamp", err, path.Root("key"))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Secret value not found after write",
			"The API accepted the value but lists no value for this key in this environment.")
		return
	}
	applyStamp(&plan, st)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the stamp. A missing value, or a stamp that changed since
// Terraform wrote it (someone set the value elsewhere), drops the resource
// from state so the next apply writes the configured value again.
func (r *SecretValueResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SecretValueResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	st, found, err := r.stamp(ctx, state.Key.ValueString(), state.EnvironmentID.ValueString())
	if err != nil {
		if api.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		api.AddAPIError(&resp.Diagnostics, "read the secret value stamp", err, path.Root("key"))
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	before := state.UpdatedAt.ValueString()
	applyStamp(&state, st)
	if before != "" && state.UpdatedAt.ValueString() != before {
		resp.Diagnostics.AddWarning("Secret value changed outside Terraform",
			fmt.Sprintf("%s in environment %s was written at %s by %s; Terraform will write the configured value again.",
				state.Key.ValueString(), state.EnvironmentID.ValueString(), state.UpdatedAt.ValueString(), state.UpdatedBy.ValueString()))
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *SecretValueResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state SecretValueResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	var value types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("value"), &value)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	plan.Value = types.StringNull()
	if !plan.ValueVersion.Equal(state.ValueVersion) {
		if err := r.write(ctx, value, &plan); err != nil {
			api.AddAPIError(&resp.Diagnostics, "write the secret value", err, path.Root("value_version"))
			return
		}
	}
	st, found, err := r.stamp(ctx, plan.Key.ValueString(), plan.EnvironmentID.ValueString())
	if err != nil {
		api.AddAPIError(&resp.Diagnostics, "read the secret value stamp", err, path.Root("key"))
		return
	}
	if found {
		applyStamp(&plan, st)
	} else {
		plan.UpdatedAt, plan.UpdatedBy = state.UpdatedAt, state.UpdatedBy
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SecretValueResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SecretValueResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p := api.SecretEnvironmentValuePath(api.PathEscape(state.Key.ValueString()), state.EnvironmentID.ValueString())
	if err := api.Delete(ctx, r.client, p); err != nil && !api.IsNotFound(err) {
		api.AddAPIError(&resp.Diagnostics, "delete the secret value", err, path.Root("key"))
	}
}

// ImportState accepts `<key>/<environment_id>`. The value stays unknown to
// Terraform; set `value` in configuration (and bump `value_version` to
// overwrite what's there).
func (r *SecretValueResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	i := strings.LastIndex(req.ID, "/")
	if i <= 0 || i == len(req.ID)-1 {
		resp.Diagnostics.AddError("Invalid import ID", "Use `<key>/<environment_id>`, e.g. CARD_TOKEN/2f0c…")
		return
	}
	model := SecretValueResourceModel{
		ID:            types.StringValue(req.ID),
		Key:           types.StringValue(req.ID[:i]),
		EnvironmentID: types.StringValue(req.ID[i+1:]),
		Value:         types.StringNull(),
		ValueVersion:  types.StringNull(),
	}
	st, found, err := r.stamp(ctx, model.Key.ValueString(), model.EnvironmentID.ValueString())
	if err != nil {
		api.AddAPIError(&resp.Diagnostics, "read the secret value stamp", err, path.Root("id"))
		return
	}
	if !found {
		resp.Diagnostics.AddError("Secret value not found",
			fmt.Sprintf("Environment %s has no value for %s.", model.EnvironmentID.ValueString(), model.Key.ValueString()))
		return
	}
	applyStamp(&model, st)
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
