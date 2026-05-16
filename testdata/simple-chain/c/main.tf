variable "upstream_subnet_id" {
  type = string
}

resource "null_resource" "this" {
  triggers = {
    subnet_id = var.upstream_subnet_id
  }
}

output "instance_id" {
  value = null_resource.this.id
}
