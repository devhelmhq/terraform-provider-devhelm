package datasources

import (
	"strings"
	"testing"
)

// The datasource package keeps its own copy of normalizeJSON to avoid an
// import cycle with the resources package (see helpers.go for the
// rationale). These tests pin its behaviour to the same contract as
// resources.normalizeJSON so the two never silently diverge — a divergence
// would cause data-source `config` outputs to differ from resource state
// for the same monitor, breaking referential transparency.

func TestNormalizeConfigJSON_StripsAllNulls(t *testing.T) {
	in := `{"a":1,"b":null,"c":{"d":null,"e":2,"f":[null,{"g":null,"h":3}]}}`
	got := normalizeConfigJSON([]byte(in))
	if strings.Contains(got, "null") {
		t.Errorf("did not strip nulls: %s", got)
	}
	for _, want := range []string{`"a":1`, `"e":2`, `"h":3`} {
		if !strings.Contains(got, want) {
			t.Errorf("dropped %s: %s", want, got)
		}
	}
}

func TestNormalizeConfigJSON_PreservesZeroValues(t *testing.T) {
	in := `{"a":0,"b":false,"c":""}`
	got := normalizeConfigJSON([]byte(in))
	for _, want := range []string{`"a":0`, `"b":false`, `"c":""`} {
		if !strings.Contains(got, want) {
			t.Errorf("dropped %s: %s", want, got)
		}
	}
}

func TestNormalizeConfigJSON_PreservesDiscriminatorTypeField(t *testing.T) {
	in := `{"type":"bearer","token":"abc","extra":null}`
	got := normalizeConfigJSON([]byte(in))
	if !strings.Contains(got, `"type":"bearer"`) {
		t.Errorf("dropped discriminator: %s", got)
	}
	if strings.Contains(got, "extra") {
		t.Errorf("kept null-valued extra: %s", got)
	}
}

func TestNormalizeConfigJSON_PassesThroughOnInvalidJSON(t *testing.T) {
	in := `not json`
	got := normalizeConfigJSON([]byte(in))
	if got != in {
		t.Errorf("invalid input not passed through: got %q, want %q", got, in)
	}
}

func TestNormalizeConfigJSON_Idempotent(t *testing.T) {
	in := `{"deeply":{"nested":[{"k":1,"q":[2,3]}]}}`
	once := normalizeConfigJSON([]byte(in))
	twice := normalizeConfigJSON([]byte(once))
	if once != twice {
		t.Errorf("not idempotent: once=%s twice=%s", once, twice)
	}
}

// TestNormalizeConfigJSON_ParityWithResourcePackage uses inputs known to
// exercise the same code paths as resources.normalizeJSON. If the two
// implementations diverge (because someone updates one but forgets the
// other), this test alone won't catch that — but combined with the
// equivalent test in resources/helpers_test.go we get effective parity
// coverage. Keep the input rosters in lockstep.
func TestNormalizeConfigJSON_ParitySpecimens(t *testing.T) {
	cases := []struct {
		in   string
		want string // substring expected
	}{
		{`{}`, `{}`},
		{`null`, `null`},
		{`{"keep":1,"drop":null}`, `"keep":1`},
		{`[1,null,2]`, `[1,2]`},
	}
	for _, tc := range cases {
		got := normalizeConfigJSON([]byte(tc.in))
		if !strings.Contains(got, tc.want) {
			t.Errorf("input %q -> %q, missing %q", tc.in, got, tc.want)
		}
	}
}
