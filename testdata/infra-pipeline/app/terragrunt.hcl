include "root" {
  path = find_in_parent_folders("root.hcl")
}

dependency "database" {
  config_path = "../database"
  mock_outputs = {
    db_endpoint = "db-sim-00000000-0000-0000-0000-000000000002.10.0.0.0/16.internal"
    db_port     = "5432"
  }
  mock_outputs_allowed_terraform_commands = ["plan", "validate", "apply"]
}

inputs = {
  db_endpoint = dependency.database.outputs.db_endpoint
  db_port     = dependency.database.outputs.db_port
}
