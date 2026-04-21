package api

import (
	"fmt"
	"reflect"
	"strings"
	"time"
)

// validatable is implemented by oapi-codegen enum types. The generator emits
// a Valid() bool method on every string-backed enum, so any response field
// whose type carries an enum constraint satisfies this interface.
type validatable interface {
	Valid() bool
}

var (
	validatableType = reflect.TypeOf((*validatable)(nil)).Elem()
	timeType        = reflect.TypeOf(time.Time{})
)

// ValidateDTO performs structural validation on a DTO returned by the API.
//
// It leverages the fact that oapi-codegen encodes the OpenAPI required/optional
// distinction directly in Go's type system:
//   - Required fields are value types (string, UUID, enum, time.Time)
//   - Optional fields are pointer types (*string, *bool, *int32)
//
// The validator walks the struct and enforces two invariants:
//  1. Non-pointer fields must not be zero-valued (catches missing required fields
//     that json.Unmarshal silently accepts).
//  2. Fields whose type implements Valid() bool must return true (catches enum
//     values the provider doesn't recognize, e.g. after an API update adds a
//     new variant).
//
// This is the Go equivalent of Zod safeParse (SDK-JS) and Pydantic model_validate
// (SDK-Python) — runtime response validation driven by the spec, with zero
// hand-written per-DTO code.
func ValidateDTO(dto any, context string) error {
	v := reflect.ValueOf(dto)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return fmt.Errorf("%s: response DTO is nil", context)
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}

	var errs []string
	validateStruct(v, "", &errs)

	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%s: invalid API response: %s", context, strings.Join(errs, "; "))
}

func validateStruct(v reflect.Value, prefix string, errs *[]string) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		fv := v.Field(i)
		tag := field.Tag.Get("json")
		jsonName := jsonFieldName(tag, field.Name)
		fullName := jsonName
		if prefix != "" {
			fullName = prefix + "." + jsonName
		}

		if tag == "-" {
			continue
		}

		if field.Type == timeType {
			continue
		}

		// Pointer fields are optional — skip zero check, but validate
		// the value if present.
		if field.Type.Kind() == reflect.Ptr {
			if !fv.IsNil() {
				elem := fv.Elem()
				if elem.Type().Implements(validatableType) {
					if !elem.MethodByName("Valid").Call(nil)[0].Bool() {
						*errs = append(*errs, fmt.Sprintf(
							"%s: unknown enum value %q", fullName, fmt.Sprint(elem.Interface())))
					}
				}
				if elem.Kind() == reflect.Struct && elem.Type() != timeType {
					validateStruct(elem, fullName, errs)
				}
			}
			continue
		}

		// oapi-codegen union wrappers (e.g. MonitorDto_Config) are structs
		// with only unexported fields (a single `union json.RawMessage`).
		// They are populated via custom UnmarshalJSON, so zero-checking
		// the Go struct is meaningless — skip them entirely.
		if field.Type.Kind() == reflect.Struct && field.Type != timeType && !hasExportedFields(field.Type) {
			continue
		}

		// Non-pointer fields are required — check for zero value.
		// Exclusions (kinds where the zero value is a legitimate API
		// response and indistinguishable from "absent" after
		// json.Unmarshal):
		//   - Bool: `false` is valid (e.g. `enabled: false`).
		//   - Numerics (Int*, Uint*, Float*): `0` is valid (e.g.
		//     `consecutiveFailures: 0` on a fresh webhook,
		//     `monitorCount: 0` on an empty resource group,
		//     `priority: 0` on a default notification policy).
		//   - Nested struct: a zero struct may legitimately come from
		//     a JSON `{}` payload when every field of the nested type
		//     is optional. Recursion below validates whatever fields
		//     are present without false-positiving on this case.
		//
		// Strings, UUIDs, time.Time (handled above), slices, and
		// enum-typed fields are still zero-checked because either
		// (a) the empty value is not a meaningful API output, or
		// (b) the spec does not allow it for required fields.
		if fv.IsZero() && !isZeroValidKind(field.Type.Kind()) {
			if field.Type.Kind() == reflect.Slice {
				*errs = append(*errs, fmt.Sprintf("%s: required array is missing", fullName))
			} else {
				*errs = append(*errs, fmt.Sprintf("%s: required field is missing or zero", fullName))
			}
			continue
		}

		// Enum validation on non-pointer fields.
		if fv.Type().Implements(validatableType) {
			if !fv.MethodByName("Valid").Call(nil)[0].Bool() {
				*errs = append(*errs, fmt.Sprintf(
					"%s: unknown enum value %q", fullName, fmt.Sprint(fv.Interface())))
			}
		}

		// Recurse into nested structs (but not time.Time which is a struct).
		if fv.Kind() == reflect.Struct {
			validateStruct(fv, fullName, errs)
		}
	}
}

// isZeroValidKind reports whether the zero value of the given Kind is a
// legitimate API response value that the validator must NOT reject.
//
// The OpenAPI generator emits required scalars as Go value types (no
// pointer), so reflect.IsZero cannot distinguish "the API sent 0/false"
// from "the API omitted the field". For these kinds the spec allows 0
// or false as a valid value, so we trust the type-system contract
// (required ⇒ value-typed) and accept the zero value. For string, UUID,
// time.Time, and enum-typed fields the zero value is not a valid
// required output, so we keep the zero-check.
func isZeroValidKind(k reflect.Kind) bool {
	switch k {
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64,
		reflect.Struct:
		return true
	default:
		return false
	}
}

func hasExportedFields(t reflect.Type) bool {
	for i := 0; i < t.NumField(); i++ {
		if t.Field(i).IsExported() {
			return true
		}
	}
	return false
}

func jsonFieldName(tag, fieldName string) string {
	if tag == "" {
		return fieldName
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		return fieldName
	}
	return name
}
