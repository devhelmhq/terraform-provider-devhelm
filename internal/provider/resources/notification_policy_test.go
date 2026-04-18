package resources

import (
	"context"
	"strings"
	"testing"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ───────────────────────────────────────────────────────────────────────
// notification_policy tests
//
// Coverage matrix:
//   D — buildRequest / buildUpdateRequest body completeness for steps + match rules
//   E — null-vs-omit semantics on optional escalation step fields
//   F — mapToState round-trip for steps + match rules
//   G — mapToState idempotency
//   H — preservation of user-specified casing / null fields after API normalisation
// ───────────────────────────────────────────────────────────────────────

func newEscalationStepValue(t *testing.T, channelID string, delay int64, ack bool, repeat int64) types.Object {
	t.Helper()
	chList := types.ListValueMust(types.StringType, []attr.Value{types.StringValue(channelID)})
	obj, diags := types.ObjectValue(escalationStepObjectType().AttrTypes, map[string]attr.Value{
		"channel_ids":             chList,
		"delay_minutes":           types.Int64Value(delay),
		"require_ack":             types.BoolValue(ack),
		"repeat_interval_seconds": types.Int64Value(repeat),
	})
	if diags.HasError() {
		t.Fatalf("escalation step build: %v", diags)
	}
	return obj
}

func newMatchRuleValue(t *testing.T, ruleType, value string) types.Object {
	t.Helper()
	obj, diags := types.ObjectValue(matchRuleObjectType().AttrTypes, map[string]attr.Value{
		"type":        types.StringValue(ruleType),
		"value":       types.StringValue(value),
		"values":      types.ListNull(types.StringType),
		"monitor_ids": types.ListNull(types.StringType),
		"regions":     types.ListNull(types.StringType),
	})
	if diags.HasError() {
		t.Fatalf("match rule build: %v", diags)
	}
	return obj
}

// ── build*Request tests (Class D + E) ───────────────────────────────────

func TestNotificationPolicy_BuildRequest_PopulatesStepsAndRules(t *testing.T) {
	ctx := context.Background()
	r := &NotificationPolicyResource{}

	channelID := uuid.New().String()
	step := newEscalationStepValue(t, channelID, 5, true, 60)
	rule := newMatchRuleValue(t, "severity_gte", "ERROR")

	plan := &NotificationPolicyModel{
		Name:     types.StringValue("oncall"),
		Enabled:  types.BoolValue(true),
		Priority: types.Int64Value(10),
		Escalation: types.ListValueMust(
			escalationStepObjectType(),
			[]attr.Value{step},
		),
		MatchRules: types.ListValueMust(
			matchRuleObjectType(),
			[]attr.Value{rule},
		),
		OnResolve: types.StringValue("slack"),
		OnReopen:  types.StringValue("pagerduty"),
	}

	body, err := r.buildRequest(ctx, plan)
	if err != nil {
		t.Fatalf("buildRequest err: %v", err)
	}
	if body.Name != "oncall" {
		t.Errorf("Name = %q", body.Name)
	}
	if len(body.Escalation.Steps) != 1 {
		t.Fatalf("Steps = %d", len(body.Escalation.Steps))
	}
	s := body.Escalation.Steps[0]
	if len(s.ChannelIds) != 1 || s.ChannelIds[0].String() != channelID {
		t.Errorf("ChannelIds = %v", s.ChannelIds)
	}
	if s.DelayMinutes == nil || *s.DelayMinutes != 5 {
		t.Errorf("DelayMinutes = %v", s.DelayMinutes)
	}
	if s.RequireAck == nil || !*s.RequireAck {
		t.Errorf("RequireAck = %v", s.RequireAck)
	}
	if s.RepeatIntervalSeconds == nil || *s.RepeatIntervalSeconds != 60 {
		t.Errorf("RepeatIntervalSeconds = %v", s.RepeatIntervalSeconds)
	}
	if body.MatchRules == nil || len(*body.MatchRules) != 1 {
		t.Fatalf("MatchRules = %v", body.MatchRules)
	}
	mr := (*body.MatchRules)[0]
	if mr.Type != "severity_gte" {
		t.Errorf("MatchRule.Type = %q", mr.Type)
	}
	if mr.Value == nil || *mr.Value != "ERROR" {
		t.Errorf("MatchRule.Value = %v", mr.Value)
	}
	if body.Escalation.OnResolve == nil || *body.Escalation.OnResolve != "slack" {
		t.Errorf("OnResolve = %v", body.Escalation.OnResolve)
	}
}

func TestNotificationPolicy_BuildRequest_InvalidUUIDInChannelIDsErrorsWithFieldPath(t *testing.T) {
	ctx := context.Background()
	r := &NotificationPolicyResource{}

	chList := types.ListValueMust(types.StringType, []attr.Value{types.StringValue("not-a-uuid")})
	stepObj, _ := types.ObjectValue(escalationStepObjectType().AttrTypes, map[string]attr.Value{
		"channel_ids":             chList,
		"delay_minutes":           types.Int64Null(),
		"require_ack":             types.BoolNull(),
		"repeat_interval_seconds": types.Int64Null(),
	})
	plan := &NotificationPolicyModel{
		Name: types.StringValue("p"),
		Escalation: types.ListValueMust(
			escalationStepObjectType(),
			[]attr.Value{stepObj},
		),
		MatchRules: types.ListNull(matchRuleObjectType()),
	}

	_, err := r.buildRequest(ctx, plan)
	if err == nil {
		t.Fatal("expected error for invalid UUID")
	}
	if !strings.Contains(err.Error(), "escalation[0].channel_ids") {
		t.Errorf("error must include indexed field path; got %q", err.Error())
	}
}

// TestNotificationPolicy_BuildUpdateRequest_NullEnabledDefaultsToTrue:
// the Update DTO uses non-pointer `Enabled bool`, so the provider must
// fall back to a sensible default when the plan attribute is null/unknown
// rather than emitting `enabled: false` and silently disabling the policy.
func TestNotificationPolicy_BuildUpdateRequest_NullEnabledDefaultsToTrue(t *testing.T) {
	ctx := context.Background()
	r := &NotificationPolicyResource{}

	plan := &NotificationPolicyModel{
		Name:       types.StringValue("p"),
		Enabled:    types.BoolNull(),
		Priority:   types.Int64Null(),
		Escalation: types.ListValueMust(escalationStepObjectType(), []attr.Value{}),
		MatchRules: types.ListNull(matchRuleObjectType()),
	}
	body, err := r.buildUpdateRequest(ctx, plan)
	if err != nil {
		t.Fatalf("buildUpdateRequest err: %v", err)
	}
	if !body.Enabled {
		t.Errorf("Enabled = false, want true (null plan must not silently disable)")
	}
	if body.Priority != 0 {
		t.Errorf("Priority = %d, want 0 (default for null)", body.Priority)
	}
}

// TestNotificationPolicy_BuildUpdateRequest_PopulatesEveryField is the
// "fully populated" twin of BuildRequest. The Update DTO has its own
// dedicated request shape (separate from the Create DTO) and is built
// by a different code path; field drift between the two is one of the
// most frequently-recurring bug classes in this package, so we pin
// every field through the update path explicitly.
func TestNotificationPolicy_BuildUpdateRequest_PopulatesEveryField(t *testing.T) {
	ctx := context.Background()
	r := &NotificationPolicyResource{}

	channelID := uuid.New().String()
	step := newEscalationStepValue(t, channelID, 5, true, 60)
	rule := newMatchRuleValue(t, "severity_gte", "ERROR")

	plan := &NotificationPolicyModel{
		Name:     types.StringValue("oncall"),
		Enabled:  types.BoolValue(true),
		Priority: types.Int64Value(10),
		Escalation: types.ListValueMust(
			escalationStepObjectType(),
			[]attr.Value{step},
		),
		MatchRules: types.ListValueMust(
			matchRuleObjectType(),
			[]attr.Value{rule},
		),
		OnResolve: types.StringValue("slack"),
		OnReopen:  types.StringValue("pagerduty"),
	}

	body, err := r.buildUpdateRequest(ctx, plan)
	if err != nil {
		t.Fatalf("buildUpdateRequest err: %v", err)
	}
	if body.Name != "oncall" {
		t.Errorf("Name = %q", body.Name)
	}
	if !body.Enabled {
		t.Error("Enabled = false")
	}
	if body.Priority != 10 {
		t.Errorf("Priority = %d", body.Priority)
	}
	if len(body.Escalation.Steps) != 1 {
		t.Fatalf("Steps = %d", len(body.Escalation.Steps))
	}
	s := body.Escalation.Steps[0]
	if s.DelayMinutes == nil || *s.DelayMinutes != 5 {
		t.Errorf("DelayMinutes = %v", s.DelayMinutes)
	}
	if s.RequireAck == nil || !*s.RequireAck {
		t.Errorf("RequireAck = %v", s.RequireAck)
	}
	if s.RepeatIntervalSeconds == nil || *s.RepeatIntervalSeconds != 60 {
		t.Errorf("RepeatIntervalSeconds = %v", s.RepeatIntervalSeconds)
	}
	if body.Escalation.OnResolve == nil || *body.Escalation.OnResolve != "slack" {
		t.Errorf("OnResolve = %v", body.Escalation.OnResolve)
	}
	if body.Escalation.OnReopen == nil || *body.Escalation.OnReopen != "pagerduty" {
		t.Errorf("OnReopen = %v", body.Escalation.OnReopen)
	}
	if len(body.MatchRules) != 1 {
		t.Fatalf("MatchRules = %v", body.MatchRules)
	}
	mr := body.MatchRules[0]
	if mr.Type != "severity_gte" {
		t.Errorf("MatchRule.Type = %q", mr.Type)
	}
	if mr.Value == nil || *mr.Value != "ERROR" {
		t.Errorf("MatchRule.Value = %v", mr.Value)
	}
}

// TestNotificationPolicy_BuildUpdateRequest_InvalidChannelUUIDErrorsWithFieldPath
// pins the symmetry between Create and Update error wording so an
// operator hitting the same malformed UUID gets the same diagnostic
// in both lifecycle phases. Without this assertion, one of the paths
// could regress to a generic "invalid uuid" error and we'd only catch
// it via a real apply-time failure.
func TestNotificationPolicy_BuildUpdateRequest_InvalidChannelUUIDErrorsWithFieldPath(t *testing.T) {
	ctx := context.Background()
	r := &NotificationPolicyResource{}

	chList := types.ListValueMust(types.StringType, []attr.Value{types.StringValue("not-a-uuid")})
	stepObj, _ := types.ObjectValue(escalationStepObjectType().AttrTypes, map[string]attr.Value{
		"channel_ids":             chList,
		"delay_minutes":           types.Int64Null(),
		"require_ack":             types.BoolNull(),
		"repeat_interval_seconds": types.Int64Null(),
	})
	plan := &NotificationPolicyModel{
		Name:       types.StringValue("p"),
		Escalation: types.ListValueMust(escalationStepObjectType(), []attr.Value{stepObj}),
		MatchRules: types.ListNull(matchRuleObjectType()),
	}

	_, err := r.buildUpdateRequest(ctx, plan)
	if err == nil {
		t.Fatal("expected error for invalid UUID")
	}
	if !strings.Contains(err.Error(), "escalation[0].channel_ids") {
		t.Errorf("error must include indexed field path; got %q", err.Error())
	}
}

// TestNotificationPolicy_BuildUpdateRequest_InvalidMatchRuleMonitorUUIDErrorsWithFieldPath
// covers the second UUID-validation site in the update path, which
// previously had no test coverage (the error branch is structurally
// identical to the channel-IDs branch above but lives in a separate
// loop and would silently regress on its own).
func TestNotificationPolicy_BuildUpdateRequest_InvalidMatchRuleMonitorUUIDErrorsWithFieldPath(t *testing.T) {
	ctx := context.Background()
	r := &NotificationPolicyResource{}

	monList := types.ListValueMust(types.StringType, []attr.Value{types.StringValue("not-a-uuid")})
	ruleObj, _ := types.ObjectValue(matchRuleObjectType().AttrTypes, map[string]attr.Value{
		"type":        types.StringValue("monitor_in"),
		"value":       types.StringNull(),
		"values":      types.ListNull(types.StringType),
		"monitor_ids": monList,
		"regions":     types.ListNull(types.StringType),
	})
	plan := &NotificationPolicyModel{
		Name:       types.StringValue("p"),
		Escalation: types.ListValueMust(escalationStepObjectType(), []attr.Value{}),
		MatchRules: types.ListValueMust(matchRuleObjectType(), []attr.Value{ruleObj}),
	}

	_, err := r.buildUpdateRequest(ctx, plan)
	if err == nil {
		t.Fatal("expected error for invalid monitor UUID in match rule")
	}
	if !strings.Contains(err.Error(), "match_rule[0].monitor_ids") {
		t.Errorf("error must include indexed field path; got %q", err.Error())
	}
}

// ── mapToState tests (Class F + G + H) ──────────────────────────────────

func fullyPopulatedPolicyDto() *generated.NotificationPolicyDto {
	channelID := openapi_types.UUID(uuid.New())
	monitorID := openapi_types.UUID(uuid.New())
	monitorIDs := []openapi_types.UUID{monitorID}
	value := "ERROR"
	delay := int32(5)
	requireAck := true
	repeat := int32(60)
	onResolve := "slack"
	onReopen := "pagerduty"

	return &generated.NotificationPolicyDto{
		Id:       openapi_types.UUID(uuid.New()),
		Name:     "oncall",
		Enabled:  true,
		Priority: 10,
		Escalation: generated.EscalationChain{
			Steps: []generated.EscalationStep{{
				ChannelIds:            []openapi_types.UUID{channelID},
				DelayMinutes:          &delay,
				RequireAck:            &requireAck,
				RepeatIntervalSeconds: &repeat,
			}},
			OnResolve: &onResolve,
			OnReopen:  &onReopen,
		},
		MatchRules: []generated.MatchRule{{
			Type:       "severity_gte",
			Value:      &value,
			MonitorIds: &monitorIDs,
		}},
	}
}

func TestNotificationPolicy_MapToState_PopulatesEveryField(t *testing.T) {
	ctx := context.Background()
	r := &NotificationPolicyResource{}
	dto := fullyPopulatedPolicyDto()

	model := &NotificationPolicyModel{}
	r.mapToState(ctx, model, dto)

	if model.ID.ValueString() != dto.Id.String() {
		t.Errorf("ID")
	}
	if model.Name.ValueString() != "oncall" {
		t.Errorf("Name = %q", model.Name.ValueString())
	}
	if !model.Enabled.ValueBool() {
		t.Errorf("Enabled = false")
	}
	if model.Priority.ValueInt64() != 10 {
		t.Errorf("Priority = %d", model.Priority.ValueInt64())
	}
	if model.OnResolve.ValueString() != "slack" {
		t.Errorf("OnResolve = %q", model.OnResolve.ValueString())
	}

	if model.Escalation.IsNull() {
		t.Fatal("Escalation null")
	}
	if len(model.Escalation.Elements()) != 1 {
		t.Errorf("Escalation length = %d", len(model.Escalation.Elements()))
	}
	if model.MatchRules.IsNull() {
		t.Fatal("MatchRules null")
	}
	if len(model.MatchRules.Elements()) != 1 {
		t.Errorf("MatchRules length = %d", len(model.MatchRules.Elements()))
	}
}

func TestNotificationPolicy_MapToState_PreservesUserCasingOnRuleValue(t *testing.T) {
	ctx := context.Background()
	r := &NotificationPolicyResource{}
	dto := fullyPopulatedPolicyDto()

	priorRule := newMatchRuleValue(t, "severity_gte", "Error") // user-supplied casing
	model := &NotificationPolicyModel{
		MatchRules: types.ListValueMust(matchRuleObjectType(), []attr.Value{priorRule}),
	}
	r.mapToState(ctx, model, dto)

	rules := model.MatchRules.Elements()
	if len(rules) != 1 {
		t.Fatalf("rules len = %d", len(rules))
	}
	var parsed []matchRuleModel
	_ = model.MatchRules.ElementsAs(ctx, &parsed, false)
	if parsed[0].Value.ValueString() != "Error" {
		t.Errorf("Value = %q, want preserved 'Error' casing", parsed[0].Value.ValueString())
	}
}

func TestNotificationPolicy_MapToState_PreservesNullOptionalsOnRoundTrip(t *testing.T) {
	ctx := context.Background()
	r := &NotificationPolicyResource{}
	dto := fullyPopulatedPolicyDto()
	channelID := dto.Escalation.Steps[0].ChannelIds[0].String()

	chList := types.ListValueMust(types.StringType, []attr.Value{types.StringValue(channelID)})
	priorStep, _ := types.ObjectValue(escalationStepObjectType().AttrTypes, map[string]attr.Value{
		"channel_ids":             chList,
		"delay_minutes":           types.Int64Null(),
		"require_ack":             types.BoolNull(),
		"repeat_interval_seconds": types.Int64Null(),
	})
	model := &NotificationPolicyModel{
		Escalation: types.ListValueMust(escalationStepObjectType(), []attr.Value{priorStep}),
		MatchRules: types.ListNull(matchRuleObjectType()),
	}
	r.mapToState(ctx, model, dto)

	var got []escalationStepModel
	_ = model.Escalation.ElementsAs(ctx, &got, false)
	if !got[0].DelayMinutes.IsNull() {
		t.Errorf("DelayMinutes = %v, want null preserved", got[0].DelayMinutes)
	}
	if !got[0].RequireAck.IsNull() {
		t.Errorf("RequireAck = %v, want null preserved", got[0].RequireAck)
	}
	if !got[0].RepeatIntervalSeconds.IsNull() {
		t.Errorf("RepeatIntervalSeconds = %v, want null preserved", got[0].RepeatIntervalSeconds)
	}
}

func TestNotificationPolicy_MapToState_EmptyStepsBecomesNull(t *testing.T) {
	ctx := context.Background()
	r := &NotificationPolicyResource{}
	dto := fullyPopulatedPolicyDto()
	dto.Escalation.Steps = nil
	dto.MatchRules = nil

	model := &NotificationPolicyModel{}
	r.mapToState(ctx, model, dto)
	if !model.Escalation.IsNull() {
		t.Errorf("Escalation = %v, want null", model.Escalation)
	}
	if !model.MatchRules.IsNull() {
		t.Errorf("MatchRules = %v, want null", model.MatchRules)
	}
}

func TestNotificationPolicy_MapToState_Idempotent(t *testing.T) {
	ctx := context.Background()
	r := &NotificationPolicyResource{}
	dto := fullyPopulatedPolicyDto()

	first := &NotificationPolicyModel{}
	r.mapToState(ctx, first, dto)
	second := *first
	r.mapToState(ctx, &second, dto)

	if !first.Escalation.Equal(second.Escalation) {
		t.Errorf("escalation not idempotent")
	}
	if !first.MatchRules.Equal(second.MatchRules) {
		t.Errorf("match rules not idempotent")
	}
}
