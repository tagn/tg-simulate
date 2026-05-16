variable "upstream_network_id" {
  type = string
}

resource "null_resource" "this" {
  triggers = {
    network_id = var.upstream_network_id
  }
}

output "subnet_id" {
  value = null_resource.this.id
}
