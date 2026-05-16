# Simulates an application server that connects to the database.
# When the db_endpoint changes the app server is replaced and receives
# a new instance_id, changing app_url — end of the propagation chain.

resource "null_resource" "app" {
  triggers = {
    db_endpoint = var.db_endpoint
  }
}

output "instance_id" {
  value = null_resource.app.id
}

output "app_url" {
  value = "https://app-${null_resource.app.id}.example.com"
}
