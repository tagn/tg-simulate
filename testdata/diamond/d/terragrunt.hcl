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
    db_id = "sim-00000000-0000-0000-0000-000000000002"
  }
  mock_outputs_allowed_terraform_commands = ["plan", "validate", "apply"]
}

dependency "c" {
  config_path = "../c"
  mock_outputs = {
    cache_id = "sim-00000000-0000-0000-0000-000000000003"
  }
  mock_outputs_allowed_terraform_commands = ["plan", "validate", "apply"]
}

inputs = {
  upstream_db_id    = dependency.b.outputs.db_id
  upstream_cache_id = dependency.c.outputs.cache_id
}
