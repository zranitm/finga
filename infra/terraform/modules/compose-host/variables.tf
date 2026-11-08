variable "name" {
  description = "Prefix for every resource, e.g. myapp-prod; letters, digits and hyphens."
  type        = string

  validation {
    condition     = can(regex("^[A-Za-z0-9-]{1,40}$", var.name))
    error_message = "name: letters, digits and hyphens, at most 40 characters."
  }
}

variable "vpc_id" {
  description = "VPC of the instance."
  type        = string
}

variable "subnet_id" {
  description = "A public subnet: the instance needs a public address to reach ECR, S3 and SSM without a NAT gateway."
  type        = string
}

variable "instance_type" {
  description = "EC2 instance type; architecture must match it (t4g, m7g: arm64)."
  type        = string
  default     = "t4g.medium"
}

variable "architecture" {
  description = "arm64 or x86_64: picks the Amazon Linux 2023 image, and must match the images you deploy."
  type        = string
  default     = "arm64"

  validation {
    condition     = contains(["arm64", "x86_64"], var.architecture)
    error_message = "architecture: arm64 or x86_64."
  }
}

variable "cpu_credits" {
  description = "For burstable (t*) types: unlimited keeps full speed past the credit balance at a small surcharge; standard throttles."
  type        = string
  default     = "unlimited"
}

variable "root_volume_size_gb" {
  description = "Root volume size; Docker's images and volumes live on it."
  type        = number
  default     = 40
}

variable "swap_mb" {
  description = "Swap file size, a cushion against memory spikes; 0 for none."
  type        = number
  default     = 2048
}

variable "compose_version" {
  description = "Docker Compose plugin release installed at first boot."
  type        = string
  default     = "v5.7.0"
}

variable "log_max_size" {
  description = "Docker json-file log rotation: size of one file."
  type        = string
  default     = "10m"
}

variable "log_max_files" {
  description = "Docker json-file log rotation: files kept per container."
  type        = number
  default     = 5
}

variable "ingress" {
  description = "Inbound TCP rules: a port and the CIDR blocks (IPv4 or IPv6) allowed to reach it. Everything else is closed; operators use SSM."
  type = list(object({
    description = string
    port        = number
    cidrs       = list(string)
  }))
  default = []
}

variable "deploy_root" {
  description = "Where releases, shared secrets and logs live on the instance."
  type        = string
}

variable "host_env" {
  description = "Settings written to /etc/compose-host/host.env for the deploy scripts; AWS_REGION and DEPLOY_ROOT are added."
  type        = map(string)

  validation {
    condition     = alltrue([for k, v in var.host_env : can(regex("^[A-Z_][A-Z0-9_]*$", k)) && !strcontains(v, "'") && !strcontains(v, "\n")])
    error_message = "host_env: keys are upper-case variable names, values hold no single quote or newline."
  }
}

variable "inline_policies" {
  description = "IAM policy documents (JSON) for the instance role, by name; AmazonSSMManagedInstanceCore is always attached."
  type        = map(string)
  default     = {}
}

variable "managed_policy_arns" {
  description = "More managed policies for the instance role."
  type        = list(string)
  default     = []
}

variable "termination_protection" {
  description = "Refuse API termination of the instance, which holds the database."
  type        = bool
  default     = true
}

variable "snapshot_retain_count" {
  description = "Daily EBS snapshots kept by Data Lifecycle Manager; 0 turns them off."
  type        = number
  default     = 7
}

variable "snapshot_time" {
  description = "UTC time of the daily snapshot, HH:MM."
  type        = string
  default     = "18:30"
}

variable "alarm_email" {
  description = "Address that hears about failed status checks (confirm the SNS subscription mail); null for none."
  type        = string
  default     = null
}

variable "tags" {
  description = "Tags for every resource, on top of the provider's default tags."
  type        = map(string)
  default     = {}
}
