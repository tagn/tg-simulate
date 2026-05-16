include "root" {
  path = find_in_parent_folders("root.hcl")
}

inputs = {
  cidr_block = "10.0.0.0/16"
}
