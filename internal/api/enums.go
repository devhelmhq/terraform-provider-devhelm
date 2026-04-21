// Package api — derived enum lists.
//
// The TF schema validators (stringvalidator.OneOf, …) need string slices to
// surface the legal value set in `terraform validate` output. The generated
// types declare each enum constant individually, so we re-export them as
// flat slices here. Codegen stays the source of truth — a new spec value
// will appear as a new constant in the generated package, and the test
// `TestEnumSliceCoverage` (in `enums_coverage_test.go`) verifies these
// slices stay exhaustive by reflecting over the generated package.
//
// Adding a new slice for a generated enum type:
//
//  1. Add the slice here, populated from the generated constants.
//  2. Wire it into the relevant `Schema()` via `stringvalidator.OneOf(api.<X>...)`
//     instead of literal string lists — this both eliminates DRY drift
//     and keeps `TestEnumSliceCoverage` exhaustiveness in one place.
//  3. Add the slice to the `enumSliceCoverage` table in
//     `enums_coverage_test.go` so future spec additions force-fail until
//     the slice is updated.
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

// MatchRuleTypes lists every wire-format notification-policy match-rule
// kind. Used by the notification_policy resource's
// `match_rule[*].type` validator.
var MatchRuleTypes = []string{
	string(generated.ComponentNameIn),
	string(generated.IncidentStatus),
	string(generated.MonitorIdIn),
	string(generated.MonitorTypeIn),
	string(generated.RegionIn),
	string(generated.ResourceGroupIdIn),
	string(generated.ServiceIdIn),
	string(generated.SeverityGte),
}
