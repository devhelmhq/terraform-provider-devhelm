package datasources

import (
	"testing"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ───────────────────────────────────────────────────────────────────────
// Datasource map-to-state and lookup tests
//
// These tests cover the two halves of every datasource Read path:
//
//   1. matchByName — the shared name-based lookup helper that all five
//      "lookup by display name" datasources funnel through. We pin its
//      contract here so a regression (e.g. silently returning the first
//      match instead of surfacing ambiguity) breaks at unit-test time
//      instead of only showing up as a flaky surface test.
//
//   2. mapXxxToState — the per-datasource DTO-to-state mapping that was
//      previously inlined inside the Read closure. Lifting it to a free
//      function lets us assert field-by-field round-trip fidelity, which
//      is the single most common source of "data source read drift"
//      bugs (e.g. forgetting to populate Slug after we add a new
//      computed field, or coercing nil pointers to "" instead of null).
//
// Together these get the datasources package to ≈full coverage of the
// non-IO portions of every Read; the remaining lines are framework
// boilerplate (Configure, Schema, Metadata) that's not worth a unit test
// on its own — those are exercised end-to-end by the surface suite.
// ───────────────────────────────────────────────────────────────────────

func mustUUID(t *testing.T, s string) openapi_types.UUID {
	t.Helper()
	u, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("invalid uuid in fixture: %v", err)
	}
	return openapi_types.UUID(u)
}

// ── matchByName ─────────────────────────────────────────────────────────

func TestMatchByName_EmptyWantReturnsNil(t *testing.T) {
	items := []generated.TagDto{{Name: ""}, {Name: "foo"}}
	got := matchByName(items, "", func(t generated.TagDto) string { return t.Name })
	if got != nil {
		t.Errorf("empty want should return nil, got %v", got)
	}
}

func TestMatchByName_NoMatchReturnsNil(t *testing.T) {
	items := []generated.TagDto{{Name: "foo"}, {Name: "bar"}}
	got := matchByName(items, "missing", func(t generated.TagDto) string { return t.Name })
	if got != nil {
		t.Errorf("missing name should return nil, got %v", got)
	}
}

func TestMatchByName_SingleMatchPreservesOrder(t *testing.T) {
	items := []generated.TagDto{{Name: "foo"}, {Name: "bar"}, {Name: "baz"}}
	got := matchByName(items, "bar", func(t generated.TagDto) string { return t.Name })
	if len(got) != 1 || got[0].Name != "bar" {
		t.Errorf("expected [bar], got %v", got)
	}
}

func TestMatchByName_MultipleMatchesReturnsAllInOrder(t *testing.T) {
	id1 := mustUUID(t, "00000000-0000-0000-0000-000000000001")
	id2 := mustUUID(t, "00000000-0000-0000-0000-000000000002")
	id3 := mustUUID(t, "00000000-0000-0000-0000-000000000003")
	items := []generated.TagDto{
		{Id: id1, Name: "ops"},
		{Id: id2, Name: "ops"},
		{Id: id3, Name: "ops"},
	}
	got := matchByName(items, "ops", func(t generated.TagDto) string { return t.Name })
	if len(got) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(got))
	}
	// Order must be preserved so each datasource can format a stable
	// "ids: [...]" error message — flapping order would defeat caching
	// in operator dashboards that read the message verbatim.
	if got[0].Id != id1 || got[1].Id != id2 || got[2].Id != id3 {
		t.Errorf("order not preserved: %v", got)
	}
}

func TestMatchByName_EmptyInputReturnsNil(t *testing.T) {
	got := matchByName[generated.TagDto](nil, "anything", func(t generated.TagDto) string { return t.Name })
	if got != nil {
		t.Errorf("nil input + non-empty want should return nil, got %v", got)
	}
}

// ── mapAlertChannelToState ──────────────────────────────────────────────

func TestMapAlertChannelToState_PopulatesAllSurfacedFields(t *testing.T) {
	id := mustUUID(t, "11111111-1111-1111-1111-111111111111")
	dto := &generated.AlertChannelDto{
		Id:          id,
		Name:        "ops-slack",
		ChannelType: generated.AlertChannelDtoChannelTypeSlack,
	}
	var model AlertChannelDataSourceModel
	mapAlertChannelToState(&model, dto)
	if model.ID.ValueString() != id.String() {
		t.Errorf("ID: got %q, want %q", model.ID.ValueString(), id.String())
	}
	if model.Name.ValueString() != "ops-slack" {
		t.Errorf("Name: got %q", model.Name.ValueString())
	}
	if model.ChannelType.ValueString() != string(generated.AlertChannelDtoChannelTypeSlack) {
		t.Errorf("ChannelType: got %q", model.ChannelType.ValueString())
	}
}

// ── mapMonitorToState ───────────────────────────────────────────────────

func TestMapMonitorToState_FullDtoRoundTripsToState(t *testing.T) {
	id := mustUUID(t, "22222222-2222-2222-2222-222222222222")
	ping := "https://ping.devhelm.io/abc"
	var cfg generated.MonitorDto_Config
	if err := cfg.UnmarshalJSON([]byte(`{"url":"https://example.com","method":"GET","stripped":null}`)); err != nil {
		t.Fatalf("seeding config: %v", err)
	}
	dto := &generated.MonitorDto{
		Id:               id,
		Name:             "homepage",
		Type:             generated.MonitorDtoType("HTTP"),
		FrequencySeconds: 60,
		Enabled:          true,
		Config:           cfg,
		PingUrl:          &ping,
	}
	var model MonitorDataSourceModel
	mapMonitorToState(&model, dto)

	if model.ID.ValueString() != id.String() {
		t.Errorf("ID mismatch")
	}
	if model.Name.ValueString() != "homepage" {
		t.Errorf("Name not populated: %q", model.Name.ValueString())
	}
	if model.Type.ValueString() != "HTTP" {
		t.Errorf("Type: %q", model.Type.ValueString())
	}
	if model.FrequencySeconds.ValueInt64() != 60 {
		t.Errorf("Frequency: %d", model.FrequencySeconds.ValueInt64())
	}
	if !model.Enabled.ValueBool() {
		t.Error("Enabled not true")
	}
	if model.PingUrl.ValueString() != ping {
		t.Errorf("PingUrl: %q", model.PingUrl.ValueString())
	}
	// Config must be normalized: the explicit `"stripped":null` should
	// be dropped to match the resource-side behavior. The mapping bug
	// we're guarding against is leaving the raw RawMessage through,
	// which would surface to operators as a perpetual diff between
	// the data source's `config` and the resource's `config` for the
	// same monitor.
	cfgStr := model.Config.ValueString()
	if cfgStr == "" {
		t.Error("Config not populated")
	}
	if contains(cfgStr, "stripped") {
		t.Errorf("Config did not strip null key: %s", cfgStr)
	}
	if !contains(cfgStr, `"url":"https://example.com"`) {
		t.Errorf("Config missing url: %s", cfgStr)
	}
}

func TestMapMonitorToState_NilPingUrlBecomesNullNotEmpty(t *testing.T) {
	id := mustUUID(t, "33333333-3333-3333-3333-333333333333")
	var cfg generated.MonitorDto_Config
	_ = cfg.UnmarshalJSON([]byte(`{}`))
	dto := &generated.MonitorDto{
		Id:               id,
		Name:             "tcp-check",
		Type:             generated.MonitorDtoType("TCP"),
		FrequencySeconds: 30,
		Enabled:          false,
		Config:           cfg,
		PingUrl:          nil,
	}
	var model MonitorDataSourceModel
	mapMonitorToState(&model, dto)
	if !model.PingUrl.IsNull() {
		t.Errorf("PingUrl should be null when DTO is nil, got %q", model.PingUrl.ValueString())
	}
}

func TestMapMonitorToState_EmptyConfigBecomesNull(t *testing.T) {
	id := mustUUID(t, "44444444-4444-4444-4444-444444444444")
	// Explicit JSON null in the union exercises the "len(cfgBytes)>0
	// && string(cfgBytes)!=null" guard. Without that guard the data
	// source would emit the literal string "null" into config, which
	// would then fail to parse downstream.
	var cfg generated.MonitorDto_Config
	_ = cfg.UnmarshalJSON([]byte(`null`))
	dto := &generated.MonitorDto{
		Id:               id,
		Name:             "x",
		Type:             generated.MonitorDtoType("HEARTBEAT"),
		FrequencySeconds: 300,
		Enabled:          true,
		Config:           cfg,
	}
	var model MonitorDataSourceModel
	mapMonitorToState(&model, dto)
	if !model.Config.IsNull() {
		t.Errorf("Config should be null for JSON-null DTO, got %q", model.Config.ValueString())
	}
}

// ── mapResourceGroupToState ─────────────────────────────────────────────

func TestMapResourceGroupToState_DescriptionRoundTrip(t *testing.T) {
	id := mustUUID(t, "55555555-5555-5555-5555-555555555555")
	desc := "front-end services"
	dto := &generated.ResourceGroupDto{
		Id:          id,
		Name:        "frontend",
		Slug:        "frontend",
		Description: &desc,
	}
	var model ResourceGroupDataSourceModel
	mapResourceGroupToState(&model, dto)
	if model.ID.ValueString() != id.String() {
		t.Error("ID")
	}
	if model.Name.ValueString() != "frontend" {
		t.Error("Name")
	}
	if model.Slug.ValueString() != "frontend" {
		t.Error("Slug")
	}
	if model.Description.ValueString() != desc {
		t.Errorf("Description: %q", model.Description.ValueString())
	}
}

func TestMapResourceGroupToState_NilDescriptionBecomesNull(t *testing.T) {
	id := mustUUID(t, "66666666-6666-6666-6666-666666666666")
	dto := &generated.ResourceGroupDto{
		Id:          id,
		Name:        "g",
		Slug:        "g",
		Description: nil,
	}
	var model ResourceGroupDataSourceModel
	mapResourceGroupToState(&model, dto)
	if !model.Description.IsNull() {
		t.Errorf("Description should be null for nil ptr, got %q", model.Description.ValueString())
	}
}

// ── mapStatusPageToState ────────────────────────────────────────────────

func TestMapStatusPageToState_PopulatesAllFieldsAndSyntheticPageURL(t *testing.T) {
	id := mustUUID(t, "77777777-7777-7777-7777-777777777777")
	desc := "Public health dashboard"
	dto := &generated.StatusPageDto{
		Id:           id,
		Name:         "Acme Status",
		Slug:         "acme",
		Description:  &desc,
		Visibility:   generated.StatusPageDtoVisibilityPUBLIC,
		Enabled:      true,
		IncidentMode: generated.StatusPageDtoIncidentModeMANUAL,
	}
	var model StatusPageDataSourceModel
	mapStatusPageToState(&model, dto)

	if model.ID.ValueString() != id.String() {
		t.Errorf("ID")
	}
	if model.Name.ValueString() != "Acme Status" {
		t.Error("Name")
	}
	if model.Slug.ValueString() != "acme" {
		t.Error("Slug")
	}
	if model.Description.ValueString() != desc {
		t.Errorf("Description: %q", model.Description.ValueString())
	}
	if model.Visibility.ValueString() != string(generated.StatusPageDtoVisibilityPUBLIC) {
		t.Error("Visibility")
	}
	if !model.Enabled.ValueBool() {
		t.Error("Enabled")
	}
	if model.IncidentMode.ValueString() != string(generated.StatusPageDtoIncidentModeMANUAL) {
		t.Error("IncidentMode")
	}
	// PageURL is a *synthetic* field — the API doesn't return it, the
	// data source constructs it from slug. If we ever change the
	// hosted-page domain, this assertion is the canary that forces us
	// to update both the resource and the data source in lockstep.
	wantURL := "https://acme.devhelm.page"
	if model.PageURL.ValueString() != wantURL {
		t.Errorf("PageURL: got %q, want %q", model.PageURL.ValueString(), wantURL)
	}
}

func TestMapStatusPageToState_NilDescriptionBecomesNull(t *testing.T) {
	id := mustUUID(t, "88888888-8888-8888-8888-888888888888")
	dto := &generated.StatusPageDto{
		Id:           id,
		Name:         "x",
		Slug:         "x",
		Description:  nil,
		Visibility:   generated.StatusPageDtoVisibilityPASSWORD,
		Enabled:      false,
		IncidentMode: generated.StatusPageDtoIncidentModeAUTOMATIC,
	}
	var model StatusPageDataSourceModel
	mapStatusPageToState(&model, dto)
	if !model.Description.IsNull() {
		t.Errorf("Description should be null, got %q", model.Description.ValueString())
	}
}

// ── mapTagToState ───────────────────────────────────────────────────────

func TestMapTagToState_PopulatesAllFields(t *testing.T) {
	id := mustUUID(t, "99999999-9999-9999-9999-999999999999")
	dto := &generated.TagDto{
		Id:    id,
		Name:  "production",
		Color: "#ff0000",
	}
	var model TagDataSourceModel
	mapTagToState(&model, dto)
	if model.ID.ValueString() != id.String() {
		t.Error("ID")
	}
	if model.Name.ValueString() != "production" {
		t.Error("Name")
	}
	if model.Color.ValueString() != "#ff0000" {
		t.Error("Color")
	}
}

// ── mapEnvironmentToState ───────────────────────────────────────────────

func TestMapEnvironmentToState_PopulatesAllFields(t *testing.T) {
	id := mustUUID(t, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	dto := &generated.EnvironmentDto{
		Id:        id,
		Name:      "Production",
		Slug:      "production",
		IsDefault: true,
	}
	var model EnvironmentDataSourceModel
	mapEnvironmentToState(&model, dto)
	if model.ID.ValueString() != id.String() {
		t.Error("ID")
	}
	if model.Name.ValueString() != "Production" {
		t.Error("Name")
	}
	if model.Slug.ValueString() != "production" {
		t.Errorf("Slug: %q", model.Slug.ValueString())
	}
	if !model.IsDefault.ValueBool() {
		t.Error("IsDefault")
	}
}

// contains is a tiny zero-import substring helper; we deliberately avoid
// pulling in `strings` for one call so this test file's import surface
// stays minimal and obvious to reviewers.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
