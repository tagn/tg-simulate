# This file starts with trigger "v1".
# The integration test copies this fixture to a temp dir, applies it,
# then modifies the trigger to "v2" to simulate an upstream change
# and verify that unit B shows a resource replacement in the plan.
resource "null_resource" "this" {
  triggers = {
    version = "v1"
  }
}

output "resource_id" {
  value = null_resource.this.id
}
