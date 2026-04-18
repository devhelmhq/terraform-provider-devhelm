# Membership is its own first-class resource so adding/removing a single
# monitor doesn't recreate the group, and so memberships can be managed in
# isolation (different teams, different repos) from the group definition.
resource "devhelm_resource_group_membership" "checkout_api" {
  group_id    = devhelm_resource_group.checkout.id
  member_type = "monitor"
  member_id   = devhelm_monitor.checkout_api.id
}

# Bulk membership via for_each — this is the most common idiom for fleets.
locals {
  checkout_monitor_ids = {
    api     = devhelm_monitor.checkout_api.id
    cart    = devhelm_monitor.checkout_cart.id
    payment = devhelm_monitor.checkout_payment.id
  }
}

resource "devhelm_resource_group_membership" "checkout" {
  for_each    = local.checkout_monitor_ids
  group_id    = devhelm_resource_group.checkout.id
  member_type = "monitor"
  member_id   = each.value
}

# Subscribing a tracked third-party service (devhelm_dependency) to a group.
resource "devhelm_resource_group_membership" "github_dep" {
  group_id    = devhelm_resource_group.checkout.id
  member_type = "service"
  member_id   = devhelm_dependency.github.id
}
