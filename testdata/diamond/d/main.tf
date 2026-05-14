variable "upstream_b_id" {
  type = string
}

variable "upstream_c_id" {
  type = string
}

resource "null_resource" "this" {
  triggers = {
    upstream_b_id = var.upstream_b_id
    upstream_c_id = var.upstream_c_id
  }
}

output "resource_id" {
  value = null_resource.this.id
}
