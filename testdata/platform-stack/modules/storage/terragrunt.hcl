# Source template for the "storage" unit.
# `terragrunt stack generate` (driven by ../../terragrunt.stack.hcl) copies this
# into .terragrunt-stack/storage/terragrunt.hcl, injecting a
#   terraform { source = "../../modules/storage" }
# block that points back to this module directory.
#
# Dependency paths use "../network" — that resolves correctly both here
# (modules/network) and after generation (.terragrunt-stack/network), so the
# same file works as both source template and generated unit config.

include "root" {
  path = find_in_parent_folders("root.hcl")
}

dependency "network" {
  config_path = "../network"
  mock_outputs = {
    network_id      = "sim-00000000-0000-0000-0000-000000000001"
    network_version = "v1"
  }
  mock_outputs_allowed_terraform_commands = ["plan", "validate", "apply"]
}

inputs = {
  network_id = dependency.network.outputs.network_id
}
