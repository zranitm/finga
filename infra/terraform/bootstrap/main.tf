# Run once per account, with local state: the bucket the other roots keep their state in (versioned, so an
# overwritten state can be recovered). Its own state file is small and can be re-imported if lost.
#
#   terraform -chdir=infra/terraform/bootstrap init
#   terraform -chdir=infra/terraform/bootstrap apply
#   terraform -chdir=infra/terraform/bootstrap output -raw state_bucket

terraform {
  required_version = ">= 1.10"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
}

variable "aws_region" {
  type    = string
  default = "ap-southeast-1"
}

variable "name" {
  description = "Prefix of the bucket name; the account ID is appended, as bucket names are global."
  type        = string
  default     = "finga"
}

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = { App = var.name, ManagedBy = "terraform" }
  }
}

data "aws_caller_identity" "current" {}

module "state" {
  source = "../modules/private-bucket"

  name       = "${var.name}-tfstate-${data.aws_caller_identity.current.account_id}"
  versioning = true
  # Old state versions are cheap and are the undo button; keep them a month.
  noncurrent_days = 30
}

output "state_bucket" {
  value = module.state.name
}
