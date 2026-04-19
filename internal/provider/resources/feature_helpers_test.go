package resources

import (
	"context"
	"testing"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// ── status_page branding helpers ────────────────────────────────────────

func TestBrandingObjectFromDto_RoundTripsAllFields(t *testing.T) {
	ctx := context.Background()
	brand := strPtr("#4F46E5")
	text := strPtr("#09090B")
	logo := strPtr("https://acme.com/logo.png")
	dto := generated.StatusPageBranding{
		BrandColor:    brand,
		TextColor:     text,
		LogoUrl:       logo,
		HidePoweredBy: true,
	}

	obj := brandingObjectFromDto(ctx, dto)
	if obj.IsNull() || obj.IsUnknown() {
		t.Fatalf("expected concrete object, got null/unknown")
	}

	got, diags := brandingFromObject(ctx, obj)
	if diags.HasError() {
		t.Fatalf("brandingFromObject diagnostics: %v", diags)
	}
	if got.BrandColor == nil || *got.BrandColor != "#4F46E5" {
		t.Errorf("brand_color round-trip failed: %+v", got.BrandColor)
	}
	if got.TextColor == nil || *got.TextColor != "#09090B" {
		t.Errorf("text_color round-trip failed: %+v", got.TextColor)
	}
	if got.LogoUrl == nil || *got.LogoUrl != "https://acme.com/logo.png" {
		t.Errorf("logo_url round-trip failed: %+v", got.LogoUrl)
	}
	if !got.HidePoweredBy {
		t.Errorf("hide_powered_by round-trip lost the true value")
	}
	// Untouched optional pointers must round-trip as nil, not "".
	if got.FaviconUrl != nil {
		t.Errorf("expected nil favicon_url, got %q", *got.FaviconUrl)
	}
}

func TestBrandingForCreate_NullObjectReturnsNilPointer(t *testing.T) {
	ctx := context.Background()
	obj := types.ObjectNull(brandingObjectAttrTypes())
	got, diags := brandingForCreate(ctx, obj)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if got != nil {
		t.Errorf("expected nil branding pointer for null object, got %+v", got)
	}
}

func TestBrandingForUpdate_NullObjectReturnsZeroValue(t *testing.T) {
	ctx := context.Background()
	obj := types.ObjectNull(brandingObjectAttrTypes())
	got, diags := brandingForUpdate(ctx, obj)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	// Zero value: all *string nil, HidePoweredBy false.
	if got.BrandColor != nil || got.HidePoweredBy {
		t.Errorf("expected zero StatusPageBranding, got %+v", got)
	}
}

// ── resource_group default_retry_strategy helpers ───────────────────────

func TestRetryStrategyObjectFromDto_NilDtoReturnsNullObject(t *testing.T) {
	ctx := context.Background()
	got := retryStrategyObjectFromDto(ctx, nil)
	if !got.IsNull() {
		t.Errorf("nil dto → got %v, want null object", got)
	}
}

func TestRetryStrategyObjectFromDto_NullWhenZeroValue(t *testing.T) {
	ctx := context.Background()
	obj := retryStrategyObjectFromDto(ctx, &generated.RetryStrategy{})
	if !obj.IsNull() {
		t.Errorf("expected null object for zero-value RetryStrategy, got %+v", obj)
	}
}

func TestRetryStrategyObjectFromDto_PopulatesAllFields(t *testing.T) {
	ctx := context.Background()
	rs := &generated.RetryStrategy{
		Type:       "fixed",
		Interval:   int32Ptr(60),
		MaxRetries: int32Ptr(3),
	}
	obj := retryStrategyObjectFromDto(ctx, rs)
	if obj.IsNull() || obj.IsUnknown() {
		t.Fatalf("expected concrete object, got %+v", obj)
	}

	got, diags := retryStrategyFromObject(ctx, obj)
	if diags.HasError() {
		t.Fatalf("retryStrategyFromObject diagnostics: %v", diags)
	}
	if got == nil {
		t.Fatal("expected non-nil retry strategy pointer")
	}
	if got.Type != "fixed" {
		t.Errorf("type round-trip failed: got %q", got.Type)
	}
	if got.Interval == nil || *got.Interval != 60 {
		t.Errorf("interval round-trip failed: %+v", got.Interval)
	}
	if got.MaxRetries == nil || *got.MaxRetries != 3 {
		t.Errorf("max_retries round-trip failed: %+v", got.MaxRetries)
	}
}

func TestRetryStrategyFromObject_NullObjectReturnsNilPointer(t *testing.T) {
	ctx := context.Background()
	obj := types.ObjectNull(retryStrategyObjectAttrTypes())
	got, diags := retryStrategyFromObject(ctx, obj)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if got != nil {
		t.Errorf("expected nil pointer for null object, got %+v", got)
	}
}

// ── helpers ─────────────────────────────────────────────────────────────

func strPtr(s string) *string { return &s }
func int32Ptr(i int32) *int32 { return &i }
func boolPtr(v bool) *bool   { return &v }
