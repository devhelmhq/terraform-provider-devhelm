# Monitors can be imported by their UUID or by their name. Names must be
# unique within the workspace; ambiguous matches refuse to import.
terraform import devhelm_monitor.api 9b1c2d3e-4f56-7890-abcd-ef1234567890
terraform import devhelm_monitor.api "Public API"
