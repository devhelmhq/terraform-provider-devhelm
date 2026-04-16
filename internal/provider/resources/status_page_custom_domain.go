package resources

import (
	"context"
	"fmt"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &StatusPageCustomDomainResource{}

type StatusPageCustomDomainResource struct {
	client *api.Client
}

type StatusPageCustomDomainResourceModel struct {
	ID                      types.String `tfsdk:"id"`
	StatusPageID            types.String `tfsdk:"status_page_id"`
	Hostname                types.String `tfsdk:"hostname"`
	Status                  types.String `tfsdk:"status"`
	VerificationMethod      types.String `tfsdk:"verification_method"`
	VerificationToken       types.String `tfsdk:"verification_token"`
	VerificationCnameTarget types.String `tfsdk:"verification_cname_target"`
	VerificationError       types.String `tfsdk:"verification_error"`
	Primary                 types.Bool   `tfsdk:"primary"`
}

func NewStatusPageCustomDomainResource() resource.Resource {
	return &StatusPageCustomDomainResource{}
}

func (r *StatusPageCustomDomainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_status_page_custom_domain"
}

func (r *StatusPageCustomDomainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version: 0,
		Description: "Manages a custom domain for a DevHelm status page. " +
			"After creation, use the verification_token and verification_cname_target " +
			"outputs to create DNS records (similar to AWS SES domain verification).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, Description: "Unique identifier for this custom domain",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"status_page_id": schema.StringAttribute{
				Required: true, Description: "ID of the status page this domain belongs to",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"hostname": schema.StringAttribute{
				Required: true, Description: "Custom hostname, e.g. status.acme.com",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"status": schema.StringAttribute{
				Computed: true, Description: "Domain verification status: PENDING, VERIFIED, or FAILED",
			},
			"verification_method": schema.StringAttribute{
				Computed: true, Description: "Verification method: CNAME",
			},
			"verification_token": schema.StringAttribute{
				Computed: true,
				Description: "Verification token — create a TXT record with this value " +
					"at _devhelm-verification.<hostname>",
			},
			"verification_cname_target": schema.StringAttribute{
				Computed: true,
				Description: "CNAME target — create a CNAME record pointing <hostname> " +
					"to this value for traffic routing",
			},
			"verification_error": schema.StringAttribute{
				Computed: true, Description: "Verification error message, if any",
			},
			"primary": schema.BoolAttribute{
				Computed: true, Description: "Whether this is the primary domain for the status page",
			},
		},
	}
}

func (r *StatusPageCustomDomainResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*api.Client)
}

func (r *StatusPageCustomDomainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan StatusPageCustomDomainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := generated.AddCustomDomainRequest{
		Hostname: plan.Hostname.ValueString(),
	}

	pageID := plan.StatusPageID.ValueString()
	domain, err := api.Create[generated.StatusPageCustomDomainDto](
		ctx, r.client, fmt.Sprintf("/api/v1/status-pages/%s/domains", pageID), body,
	)
	if err != nil {
		resp.Diagnostics.AddError("Error adding custom domain", err.Error())
		return
	}

	r.mapToState(&plan, domain)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *StatusPageCustomDomainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state StatusPageCustomDomainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pageID := state.StatusPageID.ValueString()
	domains, err := api.List[generated.StatusPageCustomDomainDto](
		ctx, r.client, fmt.Sprintf("/api/v1/status-pages/%s/domains", pageID),
	)
	if err != nil {
		if api.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error listing domains", err.Error())
		return
	}

	domainID := state.ID.ValueString()
	for _, d := range domains {
		if d.Id.String() == domainID {
			r.mapToState(&state, &d)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}

	resp.State.RemoveResource(ctx)
}

func (r *StatusPageCustomDomainResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Update not supported",
		"Custom domains cannot be updated — delete and recreate to change the hostname.",
	)
}

func (r *StatusPageCustomDomainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state StatusPageCustomDomainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pageID := state.StatusPageID.ValueString()
	domainID := state.ID.ValueString()

	err := api.Delete(ctx, r.client, fmt.Sprintf("/api/v1/status-pages/%s/domains/%s", pageID, domainID))
	if err != nil && !api.IsNotFound(err) {
		resp.Diagnostics.AddError("Error removing custom domain", err.Error())
	}
}

func (r *StatusPageCustomDomainResource) mapToState(model *StatusPageCustomDomainResourceModel, dto *generated.StatusPageCustomDomainDto) {
	model.ID = types.StringValue(dto.Id.String())
	model.Hostname = types.StringValue(dto.Hostname)
	model.Status = types.StringValue(string(dto.Status))
	model.VerificationMethod = types.StringValue(string(dto.VerificationMethod))
	model.VerificationToken = types.StringValue(dto.VerificationToken)
	model.VerificationCnameTarget = types.StringValue(dto.VerificationCnameTarget)
	model.VerificationError = stringValue(dto.VerificationError)
	model.Primary = types.BoolValue(dto.Primary)
}
