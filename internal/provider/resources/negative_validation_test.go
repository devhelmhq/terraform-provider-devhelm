package resources

import (
	"context"
	"strings"
	"testing"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ───────────────────────────────────────────────────────────────────────
// Negative validation tests (Class N)
//
// These tests exercise error paths in request builders and state mappers
// across all resource types. The positive paths are covered in the
// per-resource *_test.go files; this file pins the contract that invalid
// inputs produce errors rather than silently corrupting the wire payload
// or crashing mapToState.
// ───────────────────────────────────────────────────────────────────────

// ── Monitor: invalid build inputs ───────────────────────────────────────

func TestMonitor_BuildCreate_InvalidTagIDErrors(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}
	plan := &MonitorResourceModel{
		Name:   types.StringValue("neg"),
		Type:   types.StringValue("HTTP"),
		Config: types.StringValue(`{"url":"https://example.com"}`),
		TagIds: types.ListValueMust(types.StringType, []attr.Value{
			types.StringValue("not-a-valid-uuid"),
		}),
		Assertions:     types.ListNull(assertionObjectType()),
		IncidentPolicy: types.ObjectNull(incidentPolicyObjectType().AttrTypes),
	}
	_, err := r.buildCreateRequest(ctx, plan)
	if err == nil {
		t.Fatal("expected error for invalid tag UUID")
	}
	if !strings.Contains(err.Error(), "tag") {
		t.Errorf("error %q should mention 'tag'", err.Error())
	}
}

func TestMonitor_BuildCreate_InvalidEnvironmentIDErrors(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}
	plan := &MonitorResourceModel{
		Name:           types.StringValue("neg"),
		Type:           types.StringValue("HTTP"),
		Config:         types.StringValue(`{"url":"https://example.com"}`),
		EnvironmentID:  types.StringValue("definitely-not-uuid"),
		Assertions:     types.ListNull(assertionObjectType()),
		IncidentPolicy: types.ObjectNull(incidentPolicyObjectType().AttrTypes),
	}
	_, err := r.buildCreateRequest(ctx, plan)
	if err == nil {
		t.Fatal("expected error for invalid environment UUID")
	}
	if !strings.Contains(err.Error(), "environment") {
		t.Errorf("error %q should mention 'environment'", err.Error())
	}
}

func TestMonitor_BuildUpdate_InvalidConfigJSONErrors(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}
	plan := &MonitorResourceModel{
		Name:           types.StringValue("neg"),
		Type:           types.StringValue("HTTP"),
		Config:         types.StringValue(`{not valid json`),
		Assertions:     types.ListNull(assertionObjectType()),
		IncidentPolicy: types.ObjectNull(incidentPolicyObjectType().AttrTypes),
	}
	// buildUpdateRequest now uses the generated union wrapper (spec exposes
	// UpdateMonitorRequest.config as a proper oneOf). The wrapper's
	// UnmarshalJSON accepts raw bytes without validating them, so the
	// builder does a pre-flight json.Valid check to keep the same
	// "invalid JSON errors at plan time" guardrail as the create path.
	_, err := r.buildUpdateRequest(ctx, plan)
	if err == nil {
		t.Fatal("expected error for invalid JSON config in update path")
	}
}

func TestMonitor_BuildUpdate_InvalidAlertChannelIDErrors(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}
	plan := &MonitorResourceModel{
		Name:   types.StringValue("neg"),
		Type:   types.StringValue("HTTP"),
		Config: types.StringValue(`{"url":"https://example.com"}`),
		AlertChannelIds: types.ListValueMust(types.StringType, []attr.Value{
			types.StringValue("bad-uuid-here"),
		}),
		Assertions:     types.ListNull(assertionObjectType()),
		IncidentPolicy: types.ObjectNull(incidentPolicyObjectType().AttrTypes),
	}
	_, err := r.buildUpdateRequest(ctx, plan)
	if err == nil {
		t.Fatal("expected error for invalid alert channel UUID")
	}
	if !strings.Contains(err.Error(), "alert_channel_ids") {
		t.Errorf("error %q should mention field name", err.Error())
	}
}

// ── Monitor: mapToState with missing/nil DTO fields ─────────────────────

func TestMonitor_MapToState_NilAssertionsStaysNull(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}
	dto := &generated.MonitorDto{
		Id:               openapi_types.UUID(uuid.New()),
		Name:             "x",
		Type:             "HTTP",
		FrequencySeconds: 60,
		Enabled:          true,
		Assertions:       nil,
	}
	model := &MonitorResourceModel{
		Assertions: types.ListNull(assertionObjectType()),
	}
	r.mapToState(ctx, model, dto)
	if !model.Assertions.IsNull() {
		t.Errorf("Assertions should be null when DTO has nil assertions, got %v", model.Assertions)
	}
}

func TestMonitor_MapToState_NilAlertChannelIds(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}
	dto := &generated.MonitorDto{
		Id:               openapi_types.UUID(uuid.New()),
		Name:             "x",
		Type:             "HTTP",
		FrequencySeconds: 60,
		Enabled:          true,
		AlertChannelIds:  nil,
	}
	model := &MonitorResourceModel{
		Assertions: types.ListNull(assertionObjectType()),
	}
	r.mapToState(ctx, model, dto)
	if !model.AlertChannelIds.IsNull() {
		t.Errorf("AlertChannelIds should be null when DTO is nil, got %v", model.AlertChannelIds)
	}
}

func TestMonitor_MapToState_NilRegions(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}
	dto := &generated.MonitorDto{
		Id:               openapi_types.UUID(uuid.New()),
		Name:             "x",
		Type:             "HTTP",
		FrequencySeconds: 60,
		Enabled:          true,
		Regions:          nil,
	}
	model := &MonitorResourceModel{
		Assertions: types.ListNull(assertionObjectType()),
	}
	r.mapToState(ctx, model, dto)
	if !model.Regions.IsNull() {
		t.Errorf("Regions should be null when DTO regions is nil, got %v", model.Regions)
	}
}

func TestMonitor_MapToState_NilTags(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}
	dto := &generated.MonitorDto{
		Id:               openapi_types.UUID(uuid.New()),
		Name:             "x",
		Type:             "HTTP",
		FrequencySeconds: 60,
		Enabled:          true,
		Tags:             nil,
	}
	model := &MonitorResourceModel{
		Assertions: types.ListNull(assertionObjectType()),
	}
	r.mapToState(ctx, model, dto)
	if !model.TagIds.IsNull() {
		t.Errorf("TagIds should be null when DTO tags is nil, got %v", model.TagIds)
	}
}

func TestMonitor_MapToState_EmptyAuthStringSetNull(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}
	dto := &generated.MonitorDto{
		Id:               openapi_types.UUID(uuid.New()),
		Name:             "x",
		Type:             "HTTP",
		FrequencySeconds: 60,
		Enabled:          true,
		Auth:             nil,
	}
	model := &MonitorResourceModel{
		Assertions: types.ListNull(assertionObjectType()),
	}
	r.mapToState(ctx, model, dto)
	if !model.Auth.IsNull() {
		t.Errorf("Auth should be null when rawAuth is empty and DTO auth is nil, got %v", model.Auth)
	}
}

func TestMonitor_MapToState_NilPingUrl(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}
	dto := &generated.MonitorDto{
		Id:               openapi_types.UUID(uuid.New()),
		Name:             "x",
		Type:             "HTTP",
		FrequencySeconds: 60,
		Enabled:          true,
		PingUrl:          nil,
	}
	model := &MonitorResourceModel{
		Assertions: types.ListNull(assertionObjectType()),
	}
	r.mapToState(ctx, model, dto)
	if !model.PingUrl.IsNull() {
		t.Errorf("PingUrl should be null when DTO value is nil, got %q", model.PingUrl.ValueString())
	}
}

func TestMonitor_MapToState_NilIncidentPolicy(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}
	dto := &generated.MonitorDto{
		Id:               openapi_types.UUID(uuid.New()),
		Name:             "x",
		Type:             "HTTP",
		FrequencySeconds: 60,
		Enabled:          true,
		IncidentPolicy:   nil,
	}
	model := &MonitorResourceModel{
		Assertions:     types.ListNull(assertionObjectType()),
		IncidentPolicy: types.ObjectNull(incidentPolicyObjectType().AttrTypes),
	}
	r.mapToState(ctx, model, dto)
	if !model.IncidentPolicy.IsNull() {
		t.Errorf("IncidentPolicy should stay null when DTO is nil, got %v", model.IncidentPolicy)
	}
}

// ── Alert channel: negative build paths ────────────────────────────────

func TestAlertChannel_BuildConfig_UnrecognizedTypeErrors(t *testing.T) {
	r := &AlertChannelResource{}
	model := AlertChannelResourceModel{
		ChannelType: types.StringValue("carrier_pigeon"),
	}
	_, err := r.buildConfig(&model)
	if err == nil {
		t.Fatal("expected error for unknown channel type 'carrier_pigeon'")
	}
}

func TestAlertChannel_BuildConfig_SlackMissingWebhookUrl(t *testing.T) {
	r := &AlertChannelResource{}
	model := AlertChannelResourceModel{
		ChannelType: types.StringValue("slack"),
		WebhookURL:  types.StringNull(),
		MentionText: types.StringNull(),
	}
	raw, err := r.buildConfig(&model)
	if err != nil {
		t.Fatalf("buildConfig errored: %v", err)
	}
	// When webhook_url is null, the JSON body will have `"webhookUrl":null`
	// which the API should reject with a 400. The provider must not crash.
	if len(raw) == 0 {
		t.Error("buildConfig returned empty bytes")
	}
}

// ── Environment: mapToState with edge-case DTOs ────────────────────────

func TestEnvironment_MapToState_FalseIsDefaultMapsToFalse(t *testing.T) {
	ctx := context.Background()
	r := &EnvironmentResource{}
	dto := &generated.EnvironmentDto{
		Id:        openapi_types.UUID(uuid.New()),
		Name:      "x",
		Slug:      "x",
		IsDefault: false,
	}
	model := &EnvironmentResourceModel{}
	r.mapToState(ctx, model, dto)
	if model.IsDefault.ValueBool() {
		t.Errorf("IsDefault should be false for zero-value DTO, got %v", model.IsDefault.ValueBool())
	}
}

// ── Tag: negative build validations ────────────────────────────────────

func TestTag_BuildBody_EmptyNamePassesThrough(t *testing.T) {
	plan := TagResourceModel{
		Name:  types.StringValue(""),
		Color: types.StringNull(),
	}
	body := generated.CreateTagRequest{
		Name:  plan.Name.ValueString(),
		Color: stringPtrOrNil(plan.Color),
	}
	if body.Name != "" {
		t.Errorf("Name should be empty string, got %q", body.Name)
	}
}

// ── Secret: hash stability ──────────────────────────────────────────────

func TestSecret_Sha256Hex_EmptyInputProducesKnownHash(t *testing.T) {
	h := sha256Hex("")
	// SHA-256 of empty string is well-known.
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if h != want {
		t.Errorf("sha256Hex('') = %s, want %s", h, want)
	}
}

// ── Webhook: missing URL produces correct body ─────────────────────────

func TestWebhook_BuildBody_NullURL(t *testing.T) {
	// The provider builds the request inline; when URL is null, the API
	// field should be nil (omitted), not an empty string, so the API can
	// produce a proper validation error.
	url := stringPtrOrNil(types.StringNull())
	if url != nil {
		t.Errorf("stringPtrOrNil(null) = %v, want nil", url)
	}
}

// ── ResourceGroup: buildUpdateRequest with invalid UUIDs ───────────────

func TestResourceGroup_BuildUpdate_InvalidAlertPolicyIDErrors(t *testing.T) {
	ctx := context.Background()
	r := &ResourceGroupResource{}
	plan := &ResourceGroupModel{
		Name:          types.StringValue("neg"),
		Slug:          types.StringValue("neg"),
		AlertPolicyID: types.StringValue("not-a-uuid"),
	}
	_, diags := r.buildUpdateRequest(ctx, plan)
	if !diags.HasError() {
		t.Fatal("expected diagnostics error for invalid alert_policy_id UUID")
	}
}

func TestResourceGroup_BuildUpdate_InvalidDefaultEnvironmentIDErrors(t *testing.T) {
	ctx := context.Background()
	r := &ResourceGroupResource{}
	plan := &ResourceGroupModel{
		Name:                 types.StringValue("neg"),
		Slug:                 types.StringValue("neg"),
		DefaultEnvironmentID: types.StringValue("bad-uuid"),
	}
	_, diags := r.buildUpdateRequest(ctx, plan)
	if !diags.HasError() {
		t.Fatal("expected diagnostics error for invalid default_environment_id UUID")
	}
}

// ── ResourceGroup: mapToState with nil optional fields ──────────────────

func TestResourceGroup_MapToState_NilDescriptionBecomesNull(t *testing.T) {
	ctx := context.Background()
	r := &ResourceGroupResource{}
	dto := &generated.ResourceGroupDto{
		Id:          openapi_types.UUID(uuid.New()),
		Name:        "grp",
		Slug:        "grp",
		Description: nil,
	}
	model := &ResourceGroupModel{}
	r.mapToState(ctx, model, dto)
	if !model.Description.IsNull() {
		t.Errorf("Description should be null when DTO ptr is nil, got %q", model.Description.ValueString())
	}
}

// ── StatusPage: mapToState with nil optional fields ────────────────────

func TestStatusPage_MapToState_NilDescriptionBecomesNull(t *testing.T) {
	ctx := context.Background()
	r := &StatusPageResource{}
	dto := &generated.StatusPageDto{
		Id:           openapi_types.UUID(uuid.New()),
		Name:         "x",
		Slug:         "x",
		Description:  nil,
		Visibility:   "PUBLIC",
		Enabled:      true,
		IncidentMode: "MANUAL",
	}
	model := &StatusPageResourceModel{}
	r.mapToState(ctx, model, dto)
	if !model.Description.IsNull() {
		t.Errorf("Description should be null, got %q", model.Description.ValueString())
	}
}

func TestStatusPage_MapToState_SyntheticPageURL(t *testing.T) {
	ctx := context.Background()
	r := &StatusPageResource{}
	dto := &generated.StatusPageDto{
		Id:           openapi_types.UUID(uuid.New()),
		Name:         "Acme",
		Slug:         "acme",
		Visibility:   "PUBLIC",
		Enabled:      true,
		IncidentMode: "MANUAL",
	}
	model := &StatusPageResourceModel{}
	r.mapToState(ctx, model, dto)
	want := "https://acme.devhelmstatus.com"
	if model.PageURL.ValueString() != want {
		t.Errorf("PageURL = %q, want %q", model.PageURL.ValueString(), want)
	}
}

// ── StatusPageComponent: mapToState with nil group ──────────────────────

func TestStatusPageComponent_MapToState_NilGroupID(t *testing.T) {
	r := &StatusPageComponentResource{}
	dto := &generated.StatusPageComponentDto{
		Id:   openapi_types.UUID(uuid.New()),
		Name: "comp",
		Type: "STATIC",
	}
	model := &StatusPageComponentResourceModel{}
	r.mapToState(model, dto)
	if !model.GroupID.IsNull() {
		t.Errorf("GroupID should be null when DTO group is nil, got %q", model.GroupID.ValueString())
	}
}

// ── StatusPageComponentGroup: mapToState basics ────────────────────────

func TestStatusPageComponentGroup_MapToState_PopulatesFields(t *testing.T) {
	r := &StatusPageComponentGroupResource{}
	id := openapi_types.UUID(uuid.New())
	pageID := openapi_types.UUID(uuid.New())
	dto := &generated.StatusPageComponentGroupDto{
		Id:           id,
		StatusPageId: pageID,
		Name:         "Backend Services",
	}
	model := &StatusPageComponentGroupResourceModel{}
	r.mapToState(model, dto)
	if model.ID.ValueString() != id.String() {
		t.Errorf("ID = %q, want %s", model.ID.ValueString(), id)
	}
	if model.Name.ValueString() != "Backend Services" {
		t.Errorf("Name = %q", model.Name.ValueString())
	}
}

// ── StatusPageCustomDomain: mapToState basics ──────────────────────────

func TestStatusPageCustomDomain_MapToState_PopulatesFields(t *testing.T) {
	r := &StatusPageCustomDomainResource{}
	id := openapi_types.UUID(uuid.New())
	dto := &generated.StatusPageCustomDomainDto{
		Id:                      id,
		Hostname:                "status.example.com",
		Status:                  "PENDING",
		VerificationMethod:      "CNAME",
		VerificationToken:       "tok123",
		VerificationCnameTarget: "target.devhelm.io",
	}
	model := &StatusPageCustomDomainResourceModel{
		StatusPageID: types.StringValue(uuid.New().String()),
	}
	r.mapToState(model, dto)
	if model.Hostname.ValueString() != "status.example.com" {
		t.Errorf("Hostname = %q", model.Hostname.ValueString())
	}
	if model.Status.ValueString() != "PENDING" {
		t.Errorf("Status = %q", model.Status.ValueString())
	}
}

// ── NotificationPolicy: buildUpdateRequest with invalid escalation ─────

func TestNotificationPolicy_BuildUpdate_EmptyEscalation(t *testing.T) {
	ctx := context.Background()
	r := &NotificationPolicyResource{}
	plan := &NotificationPolicyModel{
		Name:       types.StringValue("neg-policy"),
		Enabled:    types.BoolValue(true),
		Priority:   types.Int64Value(1),
		MatchRules: types.ListNull(matchRuleObjectType()),
		Escalation: types.ListNull(escalationStepObjectType()),
		OnResolve:  types.StringNull(),
		OnReopen:   types.StringNull(),
	}
	body, err := r.buildUpdateRequest(ctx, plan)
	if err != nil {
		t.Fatalf("buildUpdateRequest: %v", err)
	}
	if body.Name == nil || *body.Name != "neg-policy" {
		t.Errorf("Name = %v, want 'neg-policy'", body.Name)
	}
}

// ── validateMonitorAuth: negative cases ────────────────────────────────
//
// validateMonitorAuth is the plan-time gate for the typed `auth { ... }`
// block. We exercise every failure mode it must catch BEFORE the request
// reaches the API:
//
//   • zero variants set under a non-null `auth` block
//   • multiple variants set simultaneously
//   • header / api_key without the required `header_name`
//
// Each failure must surface as an attribute-pathed diagnostic so editors
// underline the offending block and `terraform plan` exits non-zero with
// an actionable message.

func TestValidateMonitorAuth_NoVariantsSetIsError(t *testing.T) {
	ctx := context.Background()
	authObj, _ := types.ObjectValue(monitorAuthObjectType().AttrTypes, map[string]attr.Value{
		"bearer":  types.ObjectNull(authBearerObjectType().AttrTypes),
		"basic":   types.ObjectNull(authBasicObjectType().AttrTypes),
		"header":  types.ObjectNull(authHeaderObjectType().AttrTypes),
		"api_key": types.ObjectNull(authApiKeyObjectType().AttrTypes),
	})
	var diags diag.Diagnostics
	validateMonitorAuth(ctx, &diags, authObj)
	if !diags.HasError() {
		t.Fatalf("expected error for empty auth block, got %v", diags)
	}
	if !strings.Contains(diags.Errors()[0].Summary(), "Missing auth variant") {
		t.Errorf("expected missing-variant summary, got %q", diags.Errors()[0].Summary())
	}
}

func TestValidateMonitorAuth_MultipleVariantsIsError(t *testing.T) {
	ctx := context.Background()
	bearer, _ := types.ObjectValue(authBearerObjectType().AttrTypes, map[string]attr.Value{
		"vault_secret_id": types.StringValue("00000000-0000-0000-0000-000000000001"),
	})
	basic, _ := types.ObjectValue(authBasicObjectType().AttrTypes, map[string]attr.Value{
		"vault_secret_id": types.StringValue("00000000-0000-0000-0000-000000000002"),
	})
	authObj, _ := types.ObjectValue(monitorAuthObjectType().AttrTypes, map[string]attr.Value{
		"bearer":  bearer,
		"basic":   basic,
		"header":  types.ObjectNull(authHeaderObjectType().AttrTypes),
		"api_key": types.ObjectNull(authApiKeyObjectType().AttrTypes),
	})
	var diags diag.Diagnostics
	validateMonitorAuth(ctx, &diags, authObj)
	if !diags.HasError() {
		t.Fatalf("expected error for multi-variant auth, got %v", diags)
	}
	if !strings.Contains(diags.Errors()[0].Summary(), "Multiple auth variants") {
		t.Errorf("expected multi-variant summary, got %q", diags.Errors()[0].Summary())
	}
}

func TestValidateMonitorAuth_HeaderVariantRequiresHeaderName(t *testing.T) {
	ctx := context.Background()
	header, _ := types.ObjectValue(authHeaderObjectType().AttrTypes, map[string]attr.Value{
		"header_name":     types.StringNull(),
		"vault_secret_id": types.StringValue("00000000-0000-0000-0000-000000000001"),
	})
	authObj, _ := types.ObjectValue(monitorAuthObjectType().AttrTypes, map[string]attr.Value{
		"bearer":  types.ObjectNull(authBearerObjectType().AttrTypes),
		"basic":   types.ObjectNull(authBasicObjectType().AttrTypes),
		"header":  header,
		"api_key": types.ObjectNull(authApiKeyObjectType().AttrTypes),
	})
	var diags diag.Diagnostics
	validateMonitorAuth(ctx, &diags, authObj)
	if !diags.HasError() {
		t.Fatalf("expected error for header without header_name, got %v", diags)
	}
}

func TestValidateMonitorAuth_ApiKeyVariantRequiresHeaderName(t *testing.T) {
	ctx := context.Background()
	apikey, _ := types.ObjectValue(authApiKeyObjectType().AttrTypes, map[string]attr.Value{
		"header_name":     types.StringNull(),
		"vault_secret_id": types.StringValue("00000000-0000-0000-0000-000000000001"),
	})
	authObj, _ := types.ObjectValue(monitorAuthObjectType().AttrTypes, map[string]attr.Value{
		"bearer":  types.ObjectNull(authBearerObjectType().AttrTypes),
		"basic":   types.ObjectNull(authBasicObjectType().AttrTypes),
		"header":  types.ObjectNull(authHeaderObjectType().AttrTypes),
		"api_key": apikey,
	})
	var diags diag.Diagnostics
	validateMonitorAuth(ctx, &diags, authObj)
	if !diags.HasError() {
		t.Fatalf("expected error for api_key without header_name, got %v", diags)
	}
}

func TestValidateMonitorAuth_NullAuthIsNoOp(t *testing.T) {
	ctx := context.Background()
	var diags diag.Diagnostics
	validateMonitorAuth(ctx, &diags, types.ObjectNull(monitorAuthObjectType().AttrTypes))
	if diags.HasError() {
		t.Fatalf("null auth should not produce diagnostics, got %v", diags)
	}
}

func TestValidateMonitorAuth_HappyPathBearer(t *testing.T) {
	ctx := context.Background()
	bearer, _ := types.ObjectValue(authBearerObjectType().AttrTypes, map[string]attr.Value{
		"vault_secret_id": types.StringValue("00000000-0000-0000-0000-000000000001"),
	})
	authObj, _ := types.ObjectValue(monitorAuthObjectType().AttrTypes, map[string]attr.Value{
		"bearer":  bearer,
		"basic":   types.ObjectNull(authBasicObjectType().AttrTypes),
		"header":  types.ObjectNull(authHeaderObjectType().AttrTypes),
		"api_key": types.ObjectNull(authApiKeyObjectType().AttrTypes),
	})
	var diags diag.Diagnostics
	validateMonitorAuth(ctx, &diags, authObj)
	if diags.HasError() {
		t.Fatalf("expected no errors for valid bearer auth, got %v", diags)
	}
}

// ── priorHasConfigType: edge cases ─────────────────────────────────────

func TestPriorHasConfigType_NullReturnsFlse(t *testing.T) {
	if priorHasConfigType(types.StringNull()) {
		t.Error("null should return false")
	}
}

func TestPriorHasConfigType_UnknownReturnsFalse(t *testing.T) {
	if priorHasConfigType(types.StringUnknown()) {
		t.Error("unknown should return false")
	}
}

func TestPriorHasConfigType_EmptyStringReturnsFalse(t *testing.T) {
	if priorHasConfigType(types.StringValue("")) {
		t.Error("empty string should return false")
	}
}

func TestPriorHasConfigType_InvalidJSONReturnsFalse(t *testing.T) {
	if priorHasConfigType(types.StringValue("not json")) {
		t.Error("invalid JSON should return false")
	}
}

func TestPriorHasConfigType_WithTypReturnsTrue(t *testing.T) {
	if !priorHasConfigType(types.StringValue(`{"type":"HTTP","url":"x"}`)) {
		t.Error("JSON with 'type' key should return true")
	}
}

func TestPriorHasConfigType_WithoutTypeReturnsFalse(t *testing.T) {
	if priorHasConfigType(types.StringValue(`{"url":"x"}`)) {
		t.Error("JSON without 'type' key should return false")
	}
}

// ── injectConfigType: edge cases ────────────────────────────────────────

func TestInjectConfigType_EmptyRawReturnsEmpty(t *testing.T) {
	if got := injectConfigType("", "HTTP"); got != "" {
		t.Errorf("empty raw should return empty, got %q", got)
	}
}

func TestInjectConfigType_EmptyTypeReturnsRaw(t *testing.T) {
	raw := `{"url":"x"}`
	if got := injectConfigType(raw, ""); got != raw {
		t.Errorf("empty type should return raw unchanged, got %q", got)
	}
}

func TestInjectConfigType_InvalidJSONReturnsRaw(t *testing.T) {
	raw := "not json"
	if got := injectConfigType(raw, "HTTP"); got != raw {
		t.Errorf("invalid JSON should return raw unchanged, got %q", got)
	}
}

func TestInjectConfigType_InjectsType(t *testing.T) {
	got := injectConfigType(`{"url":"x"}`, "TCP")
	if !strings.Contains(got, `"type":"TCP"`) {
		t.Errorf("should contain injected type, got %s", got)
	}
	if !strings.Contains(got, `"url":"x"`) {
		t.Errorf("should preserve existing fields, got %s", got)
	}
}

// ── unionHasData: edge cases ───────────────────────────────────────────

func TestUnionHasData_NilReturnsFalse(t *testing.T) {
	if unionHasData(nil) {
		t.Error("nil should return false")
	}
}

func TestUnionHasData_EmptyReturnsFalse(t *testing.T) {
	if unionHasData([]byte{}) {
		t.Error("empty should return false")
	}
}

func TestUnionHasData_NullLiteralReturnsFalse(t *testing.T) {
	if unionHasData([]byte("null")) {
		t.Error("null literal should return false")
	}
}

func TestUnionHasData_EmptyObjectReturnsFalse(t *testing.T) {
	if unionHasData([]byte("{}")) {
		t.Error("empty object should return false")
	}
}

func TestUnionHasData_PopulatedReturnsTrue(t *testing.T) {
	if !unionHasData([]byte(`{"type":"bearer"}`)) {
		t.Error("populated object should return true")
	}
}

// ── Enum Valid() apply-time checks ──────────────────────────────────────

func TestMonitor_BuildCreate_InvalidMonitorType(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}
	plan := &MonitorResourceModel{
		Name:           types.StringValue("neg"),
		Type:           types.StringValue("INVALID_TYPE"),
		Config:         types.StringValue(`{"url":"https://example.com"}`),
		Assertions:     types.ListNull(assertionObjectType()),
		IncidentPolicy: types.ObjectNull(incidentPolicyObjectType().AttrTypes),
	}
	_, err := r.buildCreateRequest(ctx, plan)
	if err == nil {
		t.Fatal("expected error for invalid monitor type")
	}
	if !strings.Contains(err.Error(), "INVALID_TYPE") {
		t.Errorf("error should mention the invalid type, got: %s", err.Error())
	}
}

func TestMonitor_BuildIncidentPolicy_InvalidTriggerRuleType(t *testing.T) {
	ctx := context.Background()
	ruleModel := triggerRuleModel{
		Type:     types.StringValue("invalid_rule_type"),
		Severity: types.StringValue("down"),
		Count:    types.Int64Value(3),
	}
	rulesList, diags := types.ListValueFrom(ctx, triggerRuleObjectType(), []triggerRuleModel{ruleModel})
	if diags.HasError() {
		t.Fatalf("ListValueFrom diagnostics: %v", diags)
	}
	policyModel := incidentPolicyModel{
		ConfirmationType: types.StringValue("multi_region"),
		TriggerRules:     rulesList,
	}
	policyObj, diags := types.ObjectValueFrom(ctx, incidentPolicyObjectType().AttrTypes, policyModel)
	if diags.HasError() {
		t.Fatalf("ObjectValueFrom diagnostics: %v", diags)
	}

	_, err := buildIncidentPolicy(ctx, policyObj)
	if err == nil {
		t.Fatal("expected error for invalid trigger rule type")
	}
	if !strings.Contains(err.Error(), "invalid_rule_type") {
		t.Errorf("error should mention the invalid type, got: %s", err.Error())
	}
}

func TestMonitor_BuildIncidentPolicy_InvalidTriggerRuleSeverity(t *testing.T) {
	ctx := context.Background()
	ruleModel := triggerRuleModel{
		Type:     types.StringValue("consecutive_failures"),
		Severity: types.StringValue("critical"),
		Count:    types.Int64Value(3),
	}
	rulesList, diags := types.ListValueFrom(ctx, triggerRuleObjectType(), []triggerRuleModel{ruleModel})
	if diags.HasError() {
		t.Fatalf("ListValueFrom diagnostics: %v", diags)
	}
	policyModel := incidentPolicyModel{
		ConfirmationType: types.StringValue("multi_region"),
		TriggerRules:     rulesList,
	}
	policyObj, diags := types.ObjectValueFrom(ctx, incidentPolicyObjectType().AttrTypes, policyModel)
	if diags.HasError() {
		t.Fatalf("ObjectValueFrom diagnostics: %v", diags)
	}

	_, err := buildIncidentPolicy(ctx, policyObj)
	if err == nil {
		t.Fatal("expected error for invalid trigger rule severity")
	}
	if !strings.Contains(err.Error(), "critical") {
		t.Errorf("error should mention the invalid severity, got: %s", err.Error())
	}
}
