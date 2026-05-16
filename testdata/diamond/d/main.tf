variable "upstream_db_id" {
  type = string
}

variable "upstream_cache_id" {
  type = string
}

resource "null_resource" "this" {
  triggers = {
    db_id    = var.upstream_db_id
    cache_id = var.upstream_cache_id
  }
}

output "service_id" {
  value = null_resource.this.id
}
