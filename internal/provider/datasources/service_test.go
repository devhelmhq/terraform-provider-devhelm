package datasources

import (
	"testing"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
)

// ───────────────────────────────────────────────────────────────────────
// mapServiceToState
//
// Pins the DTO→state mapping for the devhelm_service data source. The
// interesting branches are the derived fields:
//
//   - overall_status comes from the optional currentStatus envelope
//   - component_count prefers componentsSummary.totalCount (summary-mode
//     responses trim the inline list) and falls back to len(components)
//   - uptime_30d is doubly-optional (uptime block AND its month field)
//
// Each null-path is asserted explicitly because "nil pointer coerced to
// zero value instead of null" is the classic data source drift bug.
// ───────────────────────────────────────────────────────────────────────

func TestMapServiceToState_PopulatesAllSurfacedFields(t *testing.T) {
	id := mustUUID(t, "11111111-2222-3333-4444-555555555555")
	category := "payments"
	statusURL := "https://status.stripe.com"
	month := 99.98
	dto := &generated.ServiceDetailDto{
		Id:                id,
		Slug:              "stripe",
		Name:              "Stripe",
		Category:          &category,
		OfficialStatusUrl: &statusURL,
		CurrentStatus:     &generated.ServiceStatusDto{OverallStatus: "operational"},
		Components: []generated.ServiceComponentDto{
			{Name: "API"}, {Name: "Dashboard"}, {Name: "Webhooks"},
		},
		Uptime: &generated.ComponentUptimeSummaryDto{Month: &month, Source: "vendor_reported"},
	}
	var model ServiceDataSourceModel
	mapServiceToState(&model, dto)

	if model.ID.ValueString() != id.String() {
		t.Errorf("ID: got %q, want %q", model.ID.ValueString(), id.String())
	}
	if model.Name.ValueString() != "Stripe" {
		t.Errorf("Name: %q", model.Name.ValueString())
	}
	if model.Category.ValueString() != "payments" {
		t.Errorf("Category: %q", model.Category.ValueString())
	}
	if model.OfficialStatusURL.ValueString() != statusURL {
		t.Errorf("OfficialStatusURL: %q", model.OfficialStatusURL.ValueString())
	}
	if model.OverallStatus.ValueString() != "operational" {
		t.Errorf("OverallStatus: %q", model.OverallStatus.ValueString())
	}
	if model.ComponentCount.ValueInt64() != 3 {
		t.Errorf("ComponentCount: got %d, want 3 (len of inline components)", model.ComponentCount.ValueInt64())
	}
	if model.Uptime30d.ValueFloat64() != month {
		t.Errorf("Uptime30d: got %v, want %v", model.Uptime30d.ValueFloat64(), month)
	}
}

func TestMapServiceToState_SlugNotOverwritten(t *testing.T) {
	// The slug attribute also accepts a UUID; the configured value must be
	// preserved verbatim or Terraform's config/state consistency check
	// trips when looking up by ID. mapServiceToState must therefore never
	// touch model.Slug.
	id := mustUUID(t, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	dto := &generated.ServiceDetailDto{Id: id, Slug: "github", Name: "GitHub"}
	var model ServiceDataSourceModel
	mapServiceToState(&model, dto)
	if !model.Slug.IsNull() {
		t.Errorf("Slug should be untouched (null on zero model), got %q", model.Slug.ValueString())
	}
}

func TestMapServiceToState_NilOptionalsBecomeNull(t *testing.T) {
	id := mustUUID(t, "99999999-8888-7777-6666-555555555555")
	dto := &generated.ServiceDetailDto{
		Id:   id,
		Slug: "smallco",
		Name: "SmallCo",
		// Category, OfficialStatusUrl, CurrentStatus, Uptime all nil;
		// Components nil (decoded from a hypothetical empty payload).
	}
	var model ServiceDataSourceModel
	mapServiceToState(&model, dto)

	if !model.Category.IsNull() {
		t.Errorf("Category should be null, got %q", model.Category.ValueString())
	}
	if !model.OfficialStatusURL.IsNull() {
		t.Errorf("OfficialStatusURL should be null, got %q", model.OfficialStatusURL.ValueString())
	}
	if !model.OverallStatus.IsNull() {
		t.Errorf("OverallStatus should be null when never polled, got %q", model.OverallStatus.ValueString())
	}
	if !model.Uptime30d.IsNull() {
		t.Errorf("Uptime30d should be null, got %v", model.Uptime30d.ValueFloat64())
	}
	if model.ComponentCount.ValueInt64() != 0 {
		t.Errorf("ComponentCount should be 0 for nil components, got %d", model.ComponentCount.ValueInt64())
	}
}

func TestMapServiceToState_UptimeBlockWithoutMonthIsNull(t *testing.T) {
	id := mustUUID(t, "12121212-3434-5656-7878-909090909090")
	day := 100.0
	dto := &generated.ServiceDetailDto{
		Id:   id,
		Slug: "x",
		Name: "X",
		// Uptime present but the 30d window has no data yet.
		Uptime: &generated.ComponentUptimeSummaryDto{Day: &day, Source: "incident_derived"},
	}
	var model ServiceDataSourceModel
	mapServiceToState(&model, dto)
	if !model.Uptime30d.IsNull() {
		t.Errorf("Uptime30d should be null when month is nil, got %v", model.Uptime30d.ValueFloat64())
	}
}

func TestMapServiceToState_SummaryTotalCountWinsOverTrimmedList(t *testing.T) {
	// Summary-mode responses (large vendors) trim the inline components
	// list and carry the authoritative count in componentsSummary. Using
	// len(components) there would silently under-report (e.g. Cloudflare
	// returning 12 showcase components out of 300+).
	id := mustUUID(t, "abababab-cdcd-efef-0101-232323232323")
	dto := &generated.ServiceDetailDto{
		Id:         id,
		Slug:       "cloudflare",
		Name:       "Cloudflare",
		Components: []generated.ServiceComponentDto{{Name: "CDN"}, {Name: "DNS"}},
		ComponentsSummary: &generated.ComponentsSummaryDto{
			TotalCount:           317,
			IncludedCount:        2,
			GroupComponentCounts: map[string]int32{},
		},
	}
	var model ServiceDataSourceModel
	mapServiceToState(&model, dto)
	if model.ComponentCount.ValueInt64() != 317 {
		t.Errorf("ComponentCount: got %d, want 317 (summary totalCount)", model.ComponentCount.ValueInt64())
	}
}
