package resources

import (
	"encoding/json"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// normalizeJSON strips null-valued keys from a JSON blob so the API-returned
// config doesn't diverge from the plan due to extra null fields.
func normalizeJSON(raw json.RawMessage) string {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return string(raw)
	}
	stripped := stripNulls(m)
	out, err := json.Marshal(stripped)
	if err != nil {
		return string(raw)
	}
	return string(out)
}

func stripNulls(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if v == nil {
			continue
		}
		if nested, ok := v.(map[string]any); ok {
			out[k] = stripNulls(nested)
		} else {
			out[k] = v
		}
	}
	return out
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

func intPtrOrNil(v types.Int64) *int {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := int(v.ValueInt64())
	return &i
}

func float64PtrOrNil(v types.Float64) *float64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	f := v.ValueFloat64()
	return &f
}

func stringValue(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

func boolValue(b *bool) types.Bool {
	if b == nil {
		return types.BoolNull()
	}
	return types.BoolValue(*b)
}

func intValue(i *int) types.Int64 {
	if i == nil {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*i))
}

func float64Value(f *float64) types.Float64 {
	if f == nil {
		return types.Float64Null()
	}
	return types.Float64Value(*f)
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

func mapToStringMap(m types.Map) map[string]string {
	if m.IsNull() || m.IsUnknown() {
		return nil
	}
	result := make(map[string]string)
	for k, v := range m.Elements() {
		if sv, ok := v.(types.String); ok {
			result[k] = sv.ValueString()
		}
	}
	return result
}
