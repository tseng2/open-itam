# syntax=docker/dockerfile:1
# itagent 服务端一体化镜像：web 前端 + Go 服务端 + SQLite 存储
# 构建全程网络友好：默认走 npmmirror / goproxy.cn，可用 build-arg 改回官方源

# 所有 FROM 引用的 ARG 必须集中在第一个 FROM 之前声明（FROM 只能看到全局 ARG）
ARG NODE_IMAGE=node:20-bookworm-slim
ARG GO_IMAGE=golang:bookworm

# ---------- 阶段 1: Web 前端构建 ----------
FROM ${NODE_IMAGE} AS web
ARG NPM_REGISTRY=https://registry.npmmirror.com
WORKDIR /build
COPY web/package.json web/package-lock.json ./
# npmmirror 会自动重写 lockfile 里 registry.npmjs.org 的下载地址 (npm replace-registry-host)
RUN npm config set registry "$NPM_REGISTRY" && npm ci
COPY web/index.html web/vite.config.js ./
COPY web/src ./src
RUN npm run build

# ---------- 阶段 2: Go 服务端构建 ----------
FROM ${GO_IMAGE} AS gobuild
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY} \
    CGO_ENABLED=0 \
    GOOS=linux
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
# 把阶段 1 的前端产物放进 embed 目录（internal/server/ui/ui.go 的 //go:embed dist）
COPY --from=web /build/dist ./internal/server/ui/dist
RUN GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/itagent-server ./cmd/server

# ---------- 阶段 3: 运行时（Debian 13 trixie，与 VM OS 同系） ----------
FROM debian:trixie-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends curl ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --create-home --shell /usr/sbin/nologin itagent

WORKDIR /app
COPY --from=gobuild /out/itagent-server /app/itagent-server
# 镜像内置一份默认配置，compose 会用 ./configs/server.json 挂载覆盖它
COPY configs/server.json /app/configs/server.json
RUN mkdir -p /app/data && chown -R itagent:itagent /app

USER itagent
EXPOSE 8443
VOLUME ["/app/data"]

ENTRYPOINT ["/app/itagent-server"]
CMD ["-config", "configs/server.json"]
