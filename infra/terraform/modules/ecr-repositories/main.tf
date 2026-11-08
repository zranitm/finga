# ECR repositories with immutable tags, scanning on push, and a lifecycle that keeps the last keep_images images.

terraform {
  required_version = ">= 1.10"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 6.0"
    }
  }
}

variable "names" {
  description = "Repository names, e.g. [\"myapp/api\", \"myapp/web\"]."
  type        = list(string)
}

variable "keep_images" {
  description = "Tagged images kept per repository: the window you can roll back within."
  type        = number
  default     = 30
}

variable "immutable_tags" {
  description = "Refuse to overwrite a tag, so a release tag always names the same image."
  type        = bool
  default     = true
}

variable "scan_on_push" {
  type    = bool
  default = true
}

variable "force_delete" {
  description = "Let terraform destroy delete repositories that still hold images."
  type        = bool
  default     = false
}

variable "tags" {
  type    = map(string)
  default = {}
}

data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}
data "aws_region" "current" {}

resource "aws_ecr_repository" "this" {
  for_each = toset(var.names)

  name                 = each.value
  image_tag_mutability = var.immutable_tags ? "IMMUTABLE" : "MUTABLE"
  force_delete         = var.force_delete
  tags                 = var.tags

  image_scanning_configuration {
    scan_on_push = var.scan_on_push
  }

  encryption_configuration {
    encryption_type = "AES256"
  }
}

resource "aws_ecr_lifecycle_policy" "this" {
  for_each = aws_ecr_repository.this

  repository = each.value.name
  policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "Untagged images after a day"
        selection    = { tagStatus = "untagged", countType = "sinceImagePushed", countUnit = "days", countNumber = 1 }
        action       = { type = "expire" }
      },
      {
        rulePriority = 2
        description  = "All but the newest ${var.keep_images}"
        selection    = { tagStatus = "any", countType = "imageCountMoreThan", countNumber = var.keep_images }
        action       = { type = "expire" }
      },
    ]
  })
}

output "registry" {
  description = "The registry host, ACCOUNT.dkr.ecr.REGION.amazonaws.com, for docker login and image names."
  value       = "${data.aws_caller_identity.current.account_id}.dkr.ecr.${data.aws_region.current.region}.${data.aws_partition.current.dns_suffix}"
}

output "repository_urls" {
  value = { for name, repo in aws_ecr_repository.this : name => repo.repository_url }
}

output "repository_arns" {
  description = "For IAM policies that pull from or push to these repositories."
  value       = [for repo in aws_ecr_repository.this : repo.arn]
}
