package api

import (
	"encoding/json"
	"testing"

	generated "github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
)

// Postel's Law by reachability: enums on schemas reached only from responses
// are generated as plain strings, so a value this provider has never seen
// decodes and validates. ErrorCategory is a named enum component referenced
// from ErrorResponse; it used to be a closed enum type with Valid().
func TestValidateDTO_ResponseOnlyEnumComponentAcceptsUnknownValue(t *testing.T) {
	body := []byte(`{
		"status": 429,
		"code": "SOMETHING_NEW",
		"category": "future_category",
		"retryable": true,
		"message": "Slow down",
		"timestamp": 1700000000000,
		"next": {"command": "devhelm account usage", "docs": null, "video": "x"}
	}`)
	var dto generated.ErrorResponse
	if err := json.Unmarshal(body, &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := ValidateDTO(&dto, "error"); err != nil {
		t.Fatalf("expected tolerant decode, got: %v", err)
	}
	if dto.Category != "future_category" {
		t.Errorf("category = %q", dto.Category)
	}
}

// Nested response-only object with an inline enum (no Dto/Response suffix).
func TestValidateDTO_ResponseOnlyNestedEnumAcceptsUnknownValue(t *testing.T) {
	dto := generated.StateTransitionDetails{Source: "future-source"}
	if err := ValidateDTO(&dto, "details"); err != nil {
		t.Fatalf("expected tolerant decode, got: %v", err)
	}
}
