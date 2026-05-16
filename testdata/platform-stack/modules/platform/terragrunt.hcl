# Source template for the "platform" unit.
# `terragrunt stack generate` (driven by ../../terragrunt.stack.hcl) copies this
# into .terragrunt-stack/platform/terragrunt.hcl, injecting a
#   terraform { source = "../../modules/platform" }
# block that points back to this module directory.
#
# Platform sits at the bottom of the partial-diamond: depends on both network
# (for network_id) and storage (for bucket_name). When network_version is
# bumped, network_id becomes unknown, which forces storage to replace, which
# in turn forces platform to replace — a 3-hop cascade.

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

dependency "storage" {
  config_path = "../storage"
  mock_outputs = {
    bucket_name = "bucket-sim-00000000-0000-0000-0000-000000000002"
    storage_url = "s3://bucket-sim-00000000-0000-0000-0000-000000000002.storage.example.internal"
  }
  mock_outputs_allowed_terraform_commands = ["plan", "validate", "apply"]
}

inputs = {
  network_id  = dependency.network.outputs.network_id
  bucket_name = dependency.storage.outputs.bucket_name
}
