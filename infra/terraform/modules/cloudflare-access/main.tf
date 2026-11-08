# Cloudflare Access in front of a proxied hostname: nobody reaches the origin until they sign in, and only the
# listed email addresses (or domains) can. Login is a one-time PIN mailed to the address, so testers need no
# Cloudflare or other account. Everything on the hostname is covered, API and WebSockets too: the browser carries
# the CF_Authorization cookie on same-origin requests. Clients without a browser session (scripts, bots, smoke
# tests) are turned away.
#
# Needs a Zero Trust organization on the account (dashboard onboarding: a team name and the Free plan), and an
# API token with Access: Apps and Policies Edit and Access: Organizations, Identity Providers, and Groups Edit.

terraform {
  required_version = ">= 1.10"

  required_providers {
    cloudflare = {
      source  = "cloudflare/cloudflare"
      version = ">= 5.0"
    }
  }
}

variable "account_id" {
  type = string
}

variable "name" {
  description = "The application's name in the Zero Trust dashboard."
  type        = string
}

variable "hostname" {
  description = "The hostname to protect, optionally with a path: play.example.com, or play.example.com/admin."
  type        = string
}

variable "allowed_emails" {
  description = "Addresses that may sign in."
  type        = list(string)
  default     = []
}

variable "allowed_email_domains" {
  description = "Email domains whose every address may sign in, e.g. [\"example.com\"]."
  type        = list(string)
  default     = []
}

variable "session_duration" {
  description = "How long a sign-in lasts, as a Go duration (24h, 168h). When it runs out mid-game, the page has to reload."
  type        = string
  default     = "168h"
}

variable "create_one_time_pin" {
  description = "Add the one-time PIN login method and make it the only one offered. false when the account already has it: then every login method on the account is offered."
  type        = bool
  default     = true
}

locals {
  # Each rule names both kinds, one of them null, so the two lists share one type.
  include = concat(
    [for email in var.allowed_emails : { email = { email = email }, email_domain = null }],
    [for domain in var.allowed_email_domains : { email = null, email_domain = { domain = domain } }],
  )
}

resource "cloudflare_zero_trust_access_identity_provider" "one_time_pin" {
  count = var.create_one_time_pin ? 1 : 0

  account_id = var.account_id
  name       = "One-time PIN"
  type       = "onetimepin"
  config     = {}
}

resource "cloudflare_zero_trust_access_policy" "allow" {
  account_id = var.account_id
  name       = "${var.name}: allowed people"
  decision   = "allow"
  include    = local.include

  lifecycle {
    precondition {
      condition     = length(local.include) > 0
      error_message = "Access with nobody allowed would lock everyone out: list allowed_emails or allowed_email_domains."
    }
  }
}

resource "cloudflare_zero_trust_access_application" "this" {
  account_id                = var.account_id
  name                      = var.name
  type                      = "self_hosted"
  domain                    = var.hostname
  session_duration          = var.session_duration
  app_launcher_visible      = false
  allowed_idps              = var.create_one_time_pin ? [cloudflare_zero_trust_access_identity_provider.one_time_pin[0].id] : null
  auto_redirect_to_identity = var.create_one_time_pin # one login method: straight to the email form

  policies = [{
    id         = cloudflare_zero_trust_access_policy.allow.id
    precedence = 1
  }]
}

output "application_id" {
  value = cloudflare_zero_trust_access_application.this.id
}
