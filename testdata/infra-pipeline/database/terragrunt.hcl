include "root" {
  path = find_in_parent_folders("root.hcl")
}

dependency "vpc" {
  config_path = "../vpc"
  mock_outputs = {
    vpc_id    = "sim-00000000-0000-0000-0000-000000000001"
    cidr_block = "10.0.0.0/16"
  }
  mock_outputs_allowed_terraform_commands = ["plan", "validate", "apply"]
}

inputs = {
  vpc_id   = dependency.vpc.outputs.vpc_id
  vpc_cidr = dependency.vpc.outputs.cidr_block
}
