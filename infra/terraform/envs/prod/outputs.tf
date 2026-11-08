output "url" {
  value = "https://${var.hostname}"
}

output "instance_id" {
  value = module.host.instance_id
}

output "origin_ip" {
  description = "Behind Cloudflare: players never see it, and the security group answers only Cloudflare."
  value       = module.host.public_ip
}

output "shell" {
  description = "A root-capable shell on the host, through SSM (needs the Session Manager plugin)."
  value       = "aws ssm start-session --region ${var.aws_region} --target ${module.host.instance_id}"
}

output "grafana_tunnel" {
  description = "Grafana at http://localhost:3000 while this runs."
  value       = "aws ssm start-session --region ${var.aws_region} --target ${module.host.instance_id} --document-name AWS-StartPortForwardingSession --parameters portNumber=3000,localPortNumber=3000"
}

# ../../gh-variables.sh copies these into the repository's Actions variables.
output "github_variables" {
  value = {
    AWS_REGION          = var.aws_region
    AWS_BUILD_ROLE_ARN  = module.ci_build.role_arn
    AWS_DEPLOY_ROLE_ARN = module.ci_deploy.role_arn
    ARTIFACT_BUCKET     = module.artifacts.name
    DEPLOY_INSTANCE_ID  = module.host.instance_id
    DEPLOY_URL          = "https://${var.hostname}"
  }
}
