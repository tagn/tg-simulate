# Simulates a VPC. The cidr_block is used as a trigger so changing it
# forces replacement — the key change in the force-new propagation scenario.
variable "cidr_block" {
  type    = string
  default = "10.0.0.0/16"
}

resource "null_resource" "vpc" {
  triggers = {
    cidr_block = var.cidr_block
  }
}

output "vpc_id" {
  value = null_resource.vpc.id
}

output "cidr_block" {
  value = var.cidr_block
}
