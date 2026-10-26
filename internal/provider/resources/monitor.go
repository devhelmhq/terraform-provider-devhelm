package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

var (
	_ resource.Resource                   = &MonitorResource{}
	_ resource.ResourceWithImportState    = &MonitorResource{}
	_ resource.ResourceWithValidateConfig = &MonitorResource{}
)

type MonitorResource struct {
	client *api.Client
}

type MonitorResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Type             types.String `tfsdk:"type"`
	FrequencySeconds types.Int64  `tfsdk:"frequency_seconds"`
	Enabled          types.Bool   `tfsdk:"enabled"`
	Regions          types.List   `tfsdk:"regions"`
	EnvironmentID    types.String `tfsdk:"environment_id"`
	AlertChannelIds  types.List   `tfsdk:"alert_channel_ids"`
	TagIds           types.List   `tfsdk:"tag_ids"`
	PingUrl          types.String `tfsdk:"ping_url"`

	Config         types.String `tfsdk:"config"`
	Auth           types.Object `tfsdk:"auth"`
	Assertions     types.List   `tfsdk:"assertions"`
	IncidentPolicy types.Object `tfsdk:"incident_policy"`

	// Code monitors (BROWSER, MULTI_STEP_API).
	Code                 types.Object `tfsdk:"code"`
	CodeHash             types.String `tfsdk:"code_hash"`
	CodeFiles            types.Map    `tfsdk:"code_files"`
	LiveRevisionID       types.String `tfsdk:"live_revision_id"`
	LiveRevisionDigest   types.String `tfsdk:"live_revision_digest"`
	CapturePolicy        types.Object `tfsdk:"capture_policy"`
	FastRetryMaxAttempts types.Int64  `tfsdk:"fast_retry_max_attempts"`
	RunParallel          types.Bool   `tfsdk:"run_parallel"`
	SecretKeys           types.List   `tfsdk:"secret_keys"`
	TestBeforePublish    types.Bool   `tfsdk:"test_before_publish"`
	AcknowledgeRollback  types.String `tfsdk:"acknowledge_rollback"`

	// Ownership.
	Owner         types.Object `tfsdk:"owner"`
	OwnerEnforced types.Bool   `tfsdk:"owner_enforced"`
	OwnerAddress  types.String `tfsdk:"owner_address"`

	Timeouts timeouts.Value `tfsdk:"timeouts"`
}

// authSecretOnlyVariantModel mirrors the bearer/basic variants: the only
// HCL-exposed field is `vault_secret_id` (the credential itself lives in
// vault). Required-field semantics are enforced by validateMonitorAuth.
type authSecretOnlyVariantModel struct {
	VaultSecretID types.String `tfsdk:"vault_secret_id"`
}

// authHeaderVariantModel mirrors the header/api_key variants which add
// the wire-level `header_name` attribute on top of `vault_secret_id`.
type authHeaderVariantModel struct {
	HeaderName    types.String `tfsdk:"header_name"`
	VaultSecretID types.String `tfsdk:"vault_secret_id"`
}

// authModel mirrors the shape of the `auth` attribute. Exactly one of
// the four variant fields must be non-null at plan time.
type authModel struct {
	Bearer types.Object `tfsdk:"bearer"`
	Basic  types.Object `tfsdk:"basic"`
	Header types.Object `tfsdk:"header"`
	ApiKey types.Object `tfsdk:"api_key"`
}

func NewMonitorResource() resource.Resource {
	return &MonitorResource{}
}

func (r *MonitorResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_monitor"
}

func (r *MonitorResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version: 0,
		Description: "Manages a DevHelm monitor. Supports HTTP, DNS, TCP, ICMP, Heartbeat, and MCP Server probes, " +
			"and BROWSER and MULTI_STEP_API code monitors. A code monitor points `code.entrypoint` at a " +
			"Playwright file; the provider uploads the include set at plan time and, on apply, runs a " +
			"validation run before the code goes live (`test_before_publish`). Next to Terraform, " +
			"`devhelm test --from-terraform .` runs the configured code without applying it, and " +
			"`devhelm trigger <monitor>` starts a run of the live code.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, Description: "Unique identifier for this monitor",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required: true, Description: "Human-readable name for this monitor",
			},
			"type": schema.StringAttribute{
				Required: true, Description: "Monitor type: HTTP, DNS, TCP, ICMP, HEARTBEAT, MCP_SERVER, BROWSER, or MULTI_STEP_API",
				Validators: []validator.String{
					stringvalidator.OneOf(
						string(generated.CreateMonitorRequestTypeHTTP),
						string(generated.CreateMonitorRequestTypeDNS),
						string(generated.CreateMonitorRequestTypeTCP),
						string(generated.CreateMonitorRequestTypeICMP),
						string(generated.CreateMonitorRequestTypeHEARTBEAT),
						string(generated.CreateMonitorRequestTypeMCPSERVER),
						string(generated.CreateMonitorRequestTypeBROWSER),
						string(generated.CreateMonitorRequestTypeMULTISTEPAPI),
					),
				},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"frequency_seconds": schema.Int64Attribute{
				Optional: true, Computed: true,
				Description: "Check frequency in seconds (10\u201386400); the minimum depends on the plan. " +
					"The server applies the plan minimum when omitted on create. " +
					"Omit on update to preserve the current value; the API has no way to clear this field.",
				Validators: []validator.Int64{
					int64validator.Between(10, 86400),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"enabled": schema.BoolAttribute{
				Optional: true, Computed: true,
				Description: "Whether the monitor is active; omit to leave pause state unmanaged",
				PlanModifiers: []planmodifier.Bool{
					boolplanmodifier.UseStateForUnknown(),
				},
			},
			"regions": schema.ListAttribute{
				Optional: true, Computed: true, ElementType: types.StringType,
				Description: "Probe regions (e.g. us-east, eu-west). " +
					"Omit to preserve the server's current regions; explicitly set to `[]` to clear.",
				PlanModifiers: []planmodifier.List{
					listplanmodifier.UseStateForUnknown(),
				},
			},
			"environment_id": schema.StringAttribute{
				Optional: true,
				Description: "Environment the monitor runs in (variables and secret values). " +
					"Required for BROWSER and MULTI_STEP_API monitors",
			},
			"alert_channel_ids": schema.ListAttribute{
				Optional: true, Computed: true, ElementType: types.StringType,
				Description: "Alert channel IDs to notify on incidents. " +
					"Omit to preserve the current list; explicitly set to `[]` to clear all channels.",
				PlanModifiers: []planmodifier.List{
					UseStateForUnknownAlwaysList(),
				},
			},
			"tag_ids": schema.ListAttribute{
				Optional: true, ElementType: types.StringType,
				Description: "Tag IDs to attach to this monitor",
			},
			"ping_url": schema.StringAttribute{
				Computed: true, Description: "Heartbeat ping URL (only set for HEARTBEAT monitors)",
				PlanModifiers: []planmodifier.String{UseStateForUnknownAlwaysString()},
			},
			"config": schema.StringAttribute{
				Optional: true, Computed: true,
				Description: "Monitor configuration as JSON. Shape depends on type (HttpMonitorConfig, DnsMonitorConfig, etc.). " +
					"Required for probes; optional for BROWSER and MULTI_STEP_API (ScriptMonitorConfig: " +
					"`timeoutSeconds`, `runtimeId`)",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"code": schema.SingleNestedAttribute{
				Optional: true,
				Description: "Code for a BROWSER or MULTI_STEP_API monitor. The provider uploads the include set " +
					"(the entry file's directory, or `include`) at plan time; the server resolves the import closure. " +
					"Forbidden for probes",
				Attributes: map[string]schema.Attribute{
					"entrypoint": schema.StringAttribute{
						Required:    true,
						Description: "Path to the file that runs, e.g. `${path.module}/synthetics/checkout.spec.ts`",
						Validators:  []validator.String{stringvalidator.LengthAtLeast(1)},
					},
					"include": schema.ListAttribute{
						Optional: true, ElementType: types.StringType,
						Description: "Globs for files that ship even though nothing imports them (fixtures, data), " +
							"added to the default set: the entry file's directory, recursively, minus node_modules/, " +
							"dist/, build/, coverage/, *.map, dotfiles and .devhelmignore matches",
					},
					"root": schema.StringAttribute{
						Optional: true,
						Description: "Bundle root; paths are uploaded relative to it (default: the nearest ancestor " +
							"containing .git, else the entry file's directory)",
					},
				},
			},
			"code_hash": schema.StringAttribute{
				Computed:    true,
				Description: "Closure digest (sha256:<hex>) the server computed for the configured code at plan time",
			},
			"code_files": schema.MapAttribute{
				Computed: true, ElementType: types.StringType,
				Description: "Files in the import closure, each mapped to its content digest; the plan diff lists " +
					"added, removed and changed files",
			},
			"live_revision_id": schema.StringAttribute{
				Computed:    true,
				Description: "Revision that scheduled runs execute",
			},
			"live_revision_digest": schema.StringAttribute{
				Computed:    true,
				Description: "Closure digest of the live revision; differs from code_hash after a rollback",
			},
			"capture_policy": schema.SingleNestedAttribute{
				Optional: true, Computed: true,
				Description:   "When to keep screenshots, traces, video and network evidence (code monitors)",
				PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
				Attributes: map[string]schema.Attribute{
					"screenshots": capturePolicyAttribute("When to capture screenshots"),
					"trace":       capturePolicyAttribute("When to capture a Playwright trace"),
					"video":       capturePolicyAttribute("When to capture video"),
					"network":     capturePolicyAttribute("When network rows and HAR are kept"),
				},
			},
			"fast_retry_max_attempts": schema.Int64Attribute{
				Optional: true, Computed: true,
				Description:   "Fast-retry attempts after a failure; 0 disables",
				Validators:    []validator.Int64{int64validator.AtLeast(0)},
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"run_parallel": schema.BoolAttribute{
				Optional: true, Computed: true,
				Description:   "Run every region each interval; false rotates one region per interval (code monitors)",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"secret_keys": schema.ListAttribute{
				Optional: true, Computed: true, ElementType: types.StringType,
				Description:   "Secret keys the code may read; omit to use the keys the server scanned in the code",
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"test_before_publish": schema.BoolAttribute{
				Optional: true,
				Description: "Run a validation run before new code goes live (default: true). A failed run stops " +
					"the apply and the old code stays live. false publishes the code untested in one call",
			},
			"acknowledge_rollback": schema.StringAttribute{
				Optional: true,
				Description: "Rolled-back digest to publish again on purpose; needed when the configured code " +
					"was rolled back in the dashboard",
			},
			"owner": schema.SingleNestedAttribute{
				Computed:      true,
				Description:   "Tool that owns this monitor and what it declared",
				PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
				Attributes: map[string]schema.Attribute{
					"type":    schema.StringAttribute{Computed: true, Description: "Owning tool: DASHBOARD, GIT, TERRAFORM, MCP, or API"},
					"project": schema.StringAttribute{Computed: true, Description: "Project the owning tool declares"},
					"address": schema.StringAttribute{Computed: true, Description: "Resource address the owning tool declares"},
					"label":   schema.StringAttribute{Computed: true, Description: "Display label the owning tool declares"},
				},
			},
			"owner_enforced": schema.BoolAttribute{
				Optional: true, Computed: true,
				Description: "Only Terraform may change this monitor's configuration (default: true on create). " +
					"Use an API key created with tool TERRAFORM",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"owner_address": schema.StringAttribute{
				Optional: true,
				Description: "Resource address recorded as the owner declaration, e.g. " +
					"`module.web.devhelm_monitor.checkout` (Terraform doesn't tell providers the address)",
			},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{Create: true, Update: true}),
			"auth": schema.SingleNestedAttribute{
				Optional: true,
				Description: "Monitor authentication. Specify exactly one variant " +
					"(`bearer`, `basic`, `header`, or `api_key`). Credentials are " +
					"never sent through the API in plaintext — set " +
					"`vault_secret_id` to a `devhelm_secret` ID; the probe resolves " +
					"the secret server-side at check time. The block itself is not " +
					"marked sensitive because it carries only UUID references; the " +
					"actual secret material lives on `devhelm_secret.value` which is " +
					"sensitive.",
				Attributes: map[string]schema.Attribute{
					"bearer": schema.SingleNestedAttribute{
						Optional:    true,
						Description: "Bearer token sent in the `Authorization` header. Reference the token via `vault_secret_id`.",
						Attributes: map[string]schema.Attribute{
							"vault_secret_id": schema.StringAttribute{
								Required:    true,
								Description: "Vault secret ID holding the bearer token value (UUID).",
								Validators:  []validator.String{uuidStringValidator{}},
							},
						},
					},
					"basic": schema.SingleNestedAttribute{
						Optional:    true,
						Description: "HTTP Basic auth. Reference the username/password vault secret via `vault_secret_id`.",
						Attributes: map[string]schema.Attribute{
							"vault_secret_id": schema.StringAttribute{
								Required:    true,
								Description: "Vault secret ID holding Basic auth username and password (UUID).",
								Validators:  []validator.String{uuidStringValidator{}},
							},
						},
					},
					"header": schema.SingleNestedAttribute{
						Optional:    true,
						Description: "Custom header with an arbitrary name and a secret value resolved from the vault.",
						Attributes: map[string]schema.Attribute{
							"header_name": schema.StringAttribute{
								Required:    true,
								Description: "Custom HTTP header name for the secret value (matches `^[A-Za-z0-9\\-_]+$`).",
							},
							"vault_secret_id": schema.StringAttribute{
								Required:    true,
								Description: "Vault secret ID for the header value (UUID).",
								Validators:  []validator.String{uuidStringValidator{}},
							},
						},
					},
					"api_key": schema.SingleNestedAttribute{
						Optional:    true,
						Description: "API key sent in a configurable header.",
						Attributes: map[string]schema.Attribute{
							"header_name": schema.StringAttribute{
								Required:    true,
								Description: "HTTP header name that carries the API key (matches `^[A-Za-z0-9\\-_]+$`).",
							},
							"vault_secret_id": schema.StringAttribute{
								Required:    true,
								Description: "Vault secret ID for the API key value (UUID).",
								Validators:  []validator.String{uuidStringValidator{}},
							},
						},
					},
				},
			},
			"incident_policy": schema.SingleNestedAttribute{
				Optional: true, Computed: true,
				Description: "Incident policy controlling when failures escalate to incidents. " +
					"The API auto-creates a default policy on monitor creation, so omitting this attribute " +
					"adopts the server defaults; supplying any field overrides the policy in full.",
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.UseStateForUnknown(),
				},
				Attributes: map[string]schema.Attribute{
					"confirmation_type": schema.StringAttribute{
						Optional: true, Computed: true,
						Description:   "Confirmation strategy type (e.g. multi_region)",
						PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
					},
					"min_regions_failing": schema.Int64Attribute{
						Optional: true, Computed: true,
						Description:   "Minimum regions that must fail to confirm an incident",
						PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
					},
					"max_wait_seconds": schema.Int64Attribute{
						Optional: true, Computed: true,
						Description:   "Maximum seconds to wait for multi-region confirmation",
						PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
					},
					"consecutive_successes": schema.Int64Attribute{
						Optional: true, Computed: true,
						Description:   "Consecutive successes required for recovery",
						PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
					},
					"min_regions_passing": schema.Int64Attribute{
						Optional: true, Computed: true,
						Description:   "Minimum regions passing for recovery",
						PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
					},
					"cooldown_minutes": schema.Int64Attribute{
						Optional: true, Computed: true,
						Description:   "Minutes to wait before auto-resolving",
						PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
					},
					"trigger_rules": schema.ListNestedAttribute{
						Optional: true, Computed: true,
						Description:   "Rules that determine when failures escalate to incidents.",
						PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
						NestedObject: schema.NestedAttributeObject{
							Attributes: map[string]schema.Attribute{
								"type": schema.StringAttribute{
									Required:    true,
									Description: "Rule type: consecutive_failures, failures_in_window, or response_time",
									Validators: []validator.String{
										stringvalidator.OneOf(
											string(generated.TriggerRuleTypeConsecutiveFailures),
											string(generated.TriggerRuleTypeFailuresInWindow),
											string(generated.TriggerRuleTypeResponseTime),
										),
									},
								},
								"severity": schema.StringAttribute{
									Required:    true,
									Description: "Incident severity: down or degraded",
									Validators: []validator.String{
										stringvalidator.OneOf(
											string(generated.TriggerRuleSeverityDown),
											string(generated.TriggerRuleSeverityDegraded),
										),
									},
								},
								"scope": schema.StringAttribute{
									Optional: true, Computed: true,
									Description: "Rule scope: per_region or any_region",
									Validators: []validator.String{
										stringvalidator.OneOf(
											string(generated.TriggerRuleScopePerRegion),
											string(generated.TriggerRuleScopeAnyRegion),
										),
									},
									PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
								},
								"count": schema.Int64Attribute{
									Optional: true, Computed: true,
									Description: "Failure count threshold (1–10)",
									Validators: []validator.Int64{
										int64validator.Between(1, 10),
									},
									PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
								},
								"window_minutes": schema.Int64Attribute{
									Optional: true, Computed: true,
									Description: "Time window in minutes (for failures_in_window)",
									Validators: []validator.Int64{
										int64validator.AtLeast(1),
									},
									PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
								},
								"threshold_ms": schema.Int64Attribute{
									Optional: true, Computed: true,
									Description: "Response time threshold in ms (for response_time)",
									Validators: []validator.Int64{
										int64validator.AtLeast(1),
									},
									PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
								},
								"aggregation_type": schema.StringAttribute{
									Optional: true, Computed: true,
									Description: "Aggregation type: all_exceed, average, p95, max",
									Validators: []validator.String{
										stringvalidator.OneOf(
											string(generated.TriggerRuleAggregationTypeAllExceed),
											string(generated.TriggerRuleAggregationTypeAverage),
											string(generated.TriggerRuleAggregationTypeP95),
											string(generated.TriggerRuleAggregationTypeMax),
										),
									},
									PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
								},
							},
						},
					},
				},
			},
		},
		Blocks: map[string]schema.Block{
			"assertions": schema.ListNestedBlock{
				Description: "Monitor assertions that define pass/fail criteria.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"type": schema.StringAttribute{
							Required:    true,
							Description: "Assertion type discriminator in snake_case wire format (e.g. `status_code`, `response_time`, `body_contains`, `header_value`, `dns_resolves`, `ssl_expiry`, `tcp_connects`). Must match an AssertionType enum value as serialized by the API.",
							Validators: []validator.String{
								stringvalidator.OneOf(api.AssertionTypes...),
							},
						},
						"config": schema.StringAttribute{
							Required:    true,
							Description: "Assertion configuration as JSON; the inner `type` field is omitted (set via the sibling `type` attribute) and the rest of the shape depends on the assertion kind. Field names inside the JSON are camelCase (the API wire format) and JSON value types must match the API contract exactly — e.g. `status_code.expected` is a STRING (`jsonencode({expected = \"200\", operator = \"equals\"})`, not `expected = 200`), while `response_time.thresholdMs` is a NUMBER (`jsonencode({thresholdMs = 500})`). Type-mismatched values plan cleanly but fail apply with \"Provider produced inconsistent result\" because the API normalizes them on the round-trip.",
						},
						"severity": schema.StringAttribute{
							Optional:    true,
							Description: "Assertion severity: fail or warn (default: fail)",
							Validators: []validator.String{
								stringvalidator.OneOf(
									string(generated.CreateAssertionRequestSeverityFail),
									string(generated.CreateAssertionRequestSeverityWarn),
								),
							},
						},
					},
				},
			},
		},
	}
}

func (r *MonitorResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var model MonitorResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &model)...)
	if resp.Diagnostics.HasError() {
		return
	}

	validateMonitorKind(&resp.Diagnostics, &model)

	// Config validation: parse as JSON, then validate the payload against
	// the generated monitor-config struct that matches the declared `type`.
	// We use json.Decoder with DisallowUnknownFields so unknown keys are
	// caught at plan time instead of being silently dropped (or surfacing
	// as a confusing 400 from the API). Required fields are caught by the
	// generated struct's required-field tags via ValidateDTO. Wire field
	// names are camelCase (HttpMonitorConfig.url, dnsMonitorConfig.hostname,
	// etc.) — the JSON key names map directly to the struct's `json:"…"`
	// tags, which is what users put inside `jsonencode({…})`.
	if !model.Config.IsNull() && !model.Config.IsUnknown() {
		configJSON := model.Config.ValueString()
		monitorType := generated.CreateMonitorRequestType(model.Type.ValueString())
		validateMonitorConfigJSON(&resp.Diagnostics, configJSON, monitorType)
	}

	// Assertion config validation: each assertion's `config` is a JSON
	// string whose shape depends on the sibling `type` discriminator.
	// Decode it against the generated per-assertion-type struct so type
	// mismatches (e.g. `expected = 200` where the API wants the STRING
	// "200"), typos, and missing required fields surface at plan time
	// instead of failing apply with "Provider produced inconsistent
	// result". Diagnostics are routed to the offending block's `config`.
	if !model.Assertions.IsNull() && !model.Assertions.IsUnknown() {
		var assertions []assertionModel
		if d := model.Assertions.ElementsAs(ctx, &assertions, false); !d.HasError() {
			for i, a := range assertions {
				if a.Config.IsNull() || a.Config.IsUnknown() || a.Type.IsNull() || a.Type.IsUnknown() {
					continue
				}
				cfgPath := path.Root("assertions").AtListIndex(i).AtName("config")
				validateAssertionConfigJSON(&resp.Diagnostics, a.Config.ValueString(), a.Type.ValueString(), cfgPath)
			}
		}
	}

	// Auth validation: enforce the exactly-one-variant invariant and
	// per-variant required fields at plan time. The schema cannot express
	// "exactly one of bearer/basic/header/api_key" on its own (every
	// sub-attribute is optional so the user can choose any single variant),
	// so we resolve the constraint here and route diagnostics to the
	// offending nested path.
	validateMonitorAuth(ctx, &resp.Diagnostics, model.Auth)

	// Trigger rule conditional validation: count is required for all rule types.
	if !model.IncidentPolicy.IsNull() && !model.IncidentPolicy.IsUnknown() {
		var policy incidentPolicyModel
		diags := model.IncidentPolicy.As(ctx, &policy, basetypes.ObjectAsOptions{
			UnhandledNullAsEmpty:    true,
			UnhandledUnknownAsEmpty: true,
		})
		if !diags.HasError() && !policy.TriggerRules.IsNull() && !policy.TriggerRules.IsUnknown() {
			var rules []triggerRuleModel
			if ruleDiags := policy.TriggerRules.ElementsAs(ctx, &rules, false); !ruleDiags.HasError() {
				for i, rule := range rules {
					rulePath := path.Root("incident_policy").AtName("trigger_rules").AtListIndex(i)

					if rule.Count.IsNull() || rule.Count.IsUnknown() {
						resp.Diagnostics.AddAttributeError(
							rulePath.AtName("count"),
							"Missing required attribute",
							"Trigger rule count is required for all rule types",
						)
					}

					ruleType := generated.TriggerRuleType(rule.Type.ValueString())
					if ruleType == generated.TriggerRuleTypeFailuresInWindow && (rule.WindowMinutes.IsNull() || rule.WindowMinutes.IsUnknown()) {
						resp.Diagnostics.AddAttributeError(
							rulePath.AtName("window_minutes"),
							"Missing required attribute",
							"window_minutes is required when trigger rule type is failures_in_window",
						)
					}

					if ruleType == generated.TriggerRuleTypeResponseTime && (rule.ThresholdMs.IsNull() || rule.ThresholdMs.IsUnknown()) {
						resp.Diagnostics.AddAttributeError(
							rulePath.AtName("threshold_ms"),
							"Missing required attribute",
							"threshold_ms is required when trigger rule type is response_time",
						)
					}
				}
			}
		}
	}
}

// validateMonitorKind enforces the per-kind attribute rules: code monitors
// need `code` and `environment_id`; probes need `config` and can't carry
// code-only settings.
func validateMonitorKind(diags *diag.Diagnostics, m *MonitorResourceModel) {
	if m.Type.IsUnknown() || m.Type.IsNull() {
		return
	}
	if isCodeType(m.Type) {
		if m.Code.IsNull() {
			diags.AddAttributeError(path.Root("code"), "Missing code",
				fmt.Sprintf("%s monitors need a `code` block with `entrypoint`.", m.Type.ValueString()))
		}
		if m.EnvironmentID.IsNull() {
			diags.AddAttributeError(path.Root("environment_id"), "Missing environment_id",
				fmt.Sprintf("%s monitors run in one environment; set environment_id (for example "+
					"devhelm_environment.production.id).", m.Type.ValueString()))
		}
		return
	}
	if !m.Code.IsNull() {
		diags.AddAttributeError(path.Root("code"), "code is only for BROWSER and MULTI_STEP_API monitors",
			fmt.Sprintf("%s monitors are configured with `config`; remove the `code` block.", m.Type.ValueString()))
	}
	if m.Config.IsNull() {
		diags.AddAttributeError(path.Root("config"), "Missing config",
			fmt.Sprintf("%s monitors need `config` (JSON).", m.Type.ValueString()))
	}
	for name, v := range map[string]attr.Value{
		"run_parallel":         m.RunParallel,
		"capture_policy":       m.CapturePolicy,
		"secret_keys":          m.SecretKeys,
		"acknowledge_rollback": m.AcknowledgeRollback,
	} {
		if !v.IsNull() && !v.IsUnknown() {
			diags.AddAttributeError(path.Root(name), name+" is only for code monitors",
				fmt.Sprintf("%s applies to BROWSER and MULTI_STEP_API monitors; remove it from this %s monitor.",
					name, m.Type.ValueString()))
		}
	}
}

func capturePolicyAttribute(description string) schema.StringAttribute {
	return schema.StringAttribute{
		Optional: true, Computed: true,
		Description: description + ": always, on_failure, or off",
		Validators: []validator.String{stringvalidator.OneOf(
			string(generated.CapturePolicyScreenshotsAlways),
			string(generated.CapturePolicyScreenshotsOnFailure),
			string(generated.CapturePolicyScreenshotsOff),
		)},
		PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
	}
}

func (r *MonitorResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// validateMonitorConfigJSON parses the JSON-string `config` payload and
// validates it against the generated Go struct that matches the declared
// monitor `type`. Three classes of bugs surface at plan time instead of
// apply time:
//
//  1. Invalid JSON (parse failure).
//  2. Unknown keys (typos like `mehtod` or `verifyTLS` instead of `verifyTls`).
//     Caught by `json.Decoder.DisallowUnknownFields()`.
//  3. Missing required fields (e.g. HTTP without `url`, DNS without
//     `hostname`). Caught by walking the parsed struct with `api.ValidateDTO`,
//     which uses oapi-codegen's "non-pointer = required" convention to flag
//     zero-valued required fields.
//
// All diagnostics are emitted on the `config` attribute path so the editor
// underlines the offending block instead of the resource root.
//
// Why we still keep `config` as a JSON-string overall (not a per-variant
// nested block):
//   - The OpenAPI spec for `auth` is incomplete (collapsed `oneOf` + omits
//     fields the API actually accepts), which would require either breaking
//     existing users or hardcoding undocumented fields.
//   - A full `config` nested-block migration touches 70+ surface fixtures,
//     ~60 examples/docs entries, and 5 acc tests — properly a dedicated
//     epic with a state-migration design doc, not a phase-2 bolt-on.
//   - Plan-time JSON validation here closes ~95% of the actual bug class
//     (typos, wrong field for type, missing required) without any of the
//     migration risk.
func validateMonitorConfigJSON(diags *diag.Diagnostics, raw string, monitorType generated.CreateMonitorRequestType) {
	if raw == "" {
		return
	}
	if !json.Valid([]byte(raw)) {
		diags.AddAttributeError(
			path.Root("config"),
			"Invalid JSON in config",
			fmt.Sprintf("The config attribute must be valid JSON; got %q", truncate(raw, 80)),
		)
		return
	}

	var validateAgainst func(decoder *json.Decoder) (any, error)
	switch monitorType {
	case generated.CreateMonitorRequestTypeHTTP:
		validateAgainst = func(d *json.Decoder) (any, error) {
			var v generated.HttpMonitorConfig
			err := d.Decode(&v)
			return &v, err
		}
	case generated.CreateMonitorRequestTypeDNS:
		validateAgainst = func(d *json.Decoder) (any, error) {
			var v generated.DnsMonitorConfig
			err := d.Decode(&v)
			return &v, err
		}
	case generated.CreateMonitorRequestTypeTCP:
		validateAgainst = func(d *json.Decoder) (any, error) {
			var v generated.TcpMonitorConfig
			err := d.Decode(&v)
			return &v, err
		}
	case generated.CreateMonitorRequestTypeICMP:
		validateAgainst = func(d *json.Decoder) (any, error) {
			var v generated.IcmpMonitorConfig
			err := d.Decode(&v)
			return &v, err
		}
	case generated.CreateMonitorRequestTypeHEARTBEAT:
		validateAgainst = func(d *json.Decoder) (any, error) {
			var v generated.HeartbeatMonitorConfig
			err := d.Decode(&v)
			return &v, err
		}
	case generated.CreateMonitorRequestTypeMCPSERVER:
		validateAgainst = func(d *json.Decoder) (any, error) {
			var v generated.McpServerMonitorConfig
			err := d.Decode(&v)
			return &v, err
		}
	case generated.CreateMonitorRequestTypeBROWSER, generated.CreateMonitorRequestTypeMULTISTEPAPI:
		validateAgainst = func(d *json.Decoder) (any, error) {
			var v generated.ScriptMonitorConfig
			err := d.Decode(&v)
			return &v, err
		}
	default:
		// Unknown type — `type`'s OneOf validator will already have flagged it.
		return
	}

	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	parsed, err := validateAgainst(dec)
	if err != nil {
		diags.AddAttributeError(
			path.Root("config"),
			fmt.Sprintf("Invalid config for monitor type %q", string(monitorType)),
			fmt.Sprintf("%s. Allowed fields depend on monitor type; refer to the %sMonitorConfig schema in the OpenAPI spec.",
				err, configSchemaName(monitorType)),
		)
		return
	}

	// Go's encoding/json matches struct field tags case-insensitively, so
	// `verifyTLS` happily decodes into `verifyTls` even with
	// DisallowUnknownFields. Walk the raw JSON top-level keys and compare
	// against the typed struct's json tags case-sensitively to catch this
	// class of typo at plan time.
	if extra := caseSensitiveUnknownKeys(raw, parsed); len(extra) > 0 {
		diags.AddAttributeError(
			path.Root("config"),
			fmt.Sprintf("Unknown field(s) in %q config", string(monitorType)),
			fmt.Sprintf("Field name(s) %v are not in the %sMonitorConfig schema. "+
				"JSON keys are case-sensitive — check that the field name matches the spec exactly.",
				extra, configSchemaName(monitorType)),
		)
		return
	}

	if validationErr := api.ValidateDTO(parsed, fmt.Sprintf("%s monitor config", monitorType)); validationErr != nil {
		diags.AddAttributeError(
			path.Root("config"),
			fmt.Sprintf("Missing or invalid fields in %q config", string(monitorType)),
			validationErr.Error(),
		)
	}
}

// validateAssertionConfigJSON parses an assertion's JSON-string `config`
// and validates it against the generated Go struct that matches the
// declared assertion `type`. It is the assertion analogue of
// validateMonitorConfigJSON and catches the same three bug classes at
// plan time instead of apply time:
//
//  1. Invalid JSON (parse failure).
//  2. Wrong JSON value type for a field — the bug that motivated this
//     validator. Users write `jsonencode({expected = 200})` (a NUMBER)
//     where `StatusCodeAssertion.expected` is a STRING. Decoding into the
//     typed struct fails with "cannot unmarshal number into … string",
//     surfacing the mismatch at `terraform plan` instead of the API
//     normalizing it on the round-trip and triggering "Provider produced
//     inconsistent result after apply".
//  3. Unknown keys / case-mismatched typos (DisallowUnknownFields plus the
//     case-sensitive key walk) and missing required fields (ValidateDTO).
//
// The per-assertion `config` omits the `type` discriminator (it lives on
// the sibling `type` attribute), so we inject it before decoding —
// mirroring buildAssertions — both to satisfy the struct's required
// `type` field and to keep the decode shape aligned with the wire format.
//
// This is the "Option B" interim approach: validate against generated
// structs without migrating `config` to a typed nested block (which would
// touch every surface fixture, example, and acc test).
func validateAssertionConfigJSON(diags *diag.Diagnostics, raw, assertionType string, cfgPath path.Path) {
	if raw == "" {
		return
	}
	if !json.Valid([]byte(raw)) {
		diags.AddAttributeError(
			cfgPath,
			"Invalid JSON in assertion config",
			fmt.Sprintf("The config attribute must be valid JSON; got %q", truncate(raw, 80)),
		)
		return
	}

	proto := assertionConfigPrototype(assertionType)
	if proto == nil {
		// Unknown assertion type — `type`'s OneOf validator already flags it.
		return
	}

	// Inject the discriminator (unless the user already supplied one) so the
	// generated struct's required `type` field is satisfied and the decode
	// shape matches the API wire format.
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &rawMap); err != nil {
		diags.AddAttributeError(cfgPath, "Invalid JSON in assertion config", err.Error())
		return
	}
	if _, ok := rawMap["type"]; !ok {
		rawMap["type"] = json.RawMessage(fmt.Sprintf("%q", assertionType))
	}
	wrapped, err := json.Marshal(rawMap)
	if err != nil {
		diags.AddAttributeError(cfgPath, "Invalid JSON in assertion config", err.Error())
		return
	}

	schemaName := assertionConfigSchemaName(assertionType)

	dec := json.NewDecoder(strings.NewReader(string(wrapped)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(proto); err != nil {
		diags.AddAttributeError(
			cfgPath,
			fmt.Sprintf("Invalid config for assertion type %q", assertionType),
			fmt.Sprintf("%s. Field names are camelCase and JSON value types must match the API contract "+
				"exactly — e.g. status_code.expected is a STRING (\"200\", not 200) while "+
				"response_time.thresholdMs is a NUMBER. Refer to the %s schema in the OpenAPI spec.",
				err, schemaName),
		)
		return
	}

	// Go's encoding/json matches struct field tags case-insensitively, so a
	// typo like `Substring` decodes into `substring` even with
	// DisallowUnknownFields. Catch that class case-sensitively.
	if extra := caseSensitiveUnknownKeys(string(wrapped), proto); len(extra) > 0 {
		diags.AddAttributeError(
			cfgPath,
			fmt.Sprintf("Unknown field(s) in %q assertion config", assertionType),
			fmt.Sprintf("Field name(s) %v are not in the %s schema. JSON keys are case-sensitive — "+
				"check that the field name matches the spec exactly.", extra, schemaName),
		)
		return
	}

	if validationErr := api.ValidateDTO(proto, fmt.Sprintf("%s assertion config", assertionType)); validationErr != nil {
		diags.AddAttributeError(
			cfgPath,
			fmt.Sprintf("Missing or invalid fields in %q assertion config", assertionType),
			validationErr.Error(),
		)
	}
}

// assertionConfigPrototype returns a pointer to the generated Go struct
// matching the given wire-format assertion type, or nil when the type is
// unknown (the `type` OneOf validator handles that case). The mapping
// mirrors api.AssertionTypes — every entry there has a struct here.
func assertionConfigPrototype(assertionType string) any {
	switch assertionType {
	case string(generated.BodyContainsAssertionTypeBodyContains):
		return &generated.BodyContainsAssertion{}
	case string(generated.DnsExpectedCnameAssertionTypeDnsExpectedCname):
		return &generated.DnsExpectedCnameAssertion{}
	case string(generated.DnsExpectedIpsAssertionTypeDnsExpectedIps):
		return &generated.DnsExpectedIpsAssertion{}
	case string(generated.DnsMaxAnswersAssertionTypeDnsMaxAnswers):
		return &generated.DnsMaxAnswersAssertion{}
	case string(generated.DnsMinAnswersAssertionTypeDnsMinAnswers):
		return &generated.DnsMinAnswersAssertion{}
	case string(generated.DnsRecordContainsAssertionTypeDnsRecordContains):
		return &generated.DnsRecordContainsAssertion{}
	case string(generated.DnsRecordEqualsAssertionTypeDnsRecordEquals):
		return &generated.DnsRecordEqualsAssertion{}
	case string(generated.DnsResolvesAssertionTypeDnsResolves):
		return &generated.DnsResolvesAssertion{}
	case string(generated.DnsResponseTimeAssertionTypeDnsResponseTime):
		return &generated.DnsResponseTimeAssertion{}
	case string(generated.DnsResponseTimeWarnAssertionTypeDnsResponseTimeWarn):
		return &generated.DnsResponseTimeWarnAssertion{}
	case string(generated.DnsTtlHighAssertionTypeDnsTtlHigh):
		return &generated.DnsTtlHighAssertion{}
	case string(generated.DnsTtlLowAssertionTypeDnsTtlLow):
		return &generated.DnsTtlLowAssertion{}
	case string(generated.DnsTxtContainsAssertionTypeDnsTxtContains):
		return &generated.DnsTxtContainsAssertion{}
	case string(generated.HeaderValueAssertionTypeHeaderValue):
		return &generated.HeaderValueAssertion{}
	case string(generated.HeartbeatIntervalDriftAssertionTypeHeartbeatIntervalDrift):
		return &generated.HeartbeatIntervalDriftAssertion{}
	case string(generated.HeartbeatMaxIntervalAssertionTypeHeartbeatMaxInterval):
		return &generated.HeartbeatMaxIntervalAssertion{}
	case string(generated.HeartbeatPayloadContainsAssertionTypeHeartbeatPayloadContains):
		return &generated.HeartbeatPayloadContainsAssertion{}
	case string(generated.HeartbeatReceivedAssertionTypeHeartbeatReceived):
		return &generated.HeartbeatReceivedAssertion{}
	case string(generated.IcmpPacketLossAssertionTypeIcmpPacketLoss):
		return &generated.IcmpPacketLossAssertion{}
	case string(generated.IcmpReachableAssertionTypeIcmpReachable):
		return &generated.IcmpReachableAssertion{}
	case string(generated.IcmpResponseTimeAssertionTypeIcmpResponseTime):
		return &generated.IcmpResponseTimeAssertion{}
	case string(generated.IcmpResponseTimeWarnAssertionTypeIcmpResponseTimeWarn):
		return &generated.IcmpResponseTimeWarnAssertion{}
	case string(generated.JsonPathAssertionTypeJsonPath):
		return &generated.JsonPathAssertion{}
	case string(generated.McpConnectsAssertionTypeMcpConnects):
		return &generated.McpConnectsAssertion{}
	case string(generated.McpHasCapabilityAssertionTypeMcpHasCapability):
		return &generated.McpHasCapabilityAssertion{}
	case string(generated.McpMinToolsAssertionTypeMcpMinTools):
		return &generated.McpMinToolsAssertion{}
	case string(generated.McpProtocolVersionAssertionTypeMcpProtocolVersion):
		return &generated.McpProtocolVersionAssertion{}
	case string(generated.McpResponseTimeAssertionTypeMcpResponseTime):
		return &generated.McpResponseTimeAssertion{}
	case string(generated.McpResponseTimeWarnAssertionTypeMcpResponseTimeWarn):
		return &generated.McpResponseTimeWarnAssertion{}
	case string(generated.McpToolAvailableAssertionTypeMcpToolAvailable):
		return &generated.McpToolAvailableAssertion{}
	case string(generated.McpToolCountChangedAssertionTypeMcpToolCountChanged):
		return &generated.McpToolCountChangedAssertion{}
	case string(generated.RedirectCountAssertionTypeRedirectCount):
		return &generated.RedirectCountAssertion{}
	case string(generated.RedirectTargetAssertionTypeRedirectTarget):
		return &generated.RedirectTargetAssertion{}
	case string(generated.RegexBodyAssertionTypeRegexBody):
		return &generated.RegexBodyAssertion{}
	case string(generated.ResponseSizeAssertionTypeResponseSize):
		return &generated.ResponseSizeAssertion{}
	case string(generated.ResponseTimeAssertionTypeResponseTime):
		return &generated.ResponseTimeAssertion{}
	case string(generated.ResponseTimeWarnAssertionTypeResponseTimeWarn):
		return &generated.ResponseTimeWarnAssertion{}
	case string(generated.SslExpiryAssertionTypeSslExpiry):
		return &generated.SslExpiryAssertion{}
	case string(generated.StatusCodeAssertionTypeStatusCode):
		return &generated.StatusCodeAssertion{}
	case string(generated.TcpConnectsAssertionTypeTcpConnects):
		return &generated.TcpConnectsAssertion{}
	case string(generated.TcpResponseTimeAssertionTypeTcpResponseTime):
		return &generated.TcpResponseTimeAssertion{}
	case string(generated.TcpResponseTimeWarnAssertionTypeTcpResponseTimeWarn):
		return &generated.TcpResponseTimeWarnAssertion{}
	default:
		return nil
	}
}

// assertionConfigSchemaName returns the OpenAPI schema name (the generated
// Go struct name) for a given assertion type, derived by reflecting over
// the prototype so it stays in lockstep with assertionConfigPrototype.
func assertionConfigSchemaName(assertionType string) string {
	proto := assertionConfigPrototype(assertionType)
	if proto == nil {
		return "Assertion"
	}
	t := reflect.TypeOf(proto)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Name()
}

// caseSensitiveUnknownKeys returns top-level JSON keys in `raw` that have
// no exact-case match against a json tag on `target`'s fields. It exists
// because `encoding/json` accepts case-insensitive matches even with
// DisallowUnknownFields, which silently masks typos like `verifyTLS` →
// `verifyTls`. Always-camelCase wire format is the API contract; flagging
// case mismatches here keeps Terraform plans honest.
func caseSensitiveUnknownKeys(raw string, target any) []string {
	var top map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &top); err != nil {
		return nil
	}

	v := reflect.ValueOf(target)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}

	allowed := make(map[string]struct{}, v.NumField())
	rt := v.Type()
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			continue
		}
		allowed[name] = struct{}{}
	}

	var bad []string
	for k := range top {
		if _, ok := allowed[k]; !ok {
			bad = append(bad, k)
		}
	}
	return bad
}

// configSchemaName returns the OpenAPI schema name that the user can grep
// for to look up the full set of fields allowed for a given monitor type.
func configSchemaName(t generated.CreateMonitorRequestType) string {
	switch t {
	case generated.CreateMonitorRequestTypeHTTP:
		return "Http"
	case generated.CreateMonitorRequestTypeDNS:
		return "Dns"
	case generated.CreateMonitorRequestTypeTCP:
		return "Tcp"
	case generated.CreateMonitorRequestTypeICMP:
		return "Icmp"
	case generated.CreateMonitorRequestTypeHEARTBEAT:
		return "Heartbeat"
	case generated.CreateMonitorRequestTypeMCPSERVER:
		return "McpServer"
	case generated.CreateMonitorRequestTypeBROWSER, generated.CreateMonitorRequestTypeMULTISTEPAPI:
		return "Script"
	default:
		return "Monitor"
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ── TF → API mapping ───────────────────────────────────────────────────

type assertionModel struct {
	Type     types.String `tfsdk:"type"`
	Config   types.String `tfsdk:"config"`
	Severity types.String `tfsdk:"severity"`
}

type triggerRuleModel struct {
	Type            types.String `tfsdk:"type"`
	Severity        types.String `tfsdk:"severity"`
	Scope           types.String `tfsdk:"scope"`
	Count           types.Int64  `tfsdk:"count"`
	WindowMinutes   types.Int64  `tfsdk:"window_minutes"`
	ThresholdMs     types.Int64  `tfsdk:"threshold_ms"`
	AggregationType types.String `tfsdk:"aggregation_type"`
}

type incidentPolicyModel struct {
	ConfirmationType     types.String `tfsdk:"confirmation_type"`
	MinRegionsFailing    types.Int64  `tfsdk:"min_regions_failing"`
	MaxWaitSeconds       types.Int64  `tfsdk:"max_wait_seconds"`
	ConsecutiveSuccesses types.Int64  `tfsdk:"consecutive_successes"`
	MinRegionsPassing    types.Int64  `tfsdk:"min_regions_passing"`
	CooldownMinutes      types.Int64  `tfsdk:"cooldown_minutes"`
	TriggerRules         types.List   `tfsdk:"trigger_rules"`
}

func buildAssertions(ctx context.Context, list types.List) ([]generated.CreateAssertionRequest, error) {
	if list.IsNull() || list.IsUnknown() || len(list.Elements()) == 0 {
		return nil, nil
	}

	var models []assertionModel
	diags := list.ElementsAs(ctx, &models, false)
	if diags.HasError() {
		return nil, fmt.Errorf("parsing assertions: %s", diags.Errors())
	}

	var result []generated.CreateAssertionRequest
	for _, m := range models {
		configJSON := json.RawMessage(m.Config.ValueString())

		typedConfig := map[string]json.RawMessage{
			"type": json.RawMessage(fmt.Sprintf("%q", m.Type.ValueString())),
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(configJSON, &raw); err != nil {
			return nil, fmt.Errorf("assertion config is not valid JSON: %w", err)
		}
		for k, v := range raw {
			typedConfig[k] = v
		}
		wrappedConfig, _ := json.Marshal(typedConfig)

		var configUnion generated.CreateAssertionRequest_Config
		if err := configUnion.UnmarshalJSON(wrappedConfig); err != nil {
			return nil, fmt.Errorf("assertion config unmarshal: %w", err)
		}

		req := generated.CreateAssertionRequest{
			Config:   configUnion,
			Severity: typedStringPtrOrNil[generated.CreateAssertionRequestSeverity](m.Severity),
		}
		result = append(result, req)
	}
	return result, nil
}

func buildIncidentPolicy(ctx context.Context, obj types.Object) (*generated.UpdateIncidentPolicyRequest, error) {
	// Null/unknown means "do not override the API's default policy". Empty
	// attributes inside a present object are treated the same as omitting
	// only that attribute (server-side defaults are kept).
	if obj.IsNull() || obj.IsUnknown() {
		return nil, nil
	}

	var m incidentPolicyModel
	diags := obj.As(ctx, &m, basetypes.ObjectAsOptions{
		UnhandledNullAsEmpty:    true,
		UnhandledUnknownAsEmpty: true,
	})
	if diags.HasError() {
		return nil, fmt.Errorf("parsing incident policy: %s", diags.Errors())
	}

	var triggerRules []generated.TriggerRule
	var ruleModels []triggerRuleModel
	if !m.TriggerRules.IsNull() && !m.TriggerRules.IsUnknown() {
		if ruleDiags := m.TriggerRules.ElementsAs(ctx, &ruleModels, false); ruleDiags.HasError() {
			return nil, fmt.Errorf("parsing trigger rules: %s", ruleDiags.Errors())
		}
	}
	for i, rm := range ruleModels {
		ruleType := generated.TriggerRuleType(rm.Type.ValueString())
		if !ruleType.Valid() {
			return nil, fmt.Errorf("trigger_rules[%d].type: invalid value %q", i, rm.Type.ValueString())
		}
		ruleSeverity := generated.TriggerRuleSeverity(rm.Severity.ValueString())
		if !ruleSeverity.Valid() {
			return nil, fmt.Errorf("trigger_rules[%d].severity: invalid value %q", i, rm.Severity.ValueString())
		}
		triggerRules = append(triggerRules, generated.TriggerRule{
			Type:            ruleType,
			Severity:        ruleSeverity,
			Scope:           typedStringPtrOrNil[generated.TriggerRuleScope](rm.Scope),
			Count:           int32PtrOrNil(rm.Count),
			WindowMinutes:   int32PtrOrNil(rm.WindowMinutes),
			ThresholdMs:     int32PtrOrNil(rm.ThresholdMs),
			AggregationType: typedStringPtrOrNil[generated.TriggerRuleAggregationType](rm.AggregationType),
		})
	}

	return &generated.UpdateIncidentPolicyRequest{
		TriggerRules: triggerRules,
		Confirmation: generated.ConfirmationPolicy{
			Type:              generated.ConfirmationPolicyType(m.ConfirmationType.ValueString()),
			MinRegionsFailing: int32OrZero(m.MinRegionsFailing),
			MaxWaitSeconds:    int32OrZero(m.MaxWaitSeconds),
		},
		Recovery: generated.RecoveryPolicy{
			ConsecutiveSuccesses: int32OrZero(m.ConsecutiveSuccesses),
			MinRegionsPassing:    int32OrZero(m.MinRegionsPassing),
			CooldownMinutes:      int32OrZero(m.CooldownMinutes),
		},
	}, nil
}

// buildCreateRequest maps the plan onto CreateMonitorRequest. rc carries the
// uploaded bundle for code monitors (nil for probes); enforce sets
// `ownerEnforced`.
func (r *MonitorResource) buildCreateRequest(ctx context.Context, plan *MonitorResourceModel, rc *resolvedCode, enforce bool) (*generated.CreateMonitorRequest, error) {
	assertions, err := buildAssertions(ctx, plan.Assertions)
	if err != nil {
		return nil, err
	}

	incidentPolicy, err := buildIncidentPolicy(ctx, plan.IncidentPolicy)
	if err != nil {
		return nil, err
	}

	envID, err := parseUUIDPtrChecked(plan.EnvironmentID, "environment_id")
	if err != nil {
		return nil, err
	}
	alertChannels, err := uuidSliceFromStringListChecked(plan.AlertChannelIds, "alert_channel_ids")
	if err != nil {
		return nil, err
	}

	monitorType := generated.CreateMonitorRequestType(plan.Type.ValueString())
	if !monitorType.Valid() {
		return nil, fmt.Errorf("type: invalid monitor type %q", plan.Type.ValueString())
	}

	authUnion, err := buildMonitorAuthConfig(ctx, plan.Auth)
	if err != nil {
		return nil, err
	}

	managedByTF := generated.CreateMonitorRequestManagedByTERRAFORM
	label := "Terraform"
	req := &generated.CreateMonitorRequest{
		Name:                 plan.Name.ValueString(),
		Type:                 monitorType,
		ManagedBy:            &managedByTF,
		OwnerEnforced:        &enforce,
		OwnerDeclaration:     &generated.OwnerDeclaration{Address: stringPtrOrNil(plan.OwnerAddress), Label: &label},
		FrequencySeconds:     int32PtrOrNil(plan.FrequencySeconds),
		Enabled:              boolPtrOrNil(plan.Enabled),
		Regions:              stringSliceToPtr(plan.Regions),
		EnvironmentId:        envID,
		AlertChannelIds:      alertChannels,
		IncidentPolicy:       incidentPolicy,
		Auth:                 authUnion,
		FastRetryMaxAttempts: int32PtrOrNil(plan.FastRetryMaxAttempts),
	}
	if len(assertions) > 0 || !isCodeType(plan.Type) {
		req.Assertions = &assertions
	}

	// oapi-codegen's union UnmarshalJSON accepts raw bytes without
	// validating JSON structure; pre-flight with json.Valid so we fail
	// at plan time instead of sending garbage to the API. Code monitors
	// may omit config (the server pins the newest runtime).
	if raw := knownString(plan.Config); raw != "" {
		if !json.Valid([]byte(raw)) {
			return nil, fmt.Errorf("monitor config: invalid JSON")
		}
		var configUnion generated.CreateMonitorRequest_Config
		if err := configUnion.UnmarshalJSON([]byte(raw)); err != nil {
			return nil, fmt.Errorf("monitor config: %w", err)
		}
		req.Config = &configUnion
	}

	if isCodeType(plan.Type) {
		if rc != nil {
			code := rc.spec()
			req.Code = &code
		}
		req.RunParallel = boolPtrOrNil(plan.RunParallel)
		req.SecretKeys = stringSliceToPtr(plan.SecretKeys)
		capture, err := buildCapturePolicy(ctx, plan.CapturePolicy)
		if err != nil {
			return nil, err
		}
		req.CapturePolicy = capture
	}

	tagUUIDs, err := uuidSliceFromStringListChecked(plan.TagIds, "tag_ids")
	if err != nil {
		return nil, err
	}
	if tagUUIDs != nil && len(*tagUUIDs) > 0 {
		req.Tags = &generated.AddMonitorTagsRequest{
			TagIds: tagUUIDs,
		}
	}

	return req, nil
}

// buildUpdateRequest maps the plan onto UpdateMonitorRequest (settings
// only; the caller adds `code` when it publishes through the PUT).
func (r *MonitorResource) buildUpdateRequest(ctx context.Context, plan *MonitorResourceModel) (*generated.UpdateMonitorRequest, error) {
	assertions, err := buildAssertions(ctx, plan.Assertions)
	if err != nil {
		return nil, err
	}

	incidentPolicy, err := buildIncidentPolicy(ctx, plan.IncidentPolicy)
	if err != nil {
		return nil, err
	}

	name := plan.Name.ValueString()
	codeType := isCodeType(plan.Type)

	envID, err := parseUUIDPtrChecked(plan.EnvironmentID, "environment_id")
	if err != nil {
		return nil, err
	}
	alertChannels, err := uuidSliceFromStringListChecked(plan.AlertChannelIds, "alert_channel_ids")
	if err != nil {
		return nil, err
	}

	authUnion, err := buildMonitorAuthConfig(ctx, plan.Auth)
	if err != nil {
		return nil, err
	}

	req := &generated.UpdateMonitorRequest{
		Name:                 &name,
		FrequencySeconds:     int32PtrOrNil(plan.FrequencySeconds),
		Regions:              stringSliceToPtr(plan.Regions),
		EnvironmentId:        envID,
		AlertChannelIds:      alertChannels,
		IncidentPolicy:       incidentPolicy,
		Auth:                 authUnion,
		FastRetryMaxAttempts: int32PtrOrNil(plan.FastRetryMaxAttempts),
	}
	if len(assertions) > 0 || !codeType {
		req.Assertions = &assertions
	}

	// The spec now exposes `UpdateMonitorRequest.config` as a proper
	// polymorphic union, generated by oapi-codegen as a tagged-union wrapper
	// around raw JSON. oapi-codegen's UnmarshalJSON on the wrapper accepts
	// raw bytes without validating them, so we do a pre-flight json.Valid
	// check to keep the "invalid JSON errors out" guardrail.
	if raw := knownString(plan.Config); raw != "" {
		if !json.Valid([]byte(raw)) {
			return nil, fmt.Errorf("monitor config: invalid JSON")
		}
		var configUnion generated.UpdateMonitorRequest_Config
		if err := configUnion.UnmarshalJSON([]byte(raw)); err != nil {
			return nil, fmt.Errorf("monitor config: %w", err)
		}
		req.Config = &configUnion
	}

	if codeType {
		// Ownership of a code monitor moves through transfer, never PUT
		// (MONITOR_MANAGED_BY_TAKEOVER_REQUIRED).
		req.RunParallel = boolPtrOrNil(plan.RunParallel)
		req.SecretKeys = stringSliceToPtr(plan.SecretKeys)
		capture, err := buildCapturePolicy(ctx, plan.CapturePolicy)
		if err != nil {
			return nil, err
		}
		req.CapturePolicy = capture
	} else {
		managedBy := generated.UpdateMonitorRequestManagedByTERRAFORM
		req.ManagedBy = &managedBy
		if plan.EnvironmentID.IsNull() {
			clearEnv := true
			req.ClearEnvironmentId = &clearEnv
		}
		// Auth: when the user removes the entire `auth` block we set
		// `clearAuth: true` so the server drops any previously-attached
		// credential. When auth is supplied, ClearAuth stays nil — the typed
		// `Auth` union above carries the full payload. We never send both.
		if authUnion == nil {
			clearAuth := true
			req.ClearAuth = &clearAuth
		}
	}

	// NOTE: Tag add/remove reconciliation is handled outside the PUT body
	// in (*MonitorResource).reconcileTags. PUT /monitors/{id} only supports
	// adding tags via the embedded Tags request; removing tags (including
	// clearing the list entirely) requires a separate DELETE call.

	return req, nil
}

// knownString returns the value of a known, non-null string, else "".
func knownString(v types.String) string {
	if v.IsNull() || v.IsUnknown() {
		return ""
	}
	return v.ValueString()
}

func buildCapturePolicy(ctx context.Context, obj types.Object) (*generated.CapturePolicy, error) {
	if obj.IsNull() || obj.IsUnknown() {
		return nil, nil
	}
	var m capturePolicyModel
	if d := obj.As(ctx, &m, objectAsOptions); d.HasError() {
		return nil, fmt.Errorf("parsing capture_policy: %v", d.Errors())
	}
	return &generated.CapturePolicy{
		Screenshots: typedStringPtrOrNil[generated.CapturePolicyScreenshots](m.Screenshots),
		Trace:       typedStringPtrOrNil[generated.CapturePolicyTrace](m.Trace),
		Video:       typedStringPtrOrNil[generated.CapturePolicyVideo](m.Video),
		Network:     typedStringPtrOrNil[generated.CapturePolicyNetwork](m.Network),
	}, nil
}

// ── Object type helpers for nested blocks ───────────────────────────────

func assertionObjectType() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"type":     types.StringType,
			"config":   types.StringType,
			"severity": types.StringType,
		},
	}
}

func triggerRuleObjectType() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"type":             types.StringType,
			"severity":         types.StringType,
			"scope":            types.StringType,
			"count":            types.Int64Type,
			"window_minutes":   types.Int64Type,
			"threshold_ms":     types.Int64Type,
			"aggregation_type": types.StringType,
		},
	}
}

func incidentPolicyObjectType() types.ObjectType {
	return types.ObjectType{
		AttrTypes: map[string]attr.Type{
			"confirmation_type":     types.StringType,
			"min_regions_failing":   types.Int64Type,
			"max_wait_seconds":      types.Int64Type,
			"consecutive_successes": types.Int64Type,
			"min_regions_passing":   types.Int64Type,
			"cooldown_minutes":      types.Int64Type,
			"trigger_rules":         types.ListType{ElemType: triggerRuleObjectType()},
		},
	}
}

// ── API → TF mapping ───────────────────────────────────────────────────

// mapToState mirrors a freshly-fetched MonitorDto onto the Terraform model.
//
// The `auth` round-trip uses the typed `generated.MonitorAuthConfig` union
// directly: the API echoes back only the public, non-secret discriminator
// + variant fields (e.g. `{type, vaultSecretId}`), which the codegen union
// faithfully decodes into the matching variant. We never need to bypass
// the typed shape because the API never returns secret material.
//
// Returns any diagnostics produced while marshaling collection-valued
// attributes (e.g. types.ListValueFrom). Callers should Append the
// diagnostics to their response so framework-level errors are surfaced
// instead of being silently swallowed (END-1141).
func (r *MonitorResource) mapToState(ctx context.Context, model *MonitorResourceModel, dto *generated.MonitorDto) diag.Diagnostics {
	var diags diag.Diagnostics
	model.ID = types.StringValue(dto.Id.String())
	model.Name = types.StringValue(dto.Name)
	model.Type = types.StringValue(string(dto.Type))
	model.FrequencySeconds = types.Int64Value(int64(dto.FrequencySeconds))
	model.Enabled = types.BoolValue(dto.Enabled)
	model.PingUrl = stringValue(dto.PingUrl)
	diags.Append(mapCodeMonitorFields(ctx, model, dto)...)

	if configBytes, err := dto.Config.MarshalJSON(); err == nil && unionHasData(configBytes) && codeMonitorTypes[dto.Type] {
		model.Config = types.StringValue(preserveConfig(model.Config, configBytes))
	} else if err == nil && unionHasData(configBytes) {
		// The API strips the `type` discriminator from the config object
		// in its response (it's already represented at the top level via
		// dto.Type). Users frequently include it in their HCL config,
		// though — both because it's a natural way to reason about the
		// shape of the union and because some config fields require the
		// discriminator to disambiguate (e.g. ICMP packetCount). Echo
		// the user's `type` choice back into state if they originally
		// supplied one, so the round-trip is identity-preserving.
		normalized := normalizeJSON(configBytes)
		if priorHasConfigType(model.Config) {
			normalized = injectConfigType(normalized, string(dto.Type))
		}
		model.Config = types.StringValue(normalized)
	} else if model.Config.IsUnknown() {
		model.Config = types.StringNull()
	}

	if dto.Environment != nil && dto.Environment.Id.String() != "00000000-0000-0000-0000-000000000000" {
		model.EnvironmentID = types.StringValue(dto.Environment.Id.String())
	} else {
		model.EnvironmentID = types.StringNull()
	}

	// Auth: decode the typed `MonitorAuthConfig` union the API returned.
	// The API only echoes public, non-secret material (the discriminator
	// + `vaultSecretId` + per-variant non-secret fields like `headerName`),
	// which round-trips losslessly through the typed shape. There is no
	// "redacted credential" concern here — credentials live in vault and
	// are referenced by `vault_secret_id`.
	authObj, authDiags := mapMonitorAuthToTF(ctx, dto.Auth)
	diags.Append(authDiags...)
	model.Auth = authObj

	// Regions
	if len(dto.Regions) > 0 {
		regionElems := make([]types.String, len(dto.Regions))
		for i, r := range dto.Regions {
			regionElems[i] = types.StringValue(r)
		}
		var d diag.Diagnostics
		model.Regions, d = types.ListValueFrom(ctx, types.StringType, regionElems)
		diags.Append(d...)
	} else if !model.Regions.IsNull() {
		var d diag.Diagnostics
		model.Regions, d = types.ListValueFrom(ctx, types.StringType, []types.String{})
		diags.Append(d...)
	}

	// Tag IDs
	// The API treats tags as an unordered set and may return them in a
	// different order than the user supplied (e.g. by primary key). If the
	// returned *set* matches the plan's set we preserve the plan's order
	// to avoid spurious diffs and "Provider produced inconsistent result
	// after apply" errors. Otherwise (genuine drift), we adopt the API's
	// order as the new source of truth.
	if dto.Tags != nil && len(*dto.Tags) > 0 {
		apiIDs := make([]string, len(*dto.Tags))
		for i, t := range *dto.Tags {
			apiIDs[i] = t.Id.String()
		}
		var d diag.Diagnostics
		model.TagIds, d = preserveListOrder(ctx, model.TagIds, apiIDs)
		diags.Append(d...)
	} else if !model.TagIds.IsNull() {
		var d diag.Diagnostics
		model.TagIds, d = types.ListValueFrom(ctx, types.StringType, []types.String{})
		diags.Append(d...)
	}

	// Alert Channel IDs — same set semantics as Tag IDs above.
	if dto.AlertChannelIds != nil && len(*dto.AlertChannelIds) > 0 {
		apiIDs := make([]string, len(*dto.AlertChannelIds))
		for i, id := range *dto.AlertChannelIds {
			apiIDs[i] = id.String()
		}
		var d diag.Diagnostics
		model.AlertChannelIds, d = preserveListOrder(ctx, model.AlertChannelIds, apiIDs)
		diags.Append(d...)
	} else if !model.AlertChannelIds.IsNull() {
		var d diag.Diagnostics
		model.AlertChannelIds, d = types.ListValueFrom(ctx, types.StringType, []types.String{})
		diags.Append(d...)
	}

	// Assertions
	if dto.Assertions != nil && len(*dto.Assertions) > 0 {
		// Build a content-keyed lookup of the user's prior assertions so we
		// can preserve their severity casing (e.g. "Fail" vs "fail") and
		// their decision to leave severity null even when the API order
		// differs from the HCL order. Matching by index is fragile because
		// the API does not promise a stable ordering across responses, and
		// users can reorder blocks freely in their HCL — both cases would
		// otherwise leak server-side casing into state and produce noisy
		// (or worse, incorrect) plans on the next run.
		var priorAssertions []assertionModel
		if !model.Assertions.IsNull() {
			diags.Append(model.Assertions.ElementsAs(ctx, &priorAssertions, false)...)
		}

		// Key = "<type>|<normalized-config-json>". The config is normalized
		// the same way as the API → state mapping below so the keys produced
		// from prior state and from the freshly-read DTO are comparable.
		// Multiple identical assertions (same type+config) are stored in a
		// FIFO slice so the first DTO match consumes the first prior entry.
		priorByKey := map[string][]int{}
		assertionKey := func(typ, cfg string) string { return typ + "|" + cfg }
		for idx, p := range priorAssertions {
			k := assertionKey(p.Type.ValueString(), p.Config.ValueString())
			priorByKey[k] = append(priorByKey[k], idx)
		}

		var assertionModels []assertionModel
		for _, a := range *dto.Assertions {
			am := assertionModel{
				Type: types.StringValue(string(a.AssertionType)),
			}

			// Compute normalized config first so we can use it as a lookup key.
			cfgStr := ""
			if cfgBytes, err := a.Config.MarshalJSON(); err == nil && unionHasData(cfgBytes) {
				var raw map[string]json.RawMessage
				if err := json.Unmarshal(cfgBytes, &raw); err == nil {
					delete(raw, "type")
					if stripped, err := json.Marshal(raw); err == nil {
						// Strip null-valued keys so the API echoing optional
						// fields back as `null` does not show as a diff
						// against a user-supplied config that omits them.
						cfgStr = normalizeJSON(stripped)
						am.Config = types.StringValue(cfgStr)
					}
				}
			}

			// Find a content-matched prior entry (FIFO) and consume it so
			// duplicate assertions are paired one-for-one.
			var matched *assertionModel
			k := assertionKey(string(a.AssertionType), cfgStr)
			if idxs := priorByKey[k]; len(idxs) > 0 {
				matched = &priorAssertions[idxs[0]]
				priorByKey[k] = idxs[1:]
			}

			sev := string(a.Severity)
			switch {
			case matched != nil && !matched.Severity.IsNull():
				// Preserve user-supplied casing when it matches the API value.
				priorSev := matched.Severity.ValueString()
				if strings.EqualFold(priorSev, sev) {
					sev = priorSev
				}
				am.Severity = types.StringValue(sev)
			case matched != nil && matched.Severity.IsNull():
				// User omitted severity in HCL → keep state null to match the
				// plan and avoid spurious null→value diffs.
			default:
				// New / imported assertion with no prior content match —
				// populate severity so import flows produce a complete state.
				am.Severity = types.StringValue(sev)
			}

			assertionModels = append(assertionModels, am)
		}
		var d diag.Diagnostics
		model.Assertions, d = types.ListValueFrom(ctx, types.ObjectType{
			AttrTypes: assertionObjectType().AttrTypes,
		}, assertionModels)
		diags.Append(d...)
	}

	// Incident Policy — schema is a SingleNestedAttribute (Optional+Computed
	// with UseStateForUnknown), so we always populate it from the DTO when
	// the API has one. The plan modifier ensures Terraform sees the prior
	// state when the user omits the attribute, eliminating the 0↔1 block
	// count drift that the previous ListNestedBlock design produced.
	if dto.IncidentPolicy != nil && dto.IncidentPolicy.Id.String() != "00000000-0000-0000-0000-000000000000" {
		policyModel := incidentPolicyModel{
			ConfirmationType:     types.StringValue(string(dto.IncidentPolicy.Confirmation.Type)),
			MinRegionsFailing:    types.Int64Value(int64(dto.IncidentPolicy.Confirmation.MinRegionsFailing)),
			MaxWaitSeconds:       types.Int64Value(int64(dto.IncidentPolicy.Confirmation.MaxWaitSeconds)),
			ConsecutiveSuccesses: types.Int64Value(int64(dto.IncidentPolicy.Recovery.ConsecutiveSuccesses)),
			MinRegionsPassing:    types.Int64Value(int64(dto.IncidentPolicy.Recovery.MinRegionsPassing)),
			CooldownMinutes:      types.Int64Value(int64(dto.IncidentPolicy.Recovery.CooldownMinutes)),
		}
		if len(dto.IncidentPolicy.TriggerRules) > 0 {
			var ruleModels []triggerRuleModel
			for _, tr := range dto.IncidentPolicy.TriggerRules {
				ruleModels = append(ruleModels, triggerRuleModel{
					Type:            types.StringValue(string(tr.Type)),
					Severity:        types.StringValue(string(tr.Severity)),
					Scope:           typedStringPtrValue(tr.Scope),
					Count:           int32Value(tr.Count),
					WindowMinutes:   int32Value(tr.WindowMinutes),
					ThresholdMs:     int32Value(tr.ThresholdMs),
					AggregationType: typedStringPtrValue(tr.AggregationType),
				})
			}
			var d diag.Diagnostics
			policyModel.TriggerRules, d = types.ListValueFrom(ctx, triggerRuleObjectType(), ruleModels)
			diags.Append(d...)
		} else {
			policyModel.TriggerRules = types.ListNull(triggerRuleObjectType())
		}
		obj, d := types.ObjectValueFrom(ctx, incidentPolicyObjectType().AttrTypes, policyModel)
		diags.Append(d...)
		model.IncidentPolicy = obj
	} else {
		model.IncidentPolicy = types.ObjectNull(incidentPolicyObjectType().AttrTypes)
	}

	return diags
}

// preserveConfig keeps the configured JSON when every key it sets has the
// same value in the server's answer (the server adds defaults such as the
// pinned runtimeId); otherwise the server's normalized JSON wins so drift
// shows.
func preserveConfig(prior types.String, server []byte) string {
	normalized := normalizeJSON(server)
	raw := knownString(prior)
	if raw == "" {
		return normalized
	}
	var want, got map[string]json.RawMessage
	if json.Unmarshal([]byte(normalizeJSON([]byte(raw))), &want) != nil || json.Unmarshal([]byte(normalized), &got) != nil {
		return normalized
	}
	for k, v := range want {
		if g, ok := got[k]; !ok || normalizeJSON(g) != normalizeJSON(v) {
			return normalized
		}
	}
	return raw
}

// mapCodeMonitorFields copies the ownership and code-monitor fields.
func mapCodeMonitorFields(ctx context.Context, model *MonitorResourceModel, dto *generated.MonitorDto) diag.Diagnostics {
	var diags diag.Diagnostics
	model.OwnerEnforced = types.BoolValue(dto.OwnerEnforced)
	model.LiveRevisionID = uuidPtrValue(dto.LiveRevisionId)
	model.LiveRevisionDigest = stringValue(dto.LiveRevisionDigest)
	if dto.FastRetryMaxAttempts != nil {
		model.FastRetryMaxAttempts = types.Int64Value(int64(*dto.FastRetryMaxAttempts))
	} else {
		model.FastRetryMaxAttempts = types.Int64Null()
	}
	model.RunParallel = boolValue(dto.RunParallel)
	if dto.SecretKeys != nil {
		model.SecretKeys = ptrStringSliceToList(ctx, dto.SecretKeys)
	} else {
		model.SecretKeys = types.ListNull(types.StringType)
	}

	ownerType := dto.ManagedBy
	var project, address, label *string
	if dto.Owner != nil {
		ownerType = dto.Owner.Type
		project, address, label = dto.Owner.Project, dto.Owner.Address, dto.Owner.Label
	}
	owner, d := types.ObjectValue(ownerObjectType().AttrTypes, map[string]attr.Value{
		"type":    types.StringValue(ownerType),
		"project": stringValue(project),
		"address": stringValue(address),
		"label":   stringValue(label),
	})
	diags.Append(d...)
	model.Owner = owner

	if dto.CapturePolicy != nil {
		cp := dto.CapturePolicy
		obj, d := types.ObjectValue(capturePolicyObjectType().AttrTypes, map[string]attr.Value{
			"screenshots": typedStringPtrValue(cp.Screenshots),
			"trace":       typedStringPtrValue(cp.Trace),
			"video":       typedStringPtrValue(cp.Video),
			"network":     typedStringPtrValue(cp.Network),
		})
		diags.Append(d...)
		model.CapturePolicy = obj
	} else {
		model.CapturePolicy = types.ObjectNull(capturePolicyObjectType().AttrTypes)
	}
	return diags
}

// ── CRUD ────────────────────────────────────────────────────────────────

func (r *MonitorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan MonitorResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, d := plan.Timeouts.Create(ctx, defaultCodeApplyTimeout)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	// Code monitors: upload the include set, and validate it before the
	// monitor exists (doc 42 R3) unless test_before_publish = false.
	rc, ok := r.prepareCode(ctx, &plan, &resp.Diagnostics)
	if !ok {
		return
	}
	if rc != nil && testBeforePublish(plan.TestBeforePublish) && !r.validateNewCode(ctx, &plan, rc, &resp.Diagnostics) {
		return
	}

	monitor, ok := r.createMonitor(ctx, &plan, rc, &resp.Diagnostics)
	if !ok {
		return
	}
	resp.Diagnostics.Append(applyCodeToPlan(ctx, &plan, rc)...)
	r.saveCreated(ctx, &plan, monitor, resp)
}

// createMonitor POSTs the monitor and reads it back. The create response
// omits fields resolved after the write (a code monitor's live revision
// digest), so the GET is what state is built from; if the read-back fails,
// the digest the monitor was just created with stands in.
func (r *MonitorResource) createMonitor(ctx context.Context, plan *MonitorResourceModel, rc *resolvedCode, diags *diag.Diagnostics) (*generated.MonitorDto, bool) {
	body, err := r.buildCreateRequest(ctx, plan, rc, ownerEnforcedOnCreate(plan.OwnerEnforced))
	if err != nil {
		diags.AddError("Error building monitor request", err.Error())
		return nil, false
	}
	monitor, err := api.Create[generated.MonitorDto](ctx, r.client, api.PathMonitors, body)
	if err != nil {
		api.AddAPIError(diags, "create monitor", err, path.Root("name"))
		return nil, false
	}
	if rc == nil {
		return monitor, true
	}
	fresh, err := api.Get[generated.MonitorDto](ctx, r.client, api.MonitorPath(monitor.Id.String()))
	if err != nil {
		tflog.Warn(ctx, "re-reading the new monitor failed; using the create response", map[string]any{"error": err.Error()})
		if monitor.LiveRevisionDigest == nil {
			digest := rc.bundle.Digest
			monitor.LiveRevisionDigest = &digest
		}
		return monitor, true
	}
	return fresh, true
}

// saveCreated maps the new monitor into state. From here the monitor exists
// server-side; if mapping or State.Set fails the resource never enters state
// and Terraform has no hook to delete it, so the provider deletes the orphan
// itself. (The framework's post-Create consistency check runs after this and
// isn't covered; mapToState keeps state aligned with the plan for that.)
func (r *MonitorResource) saveCreated(ctx context.Context, plan *MonitorResourceModel, monitor *generated.MonitorDto, resp *resource.CreateResponse) {
	cleanupOrphan := func(reason string) {
		id := monitor.Id.String()
		// The apply context may already be past its deadline.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err := api.Delete(cleanupCtx, r.client, api.MonitorPath(id)); err != nil {
			tflog.Warn(ctx, "orphan monitor cleanup failed; resource may be leaked",
				map[string]any{"id": id, "reason": reason, "delete_error": err.Error()})
			return
		}
		tflog.Debug(ctx, "deleted orphan monitor after create-time error",
			map[string]any{"id": id, "reason": reason})
	}

	planned := plan.Enabled
	resp.Diagnostics.Append(r.mapToState(ctx, plan, monitor)...)
	if resp.Diagnostics.HasError() {
		cleanupOrphan("mapToState returned errors")
		return
	}
	keepPlannedEnabled(plan, planned)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	if resp.Diagnostics.HasError() {
		cleanupOrphan("State.Set returned errors")
	}
}

func (r *MonitorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state MonitorResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	monitor, err := api.Get[generated.MonitorDto](ctx, r.client, api.MonitorPath(state.ID.ValueString()))
	if err != nil {
		if api.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		api.AddAPIError(&resp.Diagnostics, "read monitor", err, path.Root("id"))
		return
	}

	resp.Diagnostics.Append(r.mapToState(ctx, &state, monitor)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !codeMonitorTypes[monitor.Type] {
		state.CodeHash = types.StringNull()
		state.CodeFiles = types.MapNull(types.StringType)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *MonitorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state MonitorResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	updateTimeout, d := plan.Timeouts.Update(ctx, defaultCodeApplyTimeout)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()
	id := state.ID.ValueString()

	body, ok := r.updateBody(ctx, req, &plan, &resp.Diagnostics)
	if !ok {
		return
	}
	rc, ok := r.prepareCode(ctx, &plan, &resp.Diagnostics)
	if !ok {
		return
	}

	// Code change: the configured closure differs from the live revision.
	// With test_before_publish the draft is tested first; a failed test
	// stops here (old code live, draft kept, nothing else changed, state
	// untouched). Without it, the PUT carries the code (live, untested).
	codeChanged := rc != nil && rc.bundle.Digest != state.LiveRevisionDigest.ValueString()
	var testedDraft *generated.RevisionDto
	if codeChanged && testBeforePublish(plan.TestBeforePublish) {
		if testedDraft, ok = r.testDraft(ctx, id, rc, &resp.Diagnostics); !ok {
			return
		}
	} else if codeChanged {
		code := rc.spec()
		body.Code = &code
		body.AcknowledgeRollback = stringPtrOrNil(plan.AcknowledgeRollback)
	}

	// Ownership moves before any owner-guarded write: an imported monitor
	// is transferred to Terraform, and a changed owner_address is a new
	// declaration (a transfer to the same owner).
	if !r.ensureTerraformOwner(ctx, id, &plan, &state, &resp.Diagnostics) {
		return
	}

	if testedDraft != nil {
		if err := r.publishDraft(ctx, id, testedDraft.Id, plan.AcknowledgeRollback, state.LiveRevisionID); err != nil {
			addPublishError(&resp.Diagnostics, err)
			return
		}
	}

	// `state.ID` is the only authoritative id (plan.ID is unknown during
	// Update). The PUT response omits tags, so it's discarded and the
	// monitor is re-read after the tag reconcile.
	if _, err := api.Update[generated.MonitorDto](ctx, r.client, api.MonitorPath(id), body); err != nil {
		if body.Code != nil {
			addPublishError(&resp.Diagnostics, err)
		} else {
			api.AddAPIError(&resp.Diagnostics, "update monitor", err, path.Root("name"))
		}
		if testedDraft != nil {
			// The new code is live; keep state honest about it so the next
			// plan doesn't test and publish it again.
			r.saveLiveCode(ctx, &state, rc, resp)
		}
		return
	}

	resp.Diagnostics.Append(applyCodeToPlan(ctx, &plan, rc)...)
	r.refreshAfterUpdate(ctx, id, &plan, &state, resp)
}

// keepPlannedEnabled keeps a known planned `enabled` in state after an
// apply, so a pause or resume made elsewhere between plan and apply (or a
// server that keeps a monitor paused) never fails the apply with an
// inconsistent result; the next refresh shows the server value. An unknown
// plan (omitted on create) keeps the server value.
func keepPlannedEnabled(model *MonitorResourceModel, planned types.Bool) {
	if !planned.IsUnknown() && !planned.IsNull() {
		model.Enabled = planned
	}
}

// testBeforePublish reads test_before_publish, which defaults to true. The
// default lives here, not in the schema, so state written before the
// attribute existed (v0.2.0-beta.10) plans no diff.
func testBeforePublish(v types.Bool) bool {
	return v.IsNull() || v.IsUnknown() || v.ValueBool()
}

// updateBody builds the settings PUT. owner_enforced and enabled are sent
// only when the configuration sets them: an upgrade never flips enforcement
// on monitors created before the attribute existed, and an apply never
// resumes a monitor paused outside Terraform when `enabled` is omitted.
func (r *MonitorResource) updateBody(ctx context.Context, req resource.UpdateRequest, plan *MonitorResourceModel, diags *diag.Diagnostics) (*generated.UpdateMonitorRequest, bool) {
	body, err := r.buildUpdateRequest(ctx, plan)
	if err != nil {
		diags.AddError("Error building monitor request", err.Error())
		return nil, false
	}
	var configured types.Bool
	diags.Append(req.Config.GetAttribute(ctx, path.Root("owner_enforced"), &configured)...)
	if !configured.IsNull() && !configured.IsUnknown() {
		v := configured.ValueBool()
		body.OwnerEnforced = &v
	}
	var enabled types.Bool
	diags.Append(req.Config.GetAttribute(ctx, path.Root("enabled"), &enabled)...)
	body.Enabled = boolPtrOrNil(enabled)
	return body, !diags.HasError()
}

// ensureTerraformOwner transfers the monitor to Terraform when another tool
// owns it (the first apply after an import) or when owner_address changed.
// One transfer covers both.
func (r *MonitorResource) ensureTerraformOwner(ctx context.Context, id string, plan, state *MonitorResourceModel, diags *diag.Diagnostics) bool {
	before := ownerType(state.Owner)
	adopt := before != "" && before != api.ToolTerraform
	addressChanged := !plan.OwnerAddress.IsUnknown() && !plan.OwnerAddress.Equal(state.OwnerAddress)
	if !adopt && !addressChanged {
		return true
	}
	reason := "terraform: owner address changed"
	if adopt {
		reason = "terraform import"
	}
	if err := r.transferToTerraform(ctx, id, reason, plan.OwnerAddress); err != nil {
		api.AddAPIError(diags, "transfer the monitor to Terraform", err, path.Root("owner"))
		return false
	}
	tflog.Info(ctx, "transferred monitor to Terraform", map[string]any{
		"id": id, "owner_before": before, "owner_after": api.ToolTerraform, "reason": reason,
	})
	return true
}

// saveLiveCode records a publish that landed when the settings PUT after it
// failed: prior state, refreshed from the server, with the new code fields.
func (r *MonitorResource) saveLiveCode(ctx context.Context, state *MonitorResourceModel, rc *resolvedCode, resp *resource.UpdateResponse) {
	monitor, err := api.Get[generated.MonitorDto](ctx, r.client, api.MonitorPath(state.ID.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("The new code is live, but the monitor could not be re-read",
			fmt.Sprintf("%s. Run `terraform refresh` once the API is healthy.", err))
		return
	}
	// resp already carries the PUT error; check only this step's diagnostics.
	var diags diag.Diagnostics
	diags.Append(r.mapToState(ctx, state, monitor)...)
	diags.Append(applyCodeToPlan(ctx, state, rc)...)
	if !diags.HasError() {
		diags.Append(resp.State.Set(ctx, state)...)
	}
	resp.Diagnostics.Append(diags...)
}

// refreshAfterUpdate reconciles tags and re-reads the monitor into state.
//
// Tags are reconciled out-of-band (the PUT can only add them). The PUT has
// already mutated the monitor, so state is refreshed from the server even if
// the tag reconcile fails: the half-applied result is persisted and the next
// apply retries only the missing tag delta. `state.TagIds` is the "before"
// set because the PUT response omits tags.
func (r *MonitorResource) refreshAfterUpdate(ctx context.Context, id string, plan, state *MonitorResourceModel, resp *resource.UpdateResponse) {
	tagErr := r.reconcileTags(ctx, id, plan.TagIds, state.TagIds)

	monitor, getErr := api.Get[generated.MonitorDto](ctx, r.client, api.MonitorPath(id))
	if getErr != nil {
		if tagErr != nil {
			resp.Diagnostics.AddError(
				"Error reconciling monitor tags AND re-reading monitor",
				fmt.Sprintf("Tag reconcile failed (%s); the follow-up re-read also "+
					"failed (%s). The monitor body update was accepted server-side "+
					"but Terraform could not refresh state. Run `terraform refresh` "+
					"once the API is healthy and re-apply.", tagErr, getErr),
			)
			return
		}
		resp.Diagnostics.AddError("Error re-reading monitor after tag sync", getErr.Error())
		return
	}

	planned := plan.Enabled
	resp.Diagnostics.Append(r.mapToState(ctx, plan, monitor)...)
	if resp.Diagnostics.HasError() {
		return
	}
	keepPlannedEnabled(plan, planned)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)

	if tagErr != nil {
		resp.Diagnostics.AddError(
			"Error reconciling monitor tags",
			fmt.Sprintf("The monitor's body was updated successfully, but the "+
				"follow-up tag reconciliation failed: %s. Terraform state has "+
				"been refreshed from the server to reflect the current tag set; "+
				"re-run `terraform apply` to retry the missing tag delta.", tagErr),
		)
	}
}

// reconcileTags brings the monitor's tag set in line with the plan:
//   - plan has tag IDs not in the prior state → POST to add
//   - prior state has tag IDs absent from the plan → DELETE to remove
//
// `currentTags` is the prior Terraform state value for `tag_ids`, used as the
// authoritative "before" view of the monitor's tag set. We deliberately do
// NOT use the API's PUT /monitors/{id} response: that endpoint omits the tag
// list from its DTO entirely, so a freshly-PUT monitor surfaces an empty
// `Tags` field even when the underlying record still has tags attached. If we
// computed the delta against that empty set we'd never DELETE the tags the
// user is trying to clear.
//
// When `planTags` is null/unknown we do not touch tags (preserves existing).
// When `planTags` is an empty list we remove everything currently attached.
func (r *MonitorResource) reconcileTags(ctx context.Context, monitorID string, planTags types.List, currentTags types.List) error {
	if planTags.IsNull() || planTags.IsUnknown() {
		return nil
	}

	desired := make(map[string]bool)
	for _, el := range planTags.Elements() {
		if s, ok := el.(types.String); ok && !s.IsNull() && !s.IsUnknown() {
			desired[s.ValueString()] = true
		}
	}

	existing := make(map[string]bool)
	if !currentTags.IsNull() && !currentTags.IsUnknown() {
		for _, el := range currentTags.Elements() {
			if s, ok := el.(types.String); ok && !s.IsNull() && !s.IsUnknown() {
				existing[s.ValueString()] = true
			}
		}
	}

	var toAdd []openapi_types.UUID
	for id := range desired {
		if !existing[id] {
			u, err := uuid.Parse(id)
			if err != nil {
				return fmt.Errorf("invalid tag id %q: %w", id, err)
			}
			toAdd = append(toAdd, openapi_types.UUID(u))
		}
	}

	var toRemove []openapi_types.UUID
	for id := range existing {
		if !desired[id] {
			u, err := uuid.Parse(id)
			if err != nil {
				return fmt.Errorf("invalid existing tag id %q: %w", id, err)
			}
			toRemove = append(toRemove, u)
		}
	}

	if len(toAdd) > 0 {
		addBody := generated.AddMonitorTagsRequest{TagIds: &toAdd}
		if _, err := api.CreateList[generated.TagDto](ctx, r.client, api.MonitorTagsPath(monitorID), addBody); err != nil {
			return fmt.Errorf("adding tags: %w", err)
		}
	}

	if len(toRemove) > 0 {
		removeBody := generated.RemoveMonitorTagsRequest{TagIds: toRemove}
		if err := api.DeleteWithBody(ctx, r.client, api.MonitorTagsPath(monitorID), removeBody); err != nil {
			return fmt.Errorf("removing tags: %w", err)
		}
	}

	return nil
}

// defaultCodeApplyTimeout bounds a create or update, including the wait for
// a validation run (doc 42 §5.4's 5-minute wait plus the calls around it).
const defaultCodeApplyTimeout = 10 * time.Minute

// waitBudget is how long a validation-run wait may take: what's left of the
// apply timeout, minus a margin for the calls that follow it.
func waitBudget(ctx context.Context) time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return api.DefaultRunWait
	}
	left := time.Until(deadline) - 30*time.Second
	if left < 10*time.Second {
		left = 10 * time.Second
	}
	return left
}

// ownerEnforcedOnCreate decides `ownerEnforced` for a new monitor: the
// configured value when set, otherwise true (doc 42 §7). A key that writes
// as another tool gets a 409 MONITOR_OWNED_BY_OTHER on later writes, which
// surfaces as an `[ownership]` diagnostic naming the fix.
func ownerEnforcedOnCreate(configured types.Bool) bool {
	if !configured.IsNull() && !configured.IsUnknown() {
		return configured.ValueBool()
	}
	return true
}

// ownerType reads owner.type from state ("" when unset).
func ownerType(owner types.Object) string {
	if owner.IsNull() || owner.IsUnknown() {
		return ""
	}
	if v, ok := owner.Attributes()["type"].(types.String); ok {
		return v.ValueString()
	}
	return ""
}

// transferToTerraform moves ownership to TERRAFORM (`POST /transfer`).
func (r *MonitorResource) transferToTerraform(ctx context.Context, id, reason string, address types.String) error {
	label := "Terraform"
	body := generated.TransferMonitorRequest{
		To:               generated.TransferMonitorRequestToTERRAFORM,
		Reason:           reason,
		OwnerDeclaration: &generated.OwnerDeclaration{Address: stringPtrOrNil(address), Label: &label},
	}
	_, err := api.Create[generated.MonitorDto](ctx, r.client, api.MonitorTransferPath(id), body)
	return err
}

func (r *MonitorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state MonitorResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := api.Delete(ctx, r.client, api.MonitorPath(state.ID.ValueString()))
	if err != nil && !api.IsNotFound(err) {
		api.AddAPIError(&resp.Diagnostics, "delete monitor", err, path.Root("id"))
	}
}

func (r *MonitorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	monitors, err := api.List[generated.MonitorDto](ctx, r.client, api.PathMonitors)
	if err != nil {
		resp.Diagnostics.AddError("Error listing monitors for import", err.Error())
		return
	}

	// Accept both name and UUID as the import ID. If the import ID matches a
	// UUID we take it directly — UUIDs are unique. If it matches by name, we
	// require the match to be unique: monitor names are not unique across an
	// org, and silently picking the first match would produce a stale or
	// arbitrary import.
	var monitorID string
	var matchedByName []string
	for _, m := range monitors {
		if m.Id.String() == req.ID {
			monitorID = m.Id.String()
			matchedByName = nil
			break
		}
		if m.Name == req.ID {
			matchedByName = append(matchedByName, m.Id.String())
		}
	}
	if monitorID == "" {
		switch len(matchedByName) {
		case 0:
			resp.Diagnostics.AddError("Monitor not found", fmt.Sprintf("No monitor found with name or ID %q", req.ID))
			return
		case 1:
			monitorID = matchedByName[0]
		default:
			resp.Diagnostics.AddError(
				"Ambiguous monitor import",
				fmt.Sprintf("%d monitors share the name %q (ids: %v). Import by UUID instead.", len(matchedByName), req.ID, matchedByName),
			)
			return
		}
	}

	monitor, err := api.Get[generated.MonitorDto](ctx, r.client, api.MonitorPath(monitorID))
	if err != nil {
		resp.Diagnostics.AddError("Error fetching monitor for import", err.Error())
		return
	}

	// Import only reads. Ownership moves to Terraform on the next apply
	// (ModifyPlan shows `owner` changing): `import` blocks run ImportState
	// during `terraform plan`, and a plan must not change the monitor.

	var model MonitorResourceModel
	// All collection-valued attributes start as typed-null. mapToState
	// will populate them from the API response when present. Pre-seeding
	// to an empty list would force the post-import state to look like
	// the user wrote `tag_ids = []` even when the resource has none —
	// causing a guaranteed `[] -> null` diff against any HCL that omits
	// the attribute (the common case for greenfield imports).
	//
	// IncidentPolicy is a single nested attribute and mapToState writes
	// either a populated object or null directly, so no pre-init needed.
	model.Assertions = types.ListNull(types.ObjectType{
		AttrTypes: assertionObjectType().AttrTypes,
	})
	model.Regions = types.ListNull(types.StringType)
	model.TagIds = types.ListNull(types.StringType)
	model.AlertChannelIds = types.ListNull(types.StringType)
	model.Code = types.ObjectNull(codeObjectType().AttrTypes)
	model.CodeHash = types.StringNull()
	model.CodeFiles = types.MapNull(types.StringType)
	model.TestBeforePublish = types.BoolNull()
	model.Timeouts = timeouts.Value{Object: types.ObjectNull(map[string]attr.Type{
		"create": types.StringType, "update": types.StringType,
	})}
	resp.Diagnostics.Append(r.mapToState(ctx, &model, monitor)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if codeMonitorTypes[monitor.Type] {
		resp.Diagnostics.Append(r.importCodeFiles(ctx, &model)...)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
