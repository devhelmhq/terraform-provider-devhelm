package datasources

import (
	"testing"
	"time"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
)

func TestSelectRuntimePrefersPlatformDefault(t *testing.T) {
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	catalog := []generated.RuntimeProfileDto{
		{Id: "current", Status: "deprecated", PlatformDefault: true, AvailableAt: &older},
		{Id: "next", Status: "available", AvailableAt: &newer},
	}
	got := selectRuntime(catalog)
	if got == nil || got.Id != "current" {
		t.Fatalf("platform default = %v, want current", idOf(got))
	}

	fallback := []generated.RuntimeProfileDto{
		{Id: "a", Status: "available", AvailableAt: &older},
		{Id: "b", Status: "available", AvailableAt: &newer},
	}
	got = selectRuntime(fallback)
	if got == nil || got.Id != "b" {
		t.Fatalf("fallback = %v, want b", idOf(got))
	}

	if selectRuntime(nil) != nil {
		t.Fatal("empty catalog returned a runtime")
	}

	retired := []generated.RuntimeProfileDto{
		{Id: "old", Status: "retired", PlatformDefault: true},
		{Id: "next", Status: "available", AvailableAt: &newer},
	}
	if selectRuntime(retired) != nil {
		t.Fatal("retired platform default fell through to another runtime")
	}
}

func idOf(profile *generated.RuntimeProfileDto) string {
	if profile == nil {
		return "<nil>"
	}
	return profile.Id
}
