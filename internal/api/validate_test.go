package api

import (
	"testing"
	"time"

	"github.com/google/uuid"
	generated "github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func TestValidateDTO_ValidMonitorDto(t *testing.T) {
	dto := generated.MonitorDto{
		Id:               openapi_types.UUID(uuid.New()),
		Name:             "test-monitor",
		Type:             generated.MonitorDtoTypeHTTP,
		ManagedBy:        generated.MonitorDtoManagedByDASHBOARD,
		FrequencySeconds: 60,
		OrganizationId:   1,
		Regions:          []string{"us-east"},
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	if err := ValidateDTO(&dto, "test"); err != nil {
		t.Errorf("expected valid DTO, got error: %v", err)
	}
}

func TestValidateDTO_ZeroID(t *testing.T) {
	dto := generated.MonitorDto{
		Name:      "test",
		Type:      generated.MonitorDtoTypeHTTP,
		ManagedBy: generated.MonitorDtoManagedByDASHBOARD,
		Regions:   []string{"us-east"},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err := ValidateDTO(&dto, "monitor.Create")
	if err == nil {
		t.Fatal("expected error for zero UUID id")
	}
	if got := err.Error(); !contains(got, "id: required field is missing or zero") {
		t.Errorf("expected id error, got: %s", got)
	}
}

func TestValidateDTO_MissingName(t *testing.T) {
	dto := generated.MonitorDto{
		Id:        openapi_types.UUID(uuid.New()),
		Type:      generated.MonitorDtoTypeHTTP,
		ManagedBy: generated.MonitorDtoManagedByDASHBOARD,
		Regions:   []string{"us-east"},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err := ValidateDTO(&dto, "monitor.Read")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if got := err.Error(); !contains(got, "name: required field is missing or zero") {
		t.Errorf("expected name error, got: %s", got)
	}
}

func TestValidateDTO_InvalidEnumType(t *testing.T) {
	dto := generated.MonitorDto{
		Id:        openapi_types.UUID(uuid.New()),
		Name:      "test",
		Type:      generated.MonitorDtoType("INVALID_PROTOCOL"),
		ManagedBy: generated.MonitorDtoManagedByDASHBOARD,
		Regions:   []string{"us-east"},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err := ValidateDTO(&dto, "monitor.Read")
	if err == nil {
		t.Fatal("expected error for invalid enum")
	}
	if got := err.Error(); !contains(got, "type: unknown enum value") {
		t.Errorf("expected type enum error, got: %s", got)
	}
}

func TestValidateDTO_InvalidEnumManagedBy(t *testing.T) {
	dto := generated.MonitorDto{
		Id:        openapi_types.UUID(uuid.New()),
		Name:      "test",
		Type:      generated.MonitorDtoTypeHTTP,
		ManagedBy: generated.MonitorDtoManagedBy("UNKNOWN_SOURCE"),
		Regions:   []string{"us-east"},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err := ValidateDTO(&dto, "monitor.Read")
	if err == nil {
		t.Fatal("expected error for invalid managedBy enum")
	}
	if got := err.Error(); !contains(got, "managedBy: unknown enum value") {
		t.Errorf("expected managedBy enum error, got: %s", got)
	}
}

func TestValidateDTO_MissingRequiredRegions(t *testing.T) {
	dto := generated.MonitorDto{
		Id:        openapi_types.UUID(uuid.New()),
		Name:      "test",
		Type:      generated.MonitorDtoTypeHTTP,
		ManagedBy: generated.MonitorDtoManagedByDASHBOARD,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err := ValidateDTO(&dto, "monitor.Read")
	if err == nil {
		t.Fatal("expected error for nil required regions slice")
	}
	if got := err.Error(); !contains(got, "regions: required array is missing") {
		t.Errorf("expected regions error, got: %s", got)
	}
}

func TestValidateDTO_MultipleErrors(t *testing.T) {
	dto := generated.MonitorDto{
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	err := ValidateDTO(&dto, "monitor")
	if err == nil {
		t.Fatal("expected multiple errors")
	}
	got := err.Error()
	for _, want := range []string{"id", "name", "managedBy", "type", "regions"} {
		if !contains(got, want) {
			t.Errorf("expected error mentioning %q, got: %s", want, got)
		}
	}
}

func TestValidateDTO_OptionalFieldsSkipped(t *testing.T) {
	dto := generated.MonitorDto{
		Id:               openapi_types.UUID(uuid.New()),
		Name:             "test",
		Type:             generated.MonitorDtoTypeHTTP,
		ManagedBy:        generated.MonitorDtoManagedByDASHBOARD,
		FrequencySeconds: 60,
		OrganizationId:   1,
		Regions:          []string{"us-east"},
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	if err := ValidateDTO(&dto, "test"); err != nil {
		t.Errorf("optional nil fields should not cause errors: %v", err)
	}
}

func TestValidateDTO_AlertChannelDto(t *testing.T) {
	dto := generated.AlertChannelDto{
		Id:          openapi_types.UUID(uuid.New()),
		Name:        "slack-alerts",
		ChannelType: generated.AlertChannelDtoChannelType("NONEXISTENT"),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	err := ValidateDTO(&dto, "alert-channel.Read")
	if err == nil {
		t.Fatal("expected error for invalid channel type")
	}
	if got := err.Error(); !contains(got, "channelType: unknown enum value") {
		t.Errorf("expected channelType enum error, got: %s", got)
	}
}

func TestValidateDTO_StatusPageComponentDto(t *testing.T) {
	dto := generated.StatusPageComponentDto{
		Id:            openapi_types.UUID(uuid.New()),
		Name:          "api-component",
		Type:          generated.StatusPageComponentDtoTypeMONITOR,
		CurrentStatus: generated.StatusPageComponentDtoCurrentStatusOPERATIONAL,
		StatusPageId:  openapi_types.UUID(uuid.New()),
		DisplayOrder:  1,
		PageOrder:     1,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	if err := ValidateDTO(&dto, "component.Read"); err != nil {
		t.Errorf("valid component DTO should pass: %v", err)
	}
}

func TestValidateDTO_NilDto(t *testing.T) {
	err := ValidateDTO((*generated.MonitorDto)(nil), "test")
	if err == nil {
		t.Fatal("expected error for nil DTO")
	}
	if got := err.Error(); !contains(got, "nil") {
		t.Errorf("expected nil error, got: %s", got)
	}
}

func TestValidateDTO_NonStructPassthrough(t *testing.T) {
	s := "just a string"
	if err := ValidateDTO(s, "test"); err != nil {
		t.Errorf("non-struct should passthrough: %v", err)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsImpl(s, substr)
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
