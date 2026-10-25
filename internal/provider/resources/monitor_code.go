package resources

// Code monitors (BROWSER, MULTI_STEP_API): the `code` block, plan-time
// bundling, validate-then-create, draft → test → publish, and rollback
// drift (doc 42 §7).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/bundle"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ resource.ResourceWithModifyPlan = &MonitorResource{}

// objectAsOptions reads nested objects whose optional members may be null or
// unknown.
var objectAsOptions = basetypes.ObjectAsOptions{UnhandledNullAsEmpty: true, UnhandledUnknownAsEmpty: true}

// Code monitor types.
var codeMonitorTypes = map[string]bool{
	string(generated.CreateMonitorRequestTypeBROWSER):      true,
	string(generated.CreateMonitorRequestTypeMULTISTEPAPI): true,
}

func isCodeType(t types.String) bool {
	return !t.IsNull() && !t.IsUnknown() && codeMonitorTypes[t.ValueString()]
}

// codeModel mirrors the `code` attribute.
type codeModel struct {
	Entrypoint types.String `tfsdk:"entrypoint"`
	Include    types.List   `tfsdk:"include"`
	Root       types.String `tfsdk:"root"`
}

func codeObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"entrypoint": types.StringType,
		"include":    types.ListType{ElemType: types.StringType},
		"root":       types.StringType,
	}}
}

func ownerObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"type":    types.StringType,
		"project": types.StringType,
		"address": types.StringType,
		"label":   types.StringType,
	}}
}

func capturePolicyObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"screenshots": types.StringType,
		"trace":       types.StringType,
		"video":       types.StringType,
		"network":     types.StringType,
	}}
}

type capturePolicyModel struct {
	Screenshots types.String `tfsdk:"screenshots"`
	Trace       types.String `tfsdk:"trace"`
	Video       types.String `tfsdk:"video"`
	Network     types.String `tfsdk:"network"`
}

// resolvedCode is one bundle upload: the local include set and the server's
// answer for it.
type resolvedCode struct {
	set    *bundle.Set
	bundle *generated.BundleDto
	source *generated.CodeSource
}

// spec is the `code` body for create, draft, PUT and ad-hoc test.
func (rc *resolvedCode) spec() generated.MonitorCodeSpec {
	return generated.MonitorCodeSpec{
		Bundle:     rc.bundle.Digest,
		Entrypoint: rc.set.Entrypoint,
		Source:     rc.source,
	}
}

// files maps each closure file the server returned to its per-file digest.
func (rc *resolvedCode) files(ctx context.Context) (types.Map, diag.Diagnostics) {
	var diags diag.Diagnostics
	out := make(map[string]string, len(rc.bundle.Files))
	for _, p := range rc.bundle.Files {
		content, ok := rc.set.Files[p]
		if !ok {
			diags.AddError("[validation] Closure file not in the upload",
				fmt.Sprintf("The API's closure lists %s, which the provider didn't send.", p))
			continue
		}
		out[p] = bundle.FileDigest(p, content)
	}
	m, d := types.MapValueFrom(ctx, types.StringType, out)
	diags.Append(d...)
	return m, diags
}

// resolveCode builds the include set for the `code` block and uploads it
// (`POST /bundles`, content-addressed, no revision created). The digest
// the provider computes over what it sent must equal the server's
// `uploadDigest`; otherwise the bytes were altered in transit and the
// provider refuses with BUNDLE_DIGEST_MISMATCH.
func (r *MonitorResource) resolveCode(ctx context.Context, code types.Object, configJSON types.String) (*resolvedCode, error) {
	var m codeModel
	if d := code.As(ctx, &m, objectAsOptions); d.HasError() {
		return nil, fmt.Errorf("reading code: %v", d.Errors())
	}
	in := bundle.Input{
		Entrypoint: m.Entrypoint.ValueString(),
		Root:       m.Root.ValueString(),
		Include:    stringListToSlice(m.Include),
	}
	set, err := bundle.Build(in)
	if err != nil {
		var be *bundle.Error
		if errors.As(err, &be) {
			return nil, api.NewClientValidationError(be.Code, be.Message)
		}
		return nil, err
	}
	b, err := api.UploadBundle(ctx, r.client, set.Files, set.Entrypoint, runtimeIDFromConfig(configJSON))
	if err != nil {
		return nil, err
	}
	if local := set.UploadDigest(); b.UploadDigest != local {
		return nil, api.NewClientValidationError("BUNDLE_DIGEST_MISMATCH", fmt.Sprintf(
			"the API hashed the upload as %s but the provider computed %s for the same files", b.UploadDigest, local))
	}
	return &resolvedCode{set: set, bundle: b, source: codeSource(set)}, nil
}

// runtimeIDFromConfig reads `runtimeId` from a code monitor's config JSON so
// the upload checks imports against the pinned runtime.
func runtimeIDFromConfig(configJSON types.String) string {
	if configJSON.IsNull() || configJSON.IsUnknown() || configJSON.ValueString() == "" {
		return ""
	}
	var cfg generated.ScriptMonitorConfig
	if json.Unmarshal([]byte(configJSON.ValueString()), &cfg) != nil || cfg.RuntimeId == nil {
		return ""
	}
	return *cfg.RuntimeId
}

// codeSource fills the advisory `source` from CI env or git (doc 42 §5.3).
func codeSource(set *bundle.Set) *generated.CodeSource {
	src := generated.CodeSource{}
	pick := func(env string, fallback func() string) *string {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return &v
		}
		if v := fallback(); v != "" {
			return &v
		}
		return nil
	}
	git := func(args ...string) func() string {
		return func() string {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, "git", append([]string{"-C", set.Root}, args...)...).Output()
			if err != nil {
				return ""
			}
			return strings.TrimSpace(string(out))
		}
	}
	src.Repo = pick("GITHUB_REPOSITORY", git("config", "--get", "remote.origin.url"))
	src.Branch = pick("GITHUB_REF_NAME", git("rev-parse", "--abbrev-ref", "HEAD"))
	src.Commit = pick("GITHUB_SHA", git("rev-parse", "HEAD"))
	entry := set.Entrypoint
	src.File = &entry
	if src.Repo != nil && len(*src.Repo) > 512 {
		src.Repo = nil
	}
	if src.Branch != nil && (len(*src.Branch) > 255 || *src.Branch == "HEAD") {
		src.Branch = nil
	}
	if src.Commit != nil && len(*src.Commit) > 64 {
		src.Commit = nil
	}
	return &src
}

// ── Plan ────────────────────────────────────────────────────────────────

// ModifyPlan computes `code_hash` and `code_files` for code monitors by
// uploading the include set at plan time, and marks the live revision as
// changing when the closure digest differs from what runs now. A rollback
// (or any out-of-band publish) shows up here: the files didn't change but
// the live digest no longer matches them.
func (r *MonitorResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destroy
	}
	var plan MonitorResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state *MonitorResourceModel
	if !req.State.Raw.IsNull() {
		var s MonitorResourceModel
		resp.Diagnostics.Append(req.State.Get(ctx, &s)...)
		if resp.Diagnostics.HasError() {
			return
		}
		state = &s
	}

	if state == nil {
		r.warnIfKeyCantOwn(ctx, req, &resp.Diagnostics)
	}

	// A monitor another tool owns (imported) moves to Terraform on apply.
	if state != nil && !state.Owner.IsNull() && ownerType(state.Owner) != api.ToolTerraform {
		plan.Owner = types.ObjectUnknown(ownerObjectType().AttrTypes)
	}

	if !plan.Type.IsUnknown() && !isCodeType(plan.Type) {
		plan.CodeHash = types.StringNull()
		plan.CodeFiles = types.MapNull(types.StringType)
		plan.LiveRevisionID = types.StringNull()
		plan.LiveRevisionDigest = types.StringNull()
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
		return
	}
	if plan.Code.IsNull() || plan.Code.IsUnknown() || !codeInputsKnown(ctx, plan.Code) || r.client == nil {
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
		return
	}

	rc, err := r.resolveCode(ctx, plan.Code, plan.Config)
	if err != nil {
		api.AddAPIError(&resp.Diagnostics, "upload the monitor's code bundle", err, path.Root("code"))
		return
	}
	plan.CodeHash = types.StringValue(rc.bundle.Digest)
	files, d := rc.files(ctx)
	resp.Diagnostics.Append(d...)
	plan.CodeFiles = files

	switch {
	case state == nil:
		plan.LiveRevisionDigest = types.StringValue(rc.bundle.Digest)
		plan.LiveRevisionID = types.StringUnknown()
	case state.LiveRevisionDigest.ValueString() == rc.bundle.Digest:
		plan.LiveRevisionDigest = state.LiveRevisionDigest
		plan.LiveRevisionID = state.LiveRevisionID
	default:
		plan.LiveRevisionDigest = types.StringValue(rc.bundle.Digest)
		plan.LiveRevisionID = types.StringUnknown()
		if state.CodeHash.ValueString() == rc.bundle.Digest && !state.LiveRevisionDigest.IsNull() {
			resp.Diagnostics.AddWarning("Live code differs from the configuration",
				r.liveCodeDriftDetail(ctx, state, rc.bundle.Digest, knownString(plan.AcknowledgeRollback)))
		}
	}
	resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
}

// warnIfKeyCantOwn warns at plan time when a new monitor will be
// owner-enforced but the provider's API key writes as another tool
// (`GET /auth/me` → `key.tool`, null meaning API). Owner enforcement would
// then refuse Terraform's own later writes. Credentials that aren't API
// keys (auth/me refuses them) are skipped.
func (r *MonitorResource) warnIfKeyCantOwn(ctx context.Context, req resource.ModifyPlanRequest, diags *diag.Diagnostics) {
	if r.client == nil {
		return
	}
	var configured types.Bool
	diags.Append(req.Config.GetAttribute(ctx, path.Root("owner_enforced"), &configured)...)
	if !configured.IsNull() && (configured.IsUnknown() || !configured.ValueBool()) {
		return
	}
	tool, ok := r.client.KeyTool(ctx)
	if !ok || tool == api.ToolTerraform {
		return
	}
	diags.AddAttributeWarning(path.Root("owner_enforced"), "This API key can't own Terraform monitors",
		fmt.Sprintf("The monitor will be owner-enforced and owned by TERRAFORM, but this provider's API key "+
			"writes as %s, so the API will refuse Terraform's later changes to it. Use an API key created "+
			"with tool TERRAFORM, or set owner_enforced = false.", tool))
}

// codeInputsKnown reports whether every attribute of the code block is known
// (an entrypoint computed from another resource is unknown until apply).
func codeInputsKnown(ctx context.Context, code types.Object) bool {
	var m codeModel
	if d := code.As(ctx, &m, objectAsOptions); d.HasError() {
		return false
	}
	if m.Entrypoint.IsUnknown() || m.Root.IsUnknown() || m.Include.IsUnknown() {
		return false
	}
	for _, e := range m.Include.Elements() {
		if e.IsUnknown() {
			return false
		}
	}
	return true
}

// liveCodeDriftDetail describes why the live code differs, naming the
// rollback when the definition head carries one.
func (r *MonitorResource) liveCodeDriftDetail(ctx context.Context, state *MonitorResourceModel, digest, acknowledged string) string {
	base := fmt.Sprintf("The live revision runs %s, but these files hash to %s. Apply publishes them again.",
		state.LiveRevisionDigest.ValueString(), digest)
	monitor, err := api.Get[generated.MonitorDto](ctx, r.client, api.MonitorPath(state.ID.ValueString()))
	if err != nil || monitor.DefinitionId == nil {
		if err != nil {
			tflog.Warn(ctx, "reading the monitor for rollback context failed", map[string]any{"error": err.Error()})
		}
		return base
	}
	def, err := api.Get[generated.DefinitionDetailDto](ctx, r.client, api.DefinitionPath(monitor.DefinitionId.String()))
	if err != nil {
		tflog.Warn(ctx, "reading the definition for rollback context failed", map[string]any{"error": err.Error()})
		return base
	}
	for _, head := range def.Heads {
		if monitor.Environment == nil || head.Environment.Id != monitor.Environment.Id {
			continue
		}
		who := "someone"
		if head.ActivatedBy != nil && *head.ActivatedBy != "" {
			who = *head.ActivatedBy
		}
		msg := fmt.Sprintf("Live code differs (rolled back to revision %d by %s).", head.Revision.Number, who)
		if head.Reason != nil && *head.Reason != "" {
			msg += fmt.Sprintf(" Reason: %s.", *head.Reason)
		}
		if head.RolledBackDigest != nil && *head.RolledBackDigest == digest &&
			acknowledged != digest {
			return msg + fmt.Sprintf(" These files are the rolled-back code, so apply will be refused. To publish "+
				"them again on purpose set acknowledge_rollback = %q, or change the code.", digest)
		}
		return msg + " " + base
	}
	return base
}

// ── Apply ───────────────────────────────────────────────────────────────

// prepareCode uploads a code monitor's include set at apply time (so the
// bundle exists within its TTL) and refuses when the files changed since the
// plan. Probes return (nil, true).
func (r *MonitorResource) prepareCode(ctx context.Context, plan *MonitorResourceModel, diags *diag.Diagnostics) (*resolvedCode, bool) {
	if !isCodeType(plan.Type) {
		return nil, true
	}
	rc, err := r.resolveCode(ctx, plan.Code, plan.Config)
	if err != nil {
		api.AddAPIError(diags, "upload the monitor's code bundle", err, path.Root("code"))
		return nil, false
	}
	if !plan.CodeHash.IsUnknown() && plan.CodeHash.ValueString() != rc.bundle.Digest {
		diags.AddAttributeError(path.Root("code"), "[validation] Code changed since the plan",
			fmt.Sprintf("The plan was made for %s but the files now hash to %s. Run `terraform plan` again.",
				plan.CodeHash.ValueString(), rc.bundle.Digest))
		return nil, false
	}
	return rc, true
}

// applyCodeToPlan records the uploaded code in code_hash / code_files
// (null for probes).
func applyCodeToPlan(ctx context.Context, model *MonitorResourceModel, rc *resolvedCode) diag.Diagnostics {
	if rc == nil {
		model.CodeHash = types.StringNull()
		model.CodeFiles = types.MapNull(types.StringType)
		return nil
	}
	model.CodeHash = types.StringValue(rc.bundle.Digest)
	files, d := rc.files(ctx)
	model.CodeFiles = files
	return d
}

// causedByPlatform reports whether a run's non-pass was the platform's doing
// (RunDto.cause is set only for platform causes: no seat, runtime
// unavailable, lease lost, …), not the code's.
func causedByPlatform(run *generated.RunDto) bool {
	return run.Cause != nil && *run.Cause != ""
}

// addRunDiagnostic reports a finished, non-passing validation run:
// `[check_failed] <run.url>`, or `[platform]` when the platform caused it
// (the code wasn't at fault and a retry may pass).
func addRunDiagnostic(diags *diag.Diagnostics, op string, run *generated.RunDto, keptDraft string) {
	outcome := "unknown"
	if run.Outcome != nil {
		outcome = *run.Outcome
	}
	ref := run.Id.String()
	if run.Url != nil && *run.Url != "" {
		ref = *run.Url
	}
	category := "check_failed"
	lines := []string{fmt.Sprintf("While trying to %s, the validation run finished %s.", op, outcome)}
	if causedByPlatform(run) {
		category = api.CategoryPlatform
		lines = append(lines, fmt.Sprintf("Cause: %s (the platform, not the code). Run apply again.", *run.Cause))
	}
	if run.Failure != nil {
		if b, err := json.Marshal(run.Failure); err == nil {
			lines = append(lines, "Failure: "+string(b))
		}
	}
	if keptDraft != "" {
		lines = append(lines, fmt.Sprintf("The old code stays live. Draft %s is kept; Terraform state is unchanged.", keptDraft))
	} else {
		lines = append(lines, "Nothing was created.")
	}
	lines = append(lines, "Next: devhelm runs get "+run.Id.String())
	diags.AddError(fmt.Sprintf("[%s] %s", category, ref), strings.Join(lines, "\n"))
}

// validateNewCode runs the ad-hoc validation run for a new code monitor
// (`POST /monitors/test` with the uploaded bundle), waits, and reports a
// non-pass. It returns true when the code may be created.
func (r *MonitorResource) validateNewCode(ctx context.Context, plan *MonitorResourceModel, rc *resolvedCode, diags *diag.Diagnostics) bool {
	const op = "validate the new monitor's code"
	envID, err := parseUUIDPtrChecked(plan.EnvironmentID, "environment_id")
	if err != nil {
		diags.AddAttributeError(path.Root("environment_id"), "Invalid environment_id", err.Error())
		return false
	}
	code := rc.spec()
	body := generated.MonitorTestRequest{
		Type:          generated.MonitorTestRequestType(plan.Type.ValueString()),
		EnvironmentId: envID,
		Code:          &code,
	}
	if cfg := knownString(plan.Config); cfg != "" {
		var config generated.MonitorTestRequest_Config
		if err := config.UnmarshalJSON([]byte(cfg)); err != nil {
			diags.AddAttributeError(path.Root("config"), "Invalid config", err.Error())
			return false
		}
		body.Config = &config
	}
	run, err := api.Create[generated.RunDto](ctx, r.client, api.PathMonitorAdHocTest, body)
	if err == nil {
		run, err = api.WaitForRun(ctx, r.client, run.Id.String(), waitBudget(ctx))
	}
	if err != nil {
		api.AddAPIError(diags, op, err, path.Root("code"))
		return false
	}
	if !api.RunPassed(run) {
		addRunDiagnostic(diags, op, run, "")
		return false
	}
	return true
}

// testDraft creates (or reuses) a draft for rc, runs its validation run and
// waits. It returns the draft when the run passed.
func (r *MonitorResource) testDraft(ctx context.Context, id string, rc *resolvedCode, diags *diag.Diagnostics) (*generated.RevisionDto, bool) {
	draft, err := api.Create[generated.CreateDraftRevisionResponse](ctx, r.client, api.MonitorRevisionsPath(id),
		generated.CreateDraftRevisionRequest{Code: rc.spec()})
	if err != nil {
		api.AddAPIError(diags, "create a draft revision", err, path.Root("code"))
		return nil, false
	}
	revisionID := draft.Revision.Id.String()
	run, err := api.Create[generated.RunDto](ctx, r.client, api.MonitorRevisionTestPath(id, revisionID), generated.CodeTestTargetRequest{})
	if err == nil {
		run, err = api.WaitForRun(ctx, r.client, run.Id.String(), waitBudget(ctx))
	}
	if err != nil {
		api.AddAPIError(diags, "test the draft revision", err, path.Root("code"))
		return nil, false
	}
	if !api.RunPassed(run) {
		addRunDiagnostic(diags, "test the new code", run, revisionID)
		return nil, false
	}
	return &draft.Revision, true
}

// publishDraft makes a tested draft live with `requireTested: true` and the
// live revision as the compare-and-swap guard.
func (r *MonitorResource) publishDraft(ctx context.Context, id string, revisionID uuid.UUID, acknowledge, liveRevisionID types.String) error {
	requireTested := true
	body := generated.PublishRevisionRequest{
		RevisionId:          revisionID,
		RequireTested:       &requireTested,
		AcknowledgeRollback: stringPtrOrNil(acknowledge),
	}
	if live := knownString(liveRevisionID); live != "" {
		parsed, err := uuid.Parse(live)
		if err != nil {
			return fmt.Errorf("live_revision_id in state is not a UUID (%q): %w", live, err)
		}
		body.ExpectedLiveRevisionId = &parsed
	}
	_, err := api.Create[generated.DefinitionHeadDto](ctx, r.client, api.MonitorPublishPath(id), body)
	return err
}

// addPublishError adds the fix for the publish refusals Terraform can act on.
func addPublishError(diags *diag.Diagnostics, err error) {
	var apiErr *api.DevhelmAPIError
	if errors.As(err, &apiErr) {
		switch apiErr.Code {
		case "REVISION_ROLLED_BACK":
			var digest string
			apiErr.Detail("rolledBackDigest", &digest)
			summary, detail := api.APIErrorText("publish the new code", apiErr)
			diags.AddAttributeError(path.Root("acknowledge_rollback"), summary, detail+fmt.Sprintf(
				"\nThis code was rolled back. Change the code, or set acknowledge_rollback = %q to publish it again on purpose.", digest))
			return
		case "LIVE_REVISION_CHANGED":
			summary, detail := api.APIErrorText("publish the new code", apiErr)
			diags.AddAttributeError(path.Root("code"), summary, detail+
				"\nSomeone published or rolled back since this plan. Run `terraform plan` again to see the live code.")
			return
		}
	}
	api.AddAPIError(diags, "publish the new code", err, path.Root("code"))
}

// importCodeFiles fills code_hash / code_files for an imported code monitor
// from its live revision.
func (r *MonitorResource) importCodeFiles(ctx context.Context, model *MonitorResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics
	if model.LiveRevisionID.IsNull() || model.LiveRevisionID.ValueString() == "" {
		return diags
	}
	model.CodeHash = model.LiveRevisionDigest
	files, err := api.Get[generated.RevisionFilesDto](ctx, r.client, api.RevisionFilesPath(model.LiveRevisionID.ValueString()))
	if err != nil {
		diags.AddWarning("Live revision files not read",
			fmt.Sprintf("code_files stays empty until the next apply: %s", err))
		return diags
	}
	out := make(map[string]string, len(files.Files))
	for p, content := range files.Files {
		out[p] = bundle.FileDigest(p, []byte(content))
	}
	m, d := types.MapValueFrom(ctx, types.StringType, out)
	diags.Append(d...)
	model.CodeFiles = m
	return diags
}
