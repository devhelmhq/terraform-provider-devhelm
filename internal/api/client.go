package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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
		HTTPClient:  &http.Client{},
		UserAgent:   fmt.Sprintf("terraform-provider-devhelm/%s", version),
	}
}

func (c *Client) doRequest(ctx context.Context, method, path string, body any) ([]byte, int, error) {
	u := c.BaseURL + path

	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshaling request body: %w", err)
		}
		bodyReader = bytes.NewBuffer(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return nil, 0, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("x-phelm-org-id", c.OrgID)
	req.Header.Set("x-phelm-workspace-id", c.WorkspaceID)
	req.Header.Set("User-Agent", c.UserAgent)
	if body != nil {
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

// Single-value response wrapper used by most endpoints.
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

func PathEscape(s string) string {
	return url.PathEscape(s)
}
