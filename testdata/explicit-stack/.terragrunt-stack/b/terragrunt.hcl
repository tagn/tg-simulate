terraform {
  source = "../../modules/b"
}

include "root" {
  path = find_in_parent_folders("root.hcl")
}

dependency "a" {
  config_path = "../a"
  mock_outputs = {
    resource_id = "sim-00000000-0000-0000-0000-000000000001"
  }
  mock_outputs_allowed_terraform_commands = ["plan", "validate", "apply"]
}

inputs = {
  upstream_id = dependency.a.outputs.resource_id
}
