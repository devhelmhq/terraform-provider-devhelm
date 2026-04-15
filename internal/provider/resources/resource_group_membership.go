package resources

import (
	"context"
	"fmt"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/api"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &ResourceGroupMembershipResource{}

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
				Validators:  []validator.String{stringvalidator.OneOf("monitor", "service")},
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
	r.client = req.ProviderData.(*api.Client)
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

	found := false
	if group.Members != nil {
		for _, m := range *group.Members {
			if m.Id.String() == state.ID.ValueString() {
				found = true
				break
			}
		}
	}

	if !found {
		resp.State.RemoveResource(ctx)
	}
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

	path := fmt.Sprintf("/api/v1/resource-groups/%s/members/%s",
		state.GroupID.ValueString(), state.ID.ValueString())

	err := api.Delete(ctx, r.client, path)
	if err != nil && !api.IsNotFound(err) {
		resp.Diagnostics.AddError("Error removing resource group member", err.Error())
	}
}
