# Greenfield unit: no prior state. Every run is a pure create.
resource "null_resource" "this" {}

output "resource_id" {
  value = null_resource.this.id
}
