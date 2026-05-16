resource "null_resource" "this" {
  triggers = {
    network_id = var.upstream_network_id
  }
}

output "service_id" {
  value = null_resource.this.id
}
