#!/usr/bin/env bash
# AnuBookDEX 服务器一键部署脚本（宝塔面板 / 通用 Linux + Docker Compose）
#
# 用法：bash deploy/compose-deploy.sh
#
# 前置：
#   1. 已上传项目到服务器（/www/wwwroot/AnuBookDEX 或任意目录）
#   2. .env.aleo / .env.anubis 已创建（从 *.example 复制并填入密钥）
#   3. conf/test-aleo.yaml / conf/test-anubis.yaml 已就位
#   4. Docker + Docker Compose 已安装（宝塔 Docker 管理器插件）

set -e

cd "$(dirname "$0")/.."

echo "=============================================="
echo " AnuBookDEX 部署开始"
echo "=============================================="

# ─── 1. 检查密钥文件 ───────────────────────────────
if [ ! -f .env.aleo ]; then
  echo "❌ 缺少 .env.aleo"
  echo "   请先: cp .env.aleo.example .env.aleo 并填入 ALEO_PRIVATE_KEY / ALEO_VIEW_KEY"
  exit 1
fi
echo "✅ .env.aleo 存在"

# Anubis 引擎（双链并存；缺 .env.anubis 时 compose 会因 env_file 报错）
if [ ! -f .env.anubis ]; then
  echo "❌ 缺少 .env.anubis"
  echo "   请先: cp .env.anubis.example .env.anubis 并填入 ANUBIS_PRIVATE_KEY_METAMASK / ANUBIS_VIEW_KEY"
  exit 1
fi
echo "✅ .env.anubis 存在"

# ─── 2. 检查测试配置 ───────────────────────────────
if [ ! -f conf/test-aleo.yaml ]; then
  echo "❌ 缺少 conf/test-aleo.yaml（测试配置模板）"
  exit 1
fi
echo "✅ conf/test-aleo.yaml 存在"

# ─── 3. 更新代码（git 仓库时） ─────────────────────
# 注意：这里必须失败即停。曾用 `git pull || echo 警告` 静默忽略失败，
# 导致服务器的 compose 文件停留在旧版本（缺少新服务），
# 而 CI 仍显示成功 —— 排障花很久。
if [ -d .git ]; then
  echo "→ git pull ..."
  if ! git pull --rebase; then
    echo "❌ git pull 失败。服务器可能有本地改动，请先处理："
    echo "   git status && git stash   # 或 git checkout -- <file>"
    exit 1
  fi
fi

# 校验 compose 确实包含新服务（防旧文件）
if ! grep -q "engine-anubis:" docker-compose.yml; then
  echo "❌ docker-compose.yml 缺少 engine-anubis 服务（文件是旧版本）"
  exit 1
fi

# ─── 4. 构建并启动 ─────────────────────────────────
echo "→ 构建镜像（首次构建需数分钟）..."
docker compose build

echo "→ 启动服务..."
docker compose up -d

# ─── 5. 状态与验证 ─────────────────────────────────
echo "→ 等待服务启动..."
sleep 20

echo "→ 服务状态："
docker compose ps

# 必须真正验证可用性：`docker compose up -d` 在容器崩溃时也返回 0，
# 只看 job 状态会把「起不来」误判为部署成功。
echo "→ 健康检查..."
fail=0
check() {
  local name="$1" url="$2"
  if curl -fsS --max-time 10 "$url" >/dev/null 2>&1; then
    echo "  ✅ ${name}"
  else
    echo "  ❌ ${name} 不可用（$url）"
    fail=1
  fi
}
check "Aleo 引擎   :9000/health"        "http://localhost:9000/health"
check "Anubis 引擎 :9001/health"        "http://localhost:9001/health"
check "前端代理 /anubis/api/v1/tokens"  "http://localhost:5005/anubis/api/v1/tokens"

if [ "$fail" = "1" ]; then
  echo ""
  echo "❌ 部署校验失败，最近日志："
  docker compose logs --tail=40 engine-anubis 2>/dev/null || true
  docker compose logs --tail=20 web 2>/dev/null || true
  exit 1
fi

echo ""
echo "=============================================="
echo " ✅ 部署完成"
echo "  前端: http://<服务器IP>:5005"
echo "  Aleo 引擎:   http://<服务器IP>:9000/health"
echo "  Anubis 引擎: http://<服务器IP>:9001/health"
echo "  Anubis API:  http://<服务器IP>:5005/anubis/api/v1/tokens"
echo ""
echo "  常用命令:"
echo "    docker compose logs -f engine         # Aleo 引擎日志"
echo "    docker compose logs -f engine-anubis  # Anubis 引擎日志"
echo "    docker compose logs -f web      # 前端/代理日志"
echo "    docker compose down             # 停止"
echo "    docker compose up -d            # 启动"
echo "=============================================="
