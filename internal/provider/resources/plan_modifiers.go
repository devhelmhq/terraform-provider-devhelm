package resources

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
)

// useStateForUnknownAlwaysString is a String plan modifier that copies the
// prior state value into the planned value whenever the planned value is
// unknown, *including when the prior state value is null*.
//
// The stock `stringplanmodifier.UseStateForUnknown` returns early when the
// state value is null, which leaves Computed-only attributes flapping
// between null and "(known after apply)" on every plan after a no-op
// update — producing perpetual diffs for fields that the API only ever
// populates for a subset of records (e.g. `ping_url` on HEARTBEAT
// monitors but not HTTP monitors).
type useStateForUnknownAlwaysString struct{}

func (m useStateForUnknownAlwaysString) Description(_ context.Context) string {
	return "Once read from the API, this attribute (including null) is preserved across plans."
}

func (m useStateForUnknownAlwaysString) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m useStateForUnknownAlwaysString) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// Resource hasn't been created yet — let the framework compute the value.
	if req.State.Raw.IsNull() {
		return
	}
	if !req.PlanValue.IsUnknown() {
		return
	}
	if req.ConfigValue.IsUnknown() {
		return
	}
	resp.PlanValue = req.StateValue
}

// UseStateForUnknownAlwaysString returns the singleton instance.
func UseStateForUnknownAlwaysString() planmodifier.String {
	return useStateForUnknownAlwaysString{}
}

// useStateForUnknownAlwaysList is the List analogue of
// useStateForUnknownAlwaysString. The same root cause applies:
// `listplanmodifier.UseStateForUnknown` skips null prior-state values,
// so an Optional+Computed list attribute that was *imported* (i.e. never
// set by the user, never populated by the API) would flap between
// `null -> (known after apply)` on every plan.
//
// We need this for `alert_channel_ids`: the API treats it as a sub-
// resource (POST/DELETE on /tags etc.) so the schema marks it
// Optional+Computed to support the "omit to preserve" workflow. Without
// this modifier, importing a monitor with no channels yields a
// guaranteed diff against any HCL that omits the attribute.
type useStateForUnknownAlwaysList struct{}

func (m useStateForUnknownAlwaysList) Description(_ context.Context) string {
	return "Once read from the API, this list (including null) is preserved across plans."
}

func (m useStateForUnknownAlwaysList) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m useStateForUnknownAlwaysList) PlanModifyList(_ context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	if req.State.Raw.IsNull() {
		return
	}
	if !req.PlanValue.IsUnknown() {
		return
	}
	if req.ConfigValue.IsUnknown() {
		return
	}
	resp.PlanValue = req.StateValue
}

func UseStateForUnknownAlwaysList() planmodifier.List {
	return useStateForUnknownAlwaysList{}
}

