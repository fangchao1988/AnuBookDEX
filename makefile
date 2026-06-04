# Usage
#
# make tag=release-tag          # Linux 构建（Docker 交叉编译），产出 exchange.bin
# make mac tag=release-tag      # macOS 本地构建，产出 exchange.bin
# make dex tag=release-tag      # macOS 本地构建 DEX 模式，产出 engine.bin
# make test                     # 运行全部测试
# make docker tag=release-tag   # 构建 Docker 镜像
#

# Binary output configuration
APP=exchange
OUTPUT=exchange.bin
DEX_OUTPUT=engine.bin

# LDFLAGS: inject GitTag and BuildTime at compile time
GITTAG=${tag}
BUILD_TIME=`date +%FT%T%z`
HOSTNAME=`hostname`
REPOSITORY=
LDFLAGS=-ldflags "-X main.GitTag=${GITTAG}-${HOSTNAME} -X main.BuildTime=${BUILD_TIME}"

# === Build targets ===

# Linux 构建（Docker 交叉编译，使用与 go.mod 一致的 Go 版本）
all:
	docker run --rm -v `pwd`:/go/src/match-engine -w /go/src/match-engine  golang:1.21 sh -c 'CGO_ENABLED=0 go build -a -installsuffix cgo ${LDFLAGS} -o ${OUTPUT} ./cmd/exchange/'

# macOS 本地构建（集中式模式）
mac:
	go build ${LDFLAGS} -o ${OUTPUT} ./cmd/exchange/

# macOS 本地构建（DEX 模式）
dex:
	go build ${LDFLAGS} -o ${DEX_OUTPUT} ./cmd/engine/

# === Test target ===

test:
	go test -count=1 ./internal/core/match/ ./internal/centralized/snapshotter/ ./internal/centralized/puller/ ./internal/centralized/redis/ ./internal/infra/scheduler/ ./internal/centralized/validate/ ./internal/core/market/ ./internal/centralized/rabbitmq/ ./internal/centralized/persistence/ ./internal/core/l2quote/ ./internal/infra/common/ ./internal/infra/statistics/ ./internal/dex/runner/ ./internal/dex/auth/ ./internal/dex/ws/

# === Docker targets ===

docker: all
	docker build -t exchange:${GITTAG} .

docker-release: docker
	docker image tag ${APP}:${GITTAG} ${REPOSITORY}/${APP}:${GITTAG}
	docker push ${REPOSITORY}/${APP}:${GITTAG}
