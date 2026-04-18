package api

import (
	"bytes"
	"context"
	"encoding/json"
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

func (c *Client) doRequest(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	u := c.BaseURL + path

	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshaling request body: %w", err)
		}
	}

	var (
		respBody []byte
		status   int
		lastErr  error
	)

	for attempt := 0; attempt < retryMaxAttempts; attempt++ {
		// Honor caller cancellation between attempts so a Ctrl-C during the
		// retry sleep aborts immediately instead of finishing the back-off.
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}

		respBody, status, lastErr = c.doRequestOnce(ctx, method, u, bodyBytes, body != nil)

		// Network/transport errors are retryable on idempotent verbs (we
		// cannot distinguish "request never reached the server" from "we
		// missed the response", but POST is already excluded above).
		if lastErr != nil {
			if !isIdempotent(method) || attempt == retryMaxAttempts-1 {
				return nil, 0, lastErr
			}
			waitFor(ctx, retryDelay(attempt))
			continue
		}

		// 5xx / 429 retries: only attempt for idempotent verbs and only on
		// the small allow-list above. We rely on the next iteration's
		// checkResponse to surface the final error to the caller.
		if shouldRetryStatus(status) && isIdempotent(method) && attempt < retryMaxAttempts-1 {
			waitFor(ctx, retryDelay(attempt))
			continue
		}

		return respBody, status, nil
	}

	if lastErr != nil {
		return nil, 0, lastErr
	}
	return respBody, status, nil
}

// doRequestOnce performs a single HTTP round trip without retry logic. Split
// from doRequest so the retry loop can call it cleanly; tests can also reach
// it directly to exercise no-retry behavior in the future without faking the
// transport.
func (c *Client) doRequestOnce(ctx context.Context, method, u string, bodyBytes []byte, hasBody bool) ([]byte, int, error) {
	var bodyReader io.Reader
	if bodyBytes != nil {
		bodyReader = bytes.NewBuffer(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("creating request: %w", err)
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
		return nil, 0, fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading response body: %w", err)
	}

	return respBody, resp.StatusCode, nil
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

type APIError struct {
	StatusCode int
	Message    string
	Body       string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Body)
}

func IsNotFound(err error) bool {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.StatusCode == 404
	}
	return false
}

func checkResponse(body []byte, statusCode int) error {
	if statusCode >= 200 && statusCode < 300 {
		return nil
	}

	apiErr := &APIError{StatusCode: statusCode, Body: string(body)}

	var errResp struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(body, &errResp) == nil {
		if errResp.Message != "" {
			apiErr.Message = errResp.Message
		} else if errResp.Error != "" {
			apiErr.Message = errResp.Error
		}
	}

	return apiErr
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
	body, status, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if err := checkResponse(body, status); err != nil {
		return nil, err
	}

	var resp SingleValueResponse[T]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &resp.Data, nil
}

// GetRaw is the escape-hatch variant of Get that also returns the raw
// response body so callers can extract polymorphic / discriminated-union
// fields whose generated Go type loses information during a typed unmarshal
// (e.g. monitor `auth`, where the spec collapsed the oneOf into a base
// `MonitorAuthConfig{Type string}` and only the `type` discriminator survives).
// Use the typed result for everything else; only reach into the raw body for
// the specific field whose round-trip you need to preserve.
func GetRaw[T any](ctx context.Context, c *Client, path string) (*T, []byte, error) {
	body, status, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	if err := checkResponse(body, status); err != nil {
		return nil, nil, err
	}

	var resp SingleValueResponse[T]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, nil, fmt.Errorf("decoding response: %w", err)
	}
	return &resp.Data, body, nil
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

		body, status, err := c.doRequest(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		if err := checkResponse(body, status); err != nil {
			return nil, err
		}

		var resp TableResponse[T]
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("decoding response: %w", err)
		}

		all = append(all, resp.Data...)
		if !resp.HasNext {
			break
		}
		page++
	}

	return all, nil
}

func Create[T any](ctx context.Context, c *Client, path string, body any) (*T, error) {
	respBody, status, err := c.doRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	if err := checkResponse(respBody, status); err != nil {
		return nil, err
	}

	var resp SingleValueResponse[T]
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &resp.Data, nil
}

// CreateList POSTs to an endpoint that returns a TableResponse[T] (e.g. the
// tag-management sub-resources on monitors, which return the full collection
// after the mutation rather than a single entity). Use Create when the
// endpoint returns SingleValueResponse[T].
func CreateList[T any](ctx context.Context, c *Client, path string, body any) ([]T, error) {
	respBody, status, err := c.doRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	if err := checkResponse(respBody, status); err != nil {
		return nil, err
	}

	var resp TableResponse[T]
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return resp.Data, nil
}

// CreateRaw mirrors Create but also returns the raw response body. See GetRaw.
func CreateRaw[T any](ctx context.Context, c *Client, path string, body any) (*T, []byte, error) {
	respBody, status, err := c.doRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, nil, err
	}
	if err := checkResponse(respBody, status); err != nil {
		return nil, nil, err
	}

	var resp SingleValueResponse[T]
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, nil, fmt.Errorf("decoding response: %w", err)
	}
	return &resp.Data, respBody, nil
}

func Update[T any](ctx context.Context, c *Client, path string, body any) (*T, error) {
	respBody, status, err := c.doRequest(ctx, http.MethodPut, path, body)
	if err != nil {
		return nil, err
	}
	if err := checkResponse(respBody, status); err != nil {
		return nil, err
	}

	var resp SingleValueResponse[T]
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &resp.Data, nil
}

// UpdateRaw mirrors Update but also returns the raw response body. See GetRaw.
func UpdateRaw[T any](ctx context.Context, c *Client, path string, body any) (*T, []byte, error) {
	respBody, status, err := c.doRequest(ctx, http.MethodPut, path, body)
	if err != nil {
		return nil, nil, err
	}
	if err := checkResponse(respBody, status); err != nil {
		return nil, nil, err
	}

	var resp SingleValueResponse[T]
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, nil, fmt.Errorf("decoding response: %w", err)
	}
	return &resp.Data, respBody, nil
}

func Patch[T any](ctx context.Context, c *Client, path string, body any) (*T, error) {
	respBody, status, err := c.doRequest(ctx, http.MethodPatch, path, body)
	if err != nil {
		return nil, err
	}
	if err := checkResponse(respBody, status); err != nil {
		return nil, err
	}

	var resp SingleValueResponse[T]
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &resp.Data, nil
}

func Delete(ctx context.Context, c *Client, path string) error {
	body, status, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	return checkResponse(body, status)
}

// DeleteWithBody issues a DELETE with a JSON request body. Used for endpoints
// that accept a body (e.g. DELETE /monitors/{id}/tags with a list of tag IDs).
func DeleteWithBody(ctx context.Context, c *Client, path string, body any) error {
	respBody, status, err := c.doRequest(ctx, http.MethodDelete, path, body)
	if err != nil {
		return err
	}
	return checkResponse(respBody, status)
}

func PathEscape(s string) string {
	return url.PathEscape(s)
}
