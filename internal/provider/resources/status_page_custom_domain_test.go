package resources

import (
	"testing"
	"time"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

func TestStatusPageCustomDomain_MapToState_CNAMEMethodCollapsesRecords(t *testing.T) {
	r := &StatusPageCustomDomainResource{}
	model := &StatusPageCustomDomainResourceModel{
		StatusPageID: types.StringValue("00000000-0000-0000-0000-000000000001"),
	}

	verifiedAt := time.Date(2026, 4, 17, 12, 30, 45, 0, time.UTC)
	dto := &generated.StatusPageCustomDomainDto{
		Id:                      openapi_types.UUID(uuid.MustParse("00000000-0000-0000-0000-000000000abc")),
		Hostname:                "status.acme.com",
		Status:                  generated.VERIFIED,
		VerificationMethod:      generated.StatusPageCustomDomainDtoVerificationMethodCNAME,
		VerificationToken:       "tok-cname-unused",
		VerificationCnameTarget: "acme.devhelm.io",
		VerifiedAt:              &verifiedAt,
		Primary:                 true,
	}

	r.mapToState(model, dto)

	if got := model.Hostname.ValueString(); got != "status.acme.com" {
		t.Fatalf("hostname: got %q, want status.acme.com", got)
	}
	if got := model.VerifiedAt.ValueString(); got != "2026-04-17T12:30:45Z" {
		t.Fatalf("verified_at: got %q, want 2026-04-17T12:30:45Z", got)
	}

	v := dnsRecordFields(t, model.VerificationRecord)
	if v.name != "status.acme.com" || v.recordType != "CNAME" || v.value != "acme.devhelm.io" {
		t.Errorf("CNAME-method verification record mismatch: %+v", v)
	}

	traffic := dnsRecordFields(t, model.TrafficRecord)
	if traffic.name != "status.acme.com" || traffic.recordType != "CNAME" || traffic.value != "acme.devhelm.io" {
		t.Errorf("traffic record mismatch in CNAME-method case: %+v", traffic)
	}

	// In the CNAME-method case the records should be byte-for-byte identical.
	if !model.VerificationRecord.Equal(model.TrafficRecord) {
		t.Errorf("CNAME-method: verification_record and traffic_record should be identical")
	}
}

func TestStatusPageCustomDomain_MapToState_TXTMethodSplitsRecords(t *testing.T) {
	r := &StatusPageCustomDomainResource{}
	model := &StatusPageCustomDomainResourceModel{
		StatusPageID: types.StringValue("00000000-0000-0000-0000-000000000001"),
	}

	dto := &generated.StatusPageCustomDomainDto{
		Id:                      openapi_types.UUID(uuid.MustParse("00000000-0000-0000-0000-000000000abc")),
		Hostname:                "status.acme.com",
		Status:                  generated.PENDINGVERIFICATION,
		VerificationMethod:      generated.StatusPageCustomDomainDtoVerificationMethodTXT,
		VerificationToken:       "txt-token-xyz",
		VerificationCnameTarget: "acme.devhelm.io",
		VerifiedAt:              nil,
	}

	r.mapToState(model, dto)

	if !model.VerifiedAt.IsNull() {
		t.Errorf("verified_at should be null when DTO has nil VerifiedAt, got %q", model.VerifiedAt.ValueString())
	}

	v := dnsRecordFields(t, model.VerificationRecord)
	if v.name != "_devhelm-verification.status.acme.com" {
		t.Errorf("TXT verification.name: got %q, want _devhelm-verification.status.acme.com", v.name)
	}
	if v.recordType != "TXT" {
		t.Errorf("TXT verification.type: got %q, want TXT", v.recordType)
	}
	if v.value != "txt-token-xyz" {
		t.Errorf("TXT verification.value: got %q, want txt-token-xyz", v.value)
	}

	traffic := dnsRecordFields(t, model.TrafficRecord)
	if traffic.name != "status.acme.com" || traffic.recordType != "CNAME" || traffic.value != "acme.devhelm.io" {
		t.Errorf("traffic record should always be CNAME at hostname → cname target; got %+v", traffic)
	}

	// In TXT-method the verification and traffic records MUST differ —
	// otherwise users only create one record and verification fails.
	if model.VerificationRecord.Equal(model.TrafficRecord) {
		t.Errorf("TXT-method: verification_record and traffic_record must NOT be identical")
	}
}

func TestStatusPageCustomDomainVerification_IsVerifiedStatus(t *testing.T) {
	cases := []struct {
		status generated.StatusPageCustomDomainDtoStatus
		want   bool
	}{
		{generated.VERIFIED, true},
		{generated.SSLPENDING, true},
		{generated.ACTIVE, true},
		{generated.PENDINGVERIFICATION, false},
		{generated.VERIFICATIONFAILED, false},
		{generated.FAILED, false},
		{generated.REMOVED, false},
	}
	for _, tc := range cases {
		if got := isVerifiedStatus(tc.status); got != tc.want {
			t.Errorf("isVerifiedStatus(%s) = %v, want %v", tc.status, got, tc.want)
		}
	}
}

func TestStatusPageCustomDomainVerification_PollingParams_Defaults(t *testing.T) {
	t.Setenv("DEVHELM_TF_VERIFY_POLL_INTERVAL_MS", "")
	t.Setenv("DEVHELM_TF_VERIFY_MAX_ATTEMPTS", "")

	interval, maxAttempts := verifyPollingParams()
	if interval != defaultVerifyPollInterval {
		t.Errorf("default interval: got %s, want %s", interval, defaultVerifyPollInterval)
	}
	if maxAttempts != defaultVerifyMaxAttempts {
		t.Errorf("default max attempts: got %d, want %d", maxAttempts, defaultVerifyMaxAttempts)
	}
}

func TestStatusPageCustomDomainVerification_PollingParams_EnvOverride(t *testing.T) {
	t.Setenv("DEVHELM_TF_VERIFY_POLL_INTERVAL_MS", "250")
	t.Setenv("DEVHELM_TF_VERIFY_MAX_ATTEMPTS", "3")

	interval, maxAttempts := verifyPollingParams()
	if interval != 250*time.Millisecond {
		t.Errorf("override interval: got %s, want 250ms", interval)
	}
	if maxAttempts != 3 {
		t.Errorf("override max attempts: got %d, want 3", maxAttempts)
	}
}

func TestStatusPageCustomDomainVerification_PollingParams_BadValuesFallBackToDefaults(t *testing.T) {
	cases := []struct {
		name     string
		interval string
		attempts string
	}{
		{"non-numeric interval", "abc", "5"},
		{"zero interval", "0", "5"},
		{"negative interval", "-1", "5"},
		{"non-numeric attempts", "100", "xyz"},
		{"zero attempts", "100", "0"},
		{"negative attempts", "100", "-3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DEVHELM_TF_VERIFY_POLL_INTERVAL_MS", tc.interval)
			t.Setenv("DEVHELM_TF_VERIFY_MAX_ATTEMPTS", tc.attempts)

			interval, maxAttempts := verifyPollingParams()
			if interval <= 0 {
				t.Errorf("interval must be positive, got %s", interval)
			}
			if maxAttempts <= 0 {
				t.Errorf("max attempts must be positive, got %d", maxAttempts)
			}
		})
	}
}

// --- Test helpers ---

type extractedDnsRecord struct {
	name       string
	recordType string
	value      string
}

// dnsRecordFields pulls name/type/value out of a SingleNestedAttribute
// types.Object value. Using the framework's type-erased attribute map
// lets us assert on the Object without round-tripping through tfsdk
// schema marshalling.
func dnsRecordFields(t *testing.T, obj types.Object) extractedDnsRecord {
	t.Helper()
	if obj.IsNull() || obj.IsUnknown() {
		t.Fatalf("dns record object should not be null/unknown; got null=%v unknown=%v", obj.IsNull(), obj.IsUnknown())
	}
	attrs := obj.Attributes()
	return extractedDnsRecord{
		name:       attrStringMust(t, attrs, "name"),
		recordType: attrStringMust(t, attrs, "type"),
		value:      attrStringMust(t, attrs, "value"),
	}
}

func attrStringMust(t *testing.T, attrs map[string]attr.Value, key string) string {
	t.Helper()
	v, ok := attrs[key]
	if !ok {
		t.Fatalf("missing %q in dns record attributes", key)
	}
	s, ok := v.(types.String)
	if !ok {
		t.Fatalf("attribute %q is not a string: %T", key, v)
	}
	if s.IsNull() || s.IsUnknown() {
		t.Fatalf("attribute %q is null/unknown", key)
	}
	return s.ValueString()
}
