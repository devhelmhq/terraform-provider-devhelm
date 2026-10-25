# The revision a code monitor runs now.
data "devhelm_monitor_revision" "live" {
  monitor_id = devhelm_monitor.checkout.id
}

# A specific revision by number.
data "devhelm_monitor_revision" "first" {
  monitor_id = devhelm_monitor.checkout.id
  number     = 1
}
