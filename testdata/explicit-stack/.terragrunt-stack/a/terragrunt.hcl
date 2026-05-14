terraform {
  source = "../../modules/a"
}

include "root" {
  path = find_in_parent_folders("root.hcl")
}
