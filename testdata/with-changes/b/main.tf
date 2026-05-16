# Unit B uses A's vpc_id as a trigger.
# When A's vpc_id changes (because A was replaced), Terraform will
# also replace B — this is the force_new propagation behaviour under test.
variable "upstream_vpc_id" {
  type = string
}

resource "null_resource" "this" {
  triggers = {
    vpc_id = var.upstream_vpc_id
  }
}

output "subnet_id" {
  value = null_resource.this.id
}
