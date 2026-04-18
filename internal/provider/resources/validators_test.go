package resources

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ───────────────────────────────────────────────────────────────────────
// Validators tests (Class K)
//
// Custom validators on the resources package — currently
// componentStartDateValidator. Generic LengthAtLeast / OneOf validators
// are owned by hashicorp/terraform-plugin-framework-validators and
// already exhaustively tested upstream; we cover their wiring inside the
// schema_dto_audit_test.go (via Schema() round-trip) instead of
// re-testing the upstream library here.
// ───────────────────────────────────────────────────────────────────────

func runStartDateValidator(t *testing.T, value string, isNull bool) []string {
	t.Helper()
	req := validator.StringRequest{
		Path: path.Root("start_date"),
	}
	if isNull {
		req.ConfigValue = types.StringNull()
	} else {
		req.ConfigValue = types.StringValue(value)
	}
	resp := &validator.StringResponse{}
	componentStartDateValidator{}.ValidateString(context.Background(), req, resp)

	out := make([]string, 0, len(resp.Diagnostics))
	for _, d := range resp.Diagnostics {
		out = append(out, d.Summary())
	}
	return out
}

func TestComponentStartDateValidator_Accepts(t *testing.T) {
	cases := []string{"2024-01-15", "1999-12-31", "2024-02-29"}
	for _, v := range cases {
		if errs := runStartDateValidator(t, v, false); len(errs) > 0 {
			t.Errorf("%q rejected: %v", v, errs)
		}
	}
}

func TestComponentStartDateValidator_Rejects(t *testing.T) {
	bad := []string{
		"not a date",
		"2024/01/15",
		"01-15-2024",
		"20240115",
		"2024-13-01", // bad month
		"2024-02-30", // bad day for Feb
	}
	for _, v := range bad {
		errs := runStartDateValidator(t, v, false)
		if len(errs) == 0 {
			t.Errorf("%q accepted, want rejection", v)
		}
	}
}

// TestComponentStartDateValidator_PassesNull guards against double-validation:
// the framework already checks Required vs Optional, so a custom validator
// should silently skip null/unknown values rather than producing a noisy
// "Invalid format" error on omitted fields.
func TestComponentStartDateValidator_PassesNull(t *testing.T) {
	if errs := runStartDateValidator(t, "", true); len(errs) > 0 {
		t.Errorf("null value triggered errors: %v", errs)
	}
}

// TestComponentStartDateValidator_DescriptionsAreNonEmpty surfaces the
// human-readable validator description for `terraform validate` output —
// an empty string would degrade UX and signal a regression.
func TestComponentStartDateValidator_DescriptionsAreNonEmpty(t *testing.T) {
	v := componentStartDateValidator{}
	if got := v.Description(context.Background()); got == "" {
		t.Error("Description() empty")
	}
	if got := v.MarkdownDescription(context.Background()); got == "" {
		t.Error("MarkdownDescription() empty")
	}
}
