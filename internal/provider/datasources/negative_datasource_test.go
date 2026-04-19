package datasources

import (
	"testing"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ───────────────────────────────────────────────────────────────────────
// Negative datasource tests (Class N)
//
// These tests exercise error-class paths in the datasource lookup and
// mapping layers. The positive paths (happy-path mappings) live in
// datasources_test.go. This file covers:
//
//   1. matchByName returning nil when no match is found (already covered
//      positively; we add edge-case inputs here)
//   2. matchByName returning multiple results (ambiguity detection)
//   3. mapToState with degenerate DTO inputs (nil pointers, zero values)
//   4. normalizeConfigJSON with degenerate inputs
//
// The IO-heavy Read paths that call the API are exercised end-to-end by
// the Terraform acceptance / surface tests.
// ───────────────────────────────────────────────────────────────────────

// ── matchByName: ambiguity & edge cases ────────────────────────────────

func TestMatchByName_WhitespaceNameMatchesExactly(t *testing.T) {
	items := []generated.TagDto{
		{Name: " foo "},
		{Name: "foo"},
		{Name: "foo "},
	}
	got := matchByName(items, "foo", func(t generated.TagDto) string { return t.Name })
	if len(got) != 1 || got[0].Name != "foo" {
		t.Errorf("expected exact match for 'foo', got %d matches: %v", len(got), got)
	}
}

func TestMatchByName_CaseSensitive(t *testing.T) {
	items := []generated.TagDto{
		{Name: "Production"},
		{Name: "production"},
		{Name: "PRODUCTION"},
	}
	got := matchByName(items, "production", func(t generated.TagDto) string { return t.Name })
	if len(got) != 1 || got[0].Name != "production" {
		t.Errorf("matchByName should be case-sensitive, got %d matches", len(got))
	}
}

func TestMatchByName_SingleCharName(t *testing.T) {
	items := []generated.TagDto{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	got := matchByName(items, "b", func(t generated.TagDto) string { return t.Name })
	if len(got) != 1 || got[0].Name != "b" {
		t.Errorf("expected match for 'b', got %v", got)
	}
}

func TestMatchByName_EmptyListReturnsNilForNonEmptySearch(t *testing.T) {
	var items []generated.TagDto
	got := matchByName(items, "x", func(t generated.TagDto) string { return t.Name })
	if got != nil {
		t.Errorf("empty list should return nil, got %v", got)
	}
}

func TestMatchByName_MultipleMatches_AllReturned(t *testing.T) {
	id1 := mustUUID(t, "11111111-1111-1111-1111-111111111111")
	id2 := mustUUID(t, "22222222-2222-2222-2222-222222222222")
	items := []generated.AlertChannelDto{
		{Id: id1, Name: "ops-slack", ChannelType: "SLACK"},
		{Id: id2, Name: "ops-slack", ChannelType: "DISCORD"},
	}
	got := matchByName(items, "ops-slack", func(c generated.AlertChannelDto) string { return c.Name })
	if len(got) != 2 {
		t.Fatalf("expected 2 matches for ambiguous name, got %d", len(got))
	}
	if got[0].Id != id1 || got[1].Id != id2 {
		t.Errorf("order or identity not preserved: got ids %s, %s", got[0].Id, got[1].Id)
	}
}

// ── mapMonitorToState: nil/degenerate DTO fields ───────────────────────

func TestMapMonitorToState_ZeroFrequencyAndFalseEnabled(t *testing.T) {
	id := mustUUID(t, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	var cfg generated.MonitorDto_Config
	_ = cfg.UnmarshalJSON([]byte(`{}`))
	dto := &generated.MonitorDto{
		Id:               id,
		Name:             "x",
		Type:             "HTTP",
		FrequencySeconds: 0,
		Enabled:          false,
		Config:           cfg,
	}
	var model MonitorDataSourceModel
	mapMonitorToState(&model, dto)
	if model.FrequencySeconds.ValueInt64() != 0 {
		t.Errorf("FrequencySeconds should be 0 for zero-value DTO, got %d", model.FrequencySeconds.ValueInt64())
	}
	if model.Enabled.ValueBool() {
		t.Errorf("Enabled should be false for zero-value DTO, got %v", model.Enabled.ValueBool())
	}
}

func TestMapMonitorToState_ZeroIdPreserved(t *testing.T) {
	zeroID := openapi_types.UUID(uuid.Nil)
	var cfg generated.MonitorDto_Config
	_ = cfg.UnmarshalJSON([]byte(`{}`))
	dto := &generated.MonitorDto{
		Id:               zeroID,
		Name:             "zero",
		Type:             "HTTP",
		FrequencySeconds: 30,
		Enabled:          false,
		Config:           cfg,
	}
	var model MonitorDataSourceModel
	mapMonitorToState(&model, dto)
	if model.ID.ValueString() != uuid.Nil.String() {
		t.Errorf("ID should be zero UUID, got %q", model.ID.ValueString())
	}
}

// ── mapAlertChannelToState: edge cases ─────────────────────────────────

func TestMapAlertChannelToState_EmptyNamePreserved(t *testing.T) {
	id := mustUUID(t, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	dto := &generated.AlertChannelDto{
		Id:          id,
		Name:        "",
		ChannelType: "WEBHOOK",
	}
	var model AlertChannelDataSourceModel
	mapAlertChannelToState(&model, dto)
	if model.Name.ValueString() != "" {
		t.Errorf("Name should be empty string, got %q", model.Name.ValueString())
	}
	if model.ChannelType.ValueString() != "WEBHOOK" {
		t.Errorf("ChannelType = %q", model.ChannelType.ValueString())
	}
}

// ── mapTagToState: edge cases ──────────────────────────────────────────

func TestMapTagToState_EmptyColorPreserved(t *testing.T) {
	id := mustUUID(t, "cccccccc-cccc-cccc-cccc-cccccccccccc")
	dto := &generated.TagDto{
		Id:    id,
		Name:  "test",
		Color: "",
	}
	var model TagDataSourceModel
	mapTagToState(&model, dto)
	if model.Color.ValueString() != "" {
		t.Errorf("Color should be empty string when DTO has empty, got %q", model.Color.ValueString())
	}
}

// ── mapEnvironmentToState: edge cases ──────────────────────────────────

func TestMapEnvironmentToState_FalseIsDefaultMapsToFalse(t *testing.T) {
	id := mustUUID(t, "dddddddd-dddd-dddd-dddd-dddddddddddd")
	dto := &generated.EnvironmentDto{
		Id:        id,
		Name:      "staging",
		Slug:      "staging",
		IsDefault: false,
	}
	var model EnvironmentDataSourceModel
	mapEnvironmentToState(&model, dto)
	if model.IsDefault.ValueBool() {
		t.Errorf("IsDefault should be false for zero-value DTO, got %v", model.IsDefault.ValueBool())
	}
}

func TestMapEnvironmentToState_FalseIsDefault(t *testing.T) {
	id := mustUUID(t, "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee")
	dto := &generated.EnvironmentDto{
		Id:        id,
		Name:      "dev",
		Slug:      "dev",
		IsDefault: false,
	}
	var model EnvironmentDataSourceModel
	mapEnvironmentToState(&model, dto)
	if model.IsDefault.IsNull() {
		t.Error("IsDefault should not be null when DTO ptr is non-nil")
	}
	if model.IsDefault.ValueBool() {
		t.Error("IsDefault should be false")
	}
}

// ── mapResourceGroupToState: edge cases ────────────────────────────────

func TestMapResourceGroupToState_EmptyDescription(t *testing.T) {
	id := mustUUID(t, "ffffffff-ffff-ffff-ffff-ffffffffffff")
	empty := ""
	dto := &generated.ResourceGroupDto{
		Id:          id,
		Name:        "grp",
		Slug:        "grp",
		Description: &empty,
	}
	var model ResourceGroupDataSourceModel
	mapResourceGroupToState(&model, dto)
	if model.Description.IsNull() {
		t.Error("Description should not be null for pointer to empty string")
	}
	if model.Description.ValueString() != "" {
		t.Errorf("Description should be empty string, got %q", model.Description.ValueString())
	}
}

// ── mapStatusPageToState: edge cases ───────────────────────────────────

func TestMapStatusPageToState_FalseEnabledMapsToFalse(t *testing.T) {
	id := mustUUID(t, "11111111-2222-3333-4444-555555555555")
	dto := &generated.StatusPageDto{
		Id:           id,
		Name:         "x",
		Slug:         "x",
		Visibility:   "PUBLIC",
		Enabled:      false,
		IncidentMode: "MANUAL",
	}
	var model StatusPageDataSourceModel
	mapStatusPageToState(&model, dto)
	if model.Enabled.ValueBool() {
		t.Errorf("Enabled should be false for zero-value DTO")
	}
}

func TestMapStatusPageToState_SyntheticPageURLWithSlug(t *testing.T) {
	id := mustUUID(t, "22222222-3333-4444-5555-666666666666")
	dto := &generated.StatusPageDto{
		Id:           id,
		Name:         "Test",
		Slug:         "my-company",
		Visibility:   "PUBLIC",
		Enabled:      true,
		IncidentMode: "AUTO",
	}
	var model StatusPageDataSourceModel
	mapStatusPageToState(&model, dto)
	want := "https://my-company.devhelm.page"
	if model.PageURL.ValueString() != want {
		t.Errorf("PageURL = %q, want %q", model.PageURL.ValueString(), want)
	}
}

// ── normalizeConfigJSON: additional degenerate inputs ──────────────────

func TestNormalizeConfigJSON_EmptyBytesReturnsEmpty(t *testing.T) {
	got := normalizeConfigJSON([]byte{})
	if got != "" {
		t.Errorf("empty bytes should return empty, got %q", got)
	}
}

func TestNormalizeConfigJSON_DeepNestedNullsAllStripped(t *testing.T) {
	in := `{"a":{"b":{"c":null,"d":{"e":null,"f":1}}}}`
	got := normalizeConfigJSON([]byte(in))
	if contains(got, "null") {
		t.Errorf("deep nested nulls not stripped: %s", got)
	}
	if !contains(got, `"f":1`) {
		t.Errorf("deep non-null value dropped: %s", got)
	}
}

func TestNormalizeConfigJSON_ArrayOfNullsBecomesEmpty(t *testing.T) {
	in := `[null,null,null]`
	got := normalizeConfigJSON([]byte(in))
	if got != "[]" {
		t.Errorf("array of nulls should become [], got %q", got)
	}
}
