# AnuBookDEX Aleo 引擎镜像
#
# 构建：docker build -t anubookdex-engine-aleo:latest -f deploy/engine/Dockerfile .
#
# 注意：引擎运行时 shell out `leo execute settle/cancel_order`（链上结算/撤单），
# 基础镜像必须包含 leo CLI（从官方 GitHub release 下载 Linux x86_64 二进制）。

# ─── Stage 1: 构建引擎二进制 ─────────────────────────────
# Go 版本与 go.mod 保持一致（go 1.27.0）；GOTOOLCHAIN=local 时镜像版本必须 >= go.mod 声明
FROM golang:1.27-alpine AS builder

RUN apk add --no-cache git ca-certificates

# Go 模块代理：国内构建用 goproxy.cn（默认），海外 CI 构建
# 用 --build-arg GOPROXY=https://proxy.golang.org,direct 覆盖回官方源
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -a -installsuffix cgo \
    -ldflags="-s -w -X main.GitTag=${GIT_TAG:-unknown} -X main.BuildTime=$(date -u +%FT%T%z)" \
    -o /engine-aleo.bin \
    ./cmd/engine/aleo/

# ─── Stage 2: 运行时（debian slim + leo CLI） ─────────────
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl unzip wget \
    && rm -rf /var/lib/apt/lists/*

# leo CLI（链上结算/撤单执行依赖；与链上合约版本一致用 4.4.1 Linux release）
# 安装源优先级：deploy/leo.zip 本地离线包 → LEO_URL 前缀 → GitHub 直连 → 国内镜像。
# 国内服务器无法访问 GitHub 时，把 leo-lang-v4.4.1-x86_64-unknown-linux-gnu.zip
# 上传到项目 deploy/leo.zip（构建时自动走离线安装，跳过网络下载）。
ARG LEO_VERSION=leo-lang-v4.4.1
ARG LEO_URL=https://github.com/ProvableHQ/leo/releases/download
# 整个 deploy/ 目录进构建上下文：leo.zip 存在则离线安装，不存在则网络下载（CI 场景）
COPY deploy/ /tmp/deploy-local/
# set -ex：每条命令执行前打印 trace，构建失败时日志可直接定位失败行
# 失败退出码：10=全部下载源不可达 20=zip 解压失败 30=zip 内无 leo 二进制
#           40=非 Linux x86_64 ELF 50=leo 无法执行（glibc/架构不兼容）
RUN set -ex; \
    echo "== deploy-local 目录内容 =="; \
    ls -la /tmp/deploy-local/; \
    if [ -s /tmp/deploy-local/leo.zip ]; then \
      echo "OFFLINE: 检测到 deploy/leo.zip，使用离线包"; \
      cp /tmp/deploy-local/leo.zip /tmp/leo.zip; \
    else \
      echo "OFFLINE: 未检测到 deploy/leo.zip，尝试网络下载"; \
      for u in \
        "${LEO_URL}/${LEO_VERSION}/leo-lang-v4.4.1-x86_64-unknown-linux-gnu.zip" \
        "https://github.com/ProvableHQ/leo/releases/download/${LEO_VERSION}/leo-lang-v4.4.1-x86_64-unknown-linux-gnu.zip" \
        "https://ghfast.top/https://github.com/ProvableHQ/leo/releases/download/${LEO_VERSION}/leo-lang-v4.4.1-x86_64-unknown-linux-gnu.zip" \
        "https://ghproxy.net/https://github.com/ProvableHQ/leo/releases/download/${LEO_VERSION}/leo-lang-v4.4.1-x86_64-unknown-linux-gnu.zip" \
        "https://gh-proxy.com/https://github.com/ProvableHQ/leo/releases/download/${LEO_VERSION}/leo-lang-v4.4.1-x86_64-unknown-linux-gnu.zip"; do \
        echo "TRY: $u"; \
        if curl -fL --connect-timeout 20 --max-time 600 -o /tmp/leo.zip "$u" 2>/dev/null && [ -s /tmp/leo.zip ]; then \
          echo "DOWNLOAD OK"; break; \
        fi; \
        rm -f /tmp/leo.zip; \
      done; \
      test -s /tmp/leo.zip || { echo "DOWNLOAD FAILED: 全部源不可达，请上传 deploy/leo.zip 后重试"; exit 10; }; \
    fi; \
    echo "== zip 内容 =="; \
    unzip -l /tmp/leo.zip | head -20; \
    unzip -o /tmp/leo.zip -d /tmp/leo || { echo "UNZIP FAILED: leo.zip 不是有效 zip（可能是下载页面 HTML 或上传中断）"; exit 20; }; \
    LEO_BIN=$(find /tmp/leo -name leo -type f | head -1); \
    test -n "$LEO_BIN" || { echo "NO LEO BINARY: leo.zip 内未找到 leo 二进制（可能下成了 Source code 包）"; exit 30; }; \
    echo "LEO_BIN=$LEO_BIN"; \
    chmod +x "$LEO_BIN"; \
    echo "== 魔数（应为 7f 45 4c 46）=="; \
    head -c4 "$LEO_BIN" | od -An -tx1; \
    test "$(head -c4 "$LEO_BIN" | od -An -tx1 | tr -d ' \n')" = "7f454c46" \
      || { echo "NOT LINUX ELF: 请上传 leo-lang-v4.4.1-x86_64-unknown-linux-gnu.zip（不是 darwin/aarch64 版）"; exit 40; }; \
    mv "$LEO_BIN" /usr/local/bin/leo; \
    rm -rf /tmp/leo.zip /tmp/leo /tmp/deploy-local; \
    leo --version || { echo "LEO RUN FAILED: 二进制无法执行（glibc 不兼容或架构不匹配）"; exit 50; }

COPY --from=builder /engine-aleo.bin /app/engine-aleo.bin

WORKDIR /app
EXPOSE 9000

# 数据/日志挂载点（docker volume）
VOLUME ["/app/data", "/app/logs", "/app/sp-aleo-p3"]

ENTRYPOINT ["./app/engine-aleo.bin"]
