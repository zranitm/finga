output "instance_id" {
  description = "For SSM sessions and Run Command."
  value       = aws_instance.this.id
}

output "instance_arn" {
  description = "For IAM policies that allow ssm:SendCommand to this instance only."
  value       = aws_instance.this.arn
}

output "public_ip" {
  description = "The Elastic IP: DNS points here."
  value       = aws_eip.this.public_ip
}

output "security_group_id" {
  value = aws_security_group.this.id
}

output "role_name" {
  description = "The instance role, for attaching more policies from outside."
  value       = aws_iam_role.this.name
}

output "role_arn" {
  value = aws_iam_role.this.arn
}

output "alarm_topic_arn" {
  description = "SNS topic of the status-check alarms, or null without alarm_email."
  value       = one(aws_sns_topic.alarms[*].arn)
}
