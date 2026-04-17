package resources

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// normalizeJSON strips null-valued keys from a JSON blob so the API-returned
// config doesn't diverge from the plan due to extra null fields. Recurses
// through nested objects and arrays so nulls inside slices (e.g. heterogeneous
// assertion lists) are also cleaned.
func normalizeJSON(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	stripped := stripNullsAny(v)
	out, err := json.Marshal(stripped)
	if err != nil {
		return string(raw)
	}
	return string(out)
}

func stripNullsAny(v any) any {
	switch tv := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(tv))
		for k, val := range tv {
			if val == nil {
				continue
			}
			out[k] = stripNullsAny(val)
		}
		return out
	case []any:
		out := make([]any, 0, len(tv))
		for _, val := range tv {
			if val == nil {
				continue
			}
			out = append(out, stripNullsAny(val))
		}
		return out
	default:
		return v
	}
}

func stringPtrOrNil(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

func boolPtrOrNil(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	b := v.ValueBool()
	return &b
}

func int32PtrOrNil(v types.Int64) *int32 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := int32(v.ValueInt64())
	return &i
}

func int32OrZero(v types.Int64) int32 {
	if v.IsNull() || v.IsUnknown() {
		return 0
	}
	return int32(v.ValueInt64())
}

func float32PtrOrNil(v types.Float64) *float32 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	f := float32(v.ValueFloat64())
	return &f
}

func stringValue(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

// descriptionPtrForClear converts a Terraform string attribute into the API
// "description" field for endpoints that support the
// `null preserves current, empty string clears` contract. The Terraform
// contract is "the config is the source of truth": a null/absent attribute in
// the plan must result in the resource attribute being null on the server. We
// translate that intent into an explicit empty-string clear.
//
// Behavior:
//
//   - Unknown plan value           → return nil (defer to a later apply once
//     the value is known).
//   - Null plan value              → return pointer to "" (instructs API to
//     clear the existing description).
//   - Concrete (incl. "") value    → return pointer to that value.
//
// Pair with stringValueClearable on read-back to avoid perpetual diffs.
func descriptionPtrForClear(v types.String) *string {
	if v.IsUnknown() {
		return nil
	}
	if v.IsNull() {
		empty := ""
		return &empty
	}
	s := v.ValueString()
	return &s
}

// stringValueClearable normalizes the API's read-back of a clearable field so
// that nil and empty string both map to types.StringNull(). The API normalizes
// "" → null on write and returns null on read once cleared, so empty strings
// would otherwise produce a perpetual diff against an HCL-omitted attribute.
//
// Empty string is forbidden in HCL for these fields (see the schema-level
// validator); this helper is the read-back counterpart.
func stringValueClearable(s *string) types.String {
	if s == nil || *s == "" {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

func int32Value(i *int32) types.Int64 {
	if i == nil {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*i))
}

func float32Value(f *float32) types.Float64 {
	if f == nil {
		return types.Float64Null()
	}
	return types.Float64Value(float64(*f))
}

func stringListToSlice(list types.List) []string {
	if list.IsNull() || list.IsUnknown() {
		return nil
	}
	var result []string
	for _, v := range list.Elements() {
		if sv, ok := v.(types.String); ok {
			result = append(result, sv.ValueString())
		}
	}
	return result
}

func stringSetToSlice(set types.Set) []string {
	if set.IsNull() || set.IsUnknown() {
		return nil
	}
	var result []string
	for _, v := range set.Elements() {
		if sv, ok := v.(types.String); ok {
			result = append(result, sv.ValueString())
		}
	}
	return result
}

func stringMapToPtr(m types.Map) *map[string]*string {
	if m.IsNull() || m.IsUnknown() {
		return nil
	}
	result := make(map[string]*string)
	for k, v := range m.Elements() {
		if sv, ok := v.(types.String); ok {
			s := sv.ValueString()
			result[k] = &s
		}
	}
	return &result
}

func uuidPtrValue(u *uuid.UUID) types.String {
	if u == nil {
		return types.StringNull()
	}
	return types.StringValue(u.String())
}

// parseUUIDPtrChecked converts a Terraform string attribute into an optional
// UUID, returning an error when the string is non-empty but fails to parse.
// This is the only UUID-parsing helper retained: a previous "silent" variant
// that swallowed parse errors masked user configuration bugs (the API would
// then receive a nil UUID, treat it as "no constraint", and apply could
// silently succeed against the wrong resource). Always surface the error.
func parseUUIDPtrChecked(v types.String, fieldName string) (*uuid.UUID, error) {
	if v.IsNull() || v.IsUnknown() {
		return nil, nil
	}
	s := v.ValueString()
	if s == "" {
		return nil, nil
	}
	u, err := uuid.Parse(s)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid UUID %q: %w", fieldName, s, err)
	}
	return &u, nil
}

func emailsFromStringList(list types.List) *[]openapi_types.Email {
	strs := stringListToSlice(list)
	if strs == nil {
		return nil
	}
	emails := make([]openapi_types.Email, len(strs))
	for i, s := range strs {
		emails[i] = openapi_types.Email(s)
	}
	return &emails
}

func stringSliceToPtr(list types.List) *[]*string {
	if list.IsNull() || list.IsUnknown() {
		return nil
	}
	var result []*string
	for _, v := range list.Elements() {
		if sv, ok := v.(types.String); ok {
			s := sv.ValueString()
			result = append(result, &s)
		}
	}
	return &result
}

// uuidSliceFromStringListChecked surfaces UUID parse errors instead of
// silently dropping invalid entries. The silent variant has been removed —
// see parseUUIDPtrChecked for the rationale.
func uuidSliceFromStringListChecked(list types.List, fieldName string) (*[]*uuid.UUID, error) {
	if list.IsNull() || list.IsUnknown() {
		return nil, nil
	}
	var result []*uuid.UUID
	for i, v := range list.Elements() {
		sv, ok := v.(types.String)
		if !ok || sv.IsNull() || sv.IsUnknown() {
			continue
		}
		u, err := uuid.Parse(sv.ValueString())
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: invalid UUID %q: %w", fieldName, i, sv.ValueString(), err)
		}
		uu := u
		result = append(result, &uu)
	}
	return &result, nil
}

func stringSliceToPtrFromSet(set types.Set) *[]*string {
	if set.IsNull() || set.IsUnknown() {
		return nil
	}
	var result []*string
	for _, v := range set.Elements() {
		if sv, ok := v.(types.String); ok {
			s := sv.ValueString()
			result = append(result, &s)
		}
	}
	return &result
}

// uuidListToSliceChecked surfaces UUID parse errors instead of silently
// dropping invalid entries. The silent variant has been removed — see
// parseUUIDPtrChecked for the rationale.
func uuidListToSliceChecked(list types.List, fieldName string) ([]uuid.UUID, error) {
	if list.IsNull() || list.IsUnknown() {
		return nil, nil
	}
	var result []uuid.UUID
	for i, v := range list.Elements() {
		sv, ok := v.(types.String)
		if !ok || sv.IsNull() || sv.IsUnknown() {
			continue
		}
		u, err := uuid.Parse(sv.ValueString())
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: invalid UUID %q: %w", fieldName, i, sv.ValueString(), err)
		}
		result = append(result, u)
	}
	return result, nil
}

func typedStringPtrValue[T ~string](v *T) types.String {
	if v == nil {
		return types.StringNull()
	}
	return types.StringValue(string(*v))
}

func typedStringPtrOrNil[T ~string](v types.String) *T {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	t := T(v.ValueString())
	return &t
}

func boolValue(b *bool) types.Bool {
	if b == nil {
		return types.BoolNull()
	}
	return types.BoolValue(*b)
}

func ptrStringSliceToList(ctx context.Context, s *[]*string) types.List {
	if s == nil || len(*s) == 0 {
		return types.ListNull(types.StringType)
	}
	elems := make([]attr.Value, 0, len(*s))
	for _, v := range *s {
		if v != nil {
			elems = append(elems, types.StringValue(*v))
		}
	}
	return types.ListValueMust(types.StringType, elems)
}

func ptrUUIDSliceToList(ctx context.Context, s *[]*openapi_types.UUID) types.List {
	if s == nil || len(*s) == 0 {
		return types.ListNull(types.StringType)
	}
	elems := make([]attr.Value, 0, len(*s))
	for _, id := range *s {
		if id != nil {
			elems = append(elems, types.StringValue(id.String()))
		}
	}
	return types.ListValueMust(types.StringType, elems)
}

func stringSliceToSet(ctx context.Context, s []string) types.Set {
	if len(s) == 0 {
		return types.SetNull(types.StringType)
	}
	elems := make([]attr.Value, len(s))
	for i, v := range s {
		elems[i] = types.StringValue(v)
	}
	return types.SetValueMust(types.StringType, elems)
}
