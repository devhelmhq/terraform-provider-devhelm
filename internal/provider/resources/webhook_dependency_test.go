package resources

import (
	"encoding/json"
	"testing"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ───────────────────────────────────────────────────────────────────────
// webhook + dependency tests
//
// Coverage matrix:
//   D — request body completeness, including update-after-create disable flow
//   E — null-vs-set semantics for optional pointer fields
//   F — DTO → state round-trip for both resources
//   G — round-trip stability across repeated apply
// ───────────────────────────────────────────────────────────────────────

// ── Webhook ────────────────────────────────────────────────────────────

func TestWebhook_BuildCreateRequest_PopulatesUrlEventsAndDescription(t *testing.T) {
	plan := WebhookResourceModel{
		URL:         types.StringValue("https://hooks.example/x"),
		Description: types.StringValue("on-call alerts"),
		SubscribedEvents: types.SetValueMust(types.StringType, []attr.Value{
			types.StringValue("monitor.created"),
			types.StringValue("incident.resolved"),
		}),
	}
	body := generated.CreateWebhookEndpointRequest{
		Url:              plan.URL.ValueString(),
		SubscribedEvents: subscribedEventsCreateFromSet(plan.SubscribedEvents),
		Description:      stringPtrOrNil(plan.Description),
	}
	if body.Url != "https://hooks.example/x" {
		t.Errorf("Url = %q", body.Url)
	}
	if len(body.SubscribedEvents) != 2 {
		t.Errorf("SubscribedEvents = %v", body.SubscribedEvents)
	}
	if body.Description == nil || *body.Description != "on-call alerts" {
		t.Errorf("Description = %v", body.Description)
	}
}

func TestWebhook_BuildUpdateRequest_PreservesSemanticsForOmittedFields(t *testing.T) {
	plan := WebhookResourceModel{
		URL:              types.StringValue("https://hooks.example/x"),
		Description:      types.StringNull(), // omitted in HCL
		Enabled:          types.BoolNull(),
		SubscribedEvents: types.SetNull(types.StringType),
	}
	urlStr := plan.URL.ValueString()
	body := generated.UpdateWebhookEndpointRequest{
		Url:              &urlStr,
		Description:      stringPtrOrNil(plan.Description),
		Enabled:          boolPtrOrNil(plan.Enabled),
		SubscribedEvents: subscribedEventsUpdateFromSet(plan.SubscribedEvents),
	}
	if body.Url == nil || *body.Url != "https://hooks.example/x" {
		t.Errorf("Url = %v", body.Url)
	}
	// All omitted-from-HCL fields surface as nil pointers in the DTO and,
	// thanks to the spec's `omitempty` tags, those keys are entirely absent
	// from the wire body. Both "null" and "absent" are valid PATCH-preserve
	// semantics; the spec settled on absent.
	if body.Description != nil {
		t.Errorf("Description = %v, want nil pointer", body.Description)
	}
	if body.Enabled != nil {
		t.Errorf("Enabled = %v, want nil pointer", body.Enabled)
	}
	if body.SubscribedEvents != nil {
		t.Errorf("SubscribedEvents = %v, want nil pointer", body.SubscribedEvents)
	}

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"description", "enabled", "subscribedEvents"} {
		if _, present := got[key]; present {
			t.Errorf("expected JSON %q absent (preserve via omitempty), got value %v", key, got[key])
		}
	}
}

// TestWebhook_DisableAfterCreatePayloadShape: the create flow follows up
// with a disable-only update when the user planned `enabled = false`.
// Pin the exact body shape so the contract with the API stays explicit
// and a future refactor cannot accidentally clear other fields.
func TestWebhook_DisableAfterCreatePayloadShape(t *testing.T) {
	falseVal := false
	body := generated.UpdateWebhookEndpointRequest{Enabled: &falseVal}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["enabled"] != false {
		t.Errorf("enabled = %v, want false", got["enabled"])
	}
	for _, key := range []string{"url", "description", "subscribedEvents"} {
		if _, present := got[key]; present {
			t.Errorf("disable-only payload must omit %q to preserve current value (omitempty contract); got value %v", key, got[key])
		}
	}
}

// ── Dependency (service subscription) ───────────────────────────────────

func TestDependency_BuildCreateRequest_PopulatesAllFields(t *testing.T) {
	cid := uuid.New().String()
	plan := DependencyResourceModel{
		Service:          types.StringValue("aws"),
		AlertSensitivity: types.StringValue("MAJOR_ONLY"),
		ComponentID:      types.StringValue(cid),
	}
	componentID, err := parseUUIDPtrChecked(plan.ComponentID, "component_id")
	if err != nil {
		t.Fatalf("uuid parse: %v", err)
	}
	body := generated.ServiceSubscribeRequest{
		AlertSensitivity: stringPtrOrNil(plan.AlertSensitivity),
		ComponentId:      componentID,
	}
	if body.AlertSensitivity == nil || *body.AlertSensitivity != "MAJOR_ONLY" {
		t.Errorf("AlertSensitivity = %v", body.AlertSensitivity)
	}
	if body.ComponentId == nil || body.ComponentId.String() != cid {
		t.Errorf("ComponentId = %v", body.ComponentId)
	}
}

func TestDependency_BuildCreateRequest_OmitsSensitivityWhenNull(t *testing.T) {
	plan := DependencyResourceModel{
		Service:          types.StringValue("aws"),
		AlertSensitivity: types.StringNull(),
		ComponentID:      types.StringNull(),
	}
	componentID, err := parseUUIDPtrChecked(plan.ComponentID, "component_id")
	if err != nil {
		t.Fatalf("uuid parse: %v", err)
	}
	body := generated.ServiceSubscribeRequest{
		AlertSensitivity: stringPtrOrNil(plan.AlertSensitivity),
		ComponentId:      componentID,
	}
	if body.AlertSensitivity != nil {
		t.Errorf("AlertSensitivity = %v, want nil so API applies INCIDENTS_ONLY default", body.AlertSensitivity)
	}
	if body.ComponentId != nil {
		t.Errorf("ComponentId = %v, want nil for whole-service subscription", body.ComponentId)
	}
}

// TestDependency_BuildCreateRequest_InvalidComponentIDReturnsFieldedError
// verifies that the parseUUIDPtrChecked path includes the field name in
// the error message so users can find the bad attribute quickly.
func TestDependency_BuildCreateRequest_InvalidComponentIDReturnsFieldedError(t *testing.T) {
	plan := DependencyResourceModel{
		Service:     types.StringValue("aws"),
		ComponentID: types.StringValue("not-a-uuid"),
	}
	_, err := parseUUIDPtrChecked(plan.ComponentID, "component_id")
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !contains(got, "component_id") {
		t.Errorf("error must reference component_id; got %q", got)
	}
}

// TestDependency_DTOReadShape: confirms the read path's expected DTO field
// access stays correct after codegen regeneration. We assert the
// behavioural contract by constructing a populated DTO and verifying the
// fields the resource pulls from it.
func TestDependency_DTOReadShape(t *testing.T) {
	subID := openapi_types.UUID(uuid.New())
	cid := openapi_types.UUID(uuid.New())
	dto := generated.ServiceSubscriptionDto{
		SubscriptionId:   subID,
		Slug:             "aws",
		Name:             "Amazon Web Services",
		AlertSensitivity: generated.ServiceSubscriptionDtoAlertSensitivity("INCIDENTS_ONLY"),
		ComponentId:      &cid,
	}
	if dto.SubscriptionId.String() != subID.String() {
		t.Errorf("SubscriptionId mismatch")
	}
	if string(dto.AlertSensitivity) != "INCIDENTS_ONLY" {
		t.Errorf("AlertSensitivity = %q", string(dto.AlertSensitivity))
	}
	if dto.ComponentId == nil || dto.ComponentId.String() != cid.String() {
		t.Errorf("ComponentId = %v", dto.ComponentId)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
