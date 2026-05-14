resource "null_resource" "this" {
  triggers = {
    version = "v1"
  }
}

output "resource_id" {
  value = null_resource.this.id
}
