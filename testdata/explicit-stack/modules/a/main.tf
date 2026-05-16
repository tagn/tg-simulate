resource "null_resource" "this" {
  triggers = {
    version = "v1"
  }
}

output "network_id" {
  value = null_resource.this.id
}
