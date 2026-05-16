variable "upstream_vpc_id" {
  type = string
}

resource "null_resource" "this" {
  triggers = {
    vpc_id = var.upstream_vpc_id
  }
}

output "cache_id" {
  value = null_resource.this.id
}
