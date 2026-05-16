# Simulates an object storage bucket scoped to a network.
# Replaced when network_id changes so downstream consumers get a fresh bucket.

resource "null_resource" "storage" {
  triggers = {
    network_id = var.network_id
  }
}

output "bucket_name" {
  value = "bucket-${null_resource.storage.id}"
}

output "storage_url" {
  value = "s3://bucket-${null_resource.storage.id}.storage.example.internal"
}
