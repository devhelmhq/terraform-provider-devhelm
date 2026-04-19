package resources

import (
	"context"
	"fmt"
	"strings"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const (
	memberTypeMonitor = "monitor"
	memberTypeService = "service"
)

var (
	_ resource.Resource                = &ResourceGroupMembershipResource{}
	_ resource.ResourceWithImportState = &ResourceGroupMembershipResource{}
)

type ResourceGroupMembershipResource struct {
	client *api.Client
}

type ResourceGroupMembershipModel struct {
	ID          types.String `tfsdk:"id"`
	GroupID     types.String `tfsdk:"group_id"`
	MemberType  types.String `tfsdk:"member_type"`
	MemberID    types.String `tfsdk:"member_id"`
}

func NewResourceGroupMembershipResource() resource.Resource {
	return &ResourceGroupMembershipResource{}
}

func (r *ResourceGroupMembershipResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_resource_group_membership"
}

func (r *ResourceGroupMembershipResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Version:     0,
		Description: "Manages membership of a monitor or service in a DevHelm resource group.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, Description: "Membership identifier",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"group_id": schema.StringAttribute{
				Required: true, Description: "Resource group ID to add the member to",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"member_type": schema.StringAttribute{
				Required:    true,
				Description: "Type of member: monitor or service",
				Validators:  []validator.String{stringvalidator.OneOf(memberTypeMonitor, memberTypeService)},
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"member_id": schema.StringAttribute{
				Required: true, Description: "ID of the monitor or service subscription to add",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
		},
	}
}

func (r *ResourceGroupMembershipResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*api.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type", "Expected *api.Client")
		return
	}
	r.client = client
}

func (r *ResourceGroupMembershipResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ResourceGroupMembershipModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	memberUUID, err := uuid.Parse(plan.MemberID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Invalid member ID", err.Error())
		return
	}
	body := generated.AddResourceGroupMemberRequest{
		MemberType: plan.MemberType.ValueString(),
		MemberId:   memberUUID,
	}

	member, err := api.Create[generated.ResourceGroupMemberDto](
		ctx, r.client,
		fmt.Sprintf("/api/v1/resource-groups/%s/members", plan.GroupID.ValueString()),
		body,
	)
	if err != nil {
		resp.Diagnostics.AddError("Error adding resource group member", err.Error())
		return
	}

	plan.ID = types.StringValue(member.Id.String())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ResourceGroupMembershipResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ResourceGroupMembershipModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	group, err := api.Get[generated.ResourceGroupDto](ctx, r.client, "/api/v1/resource-groups/"+state.GroupID.ValueString())
	if err != nil {
		if api.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Error reading resource group", err.Error())
		return
	}

	var matched *generated.ResourceGroupMemberDto
	if group.Members != nil {
		for i := range *group.Members {
			m := &(*group.Members)[i]
			if m.Id.String() == state.ID.ValueString() {
				matched = m
				break
			}
		}
	}

	if matched == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	// Reconcile member_type / member_id from the DTO so that drift is
	// detected if an operator manually swaps the member out underneath
	// Terraform. The chosen identifier matches what the create endpoint
	// expects: monitor_id for monitors, subscription_id for services
	// (NOT service_id — see AddResourceGroupMemberRequest in api/v1).
	state.MemberType = types.StringValue(matched.MemberType)
	if matched.MemberType == memberTypeMonitor && matched.MonitorId != nil {
		state.MemberID = types.StringValue(matched.MonitorId.String())
	} else if matched.MemberType == memberTypeService && matched.SubscriptionId != nil {
		state.MemberID = types.StringValue(matched.SubscriptionId.String())
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ResourceGroupMembershipResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Update not supported", "Resource group memberships are immutable — delete and recreate")
}

func (r *ResourceGroupMembershipResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ResourceGroupMembershipModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	p := fmt.Sprintf("/api/v1/resource-groups/%s/members/%s",
		state.GroupID.ValueString(), state.ID.ValueString())

	err := api.Delete(ctx, r.client, p)
	if err != nil && !api.IsNotFound(err) {
		resp.Diagnostics.AddError("Error removing resource group member", err.Error())
	}
}

// ImportState parses a compound `<group_id>/<key>` identifier where `key` is
// matched against the membership row UUID, the member's monitor UUID, or the
// service member's subscription UUID. Accepting all three forms means
// operators can import using whichever identifier they have on hand —
// commonly the monitor or service ID — without needing to first look up the
// synthetic membership row ID via the API.
func (r *ResourceGroupMembershipResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	groupID, key, ok := strings.Cut(req.ID, "/")
	if !ok || groupID == "" || key == "" {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected `<group_id>/<membership_id|monitor_id|subscription_id>`, got %q", req.ID),
		)
		return
	}
	if _, err := uuid.Parse(groupID); err != nil {
		resp.Diagnostics.AddError("Invalid group ID", fmt.Sprintf("group_id %q is not a UUID: %s", groupID, err))
		return
	}

	group, err := api.Get[generated.ResourceGroupDto](ctx, r.client, "/api/v1/resource-groups/"+groupID)
	if err != nil {
		resp.Diagnostics.AddError("Error fetching resource group for import", err.Error())
		return
	}
	if group.Members == nil {
		resp.Diagnostics.AddError("Membership not found", fmt.Sprintf("Resource group %s has no members", groupID))
		return
	}

	var matched *generated.ResourceGroupMemberDto
	for i := range *group.Members {
		m := &(*group.Members)[i]
		if m.Id.String() == key ||
			(m.MonitorId != nil && m.MonitorId.String() == key) ||
			(m.SubscriptionId != nil && m.SubscriptionId.String() == key) {
			matched = m
			break
		}
	}
	if matched == nil {
		resp.Diagnostics.AddError(
			"Membership not found",
			fmt.Sprintf("No member of resource group %s matches key %q (tried membership_id, monitor_id, subscription_id)", groupID, key),
		)
		return
	}

	memberID := ""
	switch matched.MemberType {
	case memberTypeMonitor:
		if matched.MonitorId != nil {
			memberID = matched.MonitorId.String()
		}
	case memberTypeService:
		if matched.SubscriptionId != nil {
			memberID = matched.SubscriptionId.String()
		}
	}
	if memberID == "" {
		resp.Diagnostics.AddError(
			"Inconsistent membership row",
			fmt.Sprintf("Membership %s has type %q but is missing the matching identifier in the API response", matched.Id, matched.MemberType),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), matched.Id.String())...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("group_id"), groupID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("member_type"), matched.MemberType)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("member_id"), memberID)...)
}
