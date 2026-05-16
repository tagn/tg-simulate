variable "vpc_id" {
  type        = string
  description = "ID of the VPC to deploy the database into"
}

variable "vpc_cidr" {
  type        = string
  description = "CIDR block of the VPC, used to derive allowed ingress rules"
}
