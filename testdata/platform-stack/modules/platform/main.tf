# Simulates a platform service that consumes both the network and storage layer.
# Replaced when either input changes; outputs platform_id and platform_url.

resource "null_resource" "platform" {
  triggers = {
    network_id  = var.network_id
    bucket_name = var.bucket_name
  }
}

output "platform_id" {
  value = null_resource.platform.id
}

output "platform_url" {
  value = "https://platform-${null_resource.platform.id}.example.com"
}
