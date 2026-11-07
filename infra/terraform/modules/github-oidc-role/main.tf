# An IAM role GitHub Actions assumes with its OIDC token: no long-lived keys in the repository. The trust policy
# names the exact token subjects allowed, e.g. repo:owner/repo:ref:refs/heads/main or
# repo:owner/repo:environment:production. An account has one GitHub OIDC provider: the first role creates it, the
# others pass create_oidc_provider = false and its ARN.

terraform {
  required_version = ">= 1.10"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 6.0"
    }
  }
}

variable "name" {
  type = string
}

variable "subjects" {
  description = "Token subjects (sub claims) that may assume the role; wildcards allowed, but keep them narrow."
  type        = list(string)
}

variable "policy_json" {
  description = "The role's permissions, as an IAM policy document."
  type        = string
}

variable "create_oidc_provider" {
  type    = bool
  default = true
}

variable "oidc_provider_arn" {
  description = "The account's existing GitHub OIDC provider, when create_oidc_provider is false."
  type        = string
  default     = null
}

variable "max_session_duration" {
  type    = number
  default = 3600
}

variable "tags" {
  type    = map(string)
  default = {}
}

locals {
  issuer       = "token.actions.githubusercontent.com"
  provider_arn = var.create_oidc_provider ? aws_iam_openid_connect_provider.github[0].arn : var.oidc_provider_arn
}

# AWS validates GitHub's certificate against its own trusted CAs, so no thumbprint is pinned.
resource "aws_iam_openid_connect_provider" "github" {
  count = var.create_oidc_provider ? 1 : 0

  url            = "https://${local.issuer}"
  client_id_list = ["sts.amazonaws.com"]
  tags           = var.tags
}

data "aws_iam_policy_document" "trust" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [local.provider_arn]
    }

    condition {
      test     = "StringEquals"
      variable = "${local.issuer}:aud"
      values   = ["sts.amazonaws.com"]
    }

    condition {
      test     = "StringLike"
      variable = "${local.issuer}:sub"
      values   = var.subjects
    }
  }
}

resource "aws_iam_role" "this" {
  name                 = var.name
  assume_role_policy   = data.aws_iam_policy_document.trust.json
  max_session_duration = var.max_session_duration
  tags                 = var.tags
}

resource "aws_iam_role_policy" "this" {
  name   = "permissions"
  role   = aws_iam_role.this.id
  policy = var.policy_json
}

output "role_arn" {
  value = aws_iam_role.this.arn
}

output "oidc_provider_arn" {
  value = local.provider_arn
}
