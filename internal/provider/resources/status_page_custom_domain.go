package resources

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &StatusPageCustomDomainResource{}
	_ resource.ResourceWithImportState = &StatusPageCustomDomainResource{}
)

// StatusPageCustomDomainResource reserves a custom hostname on a status page
// and surfaces the DNS record(s) the operator must create to (a) prove
// ownership and (b) route traffic.
//
// **Verification is a separate resource.** Reserving the hostname only
// produces the record requirements; it does not poll DNS or wait for
// verification to complete. Use `devhelm_status_page_custom_domain_verification`
// after the DNS records are created — that resource blocks `terraform apply`
// until the API confirms the domain is verified.
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
	VerifiedAt              types.String `tfsdk:"verified_at"`
	VerificationRecord      types.Object `tfsdk:"verification_record"`
	TrafficRecord           types.Object `tfsdk:"traffic_record"`
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
		Description: "Reserves a custom hostname on a DevHelm status page and exposes the DNS records " +
			"required to verify ownership and route traffic. Use the companion " +
			"devhelm_status_page_custom_domain_verification resource to block apply until the API confirms verification.",
		MarkdownDescription: "Reserves a custom hostname on a DevHelm status page and exposes the DNS records " +
			"required to verify ownership and route traffic.\n\n" +
			"This resource only **reserves** the hostname; it does not poll for DNS verification. " +
			"After applying, create the verification record (`verification_record.{name,type,value}`) " +
			"and the traffic record (`traffic_record.{name,type,value}`) at your DNS provider, " +
			"then use [`devhelm_status_page_custom_domain_verification`](./status_page_custom_domain_verification) " +
			"to block `terraform apply` until the API confirms the domain is verified.\n\n" +
			"### Verification methods\n\n" +
			"The DevHelm API supports two verification methods (default: `CNAME`):\n\n" +
			"| Method  | DNS record to create                                 | Field that drives the value          |\n" +
			"|---------|------------------------------------------------------|--------------------------------------|\n" +
			"| `CNAME` | `CNAME` at `<hostname>` → `<page-slug>.devhelm.io`   | `verification_cname_target`          |\n" +
			"| `TXT`   | `TXT` at `_devhelm-verification.<hostname>`           | `verification_token`                 |\n\n" +
			"In the `CNAME` case, the verification record and the traffic record are the **same record**, " +
			"so `verification_record == traffic_record`. In the `TXT` case they are distinct: the TXT proves " +
			"ownership, the CNAME routes user traffic to DevHelm.\n\n" +
			"### Example: end-to-end automation with Cloudflare\n\n" +
			"```hcl\n" +
			"resource \"devhelm_status_page\" \"public\" {\n" +
			"  name = \"Acme Status\"\n" +
			"  slug = \"acme\"\n" +
			"}\n" +
			"\n" +
			"resource \"devhelm_status_page_custom_domain\" \"acme\" {\n" +
			"  status_page_id = devhelm_status_page.public.id\n" +
			"  hostname       = \"status.acme.com\"\n" +
			"}\n" +
			"\n" +
			"# Verification record — proves ownership\n" +
			"resource \"cloudflare_record\" \"verification\" {\n" +
			"  zone_id = var.cloudflare_zone_id\n" +
			"  name    = devhelm_status_page_custom_domain.acme.verification_record.name\n" +
			"  type    = devhelm_status_page_custom_domain.acme.verification_record.type\n" +
			"  value   = devhelm_status_page_custom_domain.acme.verification_record.value\n" +
			"  ttl     = 300\n" +
			"  proxied = false\n" +
			"}\n" +
			"\n" +
			"# Traffic record — only distinct from the verification record when method=TXT\n" +
			"resource \"cloudflare_record\" \"traffic\" {\n" +
			"  count = (\n" +
			"    devhelm_status_page_custom_domain.acme.verification_record.name ==\n" +
			"    devhelm_status_page_custom_domain.acme.traffic_record.name\n" +
			"  ) ? 0 : 1\n" +
			"  zone_id = var.cloudflare_zone_id\n" +
			"  name    = devhelm_status_page_custom_domain.acme.traffic_record.name\n" +
			"  type    = devhelm_status_page_custom_domain.acme.traffic_record.type\n" +
			"  value   = devhelm_status_page_custom_domain.acme.traffic_record.value\n" +
			"  ttl     = 300\n" +
			"  proxied = false\n" +
			"}\n" +
			"\n" +
			"# Wait for the API to confirm verification before considering apply complete\n" +
			"resource \"devhelm_status_page_custom_domain_verification\" \"acme\" {\n" +
			"  status_page_id   = devhelm_status_page.public.id\n" +
			"  custom_domain_id = devhelm_status_page_custom_domain.acme.id\n" +
			"\n" +
			"  depends_on = [\n" +
			"    cloudflare_record.verification,\n" +
			"    cloudflare_record.traffic,\n" +
			"  ]\n" +
			"}\n" +
			"```\n",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Unique identifier for this custom domain",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"status_page_id": schema.StringAttribute{
				Required:      true,
				Description:   "ID of the status page this domain belongs to. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"hostname": schema.StringAttribute{
				Required:      true,
				Description:   "Custom hostname to attach to the status page (e.g. status.acme.com). Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"status": schema.StringAttribute{
				Computed: true,
				Description: "Current domain lifecycle status. One of: PENDING_VERIFICATION, VERIFICATION_FAILED, " +
					"VERIFIED, SSL_PENDING, ACTIVE, FAILED, REMOVED.",
			},
			"verification_method": schema.StringAttribute{
				Computed:    true,
				Description: "DNS verification method assigned by the API: CNAME (default) or TXT.",
			},
			"verification_token": schema.StringAttribute{
				Computed: true,
				Description: "Token used to prove ownership when verification_method = TXT. " +
					"Place this value in a TXT record at _devhelm-verification.<hostname>. " +
					"Prefer the verification_record nested attribute, which already accounts for the method.",
			},
			"verification_cname_target": schema.StringAttribute{
				Computed: true,
				Description: "Hostname the CNAME record must point to (typically <page-slug>.devhelm.io). " +
					"Used both for CNAME-method verification and for routing user traffic to the status page.",
			},
			"verification_error": schema.StringAttribute{
				Computed:    true,
				Description: "Last verification error reported by the API, if any (e.g. \"No DNS record found\").",
			},
			"verified_at": schema.StringAttribute{
				Computed:    true,
				Description: "RFC3339 timestamp when the domain was successfully verified, or null if not yet verified.",
			},
			"primary": schema.BoolAttribute{
				Computed:    true,
				Description: "Whether this is the primary domain for the status page",
			},
			"verification_record": schema.SingleNestedAttribute{
				Computed: true,
				Description: "DNS record required to prove ownership of the hostname. " +
					"Wire `name`, `type`, and `value` directly into your DNS provider " +
					"(cloudflare_record, aws_route53_record, etc.) — no per-method dispatch logic needed.",
				Attributes: map[string]schema.Attribute{
					"name": schema.StringAttribute{
						Computed:    true,
						Description: "Fully-qualified record name. For CNAME-method this equals hostname; for TXT-method it equals _devhelm-verification.<hostname>.",
					},
					"type": schema.StringAttribute{
						Computed:    true,
						Description: "Record type: CNAME or TXT, mirroring verification_method.",
					},
					"value": schema.StringAttribute{
						Computed:    true,
						Description: "Record value. For CNAME-method this is verification_cname_target; for TXT-method it is verification_token.",
					},
				},
			},
			"traffic_record": schema.SingleNestedAttribute{
				Computed: true,
				Description: "DNS record required to route user traffic to the status page. " +
					"Always a CNAME at hostname → verification_cname_target. " +
					"In CNAME-method this is identical to verification_record; in TXT-method it is a separate record.",
				Attributes: map[string]schema.Attribute{
					"name": schema.StringAttribute{
						Computed:    true,
						Description: "Fully-qualified record name (always equals hostname).",
					},
					"type": schema.StringAttribute{
						Computed:    true,
						Description: "Record type: always CNAME.",
					},
					"value": schema.StringAttribute{
						Computed:    true,
						Description: "Record value: the hostname this CNAME must target (verification_cname_target).",
					},
				},
			},
		},
	}
}

func (r *StatusPageCustomDomainResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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
		ctx, r.client, api.StatusPageDomainsPath(pageID), body,
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
		ctx, r.client, api.StatusPageDomainsPath(pageID),
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

	err := api.Delete(ctx, r.client, api.StatusPageDomainPath(pageID, domainID))
	if err != nil && !api.IsNotFound(err) {
		resp.Diagnostics.AddError("Error removing custom domain", err.Error())
	}
}

// ImportState parses a compound `<status_page_id>/<custom_domain_id>` ID
// and hydrates the full resource model. The compound form is required
// because the API exposes domains as a sub-collection under the parent
// status page; there is no global GET-by-id endpoint.
func (r *StatusPageCustomDomainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	pageID, domainID, ok := strings.Cut(req.ID, "/")
	if !ok || pageID == "" || domainID == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected `<status_page_id>/<custom_domain_id>`, got %q", req.ID),
		)
		return
	}
	if _, err := uuid.Parse(pageID); err != nil {
		resp.Diagnostics.AddError("Invalid status page ID", fmt.Sprintf("status_page_id %q is not a UUID: %s", pageID, err))
		return
	}
	if _, err := uuid.Parse(domainID); err != nil {
		resp.Diagnostics.AddError("Invalid custom domain ID", fmt.Sprintf("custom_domain_id %q is not a UUID: %s", domainID, err))
		return
	}

	domains, err := api.List[generated.StatusPageCustomDomainDto](
		ctx, r.client, api.StatusPageDomainsPath(pageID),
	)
	if err != nil {
		resp.Diagnostics.AddError("Error listing domains for import", err.Error())
		return
	}
	for i := range domains {
		if domains[i].Id.String() == domainID {
			model := StatusPageCustomDomainResourceModel{
				StatusPageID: types.StringValue(pageID),
			}
			r.mapToState(&model, &domains[i])
			resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
			return
		}
	}
	resp.Diagnostics.AddError(
		"Custom domain not found",
		fmt.Sprintf("No custom domain with id %s found on status page %s", domainID, pageID),
	)
}

// dnsRecordAttrTypes is the schema-level type map for the verification_record
// and traffic_record nested attributes. Defined once to avoid drift between
// schema and state-construction sites.
var dnsRecordAttrTypes = map[string]attr.Type{
	"name":  types.StringType,
	"type":  types.StringType,
	"value": types.StringType,
}

func dnsRecordObject(name, recordType, value string) types.Object {
	return types.ObjectValueMust(dnsRecordAttrTypes, map[string]attr.Value{
		"name":  types.StringValue(name),
		"type":  types.StringValue(recordType),
		"value": types.StringValue(value),
	})
}

// mapToState copies fields from the API DTO into the resource model and
// derives the verification_record / traffic_record nested attributes from
// the verification_method, hostname, verification_token, and
// verification_cname_target fields. The derivation lives here (provider-side)
// so users do not need to write per-method dispatch logic in HCL.
//
// The StatusPageCustomDomainDto does not echo back the parent status page ID
// (the server knows it from the URL path), so we preserve whatever
// StatusPageID is already on the model. Callers MUST have set StatusPageID
// before invoking mapToState — this is guaranteed for Create (the plan
// carries it) and Read (the prior state carries it).
func (r *StatusPageCustomDomainResource) mapToState(model *StatusPageCustomDomainResourceModel, dto *generated.StatusPageCustomDomainDto) {
	if model.StatusPageID.IsNull() || model.StatusPageID.ValueString() == "" {
		// This is a defensive guard: StatusPageID is schema-required, so
		// reaching this branch indicates a provider-internal bug rather
		// than user error.
		panic("status_page_id missing when mapping StatusPageCustomDomain; this is a provider bug")
	}
	model.ID = types.StringValue(dto.Id.String())
	model.Hostname = types.StringValue(dto.Hostname)
	model.Status = types.StringValue(string(dto.Status))
	model.VerificationMethod = types.StringValue(string(dto.VerificationMethod))
	model.VerificationToken = types.StringValue(dto.VerificationToken)
	model.VerificationCnameTarget = types.StringValue(dto.VerificationCnameTarget)
	model.VerificationError = stringValue(dto.VerificationError)
	model.Primary = types.BoolValue(dto.Primary)

	if dto.VerifiedAt != nil {
		model.VerifiedAt = types.StringValue(dto.VerifiedAt.UTC().Format(time.RFC3339))
	} else {
		model.VerifiedAt = types.StringNull()
	}

	// Traffic always routes via a CNAME at the hostname pointing at the
	// devhelm-assigned target. This is independent of verification method.
	trafficName := dto.Hostname
	trafficValue := dto.VerificationCnameTarget
	model.TrafficRecord = dnsRecordObject(trafficName, "CNAME", trafficValue)

	// Verification placement depends on the method the API picked.
	switch dto.VerificationMethod {
	case generated.StatusPageCustomDomainDtoVerificationMethodTXT:
		model.VerificationRecord = dnsRecordObject(
			"_devhelm-verification."+dto.Hostname,
			"TXT",
			dto.VerificationToken,
		)
	default:
		// CNAME (default) — verification record IS the traffic record.
		model.VerificationRecord = dnsRecordObject(trafficName, "CNAME", trafficValue)
	}
}
