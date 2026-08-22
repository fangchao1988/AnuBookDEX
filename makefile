# Usage
#
# make tag=release-tag          # Linux 构建（Docker 交叉编译），产出 exchange.bin
# make mac tag=release-tag      # macOS 本地构建，产出 exchange.bin
# make anubis tag=release-tag  # macOS 本地构建 DEX（Anubis 链），产出 engine-anubis.bin
# make aleo tag=release-tag    # macOS 本地构建 DEX（Aleo 链），产出 engine-aleo.bin
# make test                     # 运行全部测试
# make docker tag=release-tag   # 构建 Docker 镜像
#

# Binary output configuration
APP=exchange
OUTPUT=exchange.bin
ANUBIS_OUTPUT=engine-anubis.bin
ALEO_OUTPUT=engine-aleo.bin

# LDFLAGS: inject GitTag and BuildTime at compile time
GITTAG=${tag}
BUILD_TIME=`date +%FT%T%z`
HOSTNAME=`hostname`
REPOSITORY=
LDFLAGS=-ldflags "-X main.GitTag=${GITTAG}-${HOSTNAME} -X main.BuildTime=${BUILD_TIME}"

# === Build targets ===

# Linux 构建（Docker 交叉编译，使用与 go.mod 一致的 Go 版本）
all:
	docker run --rm -v `pwd`:/go/src/match-engine -w /go/src/match-engine  golang:1.27 sh -c 'CGO_ENABLED=0 go build -a -installsuffix cgo ${LDFLAGS} -o ${OUTPUT} ./cmd/exchange/'

# macOS 本地构建（集中式模式）
mac:
	go build ${LDFLAGS} -o ${OUTPUT} ./cmd/exchange/

# macOS 本地构建（DEX - Anubis 链模式）
anubis:
	go build ${LDFLAGS} -o ${ANUBIS_OUTPUT} ./cmd/engine/anubis/

# macOS 本地构建（DEX - Aleo 链模式，骨架）
aleo:
	go build ${LDFLAGS} -o ${ALEO_OUTPUT} ./cmd/engine/aleo/

# 向后兼容别名：make dex == make anubis
dex: anubis

# === Test target ===

test:
	go test -count=1 ./internal/core/match/ ./internal/centralized/snapshotter/ ./internal/centralized/puller/ ./internal/centralized/redis/ ./internal/infra/scheduler/ ./internal/centralized/validate/ ./internal/core/market/ ./internal/centralized/rabbitmq/ ./internal/centralized/persistence/ ./internal/core/l2quote/ ./internal/infra/common/ ./internal/infra/statistics/ ./internal/dex/runner/ ./internal/dex/auth/ ./internal/dex/ws/ ./internal/dex/chain/anubis/ ./internal/dex/chain/aleo/

# === Docker targets ===

docker: all
	docker build -t exchange:${GITTAG} .

docker-release: docker
	docker image tag ${APP}:${GITTAG} ${REPOSITORY}/${APP}:${GITTAG}
	docker push ${REPOSITORY}/${APP}:${GITTAG}
