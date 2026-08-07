# AnuBookDEX 生产部署指南

## 前置条件

- AWS 账号（EC2 + ECR + ALB + CloudWatch 权限）
- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.5
- [Docker](https://docs.docker.com/get-docker/)
- [AWS CLI](https://aws.amazon.com/cli/) 已配置凭据
- Anubis Chain RPC 端点（WebSocket + HTTP）
- 已部署的 Settlement 合约地址
- Engine 私钥（用于链上结算签名）
- EC2 Key Pair（用于 SSH 调试）

## 架构

```
EC2 Auto Scaling Group (pair-sharded)
  ├── Instance 0: ETH_USDT
  ├── Instance 1: BTC_USDT
  └── Instance N: $NEW_PAIR
        │
        ├── ALB (HTTP :80 → :9000)
        │     ├── /health → 健康检查
        │     └── /ws     → WebSocket 行情（sticky session）
        │
        └── ECR (anubookdex:latest)
```

## 快速开始

### 1. 构建 & 推送镜像

```bash
# 登录 ECR（首次需先 terraform apply 获取仓库 URL）
aws ecr get-login-password --region ap-northeast-1 | \
  docker login --username AWS --password-stdin <account-id>.dkr.ecr.ap-northeast-1.amazonaws.com

# 构建
export GIT_TAG=$(git describe --tags --always)
docker build -t anubookdex-engine:latest -f deploy/Dockerfile .

# 打标签 & 推送
docker tag anubookdex-engine:latest <ecr-repo-url>:latest
docker push <ecr-repo-url>:latest
```

### 2. 部署基础设施

```bash
cd deploy/terraform

# 创建 terraform.tfvars
cat > terraform.tfvars <<EOF
aws_region            = "ap-northeast-1"
instance_type         = "t3.small"
instance_count        = 2
ssh_key_name          = "my-key-pair"
anubis_chain_rpc_ws   = "wss://rpc.anubis.network/ws"
anubis_chain_rpc_http = "https://rpc.anubis.network"
chain_private_key     = "0x..."
settlement_contract   = "0x..."
registry_contract     = "0x..."
pair_shards = {
  "0" = ["ETH_USDT"]
  "1" = ["BTC_USDT"]
}
EOF

# 部署
terraform init
terraform plan
terraform apply
```

### 3. 验证

```bash
# 健康检查
curl http://$(terraform output -raw alb_dns_name)/health
# → AnuBookDEX engine running

# WebSocket 行情
wscat -c "ws://$(terraform output -raw alb_dns_name)/ws?token=dev-token"
```

### 4. 滚动更新

```bash
# 推送新镜像
docker build -t <ecr-repo-url>:latest -f deploy/Dockerfile .
docker push <ecr-repo-url>:latest

# 触发 ASG 滚动替换（一次替换 50% 实例）
aws autoscaling start-instance-refresh \
  --auto-scaling-group-name $(terraform output -raw asg_name) \
  --preferences MinHealthyPercentage=50
```

### 5. 销毁

```bash
terraform destroy
```

## 运维操作

### 新增交易对

```bash
# 1. 更新 terraform.tfvars 中的 pair_shards
# 2. 判断是否需新增实例（instance_count + 1）
# 3. terraform apply
```

### 查看日志

```bash
# SSH 进入实例
ssh ec2-user@<instance-ip>

# 查看容器日志
docker logs -f anubookdex-engine

# CloudWatch（json-encode: true 时自动上传）
aws logs tail /aws/ec2/anubookdex-production
```

### 监控告警

| 指标 | 阈值建议 | CloudWatch 指标 |
|------|---------|----------------|
| 撮合延迟 | > 100ms | 来自 metrics log |
| 链上 RPC 断连 | > 30s | 来自 error log pattern |
| 快照写入失败 | any | CRITICAL error log |
| 内存使用 | > 80% | EC2 MemoryUtilization |
| CPU 使用 | > 80% | EC2 CPUUtilization |

## 成本明细（月/东京区域）

| 资源 | 规格 | 月费用 |
|------|------|--------|
| EC2 × 2 | t3.small | ~$24 |
| ALB | 1 个 | ~$18 |
| EBS × 2 | 20GB gp3 | ~$5 |
| ECR | 1GB 存储 | ~$1 |
| CloudWatch | 日志 5GB | ~$5 |
| 弹性 IP | 0（ALB 入口） | $0 |
| **合计** | | **~$53** |

## 安全注意事项

- `CHAIN_PRIVATE_KEY` 应通过 AWS Secrets Manager 注入，不要在 tfvars 中明文存放
- 生产环境应启用 HTTPS listener（需 ACM 证书 + Route53 域名）
- SSH 应限制为公司 IP CIDR，不要开放 0.0.0.0/0
