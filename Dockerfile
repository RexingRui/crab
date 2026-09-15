# 后端镜像：多阶段构建，运行阶段不含 Go 编译器。
#
# modernc.org/sqlite 是纯 Go 实现，CGO_ENABLED=0 就能静态编译，
# 不需要 gcc / libsqlite3，所以运行阶段用一个干净的 alpine 就够。

# ---------- 构建阶段 ----------
FROM golang:1.24-alpine AS builder

# 默认走国内代理（这项目的服务器基本都在国内）。海外机器可以：
#   --build-arg GOPROXY=https://proxy.golang.org,direct
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY} \
    CGO_ENABLED=0 \
    GOFLAGS=-mod=readonly

WORKDIR /src

# 先只拷依赖清单：go.mod / go.sum 没变时这一层走缓存，改代码不用重新下依赖
COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

# 不写 GOARCH，跟着构建机走，amd64 / arm64 云主机都能直接 build
RUN go build -trimpath -ldflags="-s -w" -o /out/crab-server ./cmd/server

# ---------- 运行阶段 ----------
FROM alpine:3.21

# tzdata：internal/timex 要 LoadLocation("Asia/Shanghai")，缺了只会退回固定 +08:00
# sqlite：scripts/backup.sh 的 .backup 热备份要用这个命令行工具
# bash：backup.sh 的 shebang 是 bash，alpine 默认只有 busybox ash
# ca-certificates：调微信 code2session 要校验 HTTPS 证书
RUN apk add --no-cache ca-certificates tzdata sqlite bash

# 非 root 运行。uid/gid 固定成 10001，宿主机 chown 数据目录时要用到这个数字
RUN addgroup -g 10001 crab && adduser -D -u 10001 -G crab crab

WORKDIR /app
COPY --from=builder /out/crab-server /app/crab-server
COPY scripts/backup.sh /app/scripts/backup.sh

# 数据库与备份目录，compose 里把宿主机目录挂到这两个位置
RUN mkdir -p /data /backup && chown -R crab:crab /data /backup /app

# 容器内的默认值。业务配置（AUTH_SECRET / WECHAT_* 等）由 compose 的 env_file 注入。
# 注意 HTTP_ADDR 必须监听 0.0.0.0，写成 127.0.0.1 容器外就连不上了。
ENV ENV=prod \
    HTTP_ADDR=:8080 \
    DB_PATH=/data/crab.db \
    TZ=Asia/Shanghai

USER crab
EXPOSE 8080

# 自检走 /healthz（免鉴权）。docker ps 与 compose 的 depends_on 都能看到健康状态。
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null 2>&1 || exit 1

# 直接 exec 二进制，让它当 PID 1 亲自收 SIGTERM，优雅退出才有效
ENTRYPOINT ["/app/crab-server"]
