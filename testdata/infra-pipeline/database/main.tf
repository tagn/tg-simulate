# Simulates a managed database instance tied to a VPC.
# When the vpc_id changes (VPC replaced), the database is also replaced
# and its endpoint changes — propagating the disruption downstream.

resource "null_resource" "database" {
  triggers = {
    vpc_id = var.vpc_id
  }
}

output "db_endpoint" {
  value = "db-${null_resource.database.id}.${var.vpc_cidr}.internal"
}

output "db_port" {
  value = "5432"
}
