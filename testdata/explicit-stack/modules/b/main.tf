resource "null_resource" "this" {
  triggers = {
    upstream_id = var.upstream_id
  }
}

output "resource_id" {
  value = null_resource.this.id
}
