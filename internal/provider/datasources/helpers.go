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

// matchByName scans `items` for entries whose `nameFn(item) == want` and
// returns every match. Returning a list (rather than a single result with
// diagnostics) keeps the helper trivially testable and lets each
// datasource format its own resource-specific error wording without
// flattening the wording into a generic template.
//
// All five "lookup by display name" datasources (alert channel, monitor,
// notification policy, resource group, tag) use this so the dedup
// behaviour stays in lockstep — a divergence here is the kind of bug
// that surfaces as "data source X returns first, data source Y errors
// on ambiguity".
func matchByName[T any](items []T, want string, nameFn func(T) string) []T {
	if want == "" {
		return nil
	}
	var matches []T
	for _, it := range items {
		if nameFn(it) == want {
			matches = append(matches, it)
		}
	}
	return matches
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
