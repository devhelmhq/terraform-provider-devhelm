# The platform default runtime; pass `id` to read a specific one.
data "devhelm_runtime" "latest" {}

output "playwright" {
  value = data.devhelm_runtime.latest.playwright_version
}
