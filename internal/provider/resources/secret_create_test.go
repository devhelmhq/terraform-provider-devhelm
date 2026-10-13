package resources

import (
	"testing"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestSecretCreateRequestOmitsEnvironmentWhenUnset(t *testing.T) {
	body, err := secretCreateRequest(SecretResourceModel{
		Key:           types.StringValue("api_key"),
		Value:         types.StringValue("supersecret"),
		EnvironmentID: types.StringNull(),
	})
	if err != nil {
		t.Fatalf("secretCreateRequest: %v", err)
	}
	if body.Key != "api_key" || body.Value != "supersecret" {
		t.Fatalf("body = %+v", body)
	}
	if body.EnvironmentId != nil {
		t.Fatalf("environment id = %v, want nil so existing configs omit the field", body.EnvironmentId)
	}
}

func TestSecretCreateRequestSetsEnvironment(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	body, err := secretCreateRequest(SecretResourceModel{
		Key:           types.StringValue("api_key"),
		Value:         types.StringValue("supersecret"),
		EnvironmentID: types.StringValue(id.String()),
	})
	if err != nil {
		t.Fatalf("secretCreateRequest: %v", err)
	}
	if body.EnvironmentId == nil || body.EnvironmentId.String() != id.String() {
		t.Fatalf("environment id = %v, want %s", body.EnvironmentId, id)
	}
}

func TestSecretCreateRequestRejectsInvalidEnvironment(t *testing.T) {
	_, err := secretCreateRequest(SecretResourceModel{
		Key:           types.StringValue("api_key"),
		Value:         types.StringValue("supersecret"),
		EnvironmentID: types.StringValue("not-a-uuid"),
	})
	if err == nil {
		t.Fatal("expected invalid environment id error")
	}
}
