package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
)

// Owner tools (MonitorManagedBy). Responses decode these as plain strings.
const (
	ToolTerraform = "TERRAFORM"
	ToolAPI       = "API"
)

// Error categories (doc 32 D-1). `category` is the machine contract every
// surface branches on; the HTTP status is secondary. CategoryTransport is the
// client-side category for requests that never got an answer.
const (
	CategoryValidation           = "validation"
	CategoryBlocked              = "blocked"
	CategoryAuth                 = "auth"
	CategoryPermission           = "permission"
	CategoryOwnership            = "ownership"
	CategoryPlan                 = "plan"
	CategoryQuota                = "quota"
	CategoryRateLimit            = "rate_limit"
	CategoryConflict             = "conflict"
	CategoryConfirmationRequired = "confirmation_required"
	CategoryNotFound             = "not_found"
	CategoryGone                 = "gone"
	CategoryPlatform             = "platform"
	CategoryTransport            = "transport"
)

// DevhelmAPIError represents a non-2xx response from the DevHelm API. Every
// category-specific error below wraps one, so `errors.As(err, &apiErr)`
// with `apiErr *DevhelmAPIError` matches any API refusal.
//
//   - StatusCode is the HTTP status line (zero for client-side refusals such
//     as BUNDLE_DIGEST_MISMATCH that never reached the API).
//   - Code mirrors `ErrorResponse.code` — a stable machine code such as
//     "REVISION_UNTESTED". Empty only for non-conforming bodies.
//   - Category mirrors `ErrorResponse.category`; derived from the status
//     when a non-conforming body omits it.
//   - Message is `ErrorResponse.message` (or the legacy `error` field).
//   - RequestID is the `X-Request-Id` header, else `requestId` in the body.
//   - Response is the full decoded envelope (nil for non-JSON bodies), so
//     callers can read `next`, `errors[]`, `owner`, `retryAfterSeconds` and
//     every other optional field without re-parsing.
//   - Body is the raw response body, retained for debugging.
type DevhelmAPIError struct {
	StatusCode int
	Code       string
	Category   string
	Message    string
	RequestID  string
	Body       string
	Response   *generated.ErrorResponse
}

func (e *DevhelmAPIError) Error() string {
	parts := []string{}
	if e.StatusCode != 0 {
		parts = append(parts, fmt.Sprintf("API error %d", e.StatusCode))
	} else {
		parts = append(parts, "client error")
	}
	if e.Code != "" {
		parts = append(parts, e.Code)
	}
	body := e.Message
	if body == "" {
		body = e.Body
	}
	if body != "" {
		parts = append(parts, body)
	}
	out := strings.Join(parts, ": ")
	if e.RequestID != "" {
		out = fmt.Sprintf("%s (request_id=%s)", out, e.RequestID)
	}
	return out
}

// NextCommand returns `next.command` from the envelope, or "".
func (e *DevhelmAPIError) NextCommand() string {
	if e.Response != nil && e.Response.Next != nil && e.Response.Next.Command != nil {
		return *e.Response.Next.Command
	}
	return ""
}

// NextDocs returns `next.docs` from the envelope, or "".
func (e *DevhelmAPIError) NextDocs() string {
	if e.Response != nil && e.Response.Next != nil && e.Response.Next.Docs != nil {
		return *e.Response.Next.Docs
	}
	return ""
}

// FieldErrors returns the structured per-field rejections (`errors[]`).
func (e *DevhelmAPIError) FieldErrors() []generated.ErrorEntry {
	if e.Response == nil || e.Response.Errors == nil {
		return nil
	}
	out := make([]generated.ErrorEntry, 0, len(*e.Response.Errors))
	for _, entry := range *e.Response.Errors {
		if entry != nil {
			out = append(out, *entry)
		}
	}
	return out
}

// Detail decodes `details[key]` into out and reports whether it was present.
func (e *DevhelmAPIError) Detail(key string, out any) bool {
	if e.Response == nil || e.Response.Details == nil {
		return false
	}
	raw, ok := (*e.Response.Details)[key]
	if !ok || raw == nil {
		return false
	}
	return json.Unmarshal(*raw, out) == nil
}

// One typed error per category (doc 42 §5.2). Each embeds the shared
// *DevhelmAPIError (so every ErrorResponse field is one hop away) and
// unwraps to it.
type (
	// ValidationError is a malformed or refused request (400, 426), or a
	// client-side refusal raised before the call.
	ValidationError struct{ *DevhelmAPIError }
	// BlockedError is a state precondition such as RUN_KEYS_MISSING (409).
	BlockedError struct{ *DevhelmAPIError }
	// AuthError is a missing, expired or revoked credential (401).
	AuthError struct{ *DevhelmAPIError }
	// PermissionError is a missing scope or role (403).
	PermissionError struct{ *DevhelmAPIError }
	// OwnershipError is a write refused because another tool owns the
	// monitor (409 MONITOR_OWNED_BY_OTHER).
	OwnershipError struct{ *DevhelmAPIError }
	// PlanLimitError is a plan entitlement refusal (402).
	PlanLimitError struct{ *DevhelmAPIError }
	// QuotaError is a metered usage cap such as SYNTHETICS_USAGE_CAPPED (402).
	QuotaError struct{ *DevhelmAPIError }
	// RateLimitError is a 429; RetryAfterSeconds says when to come back.
	RateLimitError struct{ *DevhelmAPIError }
	// ConflictError is a 409 conflict (CAS, rolled-back digest, lock, …).
	ConflictError struct{ *DevhelmAPIError }
	// ConfirmationRequiredError is a gated write awaiting confirmation (409).
	ConfirmationRequiredError struct{ *DevhelmAPIError }
	// NotFoundError is a 404.
	NotFoundError struct{ *DevhelmAPIError }
	// GoneError is a 410 such as RUN_EXPIRED.
	GoneError struct{ *DevhelmAPIError }
	// PlatformError is a 5xx or a platform-side refusal.
	PlatformError struct{ *DevhelmAPIError }
)

func (e *ValidationError) Unwrap() error           { return e.DevhelmAPIError }
func (e *BlockedError) Unwrap() error              { return e.DevhelmAPIError }
func (e *AuthError) Unwrap() error                 { return e.DevhelmAPIError }
func (e *PermissionError) Unwrap() error           { return e.DevhelmAPIError }
func (e *OwnershipError) Unwrap() error            { return e.DevhelmAPIError }
func (e *PlanLimitError) Unwrap() error            { return e.DevhelmAPIError }
func (e *QuotaError) Unwrap() error                { return e.DevhelmAPIError }
func (e *RateLimitError) Unwrap() error            { return e.DevhelmAPIError }
func (e *ConflictError) Unwrap() error             { return e.DevhelmAPIError }
func (e *ConfirmationRequiredError) Unwrap() error { return e.DevhelmAPIError }
func (e *NotFoundError) Unwrap() error             { return e.DevhelmAPIError }
func (e *GoneError) Unwrap() error                 { return e.DevhelmAPIError }
func (e *PlatformError) Unwrap() error             { return e.DevhelmAPIError }

// DevhelmTransportError represents a failure to complete an HTTP exchange:
// the request never reached the server, the server never returned a complete
// response, or a TLS/DNS/connection layer surfaced an error. Wraps the
// underlying error so callers can `errors.As` for the original cause. Its
// category is CategoryTransport.
type DevhelmTransportError struct {
	// Op describes the failing transport step ("build request", "send
	// request", "read response"). Stable enough to switch on in tests.
	Op string
	// URL is the resolved target URL, useful when diagnosing DNS or TLS
	// issues against a specific endpoint.
	URL string
	// Err is the underlying transport-level error.
	Err error
}

// TransportError is the doc 42 §5.2 name for DevhelmTransportError.
type TransportError = DevhelmTransportError

func (e *DevhelmTransportError) Error() string {
	return fmt.Sprintf("transport error during %s to %s: %v", e.Op, e.URL, e.Err)
}

func (e *DevhelmTransportError) Unwrap() error { return e.Err }

// legacyErrorBody covers the fields of pre-envelope error bodies that the
// generated ErrorResponse doesn't carry (`error`), plus the ones a
// non-conforming body may still set.
type legacyErrorBody struct {
	Code      string `json:"code"`
	Category  string `json:"category"`
	Message   string `json:"message"`
	Error     string `json:"error"`
	RequestID string `json:"requestId"`
}

// newAPIError builds the typed error for a non-2xx response. Error bodies
// are decoded leniently: a body that doesn't match ErrorResponse still
// yields an error with the raw body and a status-derived category.
func newAPIError(resp httpResponse) error {
	base := &DevhelmAPIError{
		StatusCode: resp.Status,
		Body:       string(resp.Body),
		RequestID:  resp.RequestID,
	}

	var legacy legacyErrorBody
	if json.Unmarshal(resp.Body, &legacy) == nil {
		base.Code = legacy.Code
		base.Category = legacy.Category
		switch {
		case legacy.Message != "":
			base.Message = legacy.Message
		case legacy.Error != "":
			base.Message = legacy.Error
		}
		// Header always wins (it is set by the server unconditionally),
		// but fall back to the body field for resilience against
		// proxies that strip headers.
		if base.RequestID == "" && legacy.RequestID != "" {
			base.RequestID = legacy.RequestID
		}
		var envelope generated.ErrorResponse
		if json.Unmarshal(resp.Body, &envelope) == nil && legacy.Code != "" {
			base.Response = &envelope
		}
	}
	if base.Category == "" {
		base.Category = categoryForStatus(resp.Status)
	}
	return wrapCategory(base)
}

// NewClientValidationError is a client-side `validation` refusal raised
// before any request is sent (for example BUNDLE_TOO_LARGE or
// BUNDLE_DIGEST_MISMATCH).
func NewClientValidationError(code, message string) error {
	return &ValidationError{&DevhelmAPIError{Code: code, Category: CategoryValidation, Message: message}}
}

// wrapCategory returns the category-specific error for base. A category this
// provider version doesn't know (Postel) keeps its value on base.Category for
// diagnostics and is classified by HTTP status.
func wrapCategory(base *DevhelmAPIError) error {
	category := base.Category
	if !knownCategory(category) {
		category = categoryForStatus(base.StatusCode)
	}
	switch category {
	case CategoryValidation:
		return &ValidationError{base}
	case CategoryBlocked:
		return &BlockedError{base}
	case CategoryAuth:
		return &AuthError{base}
	case CategoryPermission:
		return &PermissionError{base}
	case CategoryOwnership:
		return &OwnershipError{base}
	case CategoryPlan:
		return &PlanLimitError{base}
	case CategoryQuota:
		return &QuotaError{base}
	case CategoryRateLimit:
		return &RateLimitError{base}
	case CategoryConflict:
		return &ConflictError{base}
	case CategoryConfirmationRequired:
		return &ConfirmationRequiredError{base}
	case CategoryNotFound:
		return &NotFoundError{base}
	case CategoryGone:
		return &GoneError{base}
	case CategoryPlatform:
		return &PlatformError{base}
	default:
		return &ValidationError{base}
	}
}

func knownCategory(c string) bool {
	switch c {
	case CategoryValidation, CategoryBlocked, CategoryAuth, CategoryPermission, CategoryOwnership,
		CategoryPlan, CategoryQuota, CategoryRateLimit, CategoryConflict, CategoryConfirmationRequired,
		CategoryNotFound, CategoryGone, CategoryPlatform:
		return true
	default:
		return false
	}
}

// categoryForStatus is the D-1 status → category fallback for bodies that
// don't carry `category`.
func categoryForStatus(status int) string {
	switch {
	case status == http.StatusUnauthorized:
		return CategoryAuth
	case status == http.StatusPaymentRequired:
		return CategoryPlan
	case status == http.StatusForbidden:
		return CategoryPermission
	case status == http.StatusNotFound:
		return CategoryNotFound
	case status == http.StatusConflict:
		return CategoryConflict
	case status == http.StatusGone:
		return CategoryGone
	case status == http.StatusTooManyRequests:
		return CategoryRateLimit
	case status >= 500:
		return CategoryPlatform
	default:
		return CategoryValidation
	}
}

// CategoryOf returns the category of any provider error: the API category,
// CategoryTransport for transport failures, "" otherwise.
func CategoryOf(err error) string {
	var apiErr *DevhelmAPIError
	if errors.As(err, &apiErr) {
		return apiErr.Category
	}
	var transportErr *DevhelmTransportError
	if errors.As(err, &transportErr) {
		return CategoryTransport
	}
	return ""
}

// CodeOf returns the stable error code of an API error, or "".
func CodeOf(err error) string {
	var apiErr *DevhelmAPIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return ""
}
