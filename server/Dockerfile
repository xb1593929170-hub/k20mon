# 多阶段构建：产出静态二进制，运行时镜像不带 Go 工具链
FROM golang:1.24-alpine AS builder

WORKDIR /src
RUN apk add --no-cache ca-certificates git

# 国内网络可用 --build-arg GOPROXY=https://goproxy.cn,direct 覆盖
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}

# 先只拷依赖清单，利用 Docker 层缓存
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ENV CGO_ENABLED=0
RUN go build -mod=mod -trimpath -ldflags="-s -w" -o /out/k20mon-server .

# 运行时镜像可通过 --build-arg RUNTIME_IMAGE=... 指定
FROM ${RUNTIME_IMAGE:-alpine:3.20}

RUN apk add --no-cache ca-certificates tzdata wget

ENV TZ=Asia/Shanghai
WORKDIR /app
COPY --from=builder /out/k20mon-server /app/k20mon-server

# 默认以 root 运行，原因有两个，都写清楚方便你自行收紧：
#   1) config.yaml 里有 MySQL 密码，通常权限是 0600，非 root 用户读不到；
#   2) root 代理创建的 control.sock 是 root:root 0660，非 root 进程连不上。
# 想改成非 root：把 config.yaml 改成 0640 并 chgrp 到同一个 gid，代理侧把
# K20MON_CONTROL_SOCKET_MODE 设成 0660 并把 socket 目录 chgrp 给该 gid，
# 然后在 compose 里加 user: "<uid>:<gid>"。容器本身不写宿主机，风险可控。
EXPOSE 8888
ENTRYPOINT ["/app/k20mon-server"]
