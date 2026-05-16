# Simulates a network resource. The network_version trigger drives replacement
# so bumping it from "v1" to "v2" starts the force-new propagation chain.
variable "network_version" {
  type    = string
  default = "v1"
}

resource "null_resource" "network" {
  triggers = {
    version = var.network_version
  }
}

output "network_id" {
  value = null_resource.network.id
}

output "network_version" {
  value = var.network_version
}
