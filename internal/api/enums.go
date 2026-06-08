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
//
// Sourced from each assertion SUBTYPE's discriminator-tag constant
// (e.g. `BodyContainsAssertionTypeBodyContains BodyContainsAssertionType
// = "body_contains"`) rather than from the parent
// `MonitorAssertionDto.assertionType` response enum. Under the
// spec-level Postel's-Law relaxation (`mini/runbooks/api-contract.md`
// § 3), response-DTO multi-value enums are dropped and the parent typed
// alias no longer exists. Subtype discriminator tags are single-value
// enums and survive — they're the canonical source for plan-time
// validation here. Constant names are pinned by
// `compatibility.always-prefix-enum-values: true` in
// `scripts/oapi-codegen.yaml` so unrelated enum churn cannot rename
// them.
var AssertionTypes = []string{
	string(generated.BodyContainsAssertionTypeBodyContains),
	string(generated.DnsExpectedCnameAssertionTypeDnsExpectedCname),
	string(generated.DnsExpectedIpsAssertionTypeDnsExpectedIps),
	string(generated.DnsMaxAnswersAssertionTypeDnsMaxAnswers),
	string(generated.DnsMinAnswersAssertionTypeDnsMinAnswers),
	string(generated.DnsRecordContainsAssertionTypeDnsRecordContains),
	string(generated.DnsRecordEqualsAssertionTypeDnsRecordEquals),
	string(generated.DnsResolvesAssertionTypeDnsResolves),
	string(generated.DnsResponseTimeAssertionTypeDnsResponseTime),
	string(generated.DnsResponseTimeWarnAssertionTypeDnsResponseTimeWarn),
	string(generated.DnsTtlHighAssertionTypeDnsTtlHigh),
	string(generated.DnsTtlLowAssertionTypeDnsTtlLow),
	string(generated.DnsTxtContainsAssertionTypeDnsTxtContains),
	string(generated.HeaderValueAssertionTypeHeaderValue),
	string(generated.HeartbeatIntervalDriftAssertionTypeHeartbeatIntervalDrift),
	string(generated.HeartbeatMaxIntervalAssertionTypeHeartbeatMaxInterval),
	string(generated.HeartbeatPayloadContainsAssertionTypeHeartbeatPayloadContains),
	string(generated.HeartbeatReceivedAssertionTypeHeartbeatReceived),
	string(generated.IcmpPacketLossAssertionTypeIcmpPacketLoss),
	string(generated.IcmpReachableAssertionTypeIcmpReachable),
	string(generated.IcmpResponseTimeAssertionTypeIcmpResponseTime),
	string(generated.IcmpResponseTimeWarnAssertionTypeIcmpResponseTimeWarn),
	string(generated.JsonPathAssertionTypeJsonPath),
	string(generated.McpConnectsAssertionTypeMcpConnects),
	string(generated.McpHasCapabilityAssertionTypeMcpHasCapability),
	string(generated.McpMinToolsAssertionTypeMcpMinTools),
	string(generated.McpProtocolVersionAssertionTypeMcpProtocolVersion),
	string(generated.McpResponseTimeAssertionTypeMcpResponseTime),
	string(generated.McpResponseTimeWarnAssertionTypeMcpResponseTimeWarn),
	string(generated.McpToolAvailableAssertionTypeMcpToolAvailable),
	string(generated.McpToolCountChangedAssertionTypeMcpToolCountChanged),
	string(generated.RedirectCountAssertionTypeRedirectCount),
	string(generated.RedirectTargetAssertionTypeRedirectTarget),
	string(generated.RegexBodyAssertionTypeRegexBody),
	string(generated.ResponseSizeAssertionTypeResponseSize),
	string(generated.ResponseTimeAssertionTypeResponseTime),
	string(generated.ResponseTimeWarnAssertionTypeResponseTimeWarn),
	string(generated.SslExpiryAssertionTypeSslExpiry),
	string(generated.StatusCodeAssertionTypeStatusCode),
	string(generated.TcpConnectsAssertionTypeTcpConnects),
	string(generated.TcpResponseTimeAssertionTypeTcpResponseTime),
	string(generated.TcpResponseTimeWarnAssertionTypeTcpResponseTimeWarn),
}

// AlertChannelTypes lists every wire-format alert channel kind. Used by
// the alert_channel resource's `channel_type` validator (and by anything
// else that needs to discriminate channels by wire type).
//
// Sourced from each alert-channel SUBTYPE's discriminator-tag constant
// (e.g. `EmailChannelConfigChannelTypeEmail`) rather than from the
// parent `AlertChannelDto.channelType` response enum, for the same
// reason as `AssertionTypes` above.
var AlertChannelTypes = []string{
	string(generated.EmailChannelConfigChannelTypeEmail),
	string(generated.WebhookChannelConfigChannelTypeWebhook),
	string(generated.SlackChannelConfigChannelTypeSlack),
	string(generated.PagerDutyChannelConfigChannelTypePagerduty),
	string(generated.OpsGenieChannelConfigChannelTypeOpsgenie),
	string(generated.TeamsChannelConfigChannelTypeTeams),
	string(generated.DiscordChannelConfigChannelTypeDiscord),
	string(generated.TelegramChannelConfigChannelTypeTelegram),
	string(generated.GoogleChatChannelConfigChannelTypeGoogleChat),
	string(generated.PushoverChannelConfigChannelTypePushover),
	string(generated.MattermostChannelConfigChannelTypeMattermost),
	string(generated.SplunkOnCallChannelConfigChannelTypeSplunkOncall),
	string(generated.PushbulletChannelConfigChannelTypePushbullet),
	string(generated.LinearChannelConfigChannelTypeLinear),
	string(generated.IncidentIoChannelConfigChannelTypeIncidentIo),
	string(generated.RootlyChannelConfigChannelTypeRootly),
	string(generated.ZapierChannelConfigChannelTypeZapier),
	string(generated.DatadogChannelConfigChannelTypeDatadog),
	string(generated.JiraChannelConfigChannelTypeJira),
	string(generated.GitLabChannelConfigChannelTypeGitlab),
}

// AlertSensitivities lists every wire-format alert-sensitivity value for
// the dependency / service-subscription resources.
//
// `ServiceSubscriptionDto.alertSensitivity` is response-shaped, so under
// the spec-level Postel's-Law relaxation it has no typed alias. The
// request-side schema (`UpdateAlertSensitivityRequest.alertSensitivity`)
// uses an OpenAPI `pattern` instead of `enum`, so oapi-codegen also
// emits it as `string`. We therefore enumerate the allowed wire values
// here, and `TestEnumSliceCoverage` cross-checks them against the
// authoritative spec on each typegen run.
var AlertSensitivities = []string{
	"ALL",
	"INCIDENTS_ONLY",
	"MAJOR_ONLY",
}

// MatchRuleTypes lists every wire-format notification-policy match-rule
// kind. Used by the notification_policy resource's
// `match_rule[*].type` validator.
var MatchRuleTypes = []string{
	string(generated.MatchRuleTypeComponentNameIn),
	string(generated.MatchRuleTypeIncidentStatus),
	string(generated.MatchRuleTypeMonitorIdIn),
	string(generated.MatchRuleTypeMonitorTagIn),
	string(generated.MatchRuleTypeMonitorTypeIn),
	string(generated.MatchRuleTypeRegionIn),
	string(generated.MatchRuleTypeResourceGroupIdIn),
	string(generated.MatchRuleTypeServiceIdIn),
	string(generated.MatchRuleTypeSeverityGte),
}
