generate "provider" {
  path      = "provider.tf"
  if_exists = "overwrite"
  contents  = <<-EOF
    terraform {
      required_providers {
        null = {
          source  = "hashicorp/null"
          version = "~> 3.0"
        }
      }
    }
  EOF
}

remote_state {
  backend = "local"
  generate = {
    path      = "backend.tf"
    if_exists = "overwrite"
  }
  config = {
    path = "${get_terragrunt_dir()}/terraform.tfstate"
  }
}

dependency "b" {
  config_path = "../b"
  mock_outputs = {
    subnet_id = "sim-00000000-0000-0000-0000-000000000002"
  }
  mock_outputs_allowed_terraform_commands = ["plan", "validate", "apply"]
}

inputs = {
  upstream_subnet_id = dependency.b.outputs.subnet_id
}
