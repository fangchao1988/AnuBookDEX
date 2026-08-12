#!/usr/bin/env bash
# AnuBookDEX 服务器一键部署脚本（宝塔面板 / 通用 Linux + Docker Compose）
#
# 用法：bash deploy/compose-deploy.sh
#
# 前置：
#   1. 已上传项目到服务器（/www/wwwroot/AnuBookDEX 或任意目录）
#   2. .env.aleo 已创建（从 .env.aleo.example 复制并填入密钥）
#   3. conf/prod-aleo.yaml 已就位（生产配置模板）
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

# ─── 2. 检查测试配置 ───────────────────────────────
if [ ! -f conf/test-aleo.yaml ]; then
  echo "❌ 缺少 conf/test-aleo.yaml（测试配置模板）"
  exit 1
fi
echo "✅ conf/test-aleo.yaml 存在"

# ─── 3. 更新代码（git 仓库时） ─────────────────────
if [ -d .git ]; then
  echo "→ git pull ..."
  git pull --rebase || echo "⚠️  git pull 失败，使用当前代码继续"
fi

# ─── 4. 构建并启动 ─────────────────────────────────
echo "→ 构建镜像（首次构建需数分钟）..."
docker compose build

echo "→ 启动服务..."
docker compose up -d

# ─── 5. 状态与验证 ─────────────────────────────────
echo "→ 服务状态："
docker compose ps

echo ""
echo "=============================================="
echo " ✅ 部署完成"
echo "  前端: http://<服务器IP>:8080"
echo "  引擎: http://<服务器IP>:9000/health"
echo ""
echo "  常用命令:"
echo "    docker compose logs -f engine   # 引擎日志"
echo "    docker compose logs -f web      # 前端/代理日志"
echo "    docker compose down             # 停止"
echo "    docker compose up -d            # 启动"
echo "=============================================="
