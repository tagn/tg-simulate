# Root config for the with-changes fixture.
# Tests the force_new propagation scenario: applying A+B with trigger "v1",
# then modifying A's trigger to "v2" so tg-simulate shows B as replaced.

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
