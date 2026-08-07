# AnuBookDEX — AWS 生产环境 Terraform 配置
# 推荐方案: EC2 Auto Scaling + 交易对分片

terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

# ═══════════════════════════════════════════════════════════
# 1. 网络
# ═══════════════════════════════════════════════════════════

resource "aws_vpc" "main" {
  cidr_block           = var.vpc_cidr
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = { Name = "${var.app_name}-${var.env}-vpc" }
}

resource "aws_subnet" "public" {
  count                   = length(var.public_subnet_cidrs)
  vpc_id                  = aws_vpc.main.id
  cidr_block              = var.public_subnet_cidrs[count.index]
  availability_zone       = data.aws_availability_zones.available.names[count.index]
  map_public_ip_on_launch = true

  tags = { Name = "${var.app_name}-${var.env}-public-${count.index}" }
}

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = "${var.app_name}-${var.env}-igw" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main.id
  }
  tags = { Name = "${var.app_name}-${var.env}-public-rt" }
}

resource "aws_route_table_association" "public" {
  count          = length(var.public_subnet_cidrs)
  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}

data "aws_availability_zones" "available" {}

# ═══════════════════════════════════════════════════════════
# 2. 安全组
# ═══════════════════════════════════════════════════════════

resource "aws_security_group" "alb" {
  name        = "${var.app_name}-${var.env}-alb-sg"
  vpc_id      = aws_vpc.main.id

  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "engine" {
  name        = "${var.app_name}-${var.env}-engine-sg"
  vpc_id      = aws_vpc.main.id

  ingress {
    from_port       = 9000
    to_port         = 9000
    protocol        = "tcp"
    security_groups = [aws_security_group.alb.id]
  }
  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = [var.ssh_allowed_cidr]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# ═══════════════════════════════════════════════════════════
# 3. Application Load Balancer
# ═══════════════════════════════════════════════════════════

resource "aws_lb" "main" {
  name               = "${var.app_name}-${var.env}-alb"
  load_balancer_type = "application"
  subnets            = aws_subnet.public[*].id
  security_groups    = [aws_security_group.alb.id]
}

resource "aws_lb_target_group" "engine" {
  name     = "${var.app_name}-${var.env}-tg"
  port     = 9000
  protocol = "HTTP"
  vpc_id   = aws_vpc.main.id

  health_check {
    path                = "/health"
    interval            = 30
    timeout             = 5
    healthy_threshold   = 2
    unhealthy_threshold = 3
  }

  stickiness {
    type            = "lb_cookie"
    cookie_duration = 86400 # WebSocket sticky session: 24h
    enabled         = true
  }
}

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.main.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type = "redirect"
    redirect {
      port        = "443"
      protocol    = "HTTPS"
      status_code = "HTTP_301"
    }
  }
}

# ═══════════════════════════════════════════════════════════
# 4. Launch Template
# ═══════════════════════════════════════════════════════════

resource "aws_launch_template" "engine" {
  name          = "${var.app_name}-${var.env}-lt"
  image_id      = "ami-0a90f5d6aec4ca2b6" # Amazon Linux 2023 (需根据区域更新)
  instance_type = var.instance_type
  key_name      = var.ssh_key_name

  vpc_security_group_ids = [aws_security_group.engine.id]

  user_data = base64encode(templatefile("${path.module}/../user-data.sh", {
    APP_NAME             = var.app_name
    ANUBIS_RPC_WS        = var.anubis_chain_rpc_ws
    ANUBIS_RPC_HTTP      = var.anubis_chain_rpc_http
    CHAIN_PRIVATE_KEY    = var.chain_private_key
    SETTLEMENT_CONTRACT  = var.settlement_contract
    REGISTRY_CONTRACT    = var.registry_contract
    ECR_REPO_URL         = "${data.aws_caller_identity.current.account_id}.dkr.ecr.${var.aws_region}.amazonaws.com/${var.app_name}"
  }))

  block_device_mappings {
    device_name = "/dev/xvda"
    ebs {
      volume_size = 20
      volume_type = "gp3"
    }
  }

  tag_specifications {
    resource_type = "instance"
    tags = {
      Name = "${var.app_name}-${var.env}-engine"
    }
  }

  depends_on = [aws_ecr_repository.engine]
}

# ═══════════════════════════════════════════════════════════
# 5. Auto Scaling Group
# ═══════════════════════════════════════════════════════════

resource "aws_autoscaling_group" "engine" {
  name                = "${var.app_name}-${var.env}-asg"
  vpc_zone_identifier = aws_subnet.public[*].id
  min_size            = var.instance_count
  max_size            = var.instance_count * 3
  desired_capacity    = var.instance_count

  launch_template {
    id      = aws_launch_template.engine.id
    version = "$Latest"
  }

  target_group_arns = [aws_lb_target_group.engine.arn]

  tag {
    key                 = "Name"
    value               = "${var.app_name}-${var.env}-engine"
    propagate_at_launch = true
  }
}

# ═══════════════════════════════════════════════════════════
# 6. ECR（镜像仓库）
# ═══════════════════════════════════════════════════════════

resource "aws_ecr_repository" "engine" {
  name = var.app_name

  image_scanning_configuration {
    scan_on_push = true
  }
}

# ═══════════════════════════════════════════════════════════
# 7. IAM Role（EC2 拉取 ECR 镜像 + 写入 CloudWatch Logs）
# ═══════════════════════════════════════════════════════════

resource "aws_iam_role" "engine" {
  name = "${var.app_name}-${var.env}-role"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "ecr_read" {
  role       = aws_iam_role.engine.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly"
}

resource "aws_iam_role_policy_attachment" "cloudwatch_logs" {
  role       = aws_iam_role.engine.name
  policy_arn = "arn:aws:iam::aws:policy/CloudWatchLogsFullAccess"
}

resource "aws_iam_instance_profile" "engine" {
  name = "${var.app_name}-${var.env}-profile"
  role = aws_iam_role.engine.name
}

data "aws_caller_identity" "current" {}

# ═══════════════════════════════════════════════════════════
# 8. CloudWatch Logs
# ═══════════════════════════════════════════════════════════

resource "aws_cloudwatch_log_group" "engine" {
  name              = "/aws/ec2/${var.app_name}-${var.env}"
  retention_in_days = 30
}
