package resources

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	openapi_types "github.com/oapi-codegen/runtime/types"
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

// TestNormalizeJSON_PreservesZeroValues guards the boundary between "null"
// (which the API treats as semantically absent and which we strip) and "0,
// false, empty string" (which are meaningful values and must round-trip).
// A previous regression here would have caused frequency_seconds=0 to be
// silently dropped on the way to the wire.
func TestNormalizeJSON_PreservesZeroValues(t *testing.T) {
	in := `{"a":0,"b":false,"c":"","d":[],"e":{}}`
	got := normalizeJSON([]byte(in))
	for _, want := range []string{`"a":0`, `"b":false`, `"c":""`, `"d":[]`, `"e":{}`} {
		if !strings.Contains(got, want) {
			t.Errorf("normalizeJSON dropped %s: got %s", want, got)
		}
	}
}

// TestNormalizeJSON_HeterogeneousArray covers the assertion-list shape:
// an array that interleaves nulls, primitives, and objects with their own
// nested nulls. All nulls (top-level and nested) must be removed; all
// other values preserved.
func TestNormalizeJSON_HeterogeneousArray(t *testing.T) {
	in := `[null,1,"x",{"a":null,"b":2},null]`
	got := normalizeJSON([]byte(in))
	if strings.Contains(got, "null") {
		t.Errorf("nulls survived: %s", got)
	}
	for _, want := range []string{`1`, `"x"`, `"b":2`} {
		if !strings.Contains(got, want) {
			t.Errorf("dropped %s: %s", want, got)
		}
	}
}

// TestNormalizeJSON_Idempotent guards the property that normalizing an
// already-normalized value is a no-op. Re-normalizing happens implicitly
// during repeated Read calls, and any divergence here would re-introduce
// the perpetual-diff bug class.
func TestNormalizeJSON_Idempotent(t *testing.T) {
	in := `{"deeply":{"nested":[{"k":1,"q":[2,3]}]}}`
	once := normalizeJSON([]byte(in))
	twice := normalizeJSON([]byte(once))
	if once != twice {
		t.Errorf("not idempotent:\n  once  = %s\n  twice = %s", once, twice)
	}
}

// TestNormalizeJSON_PreservesDiscriminatorTypeField is critical: the
// monitor config / auth / assertion union types use a `type` discriminator
// to choose the variant. If normalizeJSON ever stripped that field (e.g.
// because of an over-eager null filter), the round-trip would silently
// lose union typing on read-back and produce permanent diffs.
func TestNormalizeJSON_PreservesDiscriminatorTypeField(t *testing.T) {
	in := `{"type":"bearer","token":"abc","extra":null}`
	got := normalizeJSON([]byte(in))
	if !strings.Contains(got, `"type":"bearer"`) {
		t.Errorf("dropped discriminator: %s", got)
	}
	if strings.Contains(got, "extra") {
		t.Errorf("kept null-valued extra: %s", got)
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

// ── int32OrZero / int32Value ────────────────────────────────────────────

func TestInt32OrZero(t *testing.T) {
	// int32OrZero is used for fields where the API requires a value; null
	// in Terraform must collapse to 0 (the API's documented default).
	if got := int32OrZero(types.Int64Null()); got != 0 {
		t.Errorf("null = %d, want 0", got)
	}
	if got := int32OrZero(types.Int64Unknown()); got != 0 {
		t.Errorf("unknown = %d, want 0", got)
	}
	if got := int32OrZero(types.Int64Value(7)); got != 7 {
		t.Errorf("7 = %d, want 7", got)
	}
}

func TestInt32Value(t *testing.T) {
	if got := int32Value(nil); !got.IsNull() {
		t.Errorf("nil = %v, want null", got)
	}
	v := int32(42)
	if got := int32Value(&v); got.IsNull() || got.ValueInt64() != 42 {
		t.Errorf("42 = %v, want 42", got)
	}
}

// ── float32PtrOrNil / float32Value ──────────────────────────────────────

func TestFloat32PtrOrNil(t *testing.T) {
	if got := float32PtrOrNil(types.Float64Null()); got != nil {
		t.Errorf("null = %v, want nil", got)
	}
	if got := float32PtrOrNil(types.Float64Unknown()); got != nil {
		t.Errorf("unknown = %v, want nil", got)
	}
	if got := float32PtrOrNil(types.Float64Value(1.5)); got == nil || *got != 1.5 {
		t.Errorf("1.5 = %v, want 1.5", got)
	}
}

func TestFloat32Value(t *testing.T) {
	if got := float32Value(nil); !got.IsNull() {
		t.Errorf("nil = %v, want null", got)
	}
	v := float32(2.5)
	if got := float32Value(&v); got.IsNull() || got.ValueFloat64() != 2.5 {
		t.Errorf("2.5 = %v, want 2.5", got)
	}
}

// ── boolValue ───────────────────────────────────────────────────────────

func TestBoolValue(t *testing.T) {
	if got := boolValue(nil); !got.IsNull() {
		t.Errorf("nil = %v, want null", got)
	}
	t1, f1 := true, false
	if got := boolValue(&t1); got.IsNull() || !got.ValueBool() {
		t.Errorf("true = %v", got)
	}
	if got := boolValue(&f1); got.IsNull() || got.ValueBool() {
		t.Errorf("false = %v", got)
	}
}

// ── uuidPtrValue ────────────────────────────────────────────────────────

func TestUuidPtrValue(t *testing.T) {
	if got := uuidPtrValue(nil); !got.IsNull() {
		t.Errorf("nil = %v, want null", got)
	}
	id := uuid.New()
	if got := uuidPtrValue(&id); got.IsNull() || got.ValueString() != id.String() {
		t.Errorf("uuid = %v, want %s", got, id)
	}
}

// ── typedString*: enum<->string conversions ─────────────────────────────

type fakeEnum string

func TestTypedStringPtrValue(t *testing.T) {
	if got := typedStringPtrValue[fakeEnum](nil); !got.IsNull() {
		t.Errorf("nil = %v, want null", got)
	}
	v := fakeEnum("alpha")
	if got := typedStringPtrValue(&v); got.IsNull() || got.ValueString() != "alpha" {
		t.Errorf("alpha = %v, want 'alpha'", got)
	}
}

func TestTypedStringPtrOrNil(t *testing.T) {
	if got := typedStringPtrOrNil[fakeEnum](types.StringNull()); got != nil {
		t.Errorf("null = %v, want nil", got)
	}
	if got := typedStringPtrOrNil[fakeEnum](types.StringUnknown()); got != nil {
		t.Errorf("unknown = %v, want nil", got)
	}
	got := typedStringPtrOrNil[fakeEnum](types.StringValue("beta"))
	if got == nil || string(*got) != "beta" {
		t.Errorf("beta = %v, want 'beta'", got)
	}
}

// ── stringListToSlice / stringSetToSlice ────────────────────────────────

func TestStringListToSlice(t *testing.T) {
	if got := stringListToSlice(types.ListNull(types.StringType)); got != nil {
		t.Errorf("null = %v, want nil", got)
	}
	if got := stringListToSlice(types.ListUnknown(types.StringType)); got != nil {
		t.Errorf("unknown = %v, want nil", got)
	}
	list := types.ListValueMust(types.StringType, []attr.Value{
		types.StringValue("a"), types.StringValue("b"),
	})
	got := stringListToSlice(list)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("got %v, want [a b]", got)
	}
}

func TestStringSetToSlice(t *testing.T) {
	if got := stringSetToSlice(types.SetNull(types.StringType)); got != nil {
		t.Errorf("null = %v, want nil", got)
	}
	set := types.SetValueMust(types.StringType, []attr.Value{
		types.StringValue("x"), types.StringValue("y"),
	})
	got := stringSetToSlice(set)
	if len(got) != 2 {
		t.Errorf("got %v, want len=2", got)
	}
}

// ── stringSliceToPtr / stringSliceToPtrFromSet ──────────────────────────

func TestStringSliceToPtr_NullVsEmpty(t *testing.T) {
	// null/unknown returns nil — the API treats null as "preserve current".
	if got := stringSliceToPtr(types.ListNull(types.StringType)); got != nil {
		t.Errorf("null = %v, want nil", got)
	}
	if got := stringSliceToPtr(types.ListUnknown(types.StringType)); got != nil {
		t.Errorf("unknown = %v, want nil", got)
	}
	// Explicit empty list returns &[]*string{} — the API treats this as
	// "clear all", which is the documented contract for collection fields.
	got := stringSliceToPtr(types.ListValueMust(types.StringType, []attr.Value{}))
	if got == nil {
		t.Fatal("explicit empty list = nil, want non-nil empty slice (else API can't distinguish 'clear' from 'preserve')")
	}
	if len(*got) != 0 {
		t.Errorf("explicit empty list = %v, want empty slice", *got)
	}
}

func TestStringSliceToPtr_PassesThroughValues(t *testing.T) {
	in := types.ListValueMust(types.StringType, []attr.Value{
		types.StringValue("us-east"), types.StringValue("eu-west"),
	})
	got := stringSliceToPtr(in)
	if got == nil || len(*got) != 2 {
		t.Fatalf("got %v, want 2 elements", got)
	}
	if (*got)[0] != "us-east" || (*got)[1] != "eu-west" {
		t.Errorf("element values incorrect: %v", got)
	}
}

func TestStringSliceToPtrFromSet(t *testing.T) {
	if got := stringSliceToPtrFromSet(types.SetNull(types.StringType)); got != nil {
		t.Errorf("null = %v, want nil", got)
	}
	in := types.SetValueMust(types.StringType, []attr.Value{
		types.StringValue("monitor.up"),
	})
	got := stringSliceToPtrFromSet(in)
	if got == nil || len(*got) != 1 || (*got)[0] != "monitor.up" {
		t.Errorf("got %v, want [monitor.up]", got)
	}
}

// ── stringMapToPtr ──────────────────────────────────────────────────────

func TestStringMapToPtr(t *testing.T) {
	if got := stringMapToPtr(types.MapNull(types.StringType)); got != nil {
		t.Errorf("null = %v, want nil", got)
	}
	in := types.MapValueMust(types.StringType, map[string]attr.Value{
		"FOO": types.StringValue("bar"),
		"BAZ": types.StringValue(""),
	})
	got := stringMapToPtr(in)
	if got == nil {
		t.Fatal("got nil, want non-nil map")
	}
	if v, ok := (*got)["FOO"]; !ok || v == nil || *v != "bar" {
		t.Errorf("FOO = %v, want 'bar'", v)
	}
	// Empty string values must round-trip as &"" (not collapsed to nil) so
	// callers can distinguish "set to empty" from "absent".
	if v, ok := (*got)["BAZ"]; !ok || v == nil || *v != "" {
		t.Errorf("BAZ = %v, want pointer to empty string", v)
	}
}

// ── uuidSliceFromStringListChecked / uuidListToSliceChecked ─────────────

func TestUUIDSliceFromStringListChecked_Happy(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	in := types.ListValueMust(types.StringType, []attr.Value{
		types.StringValue(a.String()),
		types.StringValue(b.String()),
	})
	got, err := uuidSliceFromStringListChecked(in, "ids")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got == nil || len(*got) != 2 {
		t.Fatalf("got %v, want len=2", got)
	}
	if uuid.UUID((*got)[0]) != a || uuid.UUID((*got)[1]) != b {
		t.Errorf("UUIDs not preserved")
	}
}

func TestUUIDSliceFromStringListChecked_NullInputReturnsNilNoError(t *testing.T) {
	got, err := uuidSliceFromStringListChecked(types.ListNull(types.StringType), "ids")
	if err != nil || got != nil {
		t.Errorf("got (%v, %v), want (nil, nil)", got, err)
	}
}

func TestUUIDSliceFromStringListChecked_InvalidUUIDIncludesIndexAndField(t *testing.T) {
	in := types.ListValueMust(types.StringType, []attr.Value{
		types.StringValue(uuid.New().String()),
		types.StringValue("not-a-uuid"),
	})
	_, err := uuidSliceFromStringListChecked(in, "alert_channel_ids")
	if err == nil {
		t.Fatal("want error for invalid UUID")
	}
	msg := err.Error()
	if !strings.Contains(msg, "alert_channel_ids") || !strings.Contains(msg, "[1]") {
		t.Errorf("error %q must include field name and zero-based index", msg)
	}
}

func TestUUIDListToSliceChecked_PathParity(t *testing.T) {
	a := uuid.New()
	in := types.ListValueMust(types.StringType, []attr.Value{types.StringValue(a.String())})
	got, err := uuidListToSliceChecked(in, "tag_ids")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 1 || got[0] != a {
		t.Errorf("got %v, want [%s]", got, a)
	}

	bad := types.ListValueMust(types.StringType, []attr.Value{types.StringValue("xx")})
	_, err = uuidListToSliceChecked(bad, "tag_ids")
	if err == nil || !strings.Contains(err.Error(), "tag_ids") {
		t.Errorf("invalid → err must mention field; got %v", err)
	}
}

// ── emailsFromStringList ────────────────────────────────────────────────

func TestEmailsFromStringList(t *testing.T) {
	if got := emailsFromStringList(types.ListNull(types.StringType)); got != nil {
		t.Errorf("null = %v, want nil", got)
	}
	in := types.ListValueMust(types.StringType, []attr.Value{
		types.StringValue("a@example.com"),
		types.StringValue("b@example.com"),
	})
	got := emailsFromStringList(in)
	if got == nil || len(got) != 2 {
		t.Fatalf("got %v, want 2 emails", got)
	}
	if string(got[0]) != "a@example.com" || string(got[1]) != "b@example.com" {
		t.Errorf("emails not preserved: %v", got)
	}
}

// ── ptrStringSliceToList / ptrUUIDSliceToList ───────────────────────────

func TestPtrStringSliceToList(t *testing.T) {
	ctx := context.Background()
	// Nil pointer and empty slice both collapse to typed-null list, which
	// is the framework's preferred shape for "no value" so empty lists
	// don't trigger spurious diffs against a HCL-omitted attribute.
	if got := ptrStringSliceToList(ctx, nil); !got.IsNull() {
		t.Errorf("nil = %v, want null", got)
	}
	empty := []string{}
	if got := ptrStringSliceToList(ctx, &empty); !got.IsNull() {
		t.Errorf("empty slice = %v, want null", got)
	}
	in := []string{"a", "b"}
	got := ptrStringSliceToList(ctx, &in)
	if got.IsNull() || len(got.Elements()) != 2 {
		t.Fatalf("got %v, want 2 elements", got)
	}
}

func TestPtrUUIDSliceToList(t *testing.T) {
	ctx := context.Background()
	if got := ptrUUIDSliceToList(ctx, nil); !got.IsNull() {
		t.Errorf("nil = %v, want null", got)
	}
	id := openapi_types.UUID(uuid.New())
	in := []openapi_types.UUID{id}
	got := ptrUUIDSliceToList(ctx, &in)
	if got.IsNull() || len(got.Elements()) != 1 {
		t.Fatalf("got %v, want 1 element", got)
	}
	first, ok := got.Elements()[0].(types.String)
	if !ok || first.ValueString() != id.String() {
		t.Errorf("first element = %v, want %s", got.Elements()[0], id)
	}
}

// ── stringSliceToSet ────────────────────────────────────────────────────

func TestStringSliceToSet(t *testing.T) {
	ctx := context.Background()
	if got := stringSliceToSet(ctx, nil); !got.IsNull() {
		t.Errorf("nil = %v, want null", got)
	}
	if got := stringSliceToSet(ctx, []string{}); !got.IsNull() {
		t.Errorf("empty = %v, want null", got)
	}
	got := stringSliceToSet(ctx, []string{"monitor.up", "monitor.down"})
	if got.IsNull() || len(got.Elements()) != 2 {
		t.Fatalf("got %v, want 2 elements", got)
	}
}

// ── preserveListOrder ───────────────────────────────────────────────────

// stringList is a small helper for constructing a populated types.List of
// strings in a single expression — keeps the assertions below readable.
func stringList(t *testing.T, ctx context.Context, vs ...string) types.List {
	t.Helper()
	elems := make([]attr.Value, len(vs))
	for i, v := range vs {
		elems[i] = types.StringValue(v)
	}
	out, diags := types.ListValueFrom(ctx, types.StringType, elems)
	if diags.HasError() {
		t.Fatalf("stringList: %v", diags)
	}
	return out
}

func listToSlice(t *testing.T, ctx context.Context, l types.List) []string {
	t.Helper()
	var out []string
	if !l.IsNull() && !l.IsUnknown() {
		l.ElementsAs(ctx, &out, false)
	}
	return out
}

// TestPreserveListOrder_KeepsExistingOrderWhenSetMatches guards the
// "Provider produced inconsistent result after apply" failure mode for
// API fields whose elements come back in a server-chosen (set-like) order
// but appear in HCL as an ordered list. As long as the *set* of IDs the
// API returns matches the user's plan, we must echo back the plan's order.
func TestPreserveListOrder_KeepsExistingOrderWhenSetMatches(t *testing.T) {
	ctx := context.Background()
	existing := stringList(t, ctx, "9f05422e", "e3c350f0")
	apiIDs := []string{"e3c350f0", "9f05422e"}

	got := preserveListOrder(ctx, existing, apiIDs)
	gotSlice := listToSlice(t, ctx, got)
	if len(gotSlice) != 2 || gotSlice[0] != "9f05422e" || gotSlice[1] != "e3c350f0" {
		t.Fatalf("preserveListOrder dropped existing order: got %v", gotSlice)
	}
}

// TestPreserveListOrder_FallsBackToApiOrderWhenSetsDiffer covers genuine
// drift — the server has a different element set, so we adopt its order
// as the new source of truth and let the next plan show the real diff.
func TestPreserveListOrder_FallsBackToApiOrderWhenSetsDiffer(t *testing.T) {
	ctx := context.Background()
	existing := stringList(t, ctx, "id-a", "id-b")
	apiIDs := []string{"id-c", "id-b"}

	got := preserveListOrder(ctx, existing, apiIDs)
	gotSlice := listToSlice(t, ctx, got)
	if len(gotSlice) != 2 || gotSlice[0] != "id-c" || gotSlice[1] != "id-b" {
		t.Fatalf("expected api order on drift, got %v", gotSlice)
	}
}

// TestPreserveListOrder_NullExistingTakesApiOrder covers the import / first
// read path: there is no prior state, so the API's order becomes the
// initial source of truth.
func TestPreserveListOrder_NullExistingTakesApiOrder(t *testing.T) {
	ctx := context.Background()
	got := preserveListOrder(ctx, types.ListNull(types.StringType), []string{"a", "b"})
	gotSlice := listToSlice(t, ctx, got)
	if len(gotSlice) != 2 || gotSlice[0] != "a" || gotSlice[1] != "b" {
		t.Fatalf("null-existing should take api order verbatim, got %v", gotSlice)
	}
}

// TestPreserveListOrder_DifferingLengthsTakeApiOrder is a stricter form of
// the drift case: a plan with 1 element but an API with 2 (or vice versa)
// can never be a pure reorder, so we always fall through to API order.
func TestPreserveListOrder_DifferingLengthsTakeApiOrder(t *testing.T) {
	ctx := context.Background()
	existing := stringList(t, ctx, "only-one")
	got := preserveListOrder(ctx, existing, []string{"a", "b", "c"})
	gotSlice := listToSlice(t, ctx, got)
	if len(gotSlice) != 3 {
		t.Fatalf("expected api 3-element order, got %v", gotSlice)
	}
}

// TestPreserveListOrder_EmptyApiCollapsesToEmpty is the "user cleared the
// list" path. The API echoes back nothing, so we want an empty list (not a
// null) so Terraform stops trying to manage the prior elements.
func TestPreserveListOrder_EmptyApiCollapsesToEmpty(t *testing.T) {
	ctx := context.Background()
	existing := stringList(t, ctx, "a", "b")
	got := preserveListOrder(ctx, existing, []string{})
	if got.IsNull() {
		t.Fatalf("empty api should yield empty list, got null")
	}
	if len(got.Elements()) != 0 {
		t.Fatalf("expected zero elements, got %d", len(got.Elements()))
	}
}
