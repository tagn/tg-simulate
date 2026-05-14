# Root config for the diamond fixture (A→B, A→C, B+C→D).

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
