package api

// TestEnumSliceCoverage parses `internal/generated/types.go` to enumerate
// every `const X SomeEnum = "literal"` declaration and asserts that each
// `*Types` slice in `enums.go` contains exactly the union of those literals
// for its target enum type.
//
// Why parse AST instead of using reflection
// ─────────────────────────────────────────
//
// Go's `reflect` package can introspect types and instances at runtime
// but not const declarations — there's no way to ask "give me every
// constant of type T in this package" from a running program. Parsing
// the generated source file is the only mechanism that lets us catch a
// dropped slice entry without re-listing every constant inside the test
// itself (which would defeat the test's purpose).
//
// Failure semantics
// ─────────────────
//
//  1. Slice has a value the generated package doesn't define
//     → test fails with "stale slice entry". Either the spec dropped
//     the value (slice should drop it too) or the slice has a typo.
//  2. Generated package has a value the slice doesn't list
//     → test fails with "missing slice entry". A new spec value
//     landed (e.g. `pagerduty_v2`) and `enums.go` needs the
//     corresponding `string(generated.X)` line.
//  3. The Valid() method's branches drift from the const block
//     → caught indirectly: we compare against the const block, and
//     every existing surface (validators, ValidateDTO) consults the
//     const-block-derived slice. If oapi-codegen ever splits Valid()
//     and the const block, both will fail their own tests upstream
//     before reaching this assertion.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"testing"
)

type enumSliceCase struct {
	// generatedTypeName is the name of the enum type as declared in
	// `internal/generated/types.go` (e.g. `MonitorAssertionDtoAssertionType`).
	generatedTypeName string
	// slice is the in-package slice we expect to be exhaustive for
	// `generatedTypeName`.
	slice []string
	// description is surfaced in test failure output so a maintainer
	// can immediately see which slice / which surface is affected.
	description string
}

// enumSliceCoverage is the registry of all `*Types` slices in this
// package that must stay exhaustive against generated constants. To
// cover a new enum:
//
//  1. Add `<NewName>Types` to `enums.go`.
//  2. Add the corresponding entry below.
//  3. Wire `stringvalidator.OneOf(api.<NewName>Types...)` into the
//     relevant resource Schema().
var enumSliceCoverage = []enumSliceCase{
	{
		generatedTypeName: "MonitorAssertionDtoAssertionType",
		slice:             AssertionTypes,
		description:       "monitor assertions[*].type validator",
	},
	{
		generatedTypeName: "AlertChannelDtoChannelType",
		slice:             AlertChannelTypes,
		description:       "alert_channel.channel_type validator",
	},
	{
		generatedTypeName: "MatchRuleType",
		slice:             MatchRuleTypes,
		description:       "notification_policy.match_rule[*].type validator",
	},
}

func TestEnumSliceCoverage(t *testing.T) {
	literals, err := parseGeneratedEnumLiterals()
	if err != nil {
		t.Fatalf("failed to parse generated package: %v", err)
	}

	for _, c := range enumSliceCoverage {
		c := c
		t.Run(c.generatedTypeName, func(t *testing.T) {
			expected, ok := literals[c.generatedTypeName]
			if !ok {
				t.Fatalf(
					"generated package has no `const ... %s = \"...\"` block. "+
						"Either the spec stopped declaring this enum (drop the entry "+
						"from `enumSliceCoverage`), or oapi-codegen output shape "+
						"changed (update `parseGeneratedEnumLiterals`).",
					c.generatedTypeName,
				)
			}
			actual := append([]string(nil), c.slice...)
			sort.Strings(actual)
			sort.Strings(expected)

			missing := diffStrings(expected, actual)
			extra := diffStrings(actual, expected)

			if len(missing) == 0 && len(extra) == 0 {
				return
			}

			var msg string
			if len(missing) > 0 {
				msg += fmt.Sprintf(
					"\n  Missing from slice (generated has these, slice does not):\n    %v\n"+
						"  Add `string(generated.<X>)` lines to the corresponding slice in `enums.go` "+
						"(used by: %s).",
					missing, c.description,
				)
			}
			if len(extra) > 0 {
				msg += fmt.Sprintf(
					"\n  Stale slice entry (slice has these, generated does not):\n    %v\n"+
						"  The spec dropped these values (or the slice has a typo) — remove from "+
						"the slice in `enums.go` (used by: %s).",
					extra, c.description,
				)
			}
			t.Errorf(
				"Enum slice for %s drifted from generated constants:%s",
				c.generatedTypeName, msg,
			)
		})
	}
}

// parseGeneratedEnumLiterals returns a map keyed by enum type name
// whose values are every string literal assigned to that type in
// `const ( X EnumType = "literal" ... )` blocks across
// `internal/generated/types.go`. We parse instead of reflect because
// Go has no runtime introspection over package-level constants.
func parseGeneratedEnumLiterals() (map[string][]string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return nil, fmt.Errorf("runtime.Caller failed")
	}
	// `internal/api/enums_coverage_test.go` → `internal/generated/types.go`.
	generatedPath := filepath.Join(filepath.Dir(thisFile), "..", "generated", "types.go")

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, generatedPath, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", generatedPath, err)
	}

	out := map[string][]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		// Inside a `const (...)` block, `Specs` is one entry per
		// declared name. The first spec in the block carries the type;
		// subsequent specs reuse it implicitly via Go's grouping rules,
		// BUT oapi-codegen always emits the type on every line, so we
		// rely on the per-spec `Type` rather than tracking a fallback.
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || vs.Type == nil {
				continue
			}
			ident, ok := vs.Type.(*ast.Ident)
			if !ok {
				continue
			}
			typeName := ident.Name
			for _, val := range vs.Values {
				lit, ok := val.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				unquoted, err := strconv.Unquote(lit.Value)
				if err != nil {
					return nil, fmt.Errorf(
						"unquote %s = %s: %w",
						typeName, lit.Value, err,
					)
				}
				out[typeName] = append(out[typeName], unquoted)
			}
		}
	}
	return out, nil
}

// diffStrings returns the elements present in `a` but not in `b`. Both
// inputs are pre-sorted by the caller.
func diffStrings(a, b []string) []string {
	in := make(map[string]struct{}, len(b))
	for _, s := range b {
		in[s] = struct{}{}
	}
	var out []string
	for _, s := range a {
		if _, ok := in[s]; !ok {
			out = append(out, s)
		}
	}
	return out
}
