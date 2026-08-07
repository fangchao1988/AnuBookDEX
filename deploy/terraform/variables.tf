# AnuBookDEX — Terraform 变量定义

variable "aws_region" {
  description = "AWS 区域"
  type        = string
  default     = "ap-northeast-1"
}

variable "app_name" {
  description = "应用名称，用于资源命名"
  type        = string
  default     = "anubookdex"
}

variable "env" {
  description = "部署环境"
  type        = string
  default     = "production"
}

variable "instance_type" {
  description = "EC2 实例规格"
  type        = string
  default     = "t3.small"
}

variable "instance_count" {
  description = "EC2 实例数量（每个实例负责一组交易对）"
  type        = number
  default     = 2
}

variable "pair_shards" {
  description = "交易对分片：index → symbol 列表"
  type        = map(list(string))
  default = {
    "0" = ["ETH_USDT"]
    "1" = ["BTC_USDT"]
  }
}

# ─── Anubis Chain ────────────────────────────────────────

variable "anubis_chain_rpc_ws" {
  description = "Anubis Chain WebSocket RPC 端点（事件订阅）"
  type        = string
  sensitive   = true
}

variable "anubis_chain_rpc_http" {
  description = "Anubis Chain HTTP RPC 端点（交易提交）"
  type        = string
  sensitive   = true
}

variable "chain_private_key" {
  description = "Engine 结算私钥（通过 Secrets Manager 注入）"
  type        = string
  sensitive   = true
}

variable "settlement_contract" {
  description = "Settlement 合约地址"
  type        = string
}

variable "registry_contract" {
  description = "OrderBookRegistry 合约地址"
  type        = string
  default     = ""
}

# ─── 网络 ────────────────────────────────────────────────

variable "vpc_cidr" {
  default = "10.0.0.0/16"
}

variable "public_subnet_cidrs" {
  type    = list(string)
  default = ["10.0.1.0/24", "10.0.2.0/24"]
}

# ─── SSH ─────────────────────────────────────────────────

variable "ssh_key_name" {
  description = "EC2 Key Pair 名称"
  type        = string
}

variable "ssh_allowed_cidr" {
  description = "允许 SSH 的来源 CIDR"
  type        = string
  default     = "0.0.0.0/0"
}

# ─── Domain ───────────────────────────────────────────────

variable "domain_name" {
  description = "域名（可选，留空则使用 ALB DNS）"
  type        = string
  default     = ""
}

variable "route53_zone_id" {
  description = "Route53 Hosted Zone ID（域名必填）"
  type        = string
  default     = ""
}
