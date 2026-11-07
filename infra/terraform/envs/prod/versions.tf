terraform {
  # 1.10 for the S3 backend's own lock file (no DynamoDB table).
  required_version = ">= 1.10"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = "~> 5.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
    tls = {
      source  = "hashicorp/tls"
      version = "~> 4.0"
    }
  }

  # The bucket comes from bootstrap: terraform init -backend-config="bucket=$(terraform -chdir=../../bootstrap output -raw state_bucket)"
  backend "s3" {
    key          = "finga/prod/terraform.tfstate"
    region       = "ap-southeast-1"
    encrypt      = true
    use_lockfile = true
  }
}
