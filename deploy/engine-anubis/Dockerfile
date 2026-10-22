# AnuBookDEX Anubis 引擎镜像
#
# 构建：docker build -t anubookdex-engine-anubis:latest -f deploy/engine-anubis/Dockerfile .
#
# 与 Aleo 引擎镜像的差异：
#   - 构建入口是 cmd/engine/anubis（链下撮合 + EVM 结算，不依赖 leo CLI）
#   - 运行时不装 leo，基础镜像用 alpine（体积从 ~500MB 降到 ~30MB）
#   - 链上交互走 go-ethereum（已 vendor，CGO_ENABLED=0 纯 Go）

# ─── Stage 1: 构建 ───────────────────────────────────────
FROM golang:1.27-alpine AS builder

RUN apk add --no-cache git ca-certificates

# Go 模块代理：国内构建用 goproxy.cn（默认）；海外 CI 用 build-arg 覆盖
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG GIT_TAG=unknown
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -a -installsuffix cgo \
    -ldflags="-s -w -X main.GitTag=${GIT_TAG} -X main.BuildTime=$(date -u +%FT%T%z)" \
    -o /engine-anubis.bin \
    ./cmd/engine/anubis/

# ─── Stage 2: 运行时 ─────────────────────────────────────
FROM alpine:3.20

# RPC 走 HTTPS，必须有 CA 证书；tzdata 兜底（二进制已内嵌 time/tzdata）
RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /engine-anubis.bin /app/engine-anubis.bin

WORKDIR /app
EXPOSE 9000

# 数据（订单簿快照）/ 日志挂载点
VOLUME ["/app/data", "/app/logs"]

# 绝对路径（避免 WORKDIR 相对路径二次拼接）
ENTRYPOINT ["/app/engine-anubis.bin"]
