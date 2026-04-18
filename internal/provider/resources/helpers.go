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

// emailsFromStringList returns a value slice (matches the post-spec-sync
// generated type for required `recipients` arrays). Returns nil when input
// list is null/unknown so downstream nil-checks still work.
func emailsFromStringList(list types.List) []openapi_types.Email {
	strs := stringListToSlice(list)
	if strs == nil {
		return nil
	}
	emails := make([]openapi_types.Email, len(strs))
	for i, s := range strs {
		emails[i] = openapi_types.Email(s)
	}
	return emails
}

// stringSliceToPtr converts a Terraform List<String> into a *[]string suitable
// for embedding in generated request bodies (which use *[]string for optional
// arrays after the move from `*[]*string`). nil input → nil pointer so the
// field is omitted from the request payload.
func stringSliceToPtr(list types.List) *[]string {
	if list.IsNull() || list.IsUnknown() {
		return nil
	}
	result := make([]string, 0, len(list.Elements()))
	for _, v := range list.Elements() {
		if sv, ok := v.(types.String); ok {
			result = append(result, sv.ValueString())
		}
	}
	return &result
}

// uuidSliceFromStringListChecked surfaces UUID parse errors instead of
// silently dropping invalid entries. The silent variant has been removed —
// see parseUUIDPtrChecked for the rationale.
func uuidSliceFromStringListChecked(list types.List, fieldName string) (*[]openapi_types.UUID, error) {
	if list.IsNull() || list.IsUnknown() {
		return nil, nil
	}
	result := make([]openapi_types.UUID, 0, len(list.Elements()))
	for i, v := range list.Elements() {
		sv, ok := v.(types.String)
		if !ok || sv.IsNull() || sv.IsUnknown() {
			continue
		}
		u, err := uuid.Parse(sv.ValueString())
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: invalid UUID %q: %w", fieldName, i, sv.ValueString(), err)
		}
		result = append(result, openapi_types.UUID(u))
	}
	return &result, nil
}

func stringSliceToPtrFromSet(set types.Set) *[]string {
	if set.IsNull() || set.IsUnknown() {
		return nil
	}
	result := make([]string, 0, len(set.Elements()))
	for _, v := range set.Elements() {
		if sv, ok := v.(types.String); ok {
			result = append(result, sv.ValueString())
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

func ptrStringSliceToList(ctx context.Context, s *[]string) types.List {
	if s == nil || len(*s) == 0 {
		return types.ListNull(types.StringType)
	}
	elems := make([]attr.Value, 0, len(*s))
	for _, v := range *s {
		elems = append(elems, types.StringValue(v))
	}
	return types.ListValueMust(types.StringType, elems)
}

func ptrUUIDSliceToList(ctx context.Context, s *[]openapi_types.UUID) types.List {
	if s == nil || len(*s) == 0 {
		return types.ListNull(types.StringType)
	}
	elems := make([]attr.Value, 0, len(*s))
	for _, id := range *s {
		elems = append(elems, types.StringValue(id.String()))
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

// unionHasData returns true when a generated `_Config` / oneOf union actually
// carries a JSON object, vs. an empty/zero-valued one. The generated
// MarshalJSON for these types returns the underlying RawMessage as-is, so an
// uninitialized union marshals to either nil bytes or the literal `null` —
// neither is meaningful state to mirror back into the Terraform model.
func unionHasData(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	s := string(raw)
	return s != "null" && s != "{}"
}

// marshalWithRawAuth marshals body as JSON, then injects the user-supplied
// `auth` blob as raw JSON. This is necessary because the generated
// MonitorAuthConfig type lost its polymorphic shape during the OpenAPI sync
// (the discriminator-based oneOf collapsed to {type: string} only). Sending
// the typed value would silently drop every credential field. When auth is
// null/unknown the body is returned unchanged so the caller's `clearAuth`
// flag remains the only auth-related signal.
func marshalWithRawAuth(body any, auth types.String) (json.RawMessage, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encoding monitor body: %w", err)
	}
	if auth.IsNull() || auth.IsUnknown() {
		return b, nil
	}
	rawAuth := auth.ValueString()
	if rawAuth == "" {
		return b, nil
	}
	if !json.Valid([]byte(rawAuth)) {
		return nil, fmt.Errorf("monitor auth is not valid JSON")
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("re-decoding monitor body for auth merge: %w", err)
	}
	m["auth"] = json.RawMessage(rawAuth)
	out, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("re-encoding monitor body with auth: %w", err)
	}
	return out, nil
}

// extractDataField pulls a single top-level field out of the standard
// {"data": {...}} response envelope. Used to recover polymorphic JSON blobs
// (e.g. the monitor `auth` field) that the typed generated structs cannot
// round-trip losslessly. Returns "" when the field is missing or null.
func extractDataField(body []byte, field string) string {
	if len(body) == 0 {
		return ""
	}
	var env struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return ""
	}
	raw, ok := env.Data[field]
	if !ok {
		return ""
	}
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	return string(raw)
}

// priorHasConfigType reports whether the prior Terraform value for a
// monitor `config` attribute already contained a top-level `type` key.
// We use this as the trigger for re-injecting the discriminator on
// read-back: if the user originally supplied it, we round-trip it; if
// they omitted it, we leave the API's stripped form alone (so the
// next plan stays clean).
func priorHasConfigType(prior types.String) bool {
	if prior.IsNull() || prior.IsUnknown() {
		return false
	}
	raw := prior.ValueString()
	if raw == "" {
		return false
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return false
	}
	_, ok := m["type"]
	return ok
}

// injectConfigType adds (or replaces) the top-level `type` key on a
// JSON object payload. If the input is not a JSON object we return it
// unchanged — callers always pass `normalizeJSON`-cleaned bytes, so
// the typical input is `{"foo":1}` and the typical output is
// `{"foo":1,"type":"TCP"}`.
func injectConfigType(raw, monitorType string) string {
	if raw == "" || monitorType == "" {
		return raw
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return raw
	}
	t, _ := json.Marshal(monitorType)
	m["type"] = t
	out, err := json.Marshal(m)
	if err != nil {
		return raw
	}
	return string(out)
}

// preserveListOrder returns a Terraform list of strings whose contents
// match `apiIDs` (treated as a set), but keeps the ordering of `existing`
// when its element-set is identical to `apiIDs`. This avoids spurious
// "Provider produced inconsistent result after apply" errors on
// attributes the API treats as unordered (tag IDs, alert channel IDs,
// etc.) while still allowing genuine drift to surface as a real diff.
//
// When `existing` is null/unknown, or its element-set differs from
// `apiIDs`, we return the API's order verbatim — it's the new source of
// truth.
func preserveListOrder(ctx context.Context, existing types.List, apiIDs []string) types.List {
	apiSet := make(map[string]struct{}, len(apiIDs))
	for _, id := range apiIDs {
		apiSet[id] = struct{}{}
	}

	var existingIDs []string
	if !existing.IsNull() && !existing.IsUnknown() {
		existing.ElementsAs(ctx, &existingIDs, false)
	}

	if len(existingIDs) == len(apiIDs) {
		match := true
		seen := make(map[string]struct{}, len(existingIDs))
		for _, id := range existingIDs {
			if _, ok := apiSet[id]; !ok {
				match = false
				break
			}
			seen[id] = struct{}{}
		}
		if match && len(seen) == len(apiIDs) {
			elems := make([]types.String, len(existingIDs))
			for i, id := range existingIDs {
				elems[i] = types.StringValue(id)
			}
			out, _ := types.ListValueFrom(ctx, types.StringType, elems)
			return out
		}
	}

	elems := make([]types.String, len(apiIDs))
	for i, id := range apiIDs {
		elems[i] = types.StringValue(id)
	}
	out, _ := types.ListValueFrom(ctx, types.StringType, elems)
	return out
}
