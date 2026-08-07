#!/bin/bash
# ================================================================
# AnuBookDEX — EC2 启动引导脚本 (user-data)
# ================================================================
# 功能: 安装 Docker → 拉取镜像 → 生成配置 → 启动容器
# 由 Terraform launch_template 注入，变量通过模板替换

set -euo pipefail

APP_NAME="${app_name}"
ANUBIS_RPC_WS="${anubis_rpc_ws}"
ANUBIS_RPC_HTTP="${anubis_rpc_http}"
CHAIN_PRIVATE_KEY="${chain_private_key}"
SETTLEMENT_CONTRACT="${settlement_contract}"
REGISTRY_CONTRACT="${registry_contract}"
ECR_REPO_URL="${ecr_repo_url}"

REGION=$(curl -s http://169.254.169.254/latest/meta-data/placement/region)
INSTANCE_ID=$(curl -s http://169.254.169.254/latest/meta-data/instance-id)

# ─── 安装 Docker ────────────────────────────────────────
yum update -y
yum install -y docker amazon-cloudwatch-agent jq
systemctl enable docker
systemctl start docker

# ─── ECR 登录 ────────────────────────────────────────────
aws ecr get-login-password --region "$REGION" | \
  docker login --username AWS --password-stdin "$ECR_REPO_URL"

# ─── 交易对分片: 从 ASG 实例序号派生负责的交易对 ─────────
# 通过 Terraform 将 pair_shards 编码到实例 tag
PAIR_SHARDS_JSON='${pair_shards_json}'
SHARD_INDEX=$(curl -s http://169.254.169.254/latest/meta-data/ami-launch-index)
PAIRS=$(echo "$PAIR_SHARDS_JSON" | jq -r ".[\"$SHARD_INDEX\"] | join(\",\")")

if [ -z "$PAIRS" ] || [ "$PAIRS" = "null" ]; then
  echo "WARN: no pair shard found for index $SHARD_INDEX, using default"
  PAIRS="ETH_USDT,BTC_USDT"
fi

echo "Instance $INSTANCE_ID assigned pairs: $PAIRS (shard=$SHARD_INDEX)"

# ─── 数据目录 ────────────────────────────────────────────
mkdir -p /data/engine/{rocksdb,logs,snapshots}
chown -R 1000:1000 /data/engine

# ─── 生成 config.yaml ────────────────────────────────────
cat > /data/engine/config.yaml <<CONF
app:
  profile: "dex"
  name: "${APP_NAME}"

symbols:
$(for pair in $(echo "$PAIRS" | tr ',' ' '); do echo "  - $pair"; done)

symbol-info:
  default:
    amount-scale: 18
    price-scale: 18
    l2quote-price-scale: 2
    depth-10percent-capacity: 200
    depth-steps:
      "0": [0.000000000000000001, 150]
      "1": [0.00001, 150]
      "2": [0.0001, 150]
      "3": [0.001, 150]
      "4": [0.01, 150]
      "5": [0.1, 150]
      "6": [0.000000000000000001, 20]
      "7": [0.00001, 20]
      "8": [0.0001, 20]
      "9": [0.001, 20]
      "10": [0.01, 20]
      "11": [0.1, 20]

chain:
  rpc-ws-endpoint: "${ANUBIS_RPC_WS}"
  rpc-endpoint: "${ANUBIS_RPC_HTTP}"
  settlement-contract: "${SETTLEMENT_CONTRACT}"
  private-key: "${CHAIN_PRIVATE_KEY}"
  settlement-batch-size: 100

rocksdb:
  data-dir: "/data/engine/rocksdb"

http:
  port: 9000

market:
  freq-depth-update-interval-ms: 30
  min-depth-update-interval-ms: 100
  min-stacked-depth-update-interval-ms: 1000
  default-update-interval-ms: 1000

l2quote:
  mq-send-interval-ms: 500
  kline-forward-limit: 1440
  make-new-kline-at-sec: 2
  snapshot:
    n-history: 10
    path: "/data/engine/snapshots/"

batch_result: 90

scheduler:
  snapshot: [0, 60000]
  orderbook-report: [0, 1500000]

log:
  debug: false
  json-encode: true
  info-path: "/data/engine/logs/info.log"
  error-path: "/data/engine/logs/warn.log"
  max-age: 168
  rotation: 1
  level: info

location: "Asia/Shanghai"
CONF

# ─── 启动容器 ────────────────────────────────────────────
docker run -d \
  --name anubookdex-engine \
  --restart unless-stopped \
  --network host \
  -e CONFIG_FILE=/app/conf/config.yaml \
  -v /data/engine/config.yaml:/app/conf/config.yaml:ro \
  -v /data/engine/rocksdb:/app/data \
  -v /data/engine/logs:/app/logs \
  -v /data/engine/snapshots:/app/sp \
  "${ECR_REPO_URL}:latest"

# ─── 等待健康检查 ────────────────────────────────────────
for i in $(seq 1 30); do
  if curl -sf http://localhost:9000/health > /dev/null 2>&1; then
    echo "Engine started successfully after ${i}s"
    exit 0
  fi
  sleep 1
done

echo "ERROR: Engine failed to start within 30s"
exit 1
