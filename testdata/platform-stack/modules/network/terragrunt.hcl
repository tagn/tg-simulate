# Source template for the "network" unit.
# `terragrunt stack generate` (driven by ../../terragrunt.stack.hcl) copies this
# into .terragrunt-stack/network/terragrunt.hcl, injecting a
#   terraform { source = "../../modules/network" }
# block that points back to this module directory.
#
# Topology: network is the root of the chain — no dependencies. Bumping
# network_version forces null_resource.network to replace, which cascades
# through storage and platform via their dependency on network_id.

include "root" {
  path = find_in_parent_folders("root.hcl")
}

inputs = {
  network_version = "v1"
}
