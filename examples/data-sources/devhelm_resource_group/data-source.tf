data "devhelm_resource_group" "checkout" {
  name = "Checkout"
}

resource "devhelm_resource_group_membership" "new_monitor" {
  group_id    = data.devhelm_resource_group.checkout.id
  member_type = "monitor"
  member_id   = devhelm_monitor.cart.id
}
