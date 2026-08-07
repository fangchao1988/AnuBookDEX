# AnuBookDEX — Terraform 输出

output "alb_dns_name" {
  value       = aws_lb.main.dns_name
  description = "ALB 公网 DNS，用于访问 WebSocket 行情和健康检查"
}

output "ecr_repo_url" {
  value       = aws_ecr_repository.engine.repository_url
  description = "ECR 镜像仓库地址，docker push 目标"
}

output "asg_name" {
  value       = aws_autoscaling_group.engine.name
  description = "Auto Scaling Group 名称"
}

output "engine_public_ips" {
  value       = aws_autoscaling_group.engine.instances
  description = "EC2 实例 ID 列表"
}

output "health_check_url" {
  value       = "http://${aws_lb.main.dns_name}/health"
  description = "健康检查端点"
}

output "websocket_url" {
  value       = "ws://${aws_lb.main.dns_name}/ws"
  description = "WebSocket 行情订阅端点"
}

output "ssh_command" {
  value       = formatlist("ssh ec2-user@%s", aws_autoscaling_group.engine.instances)
  description = "SSH 连接命令"
}

output "deploy_command" {
  value = <<EOT
# 构建 & 推送镜像
aws ecr get-login-password --region ${var.aws_region} | \
  docker login --username AWS --password-stdin ${aws_ecr_repository.engine.repository_url}
docker build -t ${aws_ecr_repository.engine.repository_url}:latest -f deploy/Dockerfile .
docker push ${aws_ecr_repository.engine.repository_url}:latest

# 刷新 ASG 实例
aws autoscaling start-instance-refresh \
  --auto-scaling-group-name ${aws_autoscaling_group.engine.name} \
  --preferences MinHealthyPercentage=50
EOT
  description = "镜像构建 & 推送 & 滚动更新命令"
}
