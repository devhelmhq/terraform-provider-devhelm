// Package api — derived enum lists.
//
// The TF schema validators (stringvalidator.OneOf, …) need string slices to
// surface the legal value set in `terraform validate` output. The generated
// types declare each enum constant individually, so we re-export them as
// flat slices here. Codegen stays the source of truth — a new spec value
// will appear as a new constant in the generated package, and the test
// `TestEnumSliceCoverage` verifies these slices stay exhaustive.
package api

import "github.com/devhelmhq/terraform-provider-devhelm/internal/generated"

// AssertionTypes lists every wire-format assertion type. Used by the
// monitor resource's `assertions[*].type` validator.
var AssertionTypes = []string{
	string(generated.MonitorAssertionDtoAssertionTypeBodyContains),
	string(generated.MonitorAssertionDtoAssertionTypeDnsExpectedCname),
	string(generated.MonitorAssertionDtoAssertionTypeDnsExpectedIps),
	string(generated.MonitorAssertionDtoAssertionTypeDnsMaxAnswers),
	string(generated.MonitorAssertionDtoAssertionTypeDnsMinAnswers),
	string(generated.MonitorAssertionDtoAssertionTypeDnsRecordContains),
	string(generated.MonitorAssertionDtoAssertionTypeDnsRecordEquals),
	string(generated.MonitorAssertionDtoAssertionTypeDnsResolves),
	string(generated.MonitorAssertionDtoAssertionTypeDnsResponseTime),
	string(generated.MonitorAssertionDtoAssertionTypeDnsResponseTimeWarn),
	string(generated.MonitorAssertionDtoAssertionTypeDnsTtlHigh),
	string(generated.MonitorAssertionDtoAssertionTypeDnsTtlLow),
	string(generated.MonitorAssertionDtoAssertionTypeDnsTxtContains),
	string(generated.MonitorAssertionDtoAssertionTypeHeaderValue),
	string(generated.MonitorAssertionDtoAssertionTypeHeartbeatIntervalDrift),
	string(generated.MonitorAssertionDtoAssertionTypeHeartbeatMaxInterval),
	string(generated.MonitorAssertionDtoAssertionTypeHeartbeatPayloadContains),
	string(generated.MonitorAssertionDtoAssertionTypeHeartbeatReceived),
	string(generated.MonitorAssertionDtoAssertionTypeIcmpPacketLoss),
	string(generated.MonitorAssertionDtoAssertionTypeIcmpReachable),
	string(generated.MonitorAssertionDtoAssertionTypeIcmpResponseTime),
	string(generated.MonitorAssertionDtoAssertionTypeIcmpResponseTimeWarn),
	string(generated.MonitorAssertionDtoAssertionTypeJsonPath),
	string(generated.MonitorAssertionDtoAssertionTypeMcpConnects),
	string(generated.MonitorAssertionDtoAssertionTypeMcpHasCapability),
	string(generated.MonitorAssertionDtoAssertionTypeMcpMinTools),
	string(generated.MonitorAssertionDtoAssertionTypeMcpProtocolVersion),
	string(generated.MonitorAssertionDtoAssertionTypeMcpResponseTime),
	string(generated.MonitorAssertionDtoAssertionTypeMcpResponseTimeWarn),
	string(generated.MonitorAssertionDtoAssertionTypeMcpToolAvailable),
	string(generated.MonitorAssertionDtoAssertionTypeMcpToolCountChanged),
	string(generated.MonitorAssertionDtoAssertionTypeRedirectCount),
	string(generated.MonitorAssertionDtoAssertionTypeRedirectTarget),
	string(generated.MonitorAssertionDtoAssertionTypeRegexBody),
	string(generated.MonitorAssertionDtoAssertionTypeResponseSize),
	string(generated.MonitorAssertionDtoAssertionTypeResponseTime),
	string(generated.MonitorAssertionDtoAssertionTypeResponseTimeWarn),
	string(generated.MonitorAssertionDtoAssertionTypeSslExpiry),
	string(generated.MonitorAssertionDtoAssertionTypeStatusCode),
	string(generated.MonitorAssertionDtoAssertionTypeTcpConnects),
	string(generated.MonitorAssertionDtoAssertionTypeTcpResponseTime),
	string(generated.MonitorAssertionDtoAssertionTypeTcpResponseTimeWarn),
}

// MonitorAuthTypes lists every wire-format monitor auth scheme. Used by
// the monitor resource's `auth.type` validator.
var MonitorAuthTypes = []string{
	string(generated.MonitorAuthDtoAuthTypeBearer),
	string(generated.MonitorAuthDtoAuthTypeBasic),
	string(generated.MonitorAuthDtoAuthTypeHeader),
	string(generated.MonitorAuthDtoAuthTypeApiKey),
}

// AlertChannelTypes lists every wire-format alert channel kind. Used by
// the alert_channel resource's `channel_type` validator (and by anything
// else that needs to discriminate channels by wire type).
var AlertChannelTypes = []string{
	string(generated.AlertChannelDtoChannelTypeEmail),
	string(generated.AlertChannelDtoChannelTypeWebhook),
	string(generated.AlertChannelDtoChannelTypeSlack),
	string(generated.AlertChannelDtoChannelTypePagerduty),
	string(generated.AlertChannelDtoChannelTypeOpsgenie),
	string(generated.AlertChannelDtoChannelTypeTeams),
	string(generated.AlertChannelDtoChannelTypeDiscord),
}
