# Monitors are imported by ID or exact name. The next apply transfers
# ownership to Terraform (shown in the plan as `owner` changing).
terraform import devhelm_monitor.api "Public API"
