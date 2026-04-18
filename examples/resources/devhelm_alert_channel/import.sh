# Channels can be imported by UUID or by name. Names must be unique;
# ambiguous matches refuse to import.
terraform import devhelm_alert_channel.ops_email a1b2c3d4-5678-90ab-cdef-1234567890ab
terraform import devhelm_alert_channel.ops_email "Ops Email"
