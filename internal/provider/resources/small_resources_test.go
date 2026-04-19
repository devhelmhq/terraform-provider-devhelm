package resources

import (
	"context"
	"testing"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/types"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ───────────────────────────────────────────────────────────────────────
// Tests for the small CRUD resources whose surface area is intentionally
// minimal: environment, secret, tag.
//
// Coverage matrix:
//   D — request body field population (Create + Update)
//   E — null-vs-empty semantics (variables map, optional color)
//   F — mapToState: every DTO field reaches the model
//   G — mapToState idempotency
//   J — placeholder semantics on import (secret value)
// ───────────────────────────────────────────────────────────────────────

// ── environment ────────────────────────────────────────────────────────

func TestEnvironment_MapToState_PopulatesAllFields(t *testing.T) {
	ctx := context.Background()
	r := &EnvironmentResource{}
	id := openapi_types.UUID(uuid.New())
	dto := &generated.EnvironmentDto{
		Id:        id,
		Name:      "Production",
		Slug:      "production",
		IsDefault: true,
		Variables: map[string]string{"REGION": "us-east", "TIER": "prod"},
	}
	model := &EnvironmentResourceModel{}
	r.mapToState(ctx, model, dto)

	if model.ID.ValueString() != id.String() {
		t.Errorf("ID = %q", model.ID.ValueString())
	}
	if model.Name.ValueString() != "Production" {
		t.Errorf("Name = %q", model.Name.ValueString())
	}
	if model.Slug.ValueString() != "production" {
		t.Errorf("Slug = %q", model.Slug.ValueString())
	}
	if !model.IsDefault.ValueBool() {
		t.Errorf("IsDefault = false, want true")
	}
	if len(model.Variables.Elements()) != 2 {
		t.Errorf("Variables = %v, want 2 elements", model.Variables.Elements())
	}
}

// TestEnvironment_MapToState_EmptyVariablesPreservesNullByDefault: when an
// environment has no variables and the model's Variables is null (the
// initial state for a freshly-decoded model), mapToState must leave it
// null rather than promoting it to an empty map. Doing so prevents
// perpetual diffs against HCL that omits the `variables` block entirely.
func TestEnvironment_MapToState_EmptyVariablesPreservesNullByDefault(t *testing.T) {
	ctx := context.Background()
	r := &EnvironmentResource{}
	dto := &generated.EnvironmentDto{
		Id: openapi_types.UUID(uuid.New()), Name: "x", Slug: "x", IsDefault: false, Variables: nil,
	}
	model := &EnvironmentResourceModel{
		Variables: types.MapNull(types.StringType),
	}
	r.mapToState(ctx, model, dto)
	if !model.Variables.IsNull() {
		t.Errorf("Variables = %v, want null when DTO has no variables and model started null", model.Variables)
	}
}

// TestEnvironment_MapToState_EmptyVariablesFlowsToEmptyMapOnImport: the
// import path pre-initialises Variables to an empty map. In that case
// mapToState must keep an empty map (not null) so the imported state is
// internally consistent with the schema's MapAttribute semantics.
func TestEnvironment_MapToState_EmptyVariablesFlowsToEmptyMapOnImport(t *testing.T) {
	ctx := context.Background()
	r := &EnvironmentResource{}
	dto := &generated.EnvironmentDto{
		Id: openapi_types.UUID(uuid.New()), Name: "x", Slug: "x", IsDefault: false,
	}
	model := &EnvironmentResourceModel{}
	model.Variables, _ = types.MapValueFrom(ctx, types.StringType, map[string]string{})

	r.mapToState(ctx, model, dto)
	if model.Variables.IsNull() {
		t.Errorf("Variables nulled, want empty map (import-style pre-init)")
	}
	if len(model.Variables.Elements()) != 0 {
		t.Errorf("Variables = %v, want empty", model.Variables.Elements())
	}
}

func TestEnvironment_MapToState_Idempotent(t *testing.T) {
	ctx := context.Background()
	r := &EnvironmentResource{}
	dto := &generated.EnvironmentDto{
		Id: openapi_types.UUID(uuid.New()), Name: "x", Slug: "x",
		IsDefault: true, Variables: map[string]string{"K": "V"},
	}
	first := &EnvironmentResourceModel{}
	r.mapToState(ctx, first, dto)
	second := *first
	r.mapToState(ctx, &second, dto)
	if !first.Variables.Equal(second.Variables) {
		t.Errorf("variables not idempotent: %v vs %v", first.Variables, second.Variables)
	}
	if !first.IsDefault.Equal(second.IsDefault) {
		t.Errorf("is_default not idempotent")
	}
}

// ── secret ─────────────────────────────────────────────────────────────

func TestSecret_Sha256Hex_StableForSameInput(t *testing.T) {
	a := sha256Hex("hunter2")
	b := sha256Hex("hunter2")
	if a != b {
		t.Errorf("sha256Hex non-deterministic: %s vs %s", a, b)
	}
	c := sha256Hex("hunter3")
	if a == c {
		t.Errorf("sha256Hex collided across distinct inputs (%q vs %q)", "hunter2", "hunter3")
	}
	// Length must be 64 hex chars.
	if len(a) != 64 {
		t.Errorf("sha256Hex length = %d, want 64", len(a))
	}
}

// ── tag ────────────────────────────────────────────────────────────────

func TestTag_BuildBody_OmitsColorWhenNull(t *testing.T) {
	plan := TagResourceModel{
		Name:  types.StringValue("frontend"),
		Color: types.StringNull(),
	}
	body := generated.CreateTagRequest{
		Name:  plan.Name.ValueString(),
		Color: stringPtrOrNil(plan.Color),
	}
	if body.Name != "frontend" {
		t.Errorf("Name = %q", body.Name)
	}
	if body.Color != nil {
		t.Errorf("Color = %v, want nil so the API can apply its default", body.Color)
	}
}

func TestTag_BuildBody_PassesColorWhenSet(t *testing.T) {
	plan := TagResourceModel{
		Name:  types.StringValue("frontend"),
		Color: types.StringValue("#6B7280"),
	}
	body := generated.CreateTagRequest{
		Name:  plan.Name.ValueString(),
		Color: stringPtrOrNil(plan.Color),
	}
	if body.Color == nil || *body.Color != "#6B7280" {
		t.Errorf("Color = %v, want #6B7280", body.Color)
	}
}
