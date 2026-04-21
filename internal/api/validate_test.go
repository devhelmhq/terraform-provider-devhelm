package api

import (
	"testing"
	"time"

	generated "github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
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

// TestValidateDTO_ZeroNumericIsValid pins the contract that required
// numeric primitives are allowed to be zero in a legitimate API response.
//
// Regression for: surface-level failures where the validator falsely
// rejected fresh resources because the API sent the natural zero
// (consecutiveFailures: 0 on a brand-new webhook, monitorCount: 0 on an
// empty resource group, priority: 0 on a default notification policy,
// frequencySeconds/organizationId never reaching the wire as 0 in
// practice but covered by the same invariant).
func TestValidateDTO_ZeroNumericIsValid(t *testing.T) {
	t.Run("WebhookEndpointDto with zero consecutiveFailures", func(t *testing.T) {
		dto := generated.WebhookEndpointDto{
			Id:                  openapi_types.UUID(uuid.New()),
			Url:                 "https://example.com/hook",
			Enabled:             true,
			ConsecutiveFailures: 0,
			SubscribedEvents:    []string{"incident.opened"},
			CreatedAt:           time.Now(),
			UpdatedAt:           time.Now(),
		}
		if err := ValidateDTO(&dto, "webhook.Create"); err != nil {
			t.Fatalf("zero consecutiveFailures must validate, got: %v", err)
		}
	})

	t.Run("EnvironmentDto with zero monitorCount and orgId", func(t *testing.T) {
		dto := generated.EnvironmentDto{
			Id:           openapi_types.UUID(uuid.New()),
			Name:         "empty-env",
			Slug:         "empty-env",
			MonitorCount: 0,
			OrgId:        0,
			Variables:    map[string]string{},
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
		if err := ValidateDTO(&dto, "environment.Read"); err != nil {
			t.Fatalf("zero monitorCount/orgId must validate, got: %v", err)
		}
	})

	t.Run("ResourceGroupDto with all-zero numeric fields", func(t *testing.T) {
		dto := generated.ResourceGroupDto{
			Id:             openapi_types.UUID(uuid.New()),
			Name:           "empty-group",
			Slug:           "empty-group",
			OrganizationId: 0,
			Health: generated.ResourceGroupHealthDto{
				ActiveIncidents:  0,
				OperationalCount: 0,
				TotalMembers:     0,
				Status:           generated.ResourceGroupHealthDtoStatusOperational,
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := ValidateDTO(&dto, "resource-group.Read"); err != nil {
			t.Fatalf("zero numeric fields must validate, got: %v", err)
		}
	})

	t.Run("NotificationPolicyDto with zero priority and organizationId", func(t *testing.T) {
		dto := generated.NotificationPolicyDto{
			Id:             openapi_types.UUID(uuid.New()),
			Name:           "default-policy",
			Priority:       0,
			OrganizationId: 0,
			Enabled:        true,
			MatchRules:     []generated.MatchRule{},
			Escalation: generated.EscalationChain{
				Steps: []generated.EscalationStep{},
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		if err := ValidateDTO(&dto, "notification-policy.Read"); err != nil {
			t.Fatalf("zero priority/organizationId must validate, got: %v", err)
		}
	})
}

// TestValidateDTO_RequiredStringStillRejected pins the complementary
// contract: required string fields with the zero value ("") are still
// rejected, since the spec does not allow empty strings as valid
// required output and the value carries semantic meaning the client
// would otherwise silently accept.
func TestValidateDTO_RequiredStringStillRejected(t *testing.T) {
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
		t.Fatal("expected error for empty required name string")
	}
	if got := err.Error(); !contains(got, "name: required field is missing or zero") {
		t.Errorf("expected name error, got: %s", got)
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
