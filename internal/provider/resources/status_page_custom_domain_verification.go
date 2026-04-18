package resources

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &StatusPageCustomDomainVerificationResource{}

// StatusPageCustomDomainVerificationResource is a synthetic "barrier"
// resource modelled on `aws_acm_certificate_validation`. It has no
// server-side counterpart: its sole job during Create is to repeatedly
// invoke the DevHelm verify endpoint until the API confirms that the
// operator's DNS record is in place and the domain has reached a verified
// state, or until the polling budget is exhausted.
//
// Because both inputs are RequiresReplace, in-place updates cannot occur;
// the only way to retry verification is to taint or delete-and-recreate
// this resource. Deleting it does NOT un-verify the underlying domain.
type StatusPageCustomDomainVerificationResource struct {
	client *api.Client
}

type StatusPageCustomDomainVerificationResourceModel struct {
	StatusPageID   types.String `tfsdk:"status_page_id"`
	CustomDomainID types.String `tfsdk:"custom_domain_id"`
	Status         types.String `tfsdk:"status"`
	VerifiedAt     types.String `tfsdk:"verified_at"`
}

// Polling parameters. Defaults are tuned so that DNS propagation has time
// to settle (most CDN-fronted DNS providers converge in under 5 minutes)
// while still bounding the worst-case apply duration. Both can be
// overridden via env vars; this is intended for tests, not for end users
// (who should use Terraform's built-in -timeout flag if they need to
// extend the per-operation budget further).
const (
	defaultVerifyPollInterval = 10 * time.Second
	defaultVerifyMaxAttempts  = 60 // 60 * 10s = 10 minutes
)

func NewStatusPageCustomDomainVerificationResource() resource.Resource {
	return &StatusPageCustomDomainVerificationResource{}
}

func (r *StatusPageCustomDomainVerificationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_status_page_custom_domain_verification"
}

func (r *StatusPageCustomDomainVerificationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version: 0,
		Description: "Blocks terraform apply until the DevHelm API confirms a custom domain " +
			"is verified. Models the aws_acm_certificate_validation pattern.",
		MarkdownDescription: "Blocks `terraform apply` until the DevHelm API confirms that a " +
			"[`devhelm_status_page_custom_domain`](./status_page_custom_domain) is verified.\n\n" +
			"This resource has **no server-side counterpart**: its `Create` operation polls the " +
			"DevHelm verify endpoint (which itself performs a live DNS lookup) until the domain " +
			"reaches a verified status (`VERIFIED`, `SSL_PENDING`, or `ACTIVE`), or until the " +
			"polling budget is exhausted. Modeled on " +
			"[`aws_acm_certificate_validation`](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/acm_certificate_validation).\n\n" +
			"Use `depends_on` to express the relationship to whatever DNS resource creates the " +
			"verification record. Without that dependency, Terraform may attempt verification " +
			"before the DNS record is propagated.\n\n" +
			"### Behavior\n\n" +
			"- **Create**: polls every 10 seconds for up to 10 minutes (60 attempts).\n" +
			"- **Read**: refreshes the underlying domain status into `status` and `verified_at`.\n" +
			"- **Update**: never invoked (both inputs force replacement).\n" +
			"- **Delete**: no-op. Deleting this resource does **not** un-verify the domain — " +
			"to remove the domain entirely, delete the `devhelm_status_page_custom_domain` resource.\n\n" +
			"### Re-running verification\n\n" +
			"To force re-verification after fixing a DNS issue, taint the resource:\n\n" +
			"```bash\n" +
			"terraform taint devhelm_status_page_custom_domain_verification.acme\n" +
			"terraform apply\n" +
			"```\n",
		Attributes: map[string]schema.Attribute{
			"status_page_id": schema.StringAttribute{
				Required:    true,
				Description: "ID of the status page that owns the custom domain. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"custom_domain_id": schema.StringAttribute{
				Required:    true,
				Description: "ID of the devhelm_status_page_custom_domain to verify. Changing this forces a new resource.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"status": schema.StringAttribute{
				Computed: true,
				Description: "Final domain status reported by the API after verification succeeded " +
					"(typically VERIFIED, SSL_PENDING, or ACTIVE).",
			},
			"verified_at": schema.StringAttribute{
				Computed:    true,
				Description: "RFC3339 timestamp when the API recorded successful verification.",
			},
		},
	}
}

func (r *StatusPageCustomDomainVerificationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *StatusPageCustomDomainVerificationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan StatusPageCustomDomainVerificationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pageID := plan.StatusPageID.ValueString()
	domainID := plan.CustomDomainID.ValueString()
	verifyPath := fmt.Sprintf("/api/v1/status-pages/%s/domains/%s/verify", pageID, domainID)

	pollInterval, maxAttempts := verifyPollingParams()

	var lastDomain *generated.StatusPageCustomDomainDto
	var lastError string

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		domain, err := api.Create[generated.StatusPageCustomDomainDto](ctx, r.client, verifyPath, nil)
		if err != nil {
			resp.Diagnostics.AddError(
				"Verification request failed",
				fmt.Sprintf("attempt %d/%d: %s", attempt, maxAttempts, err.Error()),
			)
			return
		}
		lastDomain = domain

		if isVerifiedStatus(domain.Status) {
			break
		}
		if domain.VerificationError != nil {
			lastError = *domain.VerificationError
		}

		if attempt == maxAttempts {
			resp.Diagnostics.AddError(
				"Verification timed out",
				fmt.Sprintf(
					"Domain %s did not reach a verified state after %d attempts (last status=%s, last_error=%q). "+
						"Confirm that the verification DNS record matches verification_record on the parent "+
						"devhelm_status_page_custom_domain resource and that DNS has propagated.",
					domainID, maxAttempts, domain.Status, lastError,
				),
			)
			return
		}

		select {
		case <-ctx.Done():
			resp.Diagnostics.AddError("Verification cancelled", ctx.Err().Error())
			return
		case <-time.After(pollInterval):
		}
	}

	r.applyDomainToState(&plan, lastDomain)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *StatusPageCustomDomainVerificationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state StatusPageCustomDomainVerificationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pageID := state.StatusPageID.ValueString()
	domainID := state.CustomDomainID.ValueString()

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

	for _, d := range domains {
		if d.Id.String() == domainID {
			r.applyDomainToState(&state, &d)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			return
		}
	}

	// Underlying domain was deleted out-of-band; the verification record is
	// also stale. Drop it from state so the next plan re-creates it (or
	// simply removes it if the user has since dropped the verification
	// resource from their config).
	resp.State.RemoveResource(ctx)
}

func (r *StatusPageCustomDomainVerificationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Both inputs carry RequiresReplace, so Update is structurally
	// unreachable for input changes. Computed fields refresh through Read.
	// We still implement the method to satisfy the resource interface and
	// preserve plan-time values without re-polling.
	var plan StatusPageCustomDomainVerificationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *StatusPageCustomDomainVerificationResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
	// Intentional no-op. There is no /verify-cancel endpoint and the
	// underlying domain remains verified server-side; the verification
	// resource is purely a Terraform-side barrier. To remove the domain,
	// delete the parent devhelm_status_page_custom_domain resource.
}

func (r *StatusPageCustomDomainVerificationResource) applyDomainToState(model *StatusPageCustomDomainVerificationResourceModel, dto *generated.StatusPageCustomDomainDto) {
	model.Status = types.StringValue(string(dto.Status))
	if dto.VerifiedAt != nil {
		model.VerifiedAt = types.StringValue(dto.VerifiedAt.UTC().Format(time.RFC3339))
	} else {
		model.VerifiedAt = types.StringNull()
	}
}

// isVerifiedStatus returns true for any domain status that means "the API
// has confirmed ownership and we can stop polling." VERIFIED is the
// immediate post-DNS state; SSL_PENDING and ACTIVE are subsequent stages
// that imply verification already succeeded.
func isVerifiedStatus(s generated.StatusPageCustomDomainDtoStatus) bool {
	switch s {
	case generated.VERIFIED, generated.SSLPENDING, generated.ACTIVE:
		return true
	}
	return false
}

// verifyPollingParams resolves the polling interval and max attempts,
// honoring DEVHELM_TF_VERIFY_POLL_INTERVAL_MS and
// DEVHELM_TF_VERIFY_MAX_ATTEMPTS for tests. Bad/zero values fall back to
// the defaults rather than producing infinite loops.
func verifyPollingParams() (time.Duration, int) {
	interval := defaultVerifyPollInterval
	if raw := os.Getenv("DEVHELM_TF_VERIFY_POLL_INTERVAL_MS"); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil && ms > 0 {
			interval = time.Duration(ms) * time.Millisecond
		}
	}

	maxAttempts := defaultVerifyMaxAttempts
	if raw := os.Getenv("DEVHELM_TF_VERIFY_MAX_ATTEMPTS"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			maxAttempts = n
		}
	}

	return interval, maxAttempts
}
