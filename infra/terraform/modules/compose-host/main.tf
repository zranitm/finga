# One EC2 instance that runs a Compose application: Amazon Linux 2023 with Docker, an Elastic IP, a security group
# that opens only the given ports, an instance role (SSM access always, the rest from the caller), daily EBS
# snapshots, and status-check alarms. No SSH: operators and CI reach the instance through SSM.

data "aws_partition" "current" {}
data "aws_region" "current" {}

data "aws_ssm_parameter" "ami" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-${var.architecture}"
}

locals {
  partition      = data.aws_partition.current.partition
  region         = data.aws_region.current.region
  snapshot_tag   = { SnapshotPolicy = var.name }
  is_burstable   = startswith(var.instance_type, "t")
  alarm_topics   = aws_sns_topic.alarms[*].arn
  ingress_by_key = { for r in flatten([for rule in var.ingress : [for cidr in rule.cidrs : merge(rule, { cidr = cidr })]]) : "${r.port}/${r.cidr}" => r }

  host_env = merge(var.host_env, {
    AWS_REGION         = local.region
    AWS_DEFAULT_REGION = local.region
    DEPLOY_ROOT        = var.deploy_root
  })
}

# Network

resource "aws_security_group" "this" {
  name_prefix = "${var.name}-"
  description = "${var.name} compose host"
  vpc_id      = var.vpc_id
  tags        = merge(var.tags, { Name = var.name })

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_vpc_security_group_ingress_rule" "this" {
  for_each = local.ingress_by_key

  security_group_id = aws_security_group.this.id
  description       = each.value.description
  ip_protocol       = "tcp"
  from_port         = each.value.port
  to_port           = each.value.port
  cidr_ipv4         = strcontains(each.value.cidr, ":") ? null : each.value.cidr
  cidr_ipv6         = strcontains(each.value.cidr, ":") ? each.value.cidr : null
}

resource "aws_vpc_security_group_egress_rule" "all" {
  security_group_id = aws_security_group.this.id
  description       = "Everything out: ECR, S3, SSM, package and image registries"
  ip_protocol       = "-1"
  cidr_ipv4         = "0.0.0.0/0"
}

# Instance role

data "aws_iam_policy_document" "assume_ec2" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "this" {
  name               = var.name
  assume_role_policy = data.aws_iam_policy_document.assume_ec2.json
  tags               = var.tags
}

resource "aws_iam_role_policy_attachment" "ssm" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:${local.partition}:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_role_policy_attachment" "extra" {
  for_each = toset(var.managed_policy_arns)

  role       = aws_iam_role.this.name
  policy_arn = each.value
}

resource "aws_iam_role_policy" "inline" {
  for_each = var.inline_policies

  name   = each.key
  role   = aws_iam_role.this.id
  policy = each.value
}

resource "aws_iam_instance_profile" "this" {
  name = var.name
  role = aws_iam_role.this.name
  tags = var.tags
}

# Instance

resource "aws_instance" "this" {
  ami                     = data.aws_ssm_parameter.ami.insecure_value
  instance_type           = var.instance_type
  subnet_id               = var.subnet_id
  vpc_security_group_ids  = [aws_security_group.this.id]
  iam_instance_profile    = aws_iam_instance_profile.this.name
  disable_api_termination = var.termination_protection

  user_data = templatefile("${path.module}/templates/user-data.sh.tftpl", {
    compose_version = var.compose_version
    log_max_size    = var.log_max_size
    log_max_files   = var.log_max_files
    swap_mb         = var.swap_mb
    host_env        = local.host_env
    deploy_root     = var.deploy_root
    compose_release = file("${path.module}/files/compose-release.sh")
  })

  # IMDSv2 only, one hop: the host's AWS CLI reaches the role's credentials, containers behind Docker's bridge can't.
  metadata_options {
    http_endpoint               = "enabled"
    http_tokens                 = "required"
    http_put_response_hop_limit = 1
  }

  root_block_device {
    volume_type           = "gp3"
    volume_size           = var.root_volume_size_gb
    encrypted             = true
    delete_on_termination = true
  }

  dynamic "credit_specification" {
    for_each = local.is_burstable ? [1] : []
    content {
      cpu_credits = var.cpu_credits
    }
  }

  volume_tags = merge(var.tags, local.snapshot_tag, { Name = var.name })
  tags        = merge(var.tags, { Name = var.name })

  # A new AMI or user data would replace the instance and its data; take those on purpose (terraform apply -replace).
  lifecycle {
    ignore_changes = [ami, user_data]
  }
}

resource "aws_eip" "this" {
  domain   = "vpc"
  instance = aws_instance.this.id
  tags     = merge(var.tags, { Name = var.name })
}

# Daily snapshots of the root volume, which holds Docker's volumes

data "aws_iam_policy_document" "assume_dlm" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["dlm.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "dlm" {
  count = var.snapshot_retain_count > 0 ? 1 : 0

  name               = "${var.name}-dlm"
  assume_role_policy = data.aws_iam_policy_document.assume_dlm.json
  tags               = var.tags
}

resource "aws_iam_role_policy_attachment" "dlm" {
  count = var.snapshot_retain_count > 0 ? 1 : 0

  role       = aws_iam_role.dlm[0].name
  policy_arn = "arn:${local.partition}:iam::aws:policy/service-role/AWSDataLifecycleManagerServiceRole"
}

resource "aws_dlm_lifecycle_policy" "this" {
  count = var.snapshot_retain_count > 0 ? 1 : 0

  description        = "${var.name} daily snapshots"
  execution_role_arn = aws_iam_role.dlm[0].arn
  state              = "ENABLED"
  tags               = var.tags

  policy_details {
    resource_types = ["VOLUME"]
    target_tags    = local.snapshot_tag

    schedule {
      name      = "daily"
      copy_tags = true

      create_rule {
        interval      = 24
        interval_unit = "HOURS"
        times         = [var.snapshot_time]
      }

      retain_rule {
        count = var.snapshot_retain_count
      }
    }
  }
}

# Status checks: EC2 already moves the instance off failed hardware (automatic recovery); a hung guest gets a reboot.

resource "aws_sns_topic" "alarms" {
  count = var.alarm_email == null ? 0 : 1

  name = "${var.name}-alarms"
  tags = var.tags
}

resource "aws_sns_topic_subscription" "email" {
  count = var.alarm_email == null ? 0 : 1

  topic_arn = aws_sns_topic.alarms[0].arn
  protocol  = "email"
  endpoint  = var.alarm_email
}

resource "aws_cloudwatch_metric_alarm" "instance_check" {
  alarm_name          = "${var.name}-instance-status-check"
  alarm_description   = "The guest stopped answering EC2's instance status check for 3 minutes: reboot it."
  namespace           = "AWS/EC2"
  metric_name         = "StatusCheckFailed_Instance"
  statistic           = "Maximum"
  period              = 60
  evaluation_periods  = 3
  threshold           = 1
  comparison_operator = "GreaterThanOrEqualToThreshold"
  dimensions          = { InstanceId = aws_instance.this.id }
  alarm_actions       = concat(["arn:${local.partition}:automate:${local.region}:ec2:reboot"], local.alarm_topics)
  ok_actions          = local.alarm_topics
  tags                = var.tags
}

resource "aws_cloudwatch_metric_alarm" "system_check" {
  count = var.alarm_email == null ? 0 : 1

  alarm_name          = "${var.name}-system-status-check"
  alarm_description   = "EC2's system status check failed: the hardware or network under the instance."
  namespace           = "AWS/EC2"
  metric_name         = "StatusCheckFailed_System"
  statistic           = "Maximum"
  period              = 60
  evaluation_periods  = 2
  threshold           = 1
  comparison_operator = "GreaterThanOrEqualToThreshold"
  dimensions          = { InstanceId = aws_instance.this.id }
  alarm_actions       = local.alarm_topics
  ok_actions          = local.alarm_topics
  tags                = var.tags
}
