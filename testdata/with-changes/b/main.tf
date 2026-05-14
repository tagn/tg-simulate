# Unit B uses A's resource_id as a trigger.
# When A's resource_id changes (because A was replaced), Terraform will
# also replace B — this is the force_new propagation behaviour under test.
variable "upstream_id" {
  type = string
}

resource "null_resource" "this" {
  triggers = {
    upstream_id = var.upstream_id
  }
}

output "resource_id" {
  value = null_resource.this.id
}
