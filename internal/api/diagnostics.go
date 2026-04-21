// Package api — diagnostics helpers for mapping API errors to Terraform diagnostics.
//
// The DevHelm API surfaces a uniform `{status, message, timestamp}` envelope
// for every non-2xx response. The TF provider's job is to translate those
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

// AddAPIError translates a generic API error into the most specific
// diagnostic shape available. `op` is a short imperative verb describing
// what the resource was attempting (e.g. "create monitor"). `identityAttr`
// is the path to the attribute that uniquely names the resource within
// the workspace (typically path.Root("name") or path.Root("slug")); pass
// path.Empty() when no such anchor applies.
//
// The function is a no-op when err is nil so callers can wrap the entire
// API call site without needing a separate guard.
func AddAPIError(diagnostics *diag.Diagnostics, op string, err error, identityAttr path.Path) {
	if err == nil {
		return
	}

	var apiErr *DevhelmAPIError
	if errors.As(err, &apiErr) {
		summary := fmt.Sprintf("Error during %s (HTTP %d)", op, apiErr.StatusCode)
		if apiErr.Code != "" {
			summary = fmt.Sprintf("Error during %s (HTTP %d %s)", op, apiErr.StatusCode, apiErr.Code)
		}
		detail := apiErr.Message
		if detail == "" {
			detail = apiErr.Body
		}
		// Always surface the request id when present so the practitioner
		// (or our support team) can correlate against server logs.
		if apiErr.RequestID != "" {
			detail = fmt.Sprintf("%s\n\n(request_id=%s)", strings.TrimRight(detail, "\n"), apiErr.RequestID)
		}

		// 409-style conflict on a known identity attribute → highlight it.
		if !identityAttr.Equal(path.Empty()) && looksLikeAlreadyExists(detail) {
			diagnostics.AddAttributeError(identityAttr, summary, detail)
			return
		}

		// 400-style validation with an embedded field hint → highlight it.
		if hint := fieldHint(detail); hint != "" {
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
	// cause via Unwrap; surface them with a clear operation prefix so the
	// practitioner sees "Network failure during create monitor" rather than
	// only the raw `dial tcp: lookup ...: no such host`.
	var transportErr *DevhelmTransportError
	if errors.As(err, &transportErr) {
		diagnostics.AddError(
			fmt.Sprintf("Network failure during %s", op),
			transportErr.Error(),
		)
		return
	}

	// Marshaling, JSON decoding, validation, etc. — no useful structured
	// context, so just attach the operation label.
	diagnostics.AddError(fmt.Sprintf("Error during %s", op), err.Error())
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
