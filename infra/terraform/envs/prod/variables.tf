variable "aws_region" {
  description = "Singapore: the closest full-service region to players in Vietnam."
  type        = string
  default     = "ap-southeast-1"
}

variable "availability_zone" {
  type    = string
  default = "ap-southeast-1a"
}

variable "instance_type" {
  description = "4 GB holds the six services, Postgres and the observability stack with room to spare; t4g.large when Kafka arrives."
  type        = string
  default     = "t4g.medium"
}

variable "hostname" {
  description = "Where players open the game, e.g. play.example.com, in a zone on Cloudflare."
  type        = string
}

variable "cloudflare_zone_id" {
  description = "The zone's ID, from its Overview page in the Cloudflare dashboard."
  type        = string
}

variable "cloudflare_zone_settings" {
  description = "Zone-wide settings. ssl = strict makes Cloudflare verify the origin's certificate; set {} if other sites on the zone can't take that."
  type        = map(string)
  default = {
    ssl              = "strict"
    always_use_https = "on"
    min_tls_version  = "1.2"
  }
}

variable "github_repository" {
  type    = string
  default = "zranitm/finga"
}

variable "github_branch" {
  description = "The branch whose workflows may build and upload releases."
  type        = string
  default     = "main"
}

variable "github_environment" {
  description = "The GitHub environment whose jobs may deploy; add protection rules to it to gate deploys."
  type        = string
  default     = "production"
}

variable "create_github_oidc_provider" {
  description = "false when the account already has GitHub's OIDC provider (another project created it)."
  type        = bool
  default     = true
}

variable "alarm_email" {
  description = "Hears about failed status checks; null for none."
  type        = string
  default     = null
}
