package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// defaultHTTPTimeout bounds each individual request, protecting against hung
// connections to the API. It is intentionally generous: we support imports and
// plan dry-runs that may fan out many list calls.
const defaultHTTPTimeout = 60 * time.Second

// requestIDHeader is the response header the API sets on every reply (success
// or error). We thread it through the client so that every DevhelmAPIError
// carries the id, which is the single most useful piece of context for a
// support ticket. See `mono/api/.../RequestCorrelationFilter.java`.
const requestIDHeader = "X-Request-Id"

// Retry tuning for transient 5xx and connection-level failures on idempotent
// verbs (GET, PUT, DELETE — POST is excluded, see isIdempotent below).
//
//   - retryMaxAttempts is total attempts including the first try.
//   - retryBaseDelay is the initial back-off; each subsequent retry doubles
//     the delay and adds jitter (50–150% of the computed value) so a spike
//     of providers retrying simultaneously does not produce a thundering
//     herd against a recovering API.
//
// Values were chosen to keep the worst-case wait under 10s (1+2+4=7s plus
// jitter), which is well below the per-request timeout and the typical
// Terraform CLI patience for an apply step.
const (
	retryMaxAttempts = 4
	retryBaseDelay   = 500 * time.Millisecond
)

type Client struct {
	BaseURL     string
	Token       string
	OrgID       string
	WorkspaceID string
	HTTPClient  *http.Client
	UserAgent   string
}

func NewClient(baseURL, token, orgID, workspaceID, version string) *Client {
	return &Client{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		Token:       token,
		OrgID:       orgID,
		WorkspaceID: workspaceID,
		HTTPClient:  &http.Client{Timeout: defaultHTTPTimeout},
		UserAgent:   fmt.Sprintf("terraform-provider-devhelm/%s", version),
	}
}

// isIdempotent returns true for HTTP verbs whose retry is safe under the
// REST contract. POST is excluded because retrying a successful-but-network-
// dropped POST would create duplicate resources; the caller (or, eventually,
// an Idempotency-Key header on the API) must handle that case explicitly.
func isIdempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

// shouldRetryStatus identifies HTTP status codes that warrant a retry on
// idempotent verbs. 408 and 429 are explicit "try again" signals; 502/503/504
// usually indicate a transient upstream blip (e.g. a rolling pod restart on
// the API). 500 is intentionally NOT retried because it commonly indicates a
// deterministic bug that retrying would only mask; 501 and 505 are similarly
// non-transient.
func shouldRetryStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// retryDelay returns the back-off for the n-th retry (0-indexed). The result
// is `base * 2^n` with ±50% jitter applied via the package-level rand source,
// which is sufficient for back-off jitter (we explicitly do not need crypto-
// strong randomness here).
func retryDelay(attempt int) time.Duration {
	d := retryBaseDelay << attempt
	jitter := time.Duration(rand.Int63n(int64(d)))
	return d/2 + jitter
}

// RequestBody is the P5-tracked boundary type for any request payload that
// `doRequest` will serialize via `json.Marshal`. Callers must pass a typed
// struct from `internal/generated` (or a properly tagged handwritten
// equivalent), never a raw `map[string]any`. The alias is here so a future
// audit can grep for `RequestBody` and find every site that crosses the
// json.Marshal boundary, even though Go's type system can't prevent the
// `map[string]any` case at compile time.
type RequestBody = any

// httpResponse captures everything `doRequest`'s callers need from a single
// successful round trip: the body bytes, the HTTP status, and the per-request
// id from the API's `X-Request-Id` response header. Carrying the request id
// on the value (rather than only on errors) lets us include it in any future
// debug/info diagnostic a resource may want to surface alongside a successful
// reply.
type httpResponse struct {
	Body      []byte
	Status    int
	RequestID string
}

func (c *Client) doRequest(ctx context.Context, method, path string, body RequestBody) (httpResponse, error) {
	u := c.BaseURL + path

	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return httpResponse{}, fmt.Errorf("marshaling request body: %w", err)
		}
	}

	var (
		resp    httpResponse
		lastErr error
	)

	for attempt := 0; attempt < retryMaxAttempts; attempt++ {
		// Honor caller cancellation between attempts so a Ctrl-C during the
		// retry sleep aborts immediately instead of finishing the back-off.
		if err := ctx.Err(); err != nil {
			return httpResponse{}, err
		}

		resp, lastErr = c.doRequestOnce(ctx, method, u, bodyBytes, body != nil)

		// Network/transport errors are retryable on idempotent verbs (we
		// cannot distinguish "request never reached the server" from "we
		// missed the response", but POST is already excluded above).
		if lastErr != nil {
			if !isIdempotent(method) || attempt == retryMaxAttempts-1 {
				return httpResponse{}, lastErr
			}
			waitFor(ctx, retryDelay(attempt))
			continue
		}

		// 5xx / 429 retries: only attempt for idempotent verbs and only on
		// the small allow-list above. We rely on the next iteration's
		// checkResponse to surface the final error to the caller.
		if shouldRetryStatus(resp.Status) && isIdempotent(method) && attempt < retryMaxAttempts-1 {
			waitFor(ctx, retryDelay(attempt))
			continue
		}

		return resp, nil
	}

	if lastErr != nil {
		return httpResponse{}, lastErr
	}
	return resp, nil
}

// doRequestOnce performs a single HTTP round trip without retry logic. Split
// from doRequest so the retry loop can call it cleanly; tests can also reach
// it directly to exercise no-retry behavior in the future without faking the
// transport.
//
// Network/transport-level failures (request construction, dial, read) are
// wrapped in a *DevhelmTransportError so callers can `errors.As` to tell
// "the API said no" apart from "we never reached the API". Non-2xx responses
// are not errors at this layer — they flow up as a populated httpResponse and
// are converted to *DevhelmAPIError by checkResponse.
func (c *Client) doRequestOnce(ctx context.Context, method, u string, bodyBytes []byte, hasBody bool) (httpResponse, error) {
	var bodyReader io.Reader
	if bodyBytes != nil {
		bodyReader = bytes.NewBuffer(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return httpResponse{}, &DevhelmTransportError{Op: "build request", URL: u, Err: err}
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("x-phelm-org-id", c.OrgID)
	req.Header.Set("x-phelm-workspace-id", c.WorkspaceID)
	req.Header.Set("User-Agent", c.UserAgent)
	if hasBody {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return httpResponse{}, &DevhelmTransportError{Op: "send request", URL: u, Err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return httpResponse{}, &DevhelmTransportError{Op: "read response", URL: u, Err: err}
	}

	return httpResponse{
		Body:      respBody,
		Status:    resp.StatusCode,
		RequestID: resp.Header.Get(requestIDHeader),
	}, nil
}

// waitFor sleeps for d, returning early when the context is cancelled. We
// ignore the context error here; the next iteration of the retry loop checks
// ctx.Err() and surfaces it to the caller.
func waitFor(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

// DevhelmAPIError represents a non-2xx response from the DevHelm API. It is
// the error class every TF resource wraps via `api.AddAPIError` (or branches
// on via `api.IsNotFound`).
//
//   - StatusCode is the HTTP status line (always non-zero for this error type).
//   - Code mirrors `ErrorResponse.code` from the API — a stable, machine-
//     readable category like "NOT_FOUND" or "RATE_LIMITED". May be empty for
//     legacy error bodies that do not include the field.
//   - Message is the human-readable text from `ErrorResponse.message`, or the
//     `error` field, or — when neither is present — the raw response body.
//   - RequestID is the `X-Request-Id` response header. Always include it in
//     support tickets; when blank, the response did not carry the header
//     (should not happen against the production API).
//   - Body is the raw response body, retained for debugging non-conforming
//     replies.
type DevhelmAPIError struct {
	StatusCode int
	Code       string
	Message    string
	RequestID  string
	Body       string
}

func (e *DevhelmAPIError) Error() string {
	parts := []string{fmt.Sprintf("API error %d", e.StatusCode)}
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

// DevhelmTransportError represents a failure to complete an HTTP exchange:
// the request never reached the server, the server never returned a complete
// response, or a TLS/DNS/connection layer surfaced an error. Wraps the
// underlying error so callers can `errors.As` for the original cause.
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

func (e *DevhelmTransportError) Error() string {
	return fmt.Sprintf("transport error during %s to %s: %v", e.Op, e.URL, e.Err)
}

func (e *DevhelmTransportError) Unwrap() error { return e.Err }

// IsNotFound reports whether err is a *DevhelmAPIError with HTTP 404. Used by
// resources during Read to translate "deleted out-of-band" into the
// disappear-from-state path Terraform expects.
func IsNotFound(err error) bool {
	var apiErr *DevhelmAPIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}
	return false
}

// errorResponseBody is the canonical envelope returned by the DevHelm API on
// every non-2xx response. We intentionally do NOT use DisallowUnknownFields
// here: error bodies are user-facing, and we'd rather surface a slightly
// underspecified message than mask a real API failure with a parse failure.
type errorResponseBody struct {
	Status    int    `json:"status"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Error     string `json:"error"`
	RequestID string `json:"requestId"`
}

func checkResponse(resp httpResponse) error {
	if resp.Status >= 200 && resp.Status < 300 {
		return nil
	}

	apiErr := &DevhelmAPIError{
		StatusCode: resp.Status,
		Body:       string(resp.Body),
		RequestID:  resp.RequestID,
	}

	var parsed errorResponseBody
	if json.Unmarshal(resp.Body, &parsed) == nil {
		if parsed.Code != "" {
			apiErr.Code = parsed.Code
		}
		switch {
		case parsed.Message != "":
			apiErr.Message = parsed.Message
		case parsed.Error != "":
			apiErr.Message = parsed.Error
		}
		// Header always wins (it is set by the server unconditionally),
		// but fall back to the body field for resilience against
		// proxies that strip headers.
		if apiErr.RequestID == "" && parsed.RequestID != "" {
			apiErr.RequestID = parsed.RequestID
		}
	}

	return apiErr
}

// decodeStrict unmarshals body into out using a strict decoder that rejects
// unknown top-level or nested fields (P1 — "unknown response fields raise
// loudly"). A drift in the API spec produces a typed decode error instead of
// silently discarding the field, which would mask spec evolution from the
// next plan/apply cycle. Error bodies stay lenient (see checkResponse).
func decodeStrict(body []byte, out any, context string) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("decoding response (%s): %w", context, err)
	}
	return nil
}

// SingleValueResponse is the single-value envelope used by most endpoints.
//
// Note on the zero-value contract: when the upstream JSON is malformed or the
// server returns a 2xx without a `data` field (which the API contract should
// never produce, but is worth being explicit about), `Data` will be the zero
// value of `T` and `Get`/`Create`/`Update`/`Patch` will return a non-nil
// pointer to that zero value. Callers should NOT treat a non-nil return as
// "the resource exists" — they should rely on the per-resource semantic
// invariants instead (e.g. `dto.Id != uuid.Nil` for newly-created resources,
// or `IsNotFound(err)` for explicit 404 handling). Nil-checking `&resp.Data`
// itself is meaningless because the address of a value-typed struct field
// inside a stack-allocated struct is always non-nil.
type SingleValueResponse[T any] struct {
	Data T `json:"data"`
}

// Table response wrapper used by list endpoints.
type TableResponse[T any] struct {
	Data    []T  `json:"data"`
	HasNext bool `json:"hasNext"`
}

func Get[T any](ctx context.Context, c *Client, path string) (*T, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if err := checkResponse(resp); err != nil {
		return nil, err
	}

	var envelope SingleValueResponse[T]
	if err := decodeStrict(resp.Body, &envelope, "GET "+path); err != nil {
		return nil, err
	}
	if err := ValidateDTO(&envelope.Data, "GET "+path); err != nil {
		return nil, err
	}
	return &envelope.Data, nil
}

// GetRaw is the escape-hatch variant of Get that also returns the raw
// response body so callers can extract polymorphic / discriminated-union
// fields whose generated Go type loses information during a typed unmarshal
// (e.g. monitor `auth`, where the spec collapsed the oneOf into a base
// `MonitorAuthConfig{Type string}` and only the `type` discriminator survives).
// Use the typed result for everything else; only reach into the raw body for
// the specific field whose round-trip you need to preserve.
func GetRaw[T any](ctx context.Context, c *Client, path string) (*T, []byte, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	if err := checkResponse(resp); err != nil {
		return nil, nil, err
	}

	var envelope SingleValueResponse[T]
	if err := decodeStrict(resp.Body, &envelope, "GET "+path); err != nil {
		return nil, nil, err
	}
	if err := ValidateDTO(&envelope.Data, "GET "+path); err != nil {
		return nil, nil, err
	}
	return &envelope.Data, resp.Body, nil
}

func List[T any](ctx context.Context, c *Client, basePath string) ([]T, error) {
	var all []T
	page := 0

	for {
		sep := "?"
		if strings.Contains(basePath, "?") {
			sep = "&"
		}
		path := fmt.Sprintf("%s%spage=%d&size=100", basePath, sep, page)

		resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		if err := checkResponse(resp); err != nil {
			return nil, err
		}

		var envelope TableResponse[T]
		if err := decodeStrict(resp.Body, &envelope, "LIST "+basePath); err != nil {
			return nil, err
		}

		for i := range envelope.Data {
			if err := ValidateDTO(&envelope.Data[i], fmt.Sprintf("LIST %s[%d]", basePath, len(all)+i)); err != nil {
				return nil, err
			}
		}

		all = append(all, envelope.Data...)
		if !envelope.HasNext {
			break
		}
		page++
	}

	return all, nil
}

func Create[T any](ctx context.Context, c *Client, path string, body any) (*T, error) {
	resp, err := c.doRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	if err := checkResponse(resp); err != nil {
		return nil, err
	}

	var envelope SingleValueResponse[T]
	if err := decodeStrict(resp.Body, &envelope, "POST "+path); err != nil {
		return nil, err
	}
	if err := ValidateDTO(&envelope.Data, "POST "+path); err != nil {
		return nil, err
	}
	return &envelope.Data, nil
}

// CreateList POSTs to an endpoint that returns a TableResponse[T] (e.g. the
// tag-management sub-resources on monitors, which return the full collection
// after the mutation rather than a single entity). Use Create when the
// endpoint returns SingleValueResponse[T].
func CreateList[T any](ctx context.Context, c *Client, path string, body any) ([]T, error) {
	resp, err := c.doRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	if err := checkResponse(resp); err != nil {
		return nil, err
	}

	var envelope TableResponse[T]
	if err := decodeStrict(resp.Body, &envelope, "POST "+path); err != nil {
		return nil, err
	}
	for i := range envelope.Data {
		if err := ValidateDTO(&envelope.Data[i], fmt.Sprintf("POST %s[%d]", path, i)); err != nil {
			return nil, err
		}
	}
	return envelope.Data, nil
}

// CreateRaw mirrors Create but also returns the raw response body. See GetRaw.
func CreateRaw[T any](ctx context.Context, c *Client, path string, body any) (*T, []byte, error) {
	resp, err := c.doRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, nil, err
	}
	if err := checkResponse(resp); err != nil {
		return nil, nil, err
	}

	var envelope SingleValueResponse[T]
	if err := decodeStrict(resp.Body, &envelope, "POST "+path); err != nil {
		return nil, nil, err
	}
	if err := ValidateDTO(&envelope.Data, "POST "+path); err != nil {
		return nil, nil, err
	}
	return &envelope.Data, resp.Body, nil
}

func Update[T any](ctx context.Context, c *Client, path string, body any) (*T, error) {
	resp, err := c.doRequest(ctx, http.MethodPut, path, body)
	if err != nil {
		return nil, err
	}
	if err := checkResponse(resp); err != nil {
		return nil, err
	}

	var envelope SingleValueResponse[T]
	if err := decodeStrict(resp.Body, &envelope, "PUT "+path); err != nil {
		return nil, err
	}
	if err := ValidateDTO(&envelope.Data, "PUT "+path); err != nil {
		return nil, err
	}
	return &envelope.Data, nil
}

// UpdateRaw mirrors Update but also returns the raw response body. See GetRaw.
func UpdateRaw[T any](ctx context.Context, c *Client, path string, body any) (*T, []byte, error) {
	resp, err := c.doRequest(ctx, http.MethodPut, path, body)
	if err != nil {
		return nil, nil, err
	}
	if err := checkResponse(resp); err != nil {
		return nil, nil, err
	}

	var envelope SingleValueResponse[T]
	if err := decodeStrict(resp.Body, &envelope, "PUT "+path); err != nil {
		return nil, nil, err
	}
	if err := ValidateDTO(&envelope.Data, "PUT "+path); err != nil {
		return nil, nil, err
	}
	return &envelope.Data, resp.Body, nil
}

func Patch[T any](ctx context.Context, c *Client, path string, body any) (*T, error) {
	resp, err := c.doRequest(ctx, http.MethodPatch, path, body)
	if err != nil {
		return nil, err
	}
	if err := checkResponse(resp); err != nil {
		return nil, err
	}

	var envelope SingleValueResponse[T]
	if err := decodeStrict(resp.Body, &envelope, "PATCH "+path); err != nil {
		return nil, err
	}
	if err := ValidateDTO(&envelope.Data, "PATCH "+path); err != nil {
		return nil, err
	}
	return &envelope.Data, nil
}

func Delete(ctx context.Context, c *Client, path string) error {
	resp, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// DeleteWithBody issues a DELETE with a JSON request body. Used for endpoints
// that accept a body (e.g. DELETE /monitors/{id}/tags with a list of tag IDs).
func DeleteWithBody(ctx context.Context, c *Client, path string, body any) error {
	resp, err := c.doRequest(ctx, http.MethodDelete, path, body)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

func PathEscape(s string) string {
	return url.PathEscape(s)
}
