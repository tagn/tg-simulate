# Terragrunt explicit stack definition.
# Run `terragrunt stack generate` from this directory to regenerate .terragrunt-stack/.
# The .terragrunt-stack/ directory is checked in so tests run without generating it.

unit "a" {
  source = "./modules/a"
  path   = "a"
}

unit "b" {
  source = "./modules/b"
  path   = "b"
}
