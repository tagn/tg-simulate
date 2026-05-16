# Root config included by all units in this fixture via find_in_parent_folders.
# Lives outside .terragrunt-stack/ so the include path traversal is tested.

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
