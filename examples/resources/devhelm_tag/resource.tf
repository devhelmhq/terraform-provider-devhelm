resource "devhelm_tag" "production" {
  name  = "production"
  color = "#10B981"
}

resource "devhelm_tag" "checkout" {
  name  = "service:checkout"
  color = "#6366F1"
}
