# Terragrunt explicit stack definition.
# Run `terragrunt stack generate` from this directory to regenerate .terragrunt-stack/.
# The .terragrunt-stack/ directory is checked in so tests run without generating it.
#
# Topology: network → storage, network → platform, storage → platform
#   (platform depends on both network and storage)

unit "network" {
  source = "./modules/network"
  path   = "network"
}

unit "storage" {
  source = "./modules/storage"
  path   = "storage"
}

unit "platform" {
  source = "./modules/platform"
  path   = "platform"
}
