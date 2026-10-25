// Package api — diagnostics helpers for mapping API errors to Terraform diagnostics.
//
// The DevHelm API surfaces a uniform `ErrorResponse` envelope (`category`,
// `code`, `message`, `next`, `requestId`, `errors[]`, …) for every non-2xx
// response. The TF provider's job is to translate those
// generic errors into per-attribute diagnostics so practitioners see the
// failing field highlighted in `terraform plan`/`apply` output instead of a
// summary message floating at the top of the run.
//
// The helpers in this file centralize that translation. Resources call
// AddAPIError (or one of the operation-specific wrappers) instead of
// resp.Diagnostics.AddError directly, passing the attribute path (when
// known) that the error most plausibly originates from. The helper picks
// the most specific diagnostic shape available:
//
//   - 404 from a Read after import     → AddAttributeError on path.Root("id")
//   - 409 ("already exists") on Create → AddAttributeError on the name field
//   - 400 with `errors[].field`         → AddAttributeError on that path
//   - 400 with a "field <X>" hint      → AddAttributeError on that path
//   - everything else                  → AddError with the operation context
//
// Centralizing the mapping keeps the resource files free of repetitive
// switch-on-error-type boilerplate and ensures we apply the same rules
// everywhere.
package api

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
)

// fieldHintPattern matches common API error messages that include an
// inline field name we can map back to an attribute path. We only attempt
// the rewrite when the field name looks like an alphanumeric identifier
// to avoid mis-mapping prose ("missing the address line").
//
// Patterns covered:
//
//   - `field "X" ...`           (Spring-style validation)
//   - `'X' must be ...`         (Bean Validation default)
//   - `<X>: must not be blank`  (Field-prefixed messages)
//
// A nil match means we fall back to the operation-level summary.
var fieldHintPattern = regexp.MustCompile(`(?i)(?:field\s+["']?([a-zA-Z][a-zA-Z0-9_]*)["']?|["']([a-zA-Z][a-zA-Z0-9_]*)["']\s+must\b|^\s*([a-zA-Z][a-zA-Z0-9_]*)\s*:\s*must\b)`)

// alreadyExistsHints are substrings the API uses for uniqueness violations.
// When detected, the helper attributes the diagnostic to the resource's
// human-facing identity field (typically `name` or `slug`).
var alreadyExistsHints = []string{
	"already exists",
	"duplicate",
	"already in use",
	"is already taken",
}

// fieldHint extracts the first plausible attribute name from an API error
// message, returning the empty string if no hint is present.
func fieldHint(msg string) string {
	m := fieldHintPattern.FindStringSubmatch(msg)
	if m == nil {
		return ""
	}
	for _, g := range m[1:] {
		if g != "" {
			return g
		}
	}
	return ""
}

// looksLikeAlreadyExists reports whether the API message indicates a
// uniqueness conflict (HTTP 409 territory). The check is substring-based
// because the API doesn't promise a stable error code beyond `status`.
func looksLikeAlreadyExists(msg string) bool {
	low := strings.ToLower(msg)
	for _, hint := range alreadyExistsHints {
		if strings.Contains(low, hint) {
			return true
		}
	}
	return false
}

// AddAPIError translates an API error into the most specific diagnostic
// shape available. `op` is a short imperative verb describing what the
// resource was attempting (e.g. "create monitor"). `identityAttr` is the path
// to the attribute that uniquely names the resource within the workspace
// (typically path.Root("name") or path.Root("slug")); pass path.Empty() when
// no such anchor applies.
//
// Diagnostic shape (doc 42 §7): the summary is
// `[<category>] <code>: <message>`, the detail names the operation, then
// `next.command`, `next.docs` and the request id.
//
// The function is a no-op when err is nil so callers can wrap the entire
// API call site without needing a separate guard.
func AddAPIError(diagnostics *diag.Diagnostics, op string, err error, identityAttr path.Path) {
	if err == nil {
		return
	}

	var apiErr *DevhelmAPIError
	if errors.As(err, &apiErr) {
		summary, detail := APIErrorText(op, apiErr)

		// Structured per-field rejection → highlight the first mapped field.
		for _, fe := range apiErr.FieldErrors() {
			if fe.Field == nil || *fe.Field == "" {
				continue
			}
			if p, ok := attributePathForField(*fe.Field); ok {
				diagnostics.AddAttributeError(p, summary, detail)
				return
			}
		}

		// 409-style conflict on a known identity attribute → highlight it.
		if !identityAttr.Equal(path.Empty()) && looksLikeAlreadyExists(apiErr.Message) {
			diagnostics.AddAttributeError(identityAttr, summary, detail)
			return
		}

		// 400-style validation with an embedded field hint → highlight it.
		if hint := fieldHint(apiErr.Message); hint != "" {
			diagnostics.AddAttributeError(path.Root(hint), summary, detail)
			return
		}

		// Fall back to attaching the error to the identity attribute when
		// available so the practitioner at least gets a focused indicator.
		if !identityAttr.Equal(path.Empty()) {
			diagnostics.AddAttributeError(identityAttr, summary, detail)
			return
		}

		diagnostics.AddError(summary, detail)
		return
	}

	// Transport-level failures (DNS, dial, TLS, read) carry the original
	// cause via Unwrap; surface them with the `transport` category and the
	// operation so the practitioner sees what was being attempted rather
	// than only the raw `dial tcp: lookup ...: no such host`.
	var transportErr *DevhelmTransportError
	if errors.As(err, &transportErr) {
		diagnostics.AddError(
			fmt.Sprintf("[%s] %s", CategoryTransport, transportErr.Op),
			fmt.Sprintf("Network failure during %s: %s", op, transportErr.Error()),
		)
		return
	}

	var waitErr *WaitTimeoutError
	if errors.As(err, &waitErr) {
		diagnostics.AddError(fmt.Sprintf("[timeout] %s", waitErr.Error()),
			fmt.Sprintf("While trying to %s. The run keeps going on the server; "+
				"raise the resource `timeouts` or check the run in the dashboard.", op))
		return
	}

	// Marshaling, JSON decoding, validation, etc. — no useful structured
	// context, so just attach the operation label.
	diagnostics.AddError(fmt.Sprintf("Error during %s", op), err.Error())
}

// APIErrorText renders the `[category] code: message` summary and the
// next-action detail for one API error.
func APIErrorText(op string, apiErr *DevhelmAPIError) (string, string) {
	message := apiErr.Message
	if message == "" {
		message = strings.TrimSpace(apiErr.Body)
	}
	if message == "" {
		message = http.StatusText(apiErr.StatusCode)
	}
	summary := fmt.Sprintf("[%s] %s", apiErr.Category, message)
	if apiErr.Code != "" {
		summary = fmt.Sprintf("[%s] %s: %s", apiErr.Category, apiErr.Code, message)
	}

	lines := []string{fmt.Sprintf("While trying to %s.", op)}
	if hint := ownershipHint(apiErr); hint != "" {
		lines = append(lines, hint)
	}
	for _, fe := range apiErr.FieldErrors() {
		field := ""
		if fe.Field != nil && *fe.Field != "" {
			field = *fe.Field + ": "
		}
		lines = append(lines, fmt.Sprintf("  - %s%s (%s)", field, fe.Message, fe.Code))
	}
	if next := apiErr.NextCommand(); next != "" {
		lines = append(lines, "Next: "+next)
	}
	if docs := apiErr.NextDocs(); docs != "" {
		lines = append(lines, "Docs: "+docs)
	}
	if apiErr.RequestID != "" {
		lines = append(lines, "Request ID: "+apiErr.RequestID)
	}
	return summary, strings.Join(lines, "\n")
}

// ownershipHint explains MONITOR_OWNED_BY_OTHER in terms of the provider's
// API key: owner enforcement compares the monitor's owner with the tool the
// calling key writes as.
func ownershipHint(apiErr *DevhelmAPIError) string {
	if apiErr.Code != "MONITOR_OWNED_BY_OTHER" {
		return ""
	}
	var owner, caller string
	apiErr.Detail("owner", &owner)
	apiErr.Detail("caller", &caller)
	if owner == "" && apiErr.Response != nil && apiErr.Response.Owner != nil {
		owner = *apiErr.Response.Owner
	}
	if owner == "TERRAFORM" && caller != "" && caller != "TERRAFORM" {
		return fmt.Sprintf("The monitor is owned by TERRAFORM, but this provider's API key writes as %s. "+
			"Use an API key created with tool TERRAFORM, or set owner_enforced = false.", caller)
	}
	if owner != "" {
		return fmt.Sprintf("The monitor is owned by %s. Change it there, or transfer it to Terraform "+
			"with `terraform import` (which transfers ownership).", owner)
	}
	return ""
}

// attributePathForField maps an `errors[].field` pointer from the API
// (camelCase, dotted, e.g. `frequencySeconds` or `code.entrypoint`) onto the
// matching snake_case attribute path.
func attributePathForField(field string) (path.Path, bool) {
	field = strings.TrimPrefix(strings.TrimPrefix(field, "/"), "$.")
	segments := strings.FieldsFunc(field, func(r rune) bool { return r == '.' || r == '/' })
	if len(segments) == 0 {
		return path.Empty(), false
	}
	first := camelToSnake(segments[0])
	if !identifierPattern.MatchString(first) {
		return path.Empty(), false
	}
	p := path.Root(first)
	for _, seg := range segments[1:] {
		if strings.IndexFunc(seg, func(r rune) bool { return r < '0' || r > '9' }) == -1 {
			// Numeric segment → list index; stop here, the parent is a
			// good enough anchor for the editor.
			break
		}
		p = p.AtName(camelToSnake(seg))
	}
	return p, true
}

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func camelToSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// AddNotFoundError emits the import-time "resource not found" diagnostic
// in a uniform shape across all resources. Use during ImportState when a
// list lookup returns zero results for the requested ID.
func AddNotFoundError(diagnostics *diag.Diagnostics, resourceLabel, id string) {
	diagnostics.AddAttributeError(
		path.Root("id"),
		fmt.Sprintf("%s not found", resourceLabel),
		fmt.Sprintf("No %s found with name or ID %q", strings.ToLower(resourceLabel), id),
	)
}
