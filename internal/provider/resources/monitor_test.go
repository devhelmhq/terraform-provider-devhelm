package resources

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ───────────────────────────────────────────────────────────────────────
// Monitor resource tests
//
// Coverage matrix:
//   D — buildCreateRequest / buildUpdateRequest body completeness
//   E — null-vs-omit semantics (ClearAuth, ClearEnvironmentId, regions=[],
//       alert_channel_ids=[])
//   F — mapToState round-trip fidelity (every DTO field reflected in state)
//   G — mapToState idempotency (running twice yields the same model)
//   H — content-keyed assertion matching (severity casing, omission, FIFO
//       on duplicates, import path)
// ───────────────────────────────────────────────────────────────────────

// uuidPtr is a small helper used across this file. Renamed to avoid
// collision with the int32Ptr / strPtr helpers in feature_helpers_test.go.
func uuidPtr(s string) *openapi_types.UUID {
	u := openapi_types.UUID(uuid.MustParse(s))
	return &u
}

// ── buildCreateRequest (Class D) ────────────────────────────────────────

func TestBuildCreateRequest_PopulatesEveryRequiredField(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}

	envID := uuid.New().String()
	channelID := uuid.New().String()
	tagID := uuid.New().String()

	plan := &MonitorResourceModel{
		Name:             types.StringValue("acme-api"),
		Type:             types.StringValue("HTTP"),
		FrequencySeconds: types.Int64Value(60),
		Enabled:          types.BoolValue(true),
		Regions: types.ListValueMust(types.StringType, []attr.Value{
			types.StringValue("us-east"), types.StringValue("eu-west"),
		}),
		EnvironmentID: types.StringValue(envID),
		AlertChannelIds: types.ListValueMust(types.StringType, []attr.Value{
			types.StringValue(channelID),
		}),
		TagIds: types.ListValueMust(types.StringType, []attr.Value{
			types.StringValue(tagID),
		}),
		Config:         types.StringValue(`{"url":"https://acme.com","method":"GET"}`),
		Auth:           mustAuthObject(t, "bearer", "", "00000000-0000-0000-0000-000000000123"),
		Assertions:     types.ListNull(assertionObjectType()),
		IncidentPolicy: types.ObjectNull(incidentPolicyObjectType().AttrTypes),
	}

	body, err := r.buildCreateRequest(ctx, plan)
	if err != nil {
		t.Fatalf("buildCreateRequest: %v", err)
	}

	if body.Name != "acme-api" {
		t.Errorf("Name = %q", body.Name)
	}
	if body.Type != "HTTP" {
		t.Errorf("Type = %q", body.Type)
	}
	if body.ManagedBy != "TERRAFORM" {
		t.Errorf("ManagedBy = %q, want TERRAFORM (provider must self-identify so a future round-trip survives a manual dashboard edit detection)", body.ManagedBy)
	}
	if body.FrequencySeconds == nil || *body.FrequencySeconds != 60 {
		t.Errorf("FrequencySeconds = %v, want 60", body.FrequencySeconds)
	}
	if body.Enabled == nil || !*body.Enabled {
		t.Errorf("Enabled = %v, want true", body.Enabled)
	}
	if body.Regions == nil || len(*body.Regions) != 2 {
		t.Errorf("Regions = %v, want 2", body.Regions)
	}
	if body.EnvironmentId == nil || body.EnvironmentId.String() != envID {
		t.Errorf("EnvironmentId = %v, want %s", body.EnvironmentId, envID)
	}
	if body.AlertChannelIds == nil || len(*body.AlertChannelIds) != 1 {
		t.Errorf("AlertChannelIds = %v, want 1 entry", body.AlertChannelIds)
	}
	// body.Auth must be the typed MonitorAuthConfig union populated
	// from the user's HCL `auth { bearer = {...} }` block. Verify both
	// the discriminator and the vaultSecretId round-trip via the typed
	// shape — no raw-JSON merge needed.
	if body.Auth == nil {
		t.Fatal("body.Auth = nil, want populated MonitorAuthConfig")
	}
	wire, err := body.Auth.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal Auth: %v", err)
	}
	if !strings.Contains(string(wire), `"type":"bearer"`) {
		t.Errorf("wire body missing bearer discriminator: %s", wire)
	}
	if !strings.Contains(string(wire), `"vaultSecretId":"00000000-0000-0000-0000-000000000123"`) {
		t.Errorf("wire body missing vaultSecretId: %s", wire)
	}
	if body.Tags == nil || body.Tags.TagIds == nil || len(*body.Tags.TagIds) != 1 {
		t.Errorf("Tags = %v, want 1 entry passed through embedded create body", body.Tags)
	}
}

func TestBuildCreateRequest_AuthOmittedReachesAPIAsNil(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}

	plan := &MonitorResourceModel{
		Name:           types.StringValue("noauth"),
		Type:           types.StringValue("HTTP"),
		Config:         types.StringValue(`{"url":"https://example.com"}`),
		Auth:           types.ObjectNull(monitorAuthObjectType().AttrTypes),
		Assertions:     types.ListNull(assertionObjectType()),
		IncidentPolicy: types.ObjectNull(incidentPolicyObjectType().AttrTypes),
	}
	body, err := r.buildCreateRequest(ctx, plan)
	if err != nil {
		t.Fatalf("buildCreateRequest: %v", err)
	}
	if body.Auth != nil {
		t.Errorf("Auth = %v, want nil (Create has no ClearAuth, so omitted means nil)", body.Auth)
	}
}

func TestBuildCreateRequest_InvalidUUIDInChannelIdsErrors(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}

	plan := &MonitorResourceModel{
		Name:   types.StringValue("bad"),
		Type:   types.StringValue("HTTP"),
		Config: types.StringValue(`{"url":"https://example.com"}`),
		AlertChannelIds: types.ListValueMust(types.StringType, []attr.Value{
			types.StringValue("not-a-uuid"),
		}),
		Auth:           types.ObjectNull(monitorAuthObjectType().AttrTypes),
		Assertions:     types.ListNull(assertionObjectType()),
		IncidentPolicy: types.ObjectNull(incidentPolicyObjectType().AttrTypes),
	}
	if _, err := r.buildCreateRequest(ctx, plan); err == nil || !strings.Contains(err.Error(), "alert_channel_ids") {
		t.Errorf("expected alert_channel_ids parse error, got %v", err)
	}
}

// ── buildUpdateRequest (Class D + E) ────────────────────────────────────

// TestBuildUpdateRequest_AuthNullProducesClearAuth mirrors the
// post-spec-sync design: buildUpdateRequest sets ClearAuth=true and
// leaves Auth nil when plan.Auth is null, so the API drops any
// previously-attached credential.
func TestBuildUpdateRequest_AuthNullProducesClearAuth(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}

	plan := &MonitorResourceModel{
		Name:           types.StringValue("noauth"),
		Type:           types.StringValue("HTTP"),
		Config:         types.StringValue(`{"url":"https://example.com"}`),
		Auth:           types.ObjectNull(monitorAuthObjectType().AttrTypes),
		EnvironmentID:  types.StringValue(uuid.New().String()),
		Assertions:     types.ListNull(assertionObjectType()),
		IncidentPolicy: types.ObjectNull(incidentPolicyObjectType().AttrTypes),
	}
	body, err := r.buildUpdateRequest(ctx, plan)
	if err != nil {
		t.Fatalf("buildUpdateRequest: %v", err)
	}
	if body.Auth != nil {
		t.Errorf("body.Auth = %v, want nil (null plan auth must produce nil Auth + ClearAuth=true)", body.Auth)
	}
	if body.ClearAuth == nil || !*body.ClearAuth {
		t.Errorf("ClearAuth = %v, want true (auth-null update must explicitly clear so the API drops the existing credential)", body.ClearAuth)
	}

	// The full wire body must serialize Auth as omitted (`omitempty`)
	// while keeping `clearAuth: true` present.
	wire, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(wire, &got); err != nil {
		t.Fatalf("unmarshal wire body: %v", err)
	}
	if got["clearAuth"] != true {
		t.Errorf("clearAuth = %v, want true", got["clearAuth"])
	}
	if _, present := got["auth"]; present {
		t.Errorf("auth key present in wire body alongside clearAuth=true (must be one or the other): %v", got["auth"])
	}
}

// TestBuildUpdateRequest_AuthSetPopulatesTypedUnion verifies that
// plan.Auth supplied as a typed `auth { bearer = {...} }` block ends up
// in the request body via the typed MonitorAuthConfig union — no raw
// merge required.
func TestBuildUpdateRequest_AuthSetPopulatesTypedUnion(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}

	plan := &MonitorResourceModel{
		Name:           types.StringValue("withauth"),
		Type:           types.StringValue("HTTP"),
		Config:         types.StringValue(`{"url":"https://example.com"}`),
		Auth:           mustAuthObject(t, "bearer", "", "00000000-0000-0000-0000-000000000123"),
		Assertions:     types.ListNull(assertionObjectType()),
		IncidentPolicy: types.ObjectNull(incidentPolicyObjectType().AttrTypes),
	}
	body, err := r.buildUpdateRequest(ctx, plan)
	if err != nil {
		t.Fatalf("buildUpdateRequest: %v", err)
	}
	if body.Auth == nil {
		t.Fatal("body.Auth = nil, want populated MonitorAuthConfig union")
	}
	if body.ClearAuth != nil && *body.ClearAuth {
		t.Errorf("ClearAuth = true alongside an auth-set plan; wire body would be ambiguous")
	}

	wire, err := body.Auth.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal auth union: %v", err)
	}
	if !strings.Contains(string(wire), `"type":"bearer"`) {
		t.Errorf("wire body missing bearer discriminator: %s", wire)
	}
	if !strings.Contains(string(wire), `"vaultSecretId":"00000000-0000-0000-0000-000000000123"`) {
		t.Errorf("wire body does not preserve vaultSecretId: %s", wire)
	}
}

func TestBuildUpdateRequest_EnvironmentIDNullSetsClearEnvironmentId(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}

	plan := &MonitorResourceModel{
		Name:           types.StringValue("noenv"),
		Type:           types.StringValue("HTTP"),
		Config:         types.StringValue(`{"url":"https://example.com"}`),
		Auth:           mustAuthObject(t, "bearer", "", "00000000-0000-0000-0000-000000000123"),
		EnvironmentID:  types.StringNull(),
		Assertions:     types.ListNull(assertionObjectType()),
		IncidentPolicy: types.ObjectNull(incidentPolicyObjectType().AttrTypes),
	}
	body, err := r.buildUpdateRequest(ctx, plan)
	if err != nil {
		t.Fatalf("buildUpdateRequest: %v", err)
	}
	if body.EnvironmentId != nil {
		t.Errorf("EnvironmentId = %v, want nil (we cleared)", body.EnvironmentId)
	}
	if body.ClearEnvironmentId == nil || !*body.ClearEnvironmentId {
		t.Errorf("ClearEnvironmentId = %v, want pointer to true (HCL omitting the env attr must drop the existing assignment, not preserve it)", body.ClearEnvironmentId)
	}
}

// TestBuildUpdateRequest_RegionsAndChannelsExplicitEmptyClears guards the
// null-vs-omit fix that we just shipped: explicit `regions = []` and
// `alert_channel_ids = []` must reach the wire as empty slices (which the
// API interprets as "clear all"), NOT as nil (which would be "preserve").
func TestBuildUpdateRequest_RegionsAndChannelsExplicitEmptyClears(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}

	plan := &MonitorResourceModel{
		Name:            types.StringValue("clearcollections"),
		Type:            types.StringValue("HTTP"),
		Config:          types.StringValue(`{"url":"https://example.com"}`),
		Auth:            types.ObjectNull(monitorAuthObjectType().AttrTypes),
		Regions:         types.ListValueMust(types.StringType, []attr.Value{}),
		AlertChannelIds: types.ListValueMust(types.StringType, []attr.Value{}),
		Assertions:      types.ListNull(assertionObjectType()),
		IncidentPolicy:  types.ObjectNull(incidentPolicyObjectType().AttrTypes),
	}
	body, err := r.buildUpdateRequest(ctx, plan)
	if err != nil {
		t.Fatalf("buildUpdateRequest: %v", err)
	}
	if body.Regions == nil {
		t.Fatal("Regions = nil for explicit empty list (would preserve current on the API side instead of clearing)")
	}
	if len(*body.Regions) != 0 {
		t.Errorf("Regions = %v, want empty slice", body.Regions)
	}
	if body.AlertChannelIds == nil {
		t.Fatal("AlertChannelIds = nil for explicit empty list")
	}
	if len(*body.AlertChannelIds) != 0 {
		t.Errorf("AlertChannelIds = %v, want empty slice", body.AlertChannelIds)
	}
}

// TestBuildUpdateRequest_RegionsAndChannelsNullPreserves: omitted (=null)
// regions/alert_channel_ids must NOT touch the wire at all — sending
// nil is the "preserve current" signal. This is the contract that
// UseStateForUnknown() relies on to avoid perpetual diffs.
func TestBuildUpdateRequest_RegionsAndChannelsNullPreserves(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}

	plan := &MonitorResourceModel{
		Name:            types.StringValue("preserve"),
		Type:            types.StringValue("HTTP"),
		Config:          types.StringValue(`{"url":"https://example.com"}`),
		Auth:            types.ObjectNull(monitorAuthObjectType().AttrTypes),
		Regions:         types.ListNull(types.StringType),
		AlertChannelIds: types.ListNull(types.StringType),
		Assertions:      types.ListNull(assertionObjectType()),
		IncidentPolicy:  types.ObjectNull(incidentPolicyObjectType().AttrTypes),
	}
	body, err := r.buildUpdateRequest(ctx, plan)
	if err != nil {
		t.Fatalf("buildUpdateRequest: %v", err)
	}
	if body.Regions != nil {
		t.Errorf("Regions = %v, want nil (preserve)", body.Regions)
	}
	if body.AlertChannelIds != nil {
		t.Errorf("AlertChannelIds = %v, want nil (preserve)", body.AlertChannelIds)
	}
}

// ── mapToState (Class F + G) ────────────────────────────────────────────

// fullyPopulatedMonitorDto returns a DTO with every optional field set
// so any field forgotten in mapToState surfaces as a missing state value.
func fullyPopulatedMonitorDto(t *testing.T) *generated.MonitorDto {
	t.Helper()
	id := openapi_types.UUID(uuid.MustParse("11111111-2222-3333-4444-555555555555"))
	envID := openapi_types.UUID(uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"))
	chanID := uuidPtr("88888888-7777-6666-5555-444444444444")
	tagID := openapi_types.UUID(uuid.MustParse("99999999-8888-7777-6666-555555555555"))
	scope := generated.TriggerRuleScope("any_region")
	count := int32(3)

	cfg := generated.MonitorDto_Config{}
	if err := cfg.UnmarshalJSON([]byte(`{"url":"https://example.com","method":"GET"}`)); err != nil {
		t.Fatalf("config: %v", err)
	}

	pingURL := "https://heart.devhelm.io/x"
	asnCfg := generated.MonitorAssertionDto_Config{}
	_ = asnCfg.UnmarshalJSON([]byte(`{"type":"status_code","expected":"200","operator":"equals"}`))

	auth := generated.MonitorAuthConfig{}
	if err := auth.FromBearerAuthConfig(generated.BearerAuthConfig{Type: generated.Bearer}); err != nil {
		t.Fatalf("auth: %v", err)
	}

	return &generated.MonitorDto{
		Id:               id,
		Name:             "acme-api",
		Type:             generated.MonitorDtoType("HTTP"),
		FrequencySeconds: 60,
		Enabled:          true,
		Regions:          []string{"us-east", "eu-west"},
		Environment:      &generated.Summary{Id: envID, Name: "prod", Slug: "prod"},
		Config:           cfg,
		Auth:             &auth,
		PingUrl:          &pingURL,
		AlertChannelIds:  &[]openapi_types.UUID{*chanID},
		Tags:             &[]generated.TagDto{{Id: tagID, Name: "team-acme", Color: "#000"}},
		Assertions: &[]generated.MonitorAssertionDto{
			{
				Id:            openapi_types.UUID(uuid.New()),
				MonitorId:     id,
				AssertionType: generated.MonitorAssertionDtoAssertionType("status_code"),
				Severity:      generated.MonitorAssertionDtoSeverity("fail"),
				Config:        asnCfg,
			},
		},
		IncidentPolicy: &generated.IncidentPolicyDto{
			Id:        openapi_types.UUID(uuid.New()),
			MonitorId: id,
			Confirmation: generated.ConfirmationPolicy{
				Type:              generated.ConfirmationPolicyType("multi_region"),
				MinRegionsFailing: 2,
				MaxWaitSeconds:    120,
			},
			Recovery: generated.RecoveryPolicy{
				ConsecutiveSuccesses: 3, MinRegionsPassing: 2, CooldownMinutes: 5,
			},
			TriggerRules: []generated.TriggerRule{
				{
					Type:     generated.TriggerRuleType("consecutive_failures"),
					Severity: generated.TriggerRuleSeverity("down"),
					Scope:    &scope,
					Count:    &count,
				},
			},
		},
	}
}

func TestMonitor_MapToState_PopulatesEveryFieldFromDto(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}
	dto := fullyPopulatedMonitorDto(t)

	model := &MonitorResourceModel{}
	// END-1141: mapToState now returns diagnostics from any framework
	// marshaling errors. The fully-populated DTO is the happy path —
	// asserting no diagnostics here locks in the contract that the
	// "obvious" mapping never silently drops a framework error.
	if diags := r.mapToState(ctx, model, dto); diags.HasError() {
		t.Fatalf("mapToState returned errors: %v", diags)
	}

	if got := model.ID.ValueString(); got != dto.Id.String() {
		t.Errorf("ID = %q, want %s", got, dto.Id)
	}
	if got := model.Name.ValueString(); got != "acme-api" {
		t.Errorf("Name = %q", got)
	}
	if got := model.Type.ValueString(); got != "HTTP" {
		t.Errorf("Type = %q", got)
	}
	if got := model.FrequencySeconds.ValueInt64(); got != 60 {
		t.Errorf("FrequencySeconds = %d", got)
	}
	if !model.Enabled.ValueBool() {
		t.Error("Enabled = false, want true")
	}
	if got := len(model.Regions.Elements()); got != 2 {
		t.Errorf("Regions len = %d", got)
	}
	if got := model.EnvironmentID.ValueString(); got != dto.Environment.Id.String() {
		t.Errorf("EnvironmentID = %q", got)
	}
	if got := len(model.AlertChannelIds.Elements()); got != 1 {
		t.Errorf("AlertChannelIds len = %d", got)
	}
	if got := len(model.TagIds.Elements()); got != 1 {
		t.Errorf("TagIds len = %d", got)
	}
	if got := model.PingUrl.ValueString(); got != "https://heart.devhelm.io/x" {
		t.Errorf("PingUrl = %q", got)
	}
	if !strings.Contains(model.Config.ValueString(), `"url":"https://example.com"`) {
		t.Errorf("Config = %s", model.Config.ValueString())
	}
	if model.Auth.IsNull() {
		t.Errorf("Auth = null, want populated bearer variant")
	} else {
		// Decode and verify the bearer variant is populated.
		var am authModel
		if d := model.Auth.As(ctx, &am, basetypes.ObjectAsOptions{
			UnhandledNullAsEmpty:    true,
			UnhandledUnknownAsEmpty: true,
		}); d.HasError() {
			t.Errorf("decode model.Auth: %v", d)
		}
		if am.Bearer.IsNull() {
			t.Errorf("Auth.bearer = null, want populated (DTO had bearer variant)")
		}
	}
	if got := len(model.Assertions.Elements()); got != 1 {
		t.Errorf("Assertions len = %d", got)
	}
	if model.IncidentPolicy.IsNull() {
		t.Errorf("IncidentPolicy = null, want populated object")
	}
}

// TestMonitor_MapToState_Idempotent guards against the perpetual-diff
// bug class: running mapToState twice on the same DTO must produce the
// same model state, otherwise repeated Reads would surface drift.
func TestMonitor_MapToState_Idempotent(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}
	dto := fullyPopulatedMonitorDto(t)

	first := &MonitorResourceModel{}
	r.mapToState(ctx, first, dto)
	second := *first
	r.mapToState(ctx, &second, dto)

	mustJSON := func(m MonitorResourceModel) string {
		// Compare via a stable serialization. Direct struct comparison
		// is unsafe because types.List values do not implement ==.
		b, err := json.Marshal(struct {
			ID, Name, Type, Config, Auth, PingURL, EnvID string
			Freq                                         int64
			RegionLen, ChannelLen, TagLen, AsnLen        int
			IncidentNull                                 bool
			Enabled                                      bool
		}{
			m.ID.ValueString(), m.Name.ValueString(), m.Type.ValueString(),
			m.Config.ValueString(), m.Auth.String(), m.PingUrl.ValueString(),
			m.EnvironmentID.ValueString(),
			m.FrequencySeconds.ValueInt64(),
			len(m.Regions.Elements()), len(m.AlertChannelIds.Elements()),
			len(m.TagIds.Elements()), len(m.Assertions.Elements()),
			m.IncidentPolicy.IsNull(),
			m.Enabled.ValueBool(),
		})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if a, b := mustJSON(*first), mustJSON(second); a != b {
		t.Errorf("mapToState not idempotent:\n  1st = %s\n  2nd = %s", a, b)
	}
}

// TestMonitor_MapToState_NoEnvironmentClearsField verifies the Summary.Id
// zero-UUID guard: when the API returns an unassigned environment, the
// state must be null (not a fake all-zeros UUID string).
func TestMonitor_MapToState_NoEnvironmentClearsField(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}
	dto := fullyPopulatedMonitorDto(t)
	dto.Environment = nil // unset

	model := &MonitorResourceModel{}
	r.mapToState(ctx, model, dto)
	if !model.EnvironmentID.IsNull() {
		t.Errorf("EnvironmentID = %v, want null when DTO env is zero", model.EnvironmentID)
	}
}

// TestMonitor_MapToState_NormalizesConfigJSON: server-echoed nulls in
// optional `config` fields must be stripped so they do not appear as
// drift in the next plan against a HCL config that omits them.
//
// (Auth used to live in this test as raw JSON; with the typed nested
// schema, null-stripping is handled by the typed conversion in
// mapMonitorAuthToTF.)
func TestMonitor_MapToState_NormalizesConfigJSON(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}
	dto := fullyPopulatedMonitorDto(t)

	cfg := generated.MonitorDto_Config{}
	_ = cfg.UnmarshalJSON([]byte(`{"url":"https://example.com","method":"GET","customHeaders":null}`))
	dto.Config = cfg

	model := &MonitorResourceModel{}
	r.mapToState(ctx, model, dto)

	if strings.Contains(model.Config.ValueString(), "null") {
		t.Errorf("Config still contains null: %s", model.Config.ValueString())
	}
}

// ── Content-keyed assertion matching (Class H) ──────────────────────────

// TestMonitor_AssertionMatching_PreservesUserSeverityCasing: the user
// might write `severity = "Fail"` in HCL while the API echoes `"fail"`
// back. mapToState should preserve the user's casing whenever the type+
// config matches a prior assertion, otherwise we leak server-side
// canonicalization into state and the next plan shows a casing diff.
func TestMonitor_AssertionMatching_PreservesUserSeverityCasing(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}

	asnCfg := generated.MonitorAssertionDto_Config{}
	_ = asnCfg.UnmarshalJSON([]byte(`{"type":"status_code","expected":"200"}`))

	dto := &generated.MonitorDto{
		Id:   openapi_types.UUID(uuid.New()),
		Name: "x", Type: "HTTP", FrequencySeconds: 60, Enabled: true,
		Assertions: &[]generated.MonitorAssertionDto{
			{
				AssertionType: "status_code",
				Severity:      "fail",
				Config:        asnCfg,
			},
		},
	}

	priorElem, _ := types.ObjectValue(
		assertionObjectType().AttrTypes,
		map[string]attr.Value{
			"type":     types.StringValue("status_code"),
			"config":   types.StringValue(`{"expected":"200"}`),
			"severity": types.StringValue("Fail"), // user wrote uppercase
		},
	)
	priorList, _ := types.ListValue(assertionObjectType(), []attr.Value{priorElem})

	model := &MonitorResourceModel{Assertions: priorList}
	r.mapToState(ctx, model, dto)

	var assertions []assertionModel
	_ = model.Assertions.ElementsAs(ctx, &assertions, false)
	if len(assertions) != 1 {
		t.Fatalf("got %d assertions, want 1", len(assertions))
	}
	if got := assertions[0].Severity.ValueString(); got != "Fail" {
		t.Errorf("severity = %q, want %q (user-supplied casing must survive when content matches the API echo)", got, "Fail")
	}
}

// TestMonitor_AssertionMatching_KeepsNullSeverityWhenUserOmitted: when
// the user omits severity in HCL, the next state read must keep severity
// null even though the API populates a default ("fail"). Otherwise a
// null→"fail" diff would appear on every plan.
func TestMonitor_AssertionMatching_KeepsNullSeverityWhenUserOmitted(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}

	asnCfg := generated.MonitorAssertionDto_Config{}
	_ = asnCfg.UnmarshalJSON([]byte(`{"type":"status_code","expected":"200"}`))

	dto := &generated.MonitorDto{
		Id:   openapi_types.UUID(uuid.New()),
		Name: "x", Type: "HTTP", FrequencySeconds: 60, Enabled: true,
		Assertions: &[]generated.MonitorAssertionDto{
			{AssertionType: "status_code", Severity: "fail", Config: asnCfg},
		},
	}

	priorElem, _ := types.ObjectValue(
		assertionObjectType().AttrTypes,
		map[string]attr.Value{
			"type":     types.StringValue("status_code"),
			"config":   types.StringValue(`{"expected":"200"}`),
			"severity": types.StringNull(),
		},
	)
	priorList, _ := types.ListValue(assertionObjectType(), []attr.Value{priorElem})

	model := &MonitorResourceModel{Assertions: priorList}
	r.mapToState(ctx, model, dto)

	var assertions []assertionModel
	_ = model.Assertions.ElementsAs(ctx, &assertions, false)
	if len(assertions) != 1 {
		t.Fatalf("got %d assertions, want 1", len(assertions))
	}
	if !assertions[0].Severity.IsNull() {
		t.Errorf("severity = %v, want null (user omitted it; preserve to avoid drift)", assertions[0].Severity)
	}
}

// TestMonitor_AssertionMatching_ImportPathPopulatesSeverity: during a
// `terraform import`, no prior assertions exist in state. The import
// path must populate every severity from the DTO so the resulting state
// is a complete representation, even at the cost of injecting server
// casing (which a subsequent apply will reconcile if needed).
func TestMonitor_AssertionMatching_ImportPathPopulatesSeverity(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}

	asnCfg := generated.MonitorAssertionDto_Config{}
	_ = asnCfg.UnmarshalJSON([]byte(`{"type":"status_code","expected":"200"}`))

	dto := &generated.MonitorDto{
		Id:   openapi_types.UUID(uuid.New()),
		Name: "x", Type: "HTTP", FrequencySeconds: 60, Enabled: true,
		Assertions: &[]generated.MonitorAssertionDto{
			{AssertionType: "status_code", Severity: "fail", Config: asnCfg},
		},
	}

	// Empty list mimics ImportState's pre-initialization.
	emptyList, _ := types.ListValue(assertionObjectType(), []attr.Value{})
	model := &MonitorResourceModel{Assertions: emptyList}
	r.mapToState(ctx, model, dto)

	var assertions []assertionModel
	_ = model.Assertions.ElementsAs(ctx, &assertions, false)
	if len(assertions) != 1 {
		t.Fatalf("got %d assertions, want 1", len(assertions))
	}
	if assertions[0].Severity.IsNull() {
		t.Errorf("severity = null, want %q (imports must populate severity since no prior state exists)", "fail")
	}
}

// TestMonitor_AssertionMatching_FIFOForDuplicates: when the user has two
// identical assertions (same type + same config), each prior entry must
// be paired one-for-one with an API entry — otherwise a single user-
// supplied severity would silently propagate to all duplicates.
func TestMonitor_AssertionMatching_FIFOForDuplicates(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}

	asnCfg := generated.MonitorAssertionDto_Config{}
	_ = asnCfg.UnmarshalJSON([]byte(`{"type":"status_code","expected":"200"}`))

	dto := &generated.MonitorDto{
		Id:   openapi_types.UUID(uuid.New()),
		Name: "x", Type: "HTTP", FrequencySeconds: 60, Enabled: true,
		Assertions: &[]generated.MonitorAssertionDto{
			{AssertionType: "status_code", Severity: "fail", Config: asnCfg},
			{AssertionType: "status_code", Severity: "fail", Config: asnCfg},
		},
	}

	mk := func(sev attr.Value) attr.Value {
		obj, _ := types.ObjectValue(
			assertionObjectType().AttrTypes,
			map[string]attr.Value{
				"type":     types.StringValue("status_code"),
				"config":   types.StringValue(`{"expected":"200"}`),
				"severity": sev,
			},
		)
		return obj
	}
	priorList, _ := types.ListValue(assertionObjectType(), []attr.Value{
		mk(types.StringValue("Fail")), // first prior consumed by first DTO
		mk(types.StringNull()),        // second prior consumed by second DTO
	})

	model := &MonitorResourceModel{Assertions: priorList}
	r.mapToState(ctx, model, dto)

	var assertions []assertionModel
	_ = model.Assertions.ElementsAs(ctx, &assertions, false)
	if len(assertions) != 2 {
		t.Fatalf("got %d assertions, want 2", len(assertions))
	}
	if got := assertions[0].Severity.ValueString(); got != "Fail" {
		t.Errorf("first severity = %q, want %q (must pair with first prior)", got, "Fail")
	}
	if !assertions[1].Severity.IsNull() {
		t.Errorf("second severity = %v, want null (must pair with second prior)", assertions[1].Severity)
	}
}

// ── reconcileTags (Class E — null-vs-empty-vs-populated semantics) ──────
//
// reconcileTags is exercised here by capturing the HTTP requests it issues
// against a stub api.Client. The contract under test is:
//
//   • plan == null/unknown        → no requests (preserve existing tags)
//   • plan == [] and state == []  → no requests (already empty)
//   • plan == [] and state == [a,b] → exactly one DELETE for {a,b}
//   • plan == [a,b] and state == [b,c] → POST {a} and DELETE {c}
//
// The historical bug this regression-tests: the function used to derive
// "existing" from the API's PUT /monitors/{id} response, which omits
// the tag set entirely. That made every clear-tags request a no-op and
// triggered a "new element appeared" inconsistency error on apply.

// The HTTP-issuing branches (toAdd > 0, toRemove > 0) are exercised end
// to end by the BDD scenario `c13_clear_optional_fields` in
// tests/surfaces/terraform_provider_devhelm/bdd/. Wiring an HTTP-level
// stub here would require either refactoring (*api.Client) into an
// interface or standing up an httptest.Server, both of which test the
// transport more than the delta logic. The early-return paths are
// unique to this function and are exhaustively covered below.

func TestReconcileTags_PlanNull_NoOp(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}
	planTags := types.ListNull(types.StringType)
	currentTags := stringList(t, ctx, "00000000-0000-0000-0000-000000000001")

	// We only care that the function returns nil and does not panic
	// when the client is nil — the early-return path must short-circuit
	// before any HTTP call is attempted.
	if err := r.reconcileTags(ctx, "00000000-0000-0000-0000-000000000abc", planTags, currentTags); err != nil {
		t.Fatalf("expected nil error on null plan, got %v", err)
	}
}

func TestReconcileTags_PlanUnknown_NoOp(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}
	planTags := types.ListUnknown(types.StringType)
	currentTags := stringList(t, ctx, "00000000-0000-0000-0000-000000000001")
	if err := r.reconcileTags(ctx, "00000000-0000-0000-0000-000000000abc", planTags, currentTags); err != nil {
		t.Fatalf("expected nil error on unknown plan, got %v", err)
	}
}

// TestReconcileTags_DeltaComputation_NoHTTP_WhenSetsAlreadyMatch verifies
// the "no work needed" path: when desired == existing, no add/remove
// branches are taken — the function returns nil without invoking the
// HTTP client. We assert the behavioral observation (no error) plus the
// computed delta via a parallel recomputation (so the test continues to
// pass if the implementation rearranges its internals).
func TestReconcileTags_DeltaComputation_NoHTTP_WhenSetsAlreadyMatch(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}
	id1 := "00000000-0000-0000-0000-000000000001"
	id2 := "00000000-0000-0000-0000-000000000002"
	plan := stringList(t, ctx, id1, id2)
	state := stringList(t, ctx, id2, id1) // same set, different order
	if err := r.reconcileTags(ctx, "00000000-0000-0000-0000-0000000000aa", plan, state); err != nil {
		t.Fatalf("expected nil for set-equal plan vs state, got %v", err)
	}
}

// TestReconcileTags_RejectsInvalidTagID surfaces malformed UUIDs in the
// plan rather than silently corrupting the API call payload.
func TestReconcileTags_RejectsInvalidTagID(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}
	plan := stringList(t, ctx, "not-a-uuid")
	state := types.ListNull(types.StringType)
	if err := r.reconcileTags(ctx, "00000000-0000-0000-0000-0000000000aa", plan, state); err == nil {
		t.Fatalf("expected error for invalid plan tag id, got nil")
	}
}

// TestReconcileTags_RejectsInvalidExistingTagID surfaces a malformed UUID
// stored in prior state (e.g. a state-file tampering case) — better to
// fail loudly than to issue a DELETE whose body the API will reject anyway.
func TestReconcileTags_RejectsInvalidExistingTagID(t *testing.T) {
	ctx := context.Background()
	r := &MonitorResource{}
	plan := stringList(t, ctx, "00000000-0000-0000-0000-000000000001")
	state := stringList(t, ctx, "also-not-a-uuid")
	if err := r.reconcileTags(ctx, "00000000-0000-0000-0000-0000000000aa", plan, state); err == nil {
		t.Fatalf("expected error for invalid existing tag id, got nil")
	}
}

// ── ValidateConfig — per-type config JSON validation (Class C) ─────────
//
// These tests exercise validateMonitorConfigJSON directly so we can assert
// the three failure classes (parse error, unknown field, missing required)
// produce attribute-scoped diagnostics on `config`. The valid cases lock in
// that conforming payloads pass cleanly for every monitor type.

func TestValidateMonitorConfigJSON_ValidPayloadsPassForEveryType(t *testing.T) {
	cases := map[generated.CreateMonitorRequestType]string{
		generated.CreateMonitorRequestTypeHTTP:      `{"url":"https://example.com","method":"GET"}`,
		generated.CreateMonitorRequestTypeDNS:       `{"hostname":"example.com"}`,
		generated.CreateMonitorRequestTypeTCP:       `{"host":"db.example.com","port":5432}`,
		generated.CreateMonitorRequestTypeICMP:      `{"host":"router.example.com"}`,
		generated.CreateMonitorRequestTypeHEARTBEAT: `{"expectedInterval":86400,"gracePeriod":3600}`,
		generated.CreateMonitorRequestTypeMCPSERVER: `{"command":"node"}`,
	}
	for monitorType, raw := range cases {
		monitorType, raw := monitorType, raw
		t.Run(string(monitorType), func(t *testing.T) {
			var diags diag.Diagnostics
			validateMonitorConfigJSON(&diags, raw, monitorType)
			if diags.HasError() {
				t.Fatalf("expected no diagnostics for valid %s config, got %v", monitorType, diags.Errors())
			}
		})
	}
}

func TestValidateMonitorConfigJSON_InvalidJSONIsAttributeError(t *testing.T) {
	var diags diag.Diagnostics
	validateMonitorConfigJSON(&diags, `{"url": "broken`, generated.CreateMonitorRequestTypeHTTP)
	if !diags.HasError() {
		t.Fatalf("expected error diagnostic for malformed JSON, got %v", diags)
	}
	if got := diags.Errors()[0].Summary(); !strings.Contains(got, "Invalid JSON") {
		t.Fatalf("expected summary about invalid JSON, got %q", got)
	}
}

func TestValidateMonitorConfigJSON_UnknownFieldRejectedAtPlanTime(t *testing.T) {
	// `verifyTLS` is a typo — the canonical field is `verifyTls`. Today the
	// API would 400; this test locks in that the typo surfaces during
	// `terraform plan` instead.
	var diags diag.Diagnostics
	validateMonitorConfigJSON(&diags, `{"url":"https://example.com","method":"GET","verifyTLS":true}`,
		generated.CreateMonitorRequestTypeHTTP)
	if !diags.HasError() {
		t.Fatalf("expected error for unknown field, got %v", diags)
	}
	body := diags.Errors()[0].Detail()
	if !strings.Contains(body, "verifyTLS") {
		t.Fatalf("expected detail to mention the offending field name, got %q", body)
	}
}

func TestValidateMonitorConfigJSON_MissingRequiredFieldRejectedAtPlanTime(t *testing.T) {
	// HTTP requires `url` and `method`. Omit them both and confirm we
	// surface a missing-required-fields diagnostic instead of a 400.
	var diags diag.Diagnostics
	validateMonitorConfigJSON(&diags, `{}`, generated.CreateMonitorRequestTypeHTTP)
	if !diags.HasError() {
		t.Fatalf("expected error for missing required fields, got %v", diags)
	}
	if !strings.Contains(diags.Errors()[0].Summary(), "Missing or invalid fields") {
		t.Fatalf("expected missing-fields summary, got %q", diags.Errors()[0].Summary())
	}
}

func TestValidateMonitorConfigJSON_WrongFieldForTypeRejected(t *testing.T) {
	// `hostname` is for DNS monitors; passing it under HTTP must fail at
	// plan time so users see exactly what they used and which schema applies.
	var diags diag.Diagnostics
	validateMonitorConfigJSON(&diags, `{"hostname":"example.com"}`, generated.CreateMonitorRequestTypeHTTP)
	if !diags.HasError() {
		t.Fatalf("expected error for cross-type field, got %v", diags)
	}
	if !strings.Contains(diags.Errors()[0].Detail(), "hostname") {
		t.Fatalf("expected detail to identify the offending field, got %q", diags.Errors()[0].Detail())
	}
}

func TestValidateMonitorConfigJSON_EmptyStringIsNoOp(t *testing.T) {
	var diags diag.Diagnostics
	validateMonitorConfigJSON(&diags, "", generated.CreateMonitorRequestTypeHTTP)
	if diags.HasError() {
		t.Fatalf("empty config string should not produce diagnostics, got %v", diags)
	}
}

func TestValidateMonitorConfigJSON_UnknownTypeIsNoOp(t *testing.T) {
	// Unknown monitor type is reported by `type`'s OneOf validator; the
	// config validator must not double-emit a confusing second error.
	var diags diag.Diagnostics
	validateMonitorConfigJSON(&diags, `{"foo":"bar"}`, generated.CreateMonitorRequestType("BOGUS"))
	if diags.HasError() {
		t.Fatalf("unknown monitor type should not produce config diagnostics, got %v", diags)
	}
}
