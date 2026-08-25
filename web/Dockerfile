# AnuBookDEX 前端镜像：Node 构建 -> Nginx 托管 + 反向代理
#
# 构建：docker build -t anubookdex-web:latest -f Dockerfile .

# ─── Stage 1: 构建 ───────────────────────────────────────
FROM node:22-alpine AS builder

# npm registry：国内构建用 npmmirror（默认），海外 CI 构建
# 用 --build-arg NPM_REGISTRY=https://registry.npmjs.org 覆盖回官方源
ARG NPM_REGISTRY=https://registry.npmmirror.com
RUN npm config set registry ${NPM_REGISTRY}

WORKDIR /build
COPY package.json package-lock.json* ./
RUN npm ci --no-audit --no-fund || npm install --no-audit --no-fund

COPY . .
RUN npm run build

# ─── Stage 2: Nginx 托管 ─────────────────────────────────
FROM nginx:alpine

COPY --from=builder /build/dist /usr/share/nginx/html
# 编译产物（部署合约用，随 dist 一起发布）
COPY --from=builder /build/public/programs /usr/share/nginx/html/programs
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80
