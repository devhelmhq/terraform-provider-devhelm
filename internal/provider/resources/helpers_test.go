package resources

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ── normalizeJSON ───────────────────────────────────────────────────────

func TestNormalizeJSON_StripsNullKeysAtAllDepths(t *testing.T) {
	in := `{"a":1,"b":null,"c":{"d":null,"e":2,"f":[null,{"g":null,"h":3}]}}`
	got := normalizeJSON([]byte(in))
	// Null keys at any depth must be removed; non-null neighbors preserved.
	if strings.Contains(got, "null") {
		t.Errorf("normalizeJSON did not strip nulls: %s", got)
	}
	if !strings.Contains(got, `"a":1`) || !strings.Contains(got, `"e":2`) || !strings.Contains(got, `"h":3`) {
		t.Errorf("normalizeJSON dropped non-null fields: %s", got)
	}
}

func TestNormalizeJSON_PassesThroughOnInvalidJSON(t *testing.T) {
	in := `not json`
	got := normalizeJSON([]byte(in))
	if got != in {
		t.Errorf("normalizeJSON(invalid) = %q, want passthrough %q", got, in)
	}
}

func TestNormalizeJSON_EmptyAndNullInputs(t *testing.T) {
	if got := normalizeJSON([]byte(`{}`)); got != `{}` {
		t.Errorf("empty object got %q", got)
	}
	if got := normalizeJSON([]byte(`null`)); got != `null` {
		// `null` parses as nil; stripNullsAny returns nil; json.Marshal(nil) = "null".
		t.Errorf("null literal got %q, want 'null'", got)
	}
}

// ── parseUUIDPtrChecked ─────────────────────────────────────────────────

func TestParseUUIDPtrChecked_NullAndEmptyReturnNil(t *testing.T) {
	cases := []types.String{types.StringNull(), types.StringUnknown(), types.StringValue("")}
	for _, v := range cases {
		got, err := parseUUIDPtrChecked(v, "field")
		if err != nil {
			t.Errorf("parseUUIDPtrChecked(%v) err = %v, want nil", v, err)
		}
		if got != nil {
			t.Errorf("parseUUIDPtrChecked(%v) = %v, want nil", v, got)
		}
	}
}

func TestParseUUIDPtrChecked_ValidUUID(t *testing.T) {
	id := uuid.New()
	got, err := parseUUIDPtrChecked(types.StringValue(id.String()), "field")
	if err != nil {
		t.Fatalf("parseUUIDPtrChecked err = %v", err)
	}
	if got == nil || *got != id {
		t.Errorf("parseUUIDPtrChecked = %v, want %v", got, id)
	}
}

func TestParseUUIDPtrChecked_InvalidUUIDIncludesFieldName(t *testing.T) {
	_, err := parseUUIDPtrChecked(types.StringValue("not-a-uuid"), "monitor_id")
	if err == nil {
		t.Fatal("expected error for invalid UUID")
	}
	if !strings.Contains(err.Error(), "monitor_id") {
		t.Errorf("error %q should include field name", err.Error())
	}
}

// ── descriptionPtrForClear ──────────────────────────────────────────────

func TestDescriptionPtrForClear_NullSendsEmptyString(t *testing.T) {
	got := descriptionPtrForClear(types.StringNull())
	if got == nil {
		t.Fatal("got nil pointer; null plan should map to &\"\" to clear the API value")
	}
	if *got != "" {
		t.Errorf("got %q, want \"\" (empty-string clears on the API side)", *got)
	}
}

func TestDescriptionPtrForClear_UnknownReturnsNil(t *testing.T) {
	got := descriptionPtrForClear(types.StringUnknown())
	if got != nil {
		t.Errorf("got %v, want nil (unknown defers to a later apply)", got)
	}
}

func TestDescriptionPtrForClear_ConcreteValuePassesThrough(t *testing.T) {
	got := descriptionPtrForClear(types.StringValue("hello"))
	if got == nil || *got != "hello" {
		t.Errorf("got %v, want pointer to 'hello'", got)
	}
}

// ── stringValueClearable ────────────────────────────────────────────────

func TestStringValueClearable_NilReturnsNull(t *testing.T) {
	got := stringValueClearable(nil)
	if !got.IsNull() {
		t.Errorf("got %v, want null", got)
	}
}

func TestStringValueClearable_EmptyStringReturnsNull(t *testing.T) {
	empty := ""
	got := stringValueClearable(&empty)
	if !got.IsNull() {
		t.Errorf("got %v, want null (empty string round-trips as cleared)", got)
	}
}

func TestStringValueClearable_NonEmptyPasses(t *testing.T) {
	v := "hello"
	got := stringValueClearable(&v)
	if got.IsNull() || got.ValueString() != "hello" {
		t.Errorf("got %v, want StringValue('hello')", got)
	}
}

// Round-trip invariant: descriptionPtrForClear(null) → API stores "" → API
// returns null on read → stringValueClearable(nil) → null. The invariant
// ensures plan==state across an apply that clears.
func TestDescriptionRoundTrip_ClearIsStable(t *testing.T) {
	plan := types.StringNull()
	apiBody := descriptionPtrForClear(plan)
	if apiBody == nil || *apiBody != "" {
		t.Fatalf("unexpected API body for null plan: %v", apiBody)
	}
	// Simulate API reading back null after clearing.
	state := stringValueClearable(nil)
	if !plan.Equal(state) {
		t.Errorf("clear round-trip drift: plan=%v state=%v", plan, state)
	}
}

// Round-trip invariant: descriptionPtrForClear("foo") → API stores "foo" →
// returns "foo" → stringValueClearable("foo") → StringValue("foo").
func TestDescriptionRoundTrip_SetIsStable(t *testing.T) {
	plan := types.StringValue("foo")
	apiBody := descriptionPtrForClear(plan)
	if apiBody == nil || *apiBody != "foo" {
		t.Fatalf("unexpected API body for set plan: %v", apiBody)
	}
	stored := *apiBody
	state := stringValueClearable(&stored)
	if !plan.Equal(state) {
		t.Errorf("set round-trip drift: plan=%v state=%v", plan, state)
	}
}

// ── int32PtrOrNil / boolPtrOrNil / stringPtrOrNil ───────────────────────

func TestInt32PtrOrNil(t *testing.T) {
	if got := int32PtrOrNil(types.Int64Null()); got != nil {
		t.Errorf("null = %v, want nil", got)
	}
	if got := int32PtrOrNil(types.Int64Unknown()); got != nil {
		t.Errorf("unknown = %v, want nil", got)
	}
	if got := int32PtrOrNil(types.Int64Value(42)); got == nil || *got != 42 {
		t.Errorf("42 = %v, want 42", got)
	}
}

func TestBoolPtrOrNil(t *testing.T) {
	if got := boolPtrOrNil(types.BoolNull()); got != nil {
		t.Errorf("null = %v, want nil", got)
	}
	if got := boolPtrOrNil(types.BoolValue(true)); got == nil || *got != true {
		t.Errorf("true = %v, want true", got)
	}
}

func TestStringPtrOrNil(t *testing.T) {
	if got := stringPtrOrNil(types.StringNull()); got != nil {
		t.Errorf("null = %v, want nil", got)
	}
	if got := stringPtrOrNil(types.StringValue("x")); got == nil || *got != "x" {
		t.Errorf("'x' = %v, want 'x'", got)
	}
}

func TestStringValue(t *testing.T) {
	if got := stringValue(nil); !got.IsNull() {
		t.Errorf("nil = %v, want null", got)
	}
	v := "y"
	if got := stringValue(&v); got.IsNull() || got.ValueString() != "y" {
		t.Errorf("'y' = %v, want 'y'", got)
	}
}
