# Finga in production: one EC2 instance running the Compose stack behind Cloudflare, images in ECR, release
# bundles and database dumps in S3, secrets in SSM, and two roles GitHub Actions assumes, one to build and one to
# deploy. infra/deploy/README.md walks through it.

locals {
  app        = "finga"
  env        = "prod"
  name       = "${local.app}-${local.env}"
  ssm_prefix = "/${local.app}/${local.env}"
  # Everything the workflows build: the six services, the migrations, and the client on nginx.
  images = ["gateway", "account", "character", "item", "world", "social", "migrate", "web"]

  partition = data.aws_partition.current.partition
  region    = data.aws_region.current.region
  account   = data.aws_caller_identity.current.account_id
}

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = { App = local.app, Env = local.env, ManagedBy = "terraform" }
  }
}

# Reads CLOUDFLARE_API_TOKEN: Zone DNS Edit, Zone SSL and Certificates Edit, Zone Settings Edit on the zone.
provider "cloudflare" {}

data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}
data "aws_region" "current" {}

data "aws_vpc" "default" {
  default = true
}

data "aws_subnet" "default" {
  vpc_id            = data.aws_vpc.default.id
  availability_zone = var.availability_zone
  default_for_az    = true
}

# Images, bundles, dumps

module "registry" {
  source = "../../modules/ecr-repositories"

  names       = [for image in local.images : "${local.app}/${image}"]
  keep_images = 30
}

module "artifacts" {
  source = "../../modules/private-bucket"

  name       = "${local.name}-artifacts-${local.account}"
  versioning = true
  expire_after_days = {
    "releases/" = 90
    "backups/"  = 35
  }
}

# The host

module "host" {
  source = "../../modules/compose-host"

  name          = local.name
  vpc_id        = data.aws_vpc.default.id
  subnet_id     = data.aws_subnet.default.id
  instance_type = var.instance_type
  alarm_email   = var.alarm_email

  # HTTPS from Cloudflare only: nobody reaches the origin around it.
  ingress = [{
    description = "HTTPS from Cloudflare"
    port        = 443
    cidrs       = module.edge.cloudflare_ipv4_cidrs
  }]

  deploy_root = "/opt/${local.app}"
  host_env = {
    APP_NAME        = local.app
    DEPLOY_ENV      = local.env
    DEPLOY_SCRIPT   = "infra/deploy/deploy.sh"
    SSM_PREFIX      = local.ssm_prefix
    ARTIFACT_BUCKET = module.artifacts.name
    RELEASE_PREFIX  = "releases"
    REGISTRY        = module.registry.registry
  }

  inline_policies = { deploy = data.aws_iam_policy_document.host.json }
}

data "aws_iam_policy_document" "host" {
  statement {
    sid       = "RegistryLogin"
    actions   = ["ecr:GetAuthorizationToken"]
    resources = ["*"]
  }

  statement {
    sid       = "PullImages"
    actions   = ["ecr:BatchGetImage", "ecr:GetDownloadUrlForLayer", "ecr:BatchCheckLayerAvailability"]
    resources = module.registry.repository_arns
  }

  statement {
    sid     = "ReadParameters"
    actions = ["ssm:GetParametersByPath", "ssm:GetParameters", "ssm:GetParameter"]
    resources = [
      "arn:${local.partition}:ssm:${local.region}:${local.account}:parameter${local.ssm_prefix}",
      "arn:${local.partition}:ssm:${local.region}:${local.account}:parameter${local.ssm_prefix}/*",
    ]
  }

  statement {
    sid       = "ReadReleases"
    actions   = ["s3:GetObject"]
    resources = ["${module.artifacts.arn}/releases/*"]
  }

  statement {
    sid       = "ReadWriteBackups"
    actions   = ["s3:GetObject", "s3:PutObject"]
    resources = ["${module.artifacts.arn}/backups/*"]
  }

  # Listing lets a missing object answer 404 rather than 403, and lets pg.sh list the dumps.
  statement {
    sid       = "ListOwnPrefixes"
    actions   = ["s3:ListBucket"]
    resources = [module.artifacts.arn]

    condition {
      test     = "StringLike"
      variable = "s3:prefix"
      values   = ["releases/*", "backups/*"]
    }
  }
}

# The edge: DNS, the origin certificate, Cloudflare's ranges

module "edge" {
  source = "../../modules/cloudflare-origin"

  zone_id       = var.cloudflare_zone_id
  hostname      = var.hostname
  ipv4_address  = module.host.public_ip
  zone_settings = var.cloudflare_zone_settings
}

# What the deploy reads from SSM (see infra/deploy/deploy.sh): env/ becomes Compose variables, files/ files to mount.
# Secrets live in Terraform's state too, which is why the state bucket is private and encrypted.

resource "random_password" "postgres" {
  length  = 32
  special = false
}

locals {
  parameters = {
    "env/FINGA_POSTGRES_PASSWORD"     = random_password.postgres.result
    "files/nginx/tls/origin.pem"      = module.edge.certificate_pem
    "files/nginx/tls/origin.key"      = module.edge.private_key_pem
    "files/nginx/finga.d/realip.conf" = module.edge.nginx_realip_conf
  }
}

resource "aws_ssm_parameter" "deploy" {
  # The names alone: for_each can't take the map, whose values are sensitive.
  for_each = toset(["env/FINGA_POSTGRES_PASSWORD", "files/nginx/tls/origin.pem", "files/nginx/tls/origin.key", "files/nginx/finga.d/realip.conf"])

  name  = "${local.ssm_prefix}/${each.key}"
  type  = "SecureString"
  value = local.parameters[each.key]
}

# GitHub Actions: main builds images and uploads bundles; only jobs in the production environment deploy.

module "ci_build" {
  source = "../../modules/github-oidc-role"

  name                 = "${local.name}-ci-build"
  subjects             = ["repo:${var.github_repository}:ref:refs/heads/${var.github_branch}"]
  create_oidc_provider = var.create_github_oidc_provider
  policy_json          = data.aws_iam_policy_document.ci_build.json
}

module "ci_deploy" {
  source = "../../modules/github-oidc-role"

  name                 = "${local.name}-ci-deploy"
  subjects             = ["repo:${var.github_repository}:environment:${var.github_environment}"]
  create_oidc_provider = false
  oidc_provider_arn    = module.ci_build.oidc_provider_arn
  policy_json          = data.aws_iam_policy_document.ci_deploy.json
}

data "aws_iam_policy_document" "ci_build" {
  statement {
    sid       = "RegistryLogin"
    actions   = ["ecr:GetAuthorizationToken"]
    resources = ["*"]
  }

  statement {
    sid = "PushImages"
    actions = [
      "ecr:BatchCheckLayerAvailability", "ecr:BatchGetImage", "ecr:CompleteLayerUpload", "ecr:DescribeImages",
      "ecr:GetDownloadUrlForLayer", "ecr:InitiateLayerUpload", "ecr:PutImage", "ecr:UploadLayerPart",
    ]
    resources = module.registry.repository_arns
  }

  statement {
    sid       = "UploadReleases"
    actions   = ["s3:PutObject"]
    resources = ["${module.artifacts.arn}/releases/*"]
  }
}

data "aws_iam_policy_document" "ci_deploy" {
  statement {
    sid     = "RunDeployOnTheHost"
    actions = ["ssm:SendCommand"]
    resources = [
      module.host.instance_arn,
      "arn:${local.partition}:ssm:${local.region}::document/AWS-RunShellScript",
    ]
  }

  statement {
    sid       = "ReadTheResult"
    actions   = ["ssm:GetCommandInvocation", "ssm:ListCommandInvocations"]
    resources = ["*"]
  }
}
