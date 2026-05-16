resource "null_resource" "this" {
  triggers = {
    version = "v1"
  }
}

output "vpc_id" {
  value = null_resource.this.id
}
