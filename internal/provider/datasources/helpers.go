package datasources

import "encoding/json"

// normalizeConfigJSON strips null-valued keys from a JSON blob so the data
// source's `config` output matches the normalized form emitted by the
// monitor resource. Recurses through nested objects and arrays. Mirrors
// resources.normalizeJSON — kept duplicated here to avoid a cross-package
// import (datasources ← resources) and the accompanying dependency cycle
// risk.
func normalizeConfigJSON(raw json.RawMessage) string {
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
