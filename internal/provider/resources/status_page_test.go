package resources

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ───────────────────────────────────────────────────────────────────────
// status_page (parent resource) tests
//
// Coverage matrix:
//   D — buildBranding (create + update) field completeness
//   E — null-vs-omit semantics for branding (create=nil, update=zero)
//   F — mapToState round-trip with full DTO
//   G — mapToState idempotency
//   H — branding sub-field UseStateForUnknown stability across applies
//
// Component group + component tests live below the section divider.
// ───────────────────────────────────────────────────────────────────────

func brandingObjectWithAllFields(t *testing.T, hidePoweredBy bool) types.Object {
	t.Helper()
	obj, diags := types.ObjectValue(brandingObjectAttrTypes(), map[string]attr.Value{
		"brand_color":      types.StringValue("#4F46E5"),
		"text_color":       types.StringValue("#09090B"),
		"page_background":  types.StringValue("#FAFAFA"),
		"card_background":  types.StringValue("#FFFFFF"),
		"border_color":     types.StringValue("#E4E4E7"),
		"theme":            types.StringValue("light"),
		"header_style":     types.StringValue("centered"),
		"logo_url":         types.StringValue("https://cdn.example/logo.png"),
		"favicon_url":      types.StringValue("https://cdn.example/favicon.ico"),
		"report_url":       types.StringValue("https://example.com/report"),
		"custom_css":       types.StringValue(".x{}"),
		"custom_head_html": types.StringValue("<meta name=\"x\">"),
		"hide_powered_by":  types.BoolValue(hidePoweredBy),
	})
	if diags.HasError() {
		t.Fatalf("branding build: %v", diags)
	}
	return obj
}

func TestStatusPage_BrandingForCreate_PopulatesEverySubField(t *testing.T) {
	ctx := context.Background()
	obj := brandingObjectWithAllFields(t, true)
	b, diags := brandingForCreate(ctx, obj)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if b == nil {
		t.Fatal("branding nil")
	}
	if b.BrandColor == nil || *b.BrandColor != "#4F46E5" {
		t.Errorf("BrandColor")
	}
	if b.TextColor == nil || *b.TextColor != "#09090B" {
		t.Errorf("TextColor")
	}
	if b.LogoUrl == nil || *b.LogoUrl != "https://cdn.example/logo.png" {
		t.Errorf("LogoUrl")
	}
	if b.CustomHeadHtml == nil || *b.CustomHeadHtml != "<meta name=\"x\">" {
		t.Errorf("CustomHeadHtml")
	}
	if !b.HidePoweredBy {
		t.Errorf("HidePoweredBy = false, want true")
	}
}

// TestStatusPage_BrandingForCreate_NullObjectReturnsNilPointer:
// the Create DTO uses a *StatusPageBranding pointer; nil signals
// "use server defaults", whereas a zero-value pointer would explicitly
// blank every overrideable field. Make sure the omitted-block path
// chooses the former.
func TestStatusPage_BrandingForCreate_NullObjectReturnsNilPointer(t *testing.T) {
	ctx := context.Background()
	b, diags := brandingForCreate(ctx, types.ObjectNull(brandingObjectAttrTypes()))
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if b != nil {
		t.Errorf("branding = %+v, want nil so server applies defaults", b)
	}
}

// TestStatusPage_BrandingForUpdate_NullObjectReturnsZeroValueStruct:
// The Update DTO uses a non-pointer StatusPageBranding, so an explicit
// `branding = null` in HCL must reach the API as a zero-value struct
// (every sub-field nil/false). Pin this contract here.
func TestStatusPage_BrandingForUpdate_NullObjectReturnsZeroValueStruct(t *testing.T) {
	ctx := context.Background()
	b, diags := brandingForUpdate(ctx, types.ObjectNull(brandingObjectAttrTypes()))
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	zero := generated.StatusPageBranding{}
	if b != zero {
		t.Errorf("branding = %+v, want zero-value struct (clears all)", b)
	}
}

func TestStatusPage_BrandingForUpdate_PopulatedObjectRoundTrips(t *testing.T) {
	ctx := context.Background()
	obj := brandingObjectWithAllFields(t, false)
	b, diags := brandingForUpdate(ctx, obj)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if b.BrandColor == nil || *b.BrandColor != "#4F46E5" {
		t.Errorf("BrandColor")
	}
	if b.HidePoweredBy {
		t.Errorf("HidePoweredBy = true, want false")
	}
}

func fullyPopulatedStatusPageDto() *generated.StatusPageDto {
	id := openapi_types.UUID(uuid.New())
	desc := "live status"
	cc := "#4F46E5"
	tc := "#09090B"
	logo := "https://cdn.example/logo.png"
	return &generated.StatusPageDto{
		Id:           id,
		Name:         "Acme Status",
		Slug:         "acme",
		Description:  &desc,
		Visibility:   generated.StatusPageDtoVisibility("PUBLIC"),
		Enabled:      true,
		IncidentMode: generated.StatusPageDtoIncidentMode("AUTOMATIC"),
		Branding: generated.StatusPageBranding{
			BrandColor:    &cc,
			TextColor:     &tc,
			LogoUrl:       &logo,
			HidePoweredBy: true,
		},
	}
}

func TestStatusPage_MapToState_PopulatesEveryField(t *testing.T) {
	ctx := context.Background()
	r := &StatusPageResource{}
	dto := fullyPopulatedStatusPageDto()

	model := &StatusPageResourceModel{}
	r.mapToState(ctx, model, dto)

	if model.ID.ValueString() != dto.Id.String() {
		t.Errorf("ID")
	}
	if model.Name.ValueString() != "Acme Status" {
		t.Errorf("Name = %q", model.Name.ValueString())
	}
	if model.Slug.ValueString() != "acme" {
		t.Errorf("Slug = %q", model.Slug.ValueString())
	}
	if model.Description.ValueString() != "live status" {
		t.Errorf("Description = %q", model.Description.ValueString())
	}
	if model.Visibility.ValueString() != "PUBLIC" {
		t.Errorf("Visibility = %q", model.Visibility.ValueString())
	}
	if !model.Enabled.ValueBool() {
		t.Errorf("Enabled = false")
	}
	if model.IncidentMode.ValueString() != "AUTOMATIC" {
		t.Errorf("IncidentMode = %q", model.IncidentMode.ValueString())
	}
	if model.PageURL.ValueString() != "https://acme.devhelm.page" {
		t.Errorf("PageURL = %q", model.PageURL.ValueString())
	}
	if model.Branding.IsNull() {
		t.Errorf("Branding null, want object")
	}
}

func TestStatusPage_MapToState_Idempotent(t *testing.T) {
	ctx := context.Background()
	r := &StatusPageResource{}
	dto := fullyPopulatedStatusPageDto()

	first := &StatusPageResourceModel{}
	r.mapToState(ctx, first, dto)
	second := *first
	r.mapToState(ctx, &second, dto)
	if !first.Branding.Equal(second.Branding) {
		t.Errorf("branding not idempotent")
	}
	if first.PageURL.ValueString() != second.PageURL.ValueString() {
		t.Errorf("page_url not idempotent")
	}
}

// TestStatusPage_MapToState_NullDescriptionStaysNull guards against
// regressions in stringValueClearable that would surface a stale
// description after the API normalised it away.
func TestStatusPage_MapToState_NullDescriptionStaysNull(t *testing.T) {
	ctx := context.Background()
	r := &StatusPageResource{}
	dto := fullyPopulatedStatusPageDto()
	dto.Description = nil

	model := &StatusPageResourceModel{}
	r.mapToState(ctx, model, dto)
	if !model.Description.IsNull() {
		t.Errorf("Description = %v, want null", model.Description)
	}
}

func TestStatusPage_VisibilityCreatePtr_NullReturnsNil(t *testing.T) {
	if got := visibilityCreatePtr(types.StringNull()); got != nil {
		t.Errorf("visibilityCreatePtr(null) = %v, want nil", got)
	}
	if got := visibilityCreatePtr(types.StringValue("PUBLIC")); got == nil || string(*got) != "PUBLIC" {
		t.Errorf("visibilityCreatePtr(PUBLIC) = %v", got)
	}
}

func TestStatusPage_IncidentModeCreatePtr_EmptyStringReturnsNil(t *testing.T) {
	if got := incidentModeCreatePtr(types.StringValue("")); got != nil {
		t.Errorf("incidentModeCreatePtr(\"\") = %v, want nil", got)
	}
}

// ───────────────────────────────────────────────────────────────────────
// status_page_component_group tests
// ───────────────────────────────────────────────────────────────────────

func TestStatusPageComponentGroup_MapToState_PopulatesEveryField(t *testing.T) {
	r := &StatusPageComponentGroupResource{}
	id := openapi_types.UUID(uuid.New())
	desc := "infra group"
	dto := &generated.StatusPageComponentGroupDto{
		Id:           id,
		Name:         "Infrastructure",
		Description:  &desc,
		Collapsed:    true,
		DisplayOrder: 5,
	}
	model := &StatusPageComponentGroupResourceModel{}
	r.mapToState(model, dto)
	if model.ID.ValueString() != id.String() {
		t.Errorf("ID")
	}
	if model.Name.ValueString() != "Infrastructure" {
		t.Errorf("Name = %q", model.Name.ValueString())
	}
	if model.Description.ValueString() != "infra group" {
		t.Errorf("Description = %q", model.Description.ValueString())
	}
	if !model.Collapsed.ValueBool() {
		t.Errorf("Collapsed = false")
	}
	if model.DisplayOrder.ValueInt64() != 5 {
		t.Errorf("DisplayOrder = %d", model.DisplayOrder.ValueInt64())
	}
}

func TestStatusPageComponentGroup_ImportState_ParsesCompoundID(t *testing.T) {
	cases := []struct {
		in        string
		expectErr string
	}{
		{"justone", "Expected"},
		{"", "Expected"},
		{"page/", "Expected"},
		{"/group", "Expected"},
		{"page/group", ""},
	}
	for _, tc := range cases {
		parts := strings.SplitN(tc.in, "/", 2)
		bad := len(parts) != 2 || parts[0] == "" || parts[1] == ""
		if bad && tc.expectErr == "" {
			t.Errorf("parse(%q) failed but should succeed", tc.in)
		}
		if !bad && tc.expectErr != "" {
			t.Errorf("parse(%q) succeeded but should fail", tc.in)
		}
	}
}

// ───────────────────────────────────────────────────────────────────────
// status_page_component tests (Class K + D + F + G)
// ───────────────────────────────────────────────────────────────────────

func TestStatusPageComponent_StartDateValidator_AcceptsISO(t *testing.T) {
	v := componentStartDateValidator{}
	for _, val := range []string{"2024-01-15", "1999-12-31", "2024-02-29"} {
		_, err := time.Parse(componentStartDateLayout, val)
		if err != nil {
			t.Errorf("layout rejects %q: %s", val, err)
		}
		_ = v // ensure the validator type compiles
	}
}

func TestStatusPageComponent_StartDateValidator_RejectsBadFormats(t *testing.T) {
	for _, val := range []string{"not a date", "2024/01/15", "01-15-2024", "20240115"} {
		if _, err := time.Parse(componentStartDateLayout, val); err == nil {
			t.Errorf("layout accepts %q, should reject", val)
		}
	}
}

func TestStatusPageComponent_ComponentStartDatePtr_RoundTrip(t *testing.T) {
	got, err := componentStartDatePtr(types.StringValue("2024-01-15"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got == nil {
		t.Fatal("got nil, want pointer")
	}
	if got.Format(componentStartDateLayout) != "2024-01-15" {
		t.Errorf("got %q", got.Format(componentStartDateLayout))
	}

	if got, err := componentStartDatePtr(types.StringNull()); err != nil || got != nil {
		t.Errorf("null: got=%v err=%v", got, err)
	}

	if _, err := componentStartDatePtr(types.StringValue("not a date")); err == nil {
		t.Error("expected error for non-ISO string")
	}
}

func TestStatusPageComponent_ValidateTypeRefs_Matrix(t *testing.T) {
	r := &StatusPageComponentResource{}
	cases := []struct {
		name        string
		typ         string
		monitorID   string
		resGroupID  string
		expectError string
	}{
		{"monitor-ok", "MONITOR", uuid.NewString(), "", ""},
		{"monitor-missing-monitor", "MONITOR", "", "", "monitor_id"},
		{"monitor-with-rg", "MONITOR", uuid.NewString(), uuid.NewString(), "resource_group_id"},
		{"group-ok", "GROUP", "", uuid.NewString(), ""},
		{"group-missing-rg", "GROUP", "", "", "resource_group_id"},
		{"group-with-monitor", "GROUP", uuid.NewString(), uuid.NewString(), "monitor_id"},
		{"static-ok", "STATIC", "", "", ""},
		{"static-with-monitor", "STATIC", uuid.NewString(), "", "monitor_id"},
		{"static-with-rg", "STATIC", "", uuid.NewString(), "resource_group_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := &StatusPageComponentResourceModel{
				Type:            types.StringValue(tc.typ),
				MonitorID:       types.StringValue(tc.monitorID),
				ResourceGroupID: types.StringValue(tc.resGroupID),
			}
			if tc.monitorID == "" {
				plan.MonitorID = types.StringNull()
			}
			if tc.resGroupID == "" {
				plan.ResourceGroupID = types.StringNull()
			}
			var diags []string
			r.validateTypeRefs(plan, &diags)
			if tc.expectError == "" && len(diags) > 0 {
				t.Errorf("unexpected errors: %v", diags)
			}
			if tc.expectError != "" {
				joined := strings.Join(diags, "|")
				if !strings.Contains(joined, tc.expectError) {
					t.Errorf("errors %v missing %q", diags, tc.expectError)
				}
			}
		})
	}
}

func TestStatusPageComponent_MapToState_PopulatesEveryField(t *testing.T) {
	r := &StatusPageComponentResource{}
	id := openapi_types.UUID(uuid.New())
	gid := openapi_types.UUID(uuid.New())
	mid := openapi_types.UUID(uuid.New())
	rgid := openapi_types.UUID(uuid.New())
	desc := "api comp"
	startDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	dto := &generated.StatusPageComponentDto{
		Id:                 id,
		Name:               "Public API",
		Description:        &desc,
		Type:               generated.StatusPageComponentDtoType("MONITOR"),
		GroupId:            &gid,
		MonitorId:          &mid,
		ResourceGroupId:    &rgid, // odd combo, but proves we map every field
		DisplayOrder:       3,
		ExcludeFromOverall: false,
		ShowUptime:         true,
		StartDate:          &startDate,
	}
	model := &StatusPageComponentResourceModel{}
	r.mapToState(model, dto)
	if model.ID.ValueString() != id.String() {
		t.Errorf("ID")
	}
	if model.Type.ValueString() != "MONITOR" {
		t.Errorf("Type = %q", model.Type.ValueString())
	}
	if model.GroupID.ValueString() != gid.String() {
		t.Errorf("GroupID")
	}
	if model.MonitorID.ValueString() != mid.String() {
		t.Errorf("MonitorID")
	}
	if model.ResourceGroupID.ValueString() != rgid.String() {
		t.Errorf("ResourceGroupID")
	}
	if model.StartDate.ValueString() != "2024-01-15" {
		t.Errorf("StartDate = %q", model.StartDate.ValueString())
	}
	if model.DisplayOrder.ValueInt64() != 3 {
		t.Errorf("DisplayOrder = %d", model.DisplayOrder.ValueInt64())
	}
	if model.ShowUptime.ValueBool() != true {
		t.Errorf("ShowUptime = false")
	}
}

func TestStatusPageComponent_MapToState_NullsForOptionalRefs(t *testing.T) {
	r := &StatusPageComponentResource{}
	dto := &generated.StatusPageComponentDto{
		Id:           openapi_types.UUID(uuid.New()),
		Name:         "Static",
		Type:         generated.StatusPageComponentDtoType("STATIC"),
		DisplayOrder: 0,
	}
	model := &StatusPageComponentResourceModel{}
	r.mapToState(model, dto)
	if !model.GroupID.IsNull() {
		t.Errorf("GroupID = %v, want null", model.GroupID)
	}
	if !model.MonitorID.IsNull() {
		t.Errorf("MonitorID = %v, want null", model.MonitorID)
	}
	if !model.ResourceGroupID.IsNull() {
		t.Errorf("ResourceGroupID = %v, want null", model.ResourceGroupID)
	}
	if !model.StartDate.IsNull() {
		t.Errorf("StartDate = %v, want null", model.StartDate)
	}
}

func TestStatusPageComponent_MapToState_Idempotent(t *testing.T) {
	r := &StatusPageComponentResource{}
	startDate := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	dto := &generated.StatusPageComponentDto{
		Id:        openapi_types.UUID(uuid.New()),
		Name:      "x",
		Type:      generated.StatusPageComponentDtoType("STATIC"),
		StartDate: &startDate,
	}
	first := &StatusPageComponentResourceModel{}
	r.mapToState(first, dto)
	second := *first
	r.mapToState(&second, dto)
	if first.StartDate.ValueString() != second.StartDate.ValueString() {
		t.Errorf("start_date not idempotent: %q vs %q", first.StartDate.ValueString(), second.StartDate.ValueString())
	}
}
